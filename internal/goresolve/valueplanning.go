package goresolve

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/compiler"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

func untyped(typ types.Type) bool {
	basic, known := typ.Underlying().(*types.Basic)
	return known && basic.Info()&types.IsUntyped != 0
}

func (e *resolver) expressionType(expression *semantic.Expr) (types.Type, error) {
	switch expression.Kind {
	case semantic.Variable:
		name, path, _ := strings.Cut(expression.Name, ".")
		value, known := e.captures[name]
		if !known {
			return nil, e.fail(expression, "unbound capture $"+expression.Name)
		}
		if path != "" {
			return e.host.Project(value.typ, strings.Split(path, "."))
		}
		return value.typ, nil
	case semantic.Symbol:
		value, err := e.constant(expression)
		if err != nil {
			return nil, err
		}
		return value.object.Type(), nil
	case semantic.Literal:
		value, err := types.Eval(token.NewFileSet(), e.host.Package, token.NoPos, expression.Name)
		if err != nil {
			return nil, e.fail(expression, "invalid local literal")
		}
		return value.Type, nil
	case semantic.Update:
		record, _, err := e.updateRecord(expression)
		if err != nil {
			return nil, err
		}
		return record.typ, nil
	case semantic.List:
		if len(expression.Args) == 0 {
			return nil, e.fail(expression, "empty local list requires a typed context")
		}
		element, err := e.expressionType(expression.Args[0])
		if err != nil {
			return nil, err
		}
		return types.NewSlice(types.Default(element)), nil
	case semantic.Call:
		if _, arithmetic := compiler.Arithmetic(expression.Name); arithmetic {
			return e.arithmeticType(expression, nil)
		}
		if record := e.recordNames[expression.Name]; record != nil {
			return record.typ, nil
		}
		if variant := e.constructors[expression.Name]; variant != nil {
			return variant.union.typ, nil
		}
		_, signature, err := e.binding(expression, "build")
		if err != nil {
			return nil, err
		}
		if signature.Results().Len() != 1 {
			return nil, e.fail(expression, "local function requires one result")
		}
		return signature.Results().At(0).Type(), nil
	default:
		return nil, e.fail(expression, "cannot infer local expression type")
	}
}

func (e *resolver) arithmeticType(expression *semantic.Expr, expected types.Type) (types.Type, error) {
	if len(expression.Args) != 2 || hasNamedArguments(expression) {
		return nil, e.fail(expression, "arithmetic requires two positional operands")
	}
	left, err := e.expressionType(expression.Args[0])
	if err != nil {
		return nil, err
	}
	right, err := e.expressionType(expression.Args[1])
	if err != nil {
		return nil, err
	}
	result := left
	if untyped(left) {
		result = right
		if untyped(right) && expected != nil {
			result = expected
		}
	}
	result = types.Default(result)
	basic, integer := result.Underlying().(*types.Basic)
	if !integer || basic.Info()&types.IsInteger == 0 || !types.AssignableTo(left, result) || !types.AssignableTo(right, result) {
		return nil, e.fail(expression, "arithmetic requires compatible integer operands")
	}
	return result, nil
}

// prepareValue binds arithmetic values before their construction context.
// Operations use ordinary Go integer semantics. Private names cannot collide
// with parsed capture identifiers.
func (e *resolver) prepareValue(expression *semantic.Expr, expected types.Type) (*semantic.Expr, error) {
	// Ordered query continuations bind their outcome captures only at the winning
	// result. Do not lift work from those arms into the matching graph.
	switch expression.Kind {
	case semantic.Query, semantic.OrderedAlternatives, semantic.Selection, semantic.Pure, semantic.Do, semantic.Decline:
		return expression, nil
	}
	if expression.Kind == semantic.Variable && strings.Contains(expression.Name, ".") {
		typ, err := e.expressionType(expression)
		if err != nil {
			return nil, err
		}
		value, err := e.resolveValue(expression, "build", typ)
		if err != nil {
			return nil, err
		}
		evaluation := &matchir.Evaluation{Kind: matchir.ForeignEvaluation, Type: e.tables.typeID(typ), Expression: e.storeValue(value)}
		output := e.cursor.Evaluate(evaluation)
		e.serial = max(e.serial, int(output))
		name := fmt.Sprintf("#%d", output)
		e.captures[name] = capture{id: output, typ: typ}
		return &semantic.Expr{Position: expression.Position, Kind: semantic.Variable, Name: name, Field: expression.Field}, nil
	}
	if expression.Kind == semantic.Call {
		if _, arithmetic := compiler.Arithmetic(expression.Name); arithmetic {
			evaluation, typ, err := e.arithmeticEvaluation(expression, expected)
			if err != nil {
				return nil, err
			}
			output := e.cursor.Evaluate(evaluation)
			e.serial = max(e.serial, int(output))
			name := fmt.Sprintf("#%d", output)
			e.captures[name] = capture{id: output, typ: typ}
			return &semantic.Expr{Position: expression.Position, Kind: semantic.Variable, Name: name, Field: expression.Field}, nil
		}
	}
	copy := *expression
	copy.Args = make([]*semantic.Expr, len(expression.Args))
	parameters, err := e.valueParameters(expression, expected)
	if err != nil {
		return nil, err
	}
	for index, argument := range expression.Args {
		prepared, err := e.prepareValue(argument, parameters[index])
		if err != nil {
			return nil, err
		}
		copy.Args[index] = prepared
	}
	return &copy, nil
}

func (e *resolver) valueParameters(expression *semantic.Expr, expected types.Type) ([]types.Type, error) {
	parameters := make([]types.Type, len(expression.Args))
	if (expression.Kind == semantic.List || expression.Kind == semantic.Effects) && expected != nil {
		if collection, known := expected.Underlying().(*types.Slice); known {
			for index := range parameters {
				parameters[index] = collection.Elem()
			}
		}
		return parameters, nil
	}
	if expression.Kind == semantic.Update {
		record, base, err := e.updateRecord(expression)
		if err != nil {
			return nil, err
		}
		indices, err := e.updateFields(expression, record)
		if err != nil {
			return nil, err
		}
		parameters[0] = base.typ
		for index, field := range indices {
			parameters[index+1] = record.fields[field].Type()
		}
		return parameters, nil
	}
	if expression.Kind != semantic.Call {
		return parameters, nil
	}
	var signature *types.Signature
	kind := "build"
	if record := e.recordNames[expression.Name]; record != nil {
		signature = e.recordSignature(record)
	} else if variant := e.constructors[expression.Name]; variant != nil {
		signature = e.constructorSignature(variant)
	} else if binding := e.bindings[expression.Name]; binding != nil && (binding.Kind == "build" || binding.Kind == "guard") {
		kind = binding.Kind
		_, candidate, err := e.host.Hook(binding.HostName)
		if err != nil {
			return nil, err
		}
		signature = candidate
	}
	if signature == nil {
		return parameters, nil
	}
	positions, err := e.builderPositions(expression, signature, kind)
	if err != nil {
		return nil, err
	}
	for index, position := range positions {
		if position < signature.Params().Len() {
			parameters[index] = signature.Params().At(position).Type()
		}
	}
	return parameters, nil
}

func (e *resolver) arithmeticEvaluation(expression *semantic.Expr, expected types.Type) (*matchir.Evaluation, types.Type, error) {
	typ, err := e.arithmeticType(expression, expected)
	if err != nil {
		return nil, nil, err
	}
	operation, _ := compiler.Arithmetic(expression.Name)
	result := &matchir.Evaluation{Kind: matchir.ArithmeticEvaluation, Type: e.tables.typeID(typ), Arithmetic: operation}
	for _, argument := range expression.Args {
		if argument.Kind == semantic.Call {
			if _, arithmetic := compiler.Arithmetic(argument.Name); arithmetic {
				nested, _, err := e.arithmeticEvaluation(argument, typ)
				if err != nil {
					return nil, nil, err
				}
				result.Arguments = append(result.Arguments, nested)
				continue
			}
		}
		if argument.Kind != semantic.Variable && argument.Kind != semantic.Symbol && argument.Kind != semantic.Literal {
			return nil, nil, e.fail(argument, "arithmetic operands require values; bind domain operations with let")
		}
		operand, _, err := e.comparisonOperand(argument)
		if err != nil {
			return nil, nil, err
		}
		value := &matchir.Evaluation{Kind: matchir.OperandEvaluation, Type: e.tables.typeID(typ), Operand: operand}
		result.Arguments = append(result.Arguments, value)
	}
	return result, typ, nil
}

func (e *resolver) local(clause semantic.Clause) error {
	if _, exists := e.captures[clause.Name]; exists {
		return e.fail(clause.Value, "local binding cannot shadow capture or local $"+clause.Name)
	}
	typ, err := e.expressionType(clause.Value)
	if err != nil {
		return err
	}
	typ = types.Default(typ)
	if typ == types.Typ[types.UntypedNil] {
		return e.fail(clause.Value, "cannot infer a local type from nil")
	}
	prepared, err := e.prepareValue(clause.Value, typ)
	if err != nil {
		return err
	}
	value, err := e.resolveValue(prepared, "build", typ)
	if err != nil {
		return err
	}
	if prepared.Kind == semantic.Variable && !strings.Contains(prepared.Name, ".") {
		e.captures[clause.Name] = e.captures[prepared.Name]
		return nil
	}
	evaluation := &matchir.Evaluation{Kind: matchir.ForeignEvaluation, Type: e.tables.typeID(typ), Expression: e.storeValue(value)}
	output := e.cursor.Evaluate(evaluation)
	e.serial = max(e.serial, int(output))
	e.captures[clause.Name] = capture{id: output, typ: typ}
	return nil
}

package goresolve

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/kojah/hyrule/internal/compiler"
	"github.com/kojah/hyrule/internal/matchir"
	"github.com/kojah/hyrule/internal/semantic"
	"github.com/kojah/hyrule/internal/valueir"
)

// Boolean operators, value comparisons, arithmetic values and conditionals are
// pure. Operands that Go evaluates only conditionally (the right side of && and
// ||, and if branches) are resolved in place rather than lifted into the
// matching graph, so a guard such as x != nil && x.F > 0 never dereferences
// before it checks.

func isBoolean(typ types.Type) bool {
	return types.Identical(typ, types.Typ[types.Bool]) || types.Identical(typ, types.Typ[types.UntypedBool])
}

// prepareLazy lifts only eagerly evaluated operands.
func (e *resolver) prepareLazy(expression *semantic.Expr) (*semantic.Expr, error) {
	copy := *expression
	copy.Args = append([]*semantic.Expr(nil), expression.Args...)
	if expression.Kind == semantic.Logic {
		prepared, err := e.prepareValue(expression.Args[0], types.Typ[types.Bool])
		if err != nil {
			return nil, err
		}
		copy.Args[0] = prepared
	}
	return &copy, nil
}

func (e *resolver) booleanOperand(x *semantic.Expr) (*valueir.Value, error) {
	if x.Kind == semantic.InlineGo {
		return e.resolveInlineGo(x, types.Typ[types.Bool])
	}
	typ, err := e.expressionType(x)
	if err != nil {
		return nil, err
	}
	if !isBoolean(typ) {
		return nil, e.fail(x, "Boolean operators and conditions require bool operands")
	}
	return e.resolveValue(x, "guard", types.Typ[types.Bool])
}

func (e *resolver) resolveLogic(x *semantic.Expr) (*valueir.Value, error) {
	operators := map[string]valueir.LogicOp{"&&": valueir.And, "||": valueir.Or, "not": valueir.Not}
	operator, known := operators[x.Name]
	if !known {
		return nil, e.fail(x, "unknown Boolean operator "+x.Name)
	}
	result := &valueir.Value{Kind: valueir.Logic, Logic: operator, Type: e.tables.typeID(types.Typ[types.Bool])}
	for _, argument := range x.Args {
		value, err := e.booleanOperand(argument)
		if err != nil {
			return nil, err
		}
		result.Arguments = append(result.Arguments, valueir.Argument{Value: value, Type: result.Type})
	}
	return result, nil
}

func (e *resolver) resolveComparisonValue(x *semantic.Expr) (*valueir.Value, error) {
	var operandTypes [2]types.Type
	result := &valueir.Value{Kind: valueir.Compare, Type: e.tables.typeID(types.Typ[types.Bool])}
	for index, argument := range x.Args {
		typ, err := e.expressionType(argument)
		if err != nil {
			return nil, err
		}
		operandTypes[index] = typ
		value, err := e.resolveValue(argument, "build", typ)
		if err != nil {
			return nil, err
		}
		result.Arguments = append(result.Arguments, valueir.Argument{Value: value, Type: e.tables.typeID(typ), Position: index})
	}
	operator, err := e.comparisonOperator(x, operandTypes[0], operandTypes[1])
	if err != nil {
		return nil, err
	}
	result.Comparison = operator
	return result, nil
}

func (e *resolver) resolveArithmeticValue(x *semantic.Expr, expected types.Type) (*valueir.Value, error) {
	typ, err := e.arithmeticType(x, expected)
	if err != nil {
		return nil, err
	}
	operation, _ := compiler.Arithmetic(x.Name)
	result := &valueir.Value{Kind: valueir.Arithmetic, Arithmetic: operation, Type: e.tables.typeID(typ)}
	for index, argument := range x.Args {
		value, err := e.resolveValue(argument, "build", typ)
		if err != nil {
			return nil, err
		}
		result.Arguments = append(result.Arguments, valueir.Argument{Value: value, Type: result.Type, Position: index})
	}
	return result, nil
}

// conditionalType is the expected type, or the then branch's default type.
func (e *resolver) conditionalType(x *semantic.Expr, expected types.Type) (types.Type, error) {
	if expected != nil {
		return expected, nil
	}
	typ, err := e.expressionType(x.Args[1])
	if err != nil {
		return nil, err
	}
	typ = types.Default(typ)
	other, err := e.expressionType(x.Args[2])
	if err != nil {
		return nil, err
	}
	if !types.AssignableTo(other, typ) {
		return nil, e.fail(x, "if branches require compatible types")
	}
	return typ, nil
}

func (e *resolver) resolveConditionalValue(x *semantic.Expr, expected types.Type) (*valueir.Value, error) {
	typ, err := e.conditionalType(x, expected)
	if err != nil {
		return nil, err
	}
	condition, err := e.booleanOperand(x.Args[0])
	if err != nil {
		return nil, err
	}
	result := &valueir.Value{Kind: valueir.Conditional, Type: e.tables.typeID(typ)}
	result.Arguments = append(result.Arguments, valueir.Argument{Value: condition, Type: e.tables.typeID(types.Typ[types.Bool])})
	for _, branch := range x.Args[1:] {
		value, err := e.resolveValue(branch, "build", typ)
		if err != nil {
			return nil, err
		}
		result.Arguments = append(result.Arguments, valueir.Argument{Value: value, Type: result.Type})
	}
	return result, nil
}

// resolveBranch commits an if whose branches may query, choose or decline.
func (e *resolver) resolveBranch(x *semantic.Expr, expected types.Type, inChoice bool) (*valueir.Result, error) {
	if e.containsHostQuery(x.Args[0]) {
		return nil, e.fail(x.Args[0], "if conditions must be pure; bind queries with do")
	}
	condition, err := e.booleanOperand(x.Args[0])
	if err != nil {
		return nil, err
	}
	result := &valueir.Result{Kind: valueir.Branch, Type: e.tables.typeID(expected), Value: condition}
	for _, branch := range x.Args[1:] {
		resolved, err := e.resolveResult(branch, expected, inChoice)
		if err != nil {
			return nil, err
		}
		result.Alternatives = append(result.Alternatives, resolved)
	}
	return result, nil
}

// patternGuard evaluates a pure expression once, then matches it like an
// equation pattern; failure falls through to the next alternative.
func (e *resolver) patternGuard(clause semantic.Clause) error {
	if e.containsHostQuery(clause.Value) {
		return e.fail(clause.Value, "pattern guard expressions must be pure; use do for queries")
	}
	typ, err := e.expressionType(clause.Value)
	if err != nil {
		return err
	}
	typ = types.Default(typ)
	if clause.Value.Kind == semantic.Variable && !strings.Contains(clause.Value.Name, ".") {
		return e.pattern(clause.Pattern, e.captures[clause.Value.Name].id, typ)
	}
	prepared, err := e.prepareValue(clause.Value, typ)
	if err != nil {
		return err
	}
	value, err := e.resolveValue(prepared, "build", typ)
	if err != nil {
		return err
	}
	output := e.cursor.Evaluate(&matchir.Evaluation{Kind: matchir.ForeignEvaluation, Type: e.tables.typeID(typ), Expression: e.storeValue(value)})
	e.serial = max(e.serial, int(output))
	e.remember(fmt.Sprintf("#%d", output), capture{id: output, typ: typ})
	return e.pattern(clause.Pattern, output, typ)
}

// containsHostQuery also finds calls to ordered (!) host queries written as
// ordinary applications, which only do and case may execute.
func (e *resolver) containsHostQuery(x *semantic.Expr) bool {
	if containsQuery(x) {
		return true
	}
	if binding := e.bindings[x.Name]; x.Kind == semantic.Call && binding != nil && binding.Kind == "query" {
		return true
	}
	for _, argument := range x.Args {
		if e.containsHostQuery(argument) {
			return true
		}
	}
	return false
}

// resolvePureDecision lets do and case branch on a pure value. It is evaluated
// once at the decision's position; ordered host queries still require !.
func (e *resolver) resolvePureDecision(value *semantic.Expr) (*valueir.QueryPlan, types.Type, error) {
	if value.Bind != "" {
		return nil, nil, e.fail(value, "query requires a query hook call")
	}
	if e.containsHostQuery(value) {
		return nil, nil, e.fail(value, "decision on a query must call it directly")
	}
	typ, err := e.expressionType(value)
	if err != nil {
		return nil, nil, err
	}
	typ = types.Default(typ)
	resolved, err := e.resolveValue(value, "build", typ)
	if err != nil {
		return nil, nil, err
	}
	e.serial++
	return &valueir.QueryPlan{Pure: resolved, Type: e.tables.typeID(typ), TupleSize: 1, Capture: matchir.ValueID(e.serial)}, typ, nil
}

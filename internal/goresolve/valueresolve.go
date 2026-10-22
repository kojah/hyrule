package goresolve

import (
	"go/token"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
	"github.com/kojah/lockdoc/tools/hyrule/internal/valueir"
)

// resolveValue performs name, schema and argument resolution before any value
// code is emitted. Its result contains no semantic expressions or capture maps.
func (e *resolver) resolveValue(x *semantic.Expr, kind string, expected types.Type) (*valueir.Value, error) {
	if x.Bind != "" {
		return nil, e.fail(x, "captures may only be bound in patterns")
	}
	switch x.Kind {
	case semantic.Variable:
		return e.resolveCapture(x)
	case semantic.Literal:
		evaluated, err := types.Eval(token.NewFileSet(), e.host.Package, token.NoPos, x.Name)
		if err != nil {
			return nil, e.fail(x, "invalid literal")
		}
		return &valueir.Value{Kind: valueir.Operand, Type: e.tables.typeID(evaluated.Type),
			Operand: matchir.Operand{Kind: matchir.LiteralOperand, Literal: e.tables.literalID(x.Name)}}, nil
	case semantic.Symbol:
		constant, err := e.constant(x)
		if err != nil {
			return nil, err
		}
		return &valueir.Value{Kind: valueir.Operand, Type: constant.schema.Type,
			Operand: matchir.Operand{Kind: matchir.ConstantOperand, Constant: constant.schema.ID}}, nil
	case semantic.Call:
		return e.resolveCall(x, kind)
	case semantic.List, semantic.Effects:
		if kind != "build" || expected == nil {
			return nil, e.fail(x, "list/effects requires a typed build context")
		}
		return e.resolveCollection(x, expected)
	case semantic.Update:
		if kind != "build" {
			return nil, e.fail(x, "updates are construction expressions")
		}
		return e.resolveUpdate(x)
	case semantic.Query, semantic.Do, semantic.Decline:
		return nil, e.fail(x, "query/do/decline is only allowed as a rule result or decision arm")
	case semantic.Alternation:
		return nil, e.fail(x, "or is only allowed in matcher patterns")
	default:
		return nil, e.fail(x, "wildcards are only allowed in patterns")
	}
}

func (e *resolver) resolveCapture(x *semantic.Expr) (*valueir.Value, error) {
	name, path, _ := strings.Cut(x.Name, ".")
	capture, known := e.captures[name]
	if !known {
		return nil, e.fail(x, "unbound capture $"+name)
	}
	value := &valueir.Value{Kind: valueir.Operand, Type: e.tables.typeID(capture.typ), Operand: matchir.Operand{Kind: matchir.ValueOperand, Value: capture.id}}
	if path == "" {
		return value, nil
	}
	typ, err := e.host.Project(capture.typ, strings.Split(path, "."))
	if err != nil {
		return nil, e.fail(x, err.Error())
	}
	id := valueir.MemberID(len(e.tables.members))
	e.tables.members = append(e.tables.members, path)
	return &valueir.Value{Kind: valueir.Project, Type: e.tables.typeID(typ), Base: value, Member: id}, nil
}

func (e *resolver) resolveCall(x *semantic.Expr, kind string) (*valueir.Value, error) {
	var result *valueir.Value
	var signature *types.Signature
	if record := e.recordNames[x.Name]; record != nil {
		if kind != "build" {
			return nil, e.fail(x, "record constructors are not guard expressions")
		}
		if record.declaration.View {
			return nil, e.fail(x, "views are read-only; use a builder for construction")
		}
		if len(x.Args) != len(record.fields) && !hasNamedArguments(x) {
			return nil, e.fail(x, "record construction requires every declared field")
		}
		signature = e.recordSignature(record)
		result = &valueir.Value{Kind: valueir.Product, Record: record.schema}
	} else if variant := e.constructors[x.Name]; variant != nil {
		if kind != "build" {
			return nil, e.fail(x, "union constructors are not guard expressions")
		}
		if len(x.Args) != len(variant.fields) && !hasNamedArguments(x) {
			return nil, e.fail(x, "union constructor requires every declared field")
		}
		signature = e.constructorSignature(variant)
		result = &valueir.Value{Kind: valueir.Tagged, Union: variant.union.schema, Variant: variant.id}
	} else {
		expanded, err := e.forwardingCall(x, kind)
		if err != nil {
			return nil, err
		}
		var name string
		if expanded != x {
			x = expanded
			name, signature, err = e.host.Hook(x.Name)
		} else {
			name, signature, err = e.binding(x, kind)
		}
		if err != nil {
			return nil, err
		}
		if signature.Results().Len() != 1 || signature.Params().Len() != len(x.Args) && !hasNamedArguments(x) {
			return nil, e.fail(x, "hook arity mismatch: require one result and one parameter per argument")
		}
		if kind == "guard" && !types.Identical(signature.Results().At(0).Type(), types.Typ[types.Bool]) {
			return nil, e.fail(x, "guard must return bool")
		}
		result = &valueir.Value{Kind: valueir.Invoke, Hook: e.tables.hookID(name)}
	}
	result.Type = e.tables.typeID(signature.Results().At(0).Type())
	positions, err := e.builderPositions(x, signature, kind)
	if err != nil {
		return nil, err
	}
	ordered := requiresWrittenOrder(x)
	for i, arg := range x.Args {
		typ := signature.Params().At(positions[i]).Type()
		value, err := e.resolveValue(arg, "build", typ)
		if err != nil {
			return nil, err
		}
		result.Arguments = append(result.Arguments, valueir.Argument{Value: value, Type: e.tables.typeID(typ),
			Position:    positions[i],
			Materialize: ordered && (arg.Kind == semantic.Call || arg.Kind == semantic.Effects || arg.Kind == semantic.List)})
	}
	return result, nil
}

func (e *resolver) resolveCollection(x *semantic.Expr, expected types.Type) (*valueir.Value, error) {
	collection, known := expected.Underlying().(*types.Slice)
	if !known {
		if x.Kind == semantic.Effects {
			return nil, e.fail(x, "effects requires a slice result or slice builder parameter")
		}
		return nil, e.fail(x, "list construction requires a slice type")
	}
	result := &valueir.Value{Kind: valueir.Collection, Type: e.tables.typeID(expected)}
	for i, arg := range x.Args {
		value, err := e.resolveValue(arg, "build", collection.Elem())
		if err != nil {
			return nil, err
		}
		validate := x.Kind == semantic.List || e.unionFor(collection.Elem()) != nil
		result.Arguments = append(result.Arguments, valueir.Argument{Value: value, Type: e.tables.typeID(collection.Elem()),
			Position: i, Materialize: true, Validate: validate})
	}
	return result, nil
}

func (e *resolver) resolveUpdate(x *semantic.Expr) (*valueir.Value, error) {
	record, base, err := e.updateRecord(x)
	if err != nil {
		return nil, err
	}
	indices, err := e.updateFields(x, record)
	if err != nil {
		return nil, err
	}
	result := &valueir.Value{Kind: valueir.Update, Type: record.schema.Type, Record: record.schema,
		Base: &valueir.Value{Kind: valueir.Operand, Type: e.tables.typeID(base.typ), Operand: matchir.Operand{Kind: matchir.ValueOperand, Value: base.id}}}
	for i, arg := range x.Args[1:] {
		typ := record.fields[indices[i]].Type()
		value, err := e.resolveValue(arg, "build", typ)
		if err != nil {
			return nil, err
		}
		result.Arguments = append(result.Arguments, valueir.Argument{Value: value, Type: e.tables.typeID(typ),
			Position: indices[i]})
	}
	return result, nil
}

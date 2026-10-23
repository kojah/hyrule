package goresolve

import (
	"go/token"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

type boundConstant struct {
	object *types.Const
	schema matchir.ConstantSchema
}

func (e *resolver) bindConstant(binding *semantic.Binding) error {
	object, known := e.host.Package.Scope().Lookup(binding.HostName).(*types.Const)
	if !known {
		return &semantic.Diagnostic{Position: binding.Position, Message: "constant binding requires a Go constant " + binding.HostName}
	}
	for _, existing := range e.tables.constants {
		if existing.object == object {
			e.constants[binding.Name] = existing
			return nil
		}
	}
	value := &boundConstant{object: object, schema: matchir.ConstantSchema{ID: matchir.ConstantID(len(e.tables.constants)), Type: e.tables.typeID(object.Type())}}
	e.constants[binding.Name] = value
	e.tables.constants = append(e.tables.constants, value)
	return nil
}

func (e *resolver) constant(expression *semantic.Expr) (*boundConstant, error) {
	value := e.constants[expression.Name]
	if value == nil {
		return nil, e.fail(expression, "unknown constant "+expression.Name)
	}
	return value, nil
}

func (e *resolver) checkConstant(expression *semantic.Expr, input types.Type) error {
	value, err := e.constant(expression)
	if err != nil {
		return err
	}
	if !types.Comparable(input) || !types.AssignableTo(value.object.Type(), input) {
		return e.fail(expression, "constant pattern type mismatch")
	}
	return nil
}

// goConstant finds a package-level Go constant of a named type, local or
// qualified by one of the binding file's import names.
func (e *resolver) goConstant(name string) *types.Const {
	scope := e.host.Package.Scope()
	if label, member, qualified := strings.Cut(name, "."); qualified {
		if _, capture := e.captures[label]; capture || e.host.ImportNames[label] == nil {
			return nil
		}
		scope, name = e.host.ImportNames[label].Scope(), member
	}
	object, ok := scope.Lookup(name).(*types.Const)
	if !ok || !object.Exported() && object.Pkg() != e.host.Package {
		return nil
	}
	if _, named := types.Unalias(object.Type()).(*types.Named); !named {
		return nil
	}
	return object
}

// implicitConstant binds a Go constant under its own name in this resolver's
// tables. Checking and planning resolvers each register their own identity.
func (e *resolver) implicitConstant(name string, object *types.Const) {
	for _, existing := range e.tables.constants {
		if existing.object == object {
			e.constants[name] = existing
			return
		}
	}
	value := &boundConstant{object: object, schema: matchir.ConstantSchema{ID: matchir.ConstantID(len(e.tables.constants)), Type: e.tables.typeID(object.Type())}}
	e.constants[name] = value
	e.tables.constants = append(e.tables.constants, value)
}

// constantPattern desugars typed Go constants used as patterns. A bare or
// nullary name whose constant has exactly the input type tests Go equality.
// A name applied to a struct input whose unique field has the constant's type
// is a tagged variant: that field equals the constant and named fields match.
// Values outside the declared constants therefore match no variant.
func (e *resolver) constantPattern(x *semantic.Expr, input types.Type) (*semantic.Expr, error) {
	eligible := x.Kind == semantic.Call || x.Kind == semantic.Variable && !x.Quoted
	if _, capture := e.captures[x.Name]; !eligible || capture || e.bindings[x.Name] != nil ||
		e.constructors[x.Name] != nil || e.recordNames[x.Name] != nil {
		return x, nil
	}
	object := e.goConstant(x.Name)
	if object == nil {
		return x, nil
	}
	if types.Identical(object.Type(), input) {
		if len(x.Args) > 0 {
			return nil, e.fail(x, "constant pattern "+x.Name+" takes no fields")
		}
		e.implicitConstant(x.Name, object)
		return &semantic.Expr{Position: x.Position, Kind: semantic.Symbol, Name: x.Name, Bind: x.Bind, Field: x.Field}, nil
	}
	variant, err := e.taggedVariant(x, object, input)
	if x.Kind == semantic.Variable && err != nil {
		return x, nil // An unrelated constant's name remains an ordinary capture.
	}
	return variant, err
}

// taggedVariant rewrites Name (Field: pattern, ...) as a view over every
// accessible field of the input struct with a constant test on its tag.
func (e *resolver) taggedVariant(x *semantic.Expr, object *types.Const, input types.Type) (*semantic.Expr, error) {
	structure, _ := input.Underlying().(*types.Struct)
	if pointer, ok := input.Underlying().(*types.Pointer); ok {
		structure, _ = pointer.Elem().Underlying().(*types.Struct)
	}
	if structure == nil {
		return nil, e.fail(x, "constant "+x.Name+" does not match input type "+e.typeText(input))
	}
	key := "variant " + types.TypeString(input, nil)
	record := &boundRecord{declaration: &semantic.Record{Position: x.Position, Name: key, View: true}, typ: input}
	var results []*types.Var
	tag := ""
	for index := range structure.NumFields() {
		field := structure.Field(index)
		if field.Name() == "_" || !field.Exported() && field.Pkg() != e.host.Package {
			continue
		}
		if types.Identical(field.Type(), object.Type()) {
			if tag != "" {
				return nil, e.fail(x, "tagged variant "+x.Name+" is ambiguous: several fields have its type")
			}
			tag = field.Name()
		}
		record.fields = append(record.fields, field)
		results = append(results, types.NewVar(token.NoPos, e.host.Package, field.Name(), field.Type()))
	}
	if tag == "" {
		return nil, e.fail(x, "constant "+x.Name+" does not match input type "+e.typeText(input))
	}
	results = append(results, types.NewVar(token.NoPos, e.host.Package, "matched", types.Typ[types.Bool]))
	parameter := types.NewTuple(types.NewVar(token.NoPos, e.host.Package, "input", input))
	record.signature = types.NewSignatureType(nil, nil, nil, parameter, types.NewTuple(results...), false)
	e.recordNames[key] = record
	e.implicitConstant(x.Name, object)
	variant := &semantic.Expr{Position: x.Position, Kind: semantic.Call, Name: key, Bind: x.Bind, Field: x.Field,
		Args: []*semantic.Expr{{Position: x.Position, Kind: semantic.Symbol, Name: x.Name, Field: tag}}}
	for _, argument := range x.Args {
		if argument.Field == "" {
			return nil, e.fail(argument, "tagged variant "+x.Name+" requires named Go fields")
		}
		variant.Args = append(variant.Args, argument)
	}
	return variant, nil
}

// valueConstant reads an unbound name as its typed Go constant, so values and
// guards can use constants such as OperationEnabled without a host alias.
func (e *resolver) valueConstant(x *semantic.Expr) *semantic.Expr {
	base, _, _ := strings.Cut(x.Name, ".")
	if _, capture := e.captures[base]; x.Kind != semantic.Variable || x.Quoted || capture {
		return x
	}
	object := e.goConstant(x.Name)
	if object == nil {
		return x
	}
	e.implicitConstant(x.Name, object)
	return &semantic.Expr{Position: x.Position, Kind: semantic.Symbol, Name: x.Name, Field: x.Field}
}

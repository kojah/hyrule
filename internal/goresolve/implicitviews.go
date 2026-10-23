package goresolve

import (
	"go/token"
	"go/types"

	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

// implicitView resolves a pattern that names a Go type without a view
// declaration, such as ChannelSnapshot (Fill: fill) or ssa.Call (Call: call).
// Labels are Go field names. The view exposes only the fields the pattern
// names, so each use is an independent read-only extraction. It returns nil
// when the name is not an undeclared Go type.
func (e *resolver) implicitView(x *semantic.Expr, input types.Type) (*boundRecord, error) {
	if x.Kind != semantic.Call || e.bindings[x.Name] != nil || e.constructors[x.Name] != nil || e.recordNames[x.Name] != nil {
		return nil, nil
	}
	named, err := e.host.Type(x.Name)
	if err != nil {
		return nil, nil
	}
	if _, isNamed := types.Unalias(named).(*types.Named); !isNamed || genericStorage(named) {
		return nil, e.fail(x, "implicit view requires a non-generic named Go type "+x.Name)
	}
	storage, err := e.implicitStorage(x, named, input)
	if err != nil {
		return nil, err
	}
	record := &boundRecord{declaration: &semantic.Record{Position: x.Position, Name: x.Name, View: true}, typ: storage}
	var results []*types.Var
	for _, argument := range x.Args {
		if argument.Field == "" {
			return nil, e.fail(argument, "implicit view "+x.Name+" requires named Go fields")
		}
		member, err := e.viewMember(argument, storage)
		if err != nil {
			return nil, err
		}
		record.fields = append(record.fields, member)
		results = append(results, types.NewVar(token.NoPos, e.host.Package, argument.Field, member.Type()))
	}
	results = append(results, types.NewVar(token.NoPos, e.host.Package, "matched", types.Typ[types.Bool]))
	parameter := types.NewTuple(types.NewVar(token.NoPos, e.host.Package, "input", storage))
	record.signature = types.NewSignatureType(nil, nil, nil, parameter, types.NewTuple(results...), false)
	return record, nil
}

// implicitStorage selects the matched Go storage: the input type itself, its
// pointer or value form, or a concrete type asserted from an interface input.
// An interface input asserts the value type when it implements the interface,
// otherwise the pointer type, as Go method sets dictate for ssa-style nodes.
func (e *resolver) implicitStorage(x *semantic.Expr, named, input types.Type) (types.Type, error) {
	pointer := types.NewPointer(named)
	switch {
	case types.Identical(input, named) || types.Identical(input, pointer):
		return input, nil
	case types.IsInterface(input) && !types.IsInterface(named):
		iface := input.Underlying().(*types.Interface)
		for _, candidate := range []types.Type{named, pointer} {
			if types.Implements(candidate, iface) {
				return candidate, nil
			}
		}
	}
	return nil, e.fail(x, "implicit view "+x.Name+" does not match input type "+types.TypeString(input, e.host.Qualifier))
}

// viewMember resolves a Go field, including fields promoted from embedded
// structs. Promotion through an embedded pointer is rejected because reading
// it could dereference nil; match the embedded field with a nested view.
func (e *resolver) viewMember(argument *semantic.Expr, storage types.Type) (*types.Var, error) {
	object, index, _ := types.LookupFieldOrMethod(storage, true, e.host.Package, argument.Field)
	field, ok := object.(*types.Var)
	if !ok || !field.IsField() {
		return nil, e.fail(argument, "unknown Go field "+argument.Field)
	}
	if !field.Exported() && field.Pkg() != e.host.Package {
		return nil, e.fail(argument, "inaccessible Go field "+argument.Field)
	}
	current := storage
	if pointer, ok := current.Underlying().(*types.Pointer); ok {
		current = pointer.Elem()
	}
	for _, step := range index[:len(index)-1] {
		embedded := current.Underlying().(*types.Struct).Field(step)
		if _, ok := embedded.Type().Underlying().(*types.Pointer); ok {
			return nil, e.fail(argument, "field "+argument.Field+" is promoted through embedded pointer "+embedded.Name()+"; match "+embedded.Name()+" explicitly")
		}
		current = embedded.Type()
	}
	return field, nil
}

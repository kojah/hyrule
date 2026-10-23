package goresolve

import (
	"go/token"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"

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
// An interface input accepts a concrete implementing type: the value type
// when its method set suffices, otherwise the pointer type.
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
	return nil, e.fail(x, "implicit view "+x.Name+" does not match input type "+e.typeText(input))
}

// viewMember resolves a Go field, including fields promoted from embedded
// structs. Promotion through an embedded pointer is rejected because reading
// it could dereference nil; match the embedded field with a nested view.
func (e *resolver) viewMember(argument *semantic.Expr, storage types.Type) (*types.Var, error) {
	if method, ok := strings.CutSuffix(argument.Field, "()"); ok {
		return e.viewMethod(argument, storage, method)
	}
	object, index, _ := types.LookupFieldOrMethod(storage, true, e.host.Package, argument.Field)
	field, ok := object.(*types.Var)
	if !ok || !field.IsField() {
		return nil, e.fail(argument, "unknown Go field "+argument.Field)
	}
	if !field.Exported() && field.Pkg() != e.host.Package {
		return nil, e.fail(argument, "inaccessible Go field "+argument.Field)
	}
	if err := e.promotion(argument, storage, index); err != nil {
		return nil, err
	}
	return field, nil
}

func (e *resolver) promotion(argument *semantic.Expr, storage types.Type, index []int) error {
	current := storage
	if pointer, ok := current.Underlying().(*types.Pointer); ok {
		current = pointer.Elem()
	}
	for _, step := range index[:len(index)-1] {
		embedded := current.Underlying().(*types.Struct).Field(step)
		if _, ok := embedded.Type().Underlying().(*types.Pointer); ok {
			return e.fail(argument, argument.Field+" is promoted through embedded pointer "+embedded.Name()+"; match "+embedded.Name()+" explicitly")
		}
		current = embedded.Type()
	}
	return nil
}

// viewMethod resolves an accessor projection, Name(): pattern. Only methods
// opted in through a .views file or a //hyrule:view comment qualify: they must
// be pure reads taking no arguments and returning one value. The result is a
// synthetic member whose name spells the call in generated extraction.
func (e *resolver) viewMethod(argument *semantic.Expr, storage types.Type, name string) (*types.Var, error) {
	object, index, _ := types.LookupFieldOrMethod(storage, true, e.host.Package, name)
	function, ok := object.(*types.Func)
	if !ok {
		return nil, e.fail(argument, "unknown Go method "+name)
	}
	if !e.host.Accessors[hostapi.MethodKey(function)] {
		return nil, e.fail(argument, "method "+name+" is not an opted-in accessor; list it in a .views file or mark it //hyrule:view")
	}
	signature := function.Type().(*types.Signature)
	if signature.Params().Len() != 0 || signature.Results().Len() != 1 || signature.Variadic() {
		return nil, e.fail(argument, "accessor "+name+" must take no arguments and return one value")
	}
	if err := e.promotion(argument, storage, index); err != nil {
		return nil, err
	}
	return types.NewField(token.NoPos, e.host.Package, argument.Field, signature.Results().At(0).Type(), false), nil
}

// typeText spells a Go type for diagnostics with source package names.
func (e *resolver) typeText(typ types.Type) string {
	return types.TypeString(typ, func(pkg *types.Package) string {
		if pkg == e.host.Package {
			return ""
		}
		return pkg.Name()
	})
}

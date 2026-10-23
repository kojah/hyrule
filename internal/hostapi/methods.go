package hostapi

import (
	"fmt"
	"go/token"
	"go/types"
)

// An explicit receiver binding is a Go method expression: the receiver becomes
// the first argument, with no captured runtime host. Prefer the value method
// set; use *T when only its pointer method set supplies the selected method.
func (h *Host) methodExpression(receiver types.Type, name string) (string, *types.Signature, error) {
	object, _, _ := types.LookupFieldOrMethod(receiver, false, h.Package, name)
	if object == nil {
		receiver = types.NewPointer(receiver)
		object, _, _ = types.LookupFieldOrMethod(receiver, false, h.Package, name)
	}
	fn, ok := object.(*types.Func)
	if !ok {
		return "", nil, fmt.Errorf("unknown receiver method %s", name)
	}
	sig := fn.Type().(*types.Signature)
	if sig.Variadic() || sig.TypeParams().Len() != 0 || sig.RecvTypeParams().Len() != 0 {
		return "", nil, fmt.Errorf("method %s: generic and variadic methods unsupported", name)
	}
	params := []*types.Var{types.NewVar(token.NoPos, h.Package, "receiver", receiver)}
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i))
	}
	explicit := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), sig.Results(), false)
	return "(" + h.TypeName(receiver) + ")." + name, explicit, nil
}

// MethodKey identifies a method by its declaring receiver type, as
// "import/path.Type.Method", for accessor opt-in checks.
func MethodKey(function *types.Func) string {
	receiver := function.Type().(*types.Signature).Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	named, ok := types.Unalias(receiver).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return ""
	}
	return named.Obj().Pkg().Path() + "." + named.Obj().Name() + "." + function.Name()
}

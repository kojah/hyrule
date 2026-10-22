package recordresolve

import (
	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
	"go/types"
)

func (op *operation) children() error {
	transform := op.plan.Kind == goplan.Transform
	results := 0
	short := !transform && op.sig.Results().Len() == 1
	var resultType types.Type
	if transform {
		results, resultType = 1, op.typ
	} else if short {
		results, resultType = 1, types.Typ[types.Bool]
	}
	if err := op.signature(2, results, resultType); err != nil {
		return err
	}
	callback, ok := op.sig.Params().At(1).Type().Underlying().(*types.Signature)
	if !ok || callback.Variadic() || callback.Params().Len() != 1 || callback.Results().Len() != results {
		return op.fail("child callback must take one value, returning it only for transform")
	}
	childType := callback.Params().At(0).Type()
	if short && !types.Identical(callback.Results().At(0).Type(), types.Typ[types.Bool]) {
		return op.fail("short-circuit visitor callback must return bool")
	}
	if transform && !types.Identical(callback.Results().At(0).Type(), childType) {
		return op.fail("transform callback must preserve child type")
	}
	op.plan.Short = short
	for _, field := range op.fields {
		planned := goplan.DerivedField{Name: field.declaration.Name, Member: field.member}
		typ := field.member.Type()
		switch field.declaration.Policy {
		case "keep":
			continue
		case "each":
			slice, ok := typ.Underlying().(*types.Slice)
			if !ok {
				return op.fail("each requires slice storage")
			}
			typ = slice.Elem()
			planned.Policy = goplan.Each
		case "apply":
			planned.Policy = goplan.Apply
		default:
			return op.fail("child policy must be keep, apply or each")
		}
		if !types.Identical(typ, childType) {
			return op.fail("child callback type mismatch for " + field.declaration.Name)
		}
		op.plan.Fields = append(op.plan.Fields, planned)
	}
	return nil
}

func (op *operation) display() error {
	if err := op.signature(2, 1, types.Typ[types.String]); err != nil {
		return err
	}
	if !types.Identical(op.sig.Params().At(1).Type(), types.Typ[types.Int]) {
		return op.fail("display budget must be int bytes")
	}
	for _, field := range op.fields {
		if field.declaration.Policy == "omit" {
			continue
		}
		planned := goplan.DerivedField{Name: field.declaration.Name, Member: field.member}
		if field.declaration.Policy == "keep" {
			basic, ok := field.member.Type().Underlying().(*types.Basic)
			if !ok || basic.Kind() == types.UnsafePointer || basic.Kind() == types.Invalid {
				return op.fail("display keep requires a scalar; use a budget-aware display hook")
			}
			planned.Policy, planned.Scalar = goplan.ScalarDisplay, basic
		} else {
			function, sig, err := op.hook(field.declaration.Policy, []types.Type{field.member.Type(), types.Typ[types.Int]}, 1)
			if err != nil {
				return err
			}
			if !types.Identical(sig.Results().At(0).Type(), types.Typ[types.String]) {
				return op.fail("display hook must return string")
			}
			planned.Policy, planned.Hook = goplan.CallHook, function
		}
		op.plan.Fields = append(op.plan.Fields, planned)
	}
	return nil
}

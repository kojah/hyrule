package recordgen

import (
	"fmt"
	"go/types"
)

func (op *operation) children() error {
	transform := op.declaration.Kind == "transform"
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
	if transform {
		op.out.WriteString("result := h0\n")
	}
	for i, field := range op.fields {
		policy := field.declaration.Policy
		if policy == "keep" {
			continue
		}
		selector := "h0." + field.member.Name()
		target := "result." + field.member.Name()
		typ := field.member.Type()
		if policy == "each" {
			slice, ok := typ.Underlying().(*types.Slice)
			if !ok {
				return op.fail("each requires slice storage")
			}
			typ = slice.Elem()
		} else if policy != "apply" {
			return op.fail("child policy must be keep, apply or each")
		}
		if !types.Identical(typ, childType) {
			return op.fail("child callback type mismatch for " + field.declaration.Name)
		}
		if policy == "apply" {
			if transform {
				fmt.Fprintf(&op.out, "%s = h1(%s)\n", target, selector)
			} else if short {
				fmt.Fprintf(&op.out, "if !h1(%s) { return false }\n", selector)
			} else {
				fmt.Fprintf(&op.out, "h1(%s)\n", selector)
			}
			continue
		}
		if transform {
			fmt.Fprintf(&op.out, "if %s != nil {\n%s = make(%s,len(%s))\n", selector, target, op.host.TypeName(field.member.Type()), selector)
			fmt.Fprintf(&op.out, "for hi%d,hvalue%d := range %s { %s[hi%d] = h1(hvalue%d) }\n}\n", i, i, selector, target, i, i)
		} else if short {
			fmt.Fprintf(&op.out, "for _,hvalue%d := range %s { if !h1(hvalue%d) { return false } }\n", i, selector, i)
		} else {
			fmt.Fprintf(&op.out, "for _,hvalue%d := range %s { h1(hvalue%d) }\n", i, selector, i)
		}
	}
	if transform {
		op.out.WriteString("return result\n")
	} else if short {
		op.out.WriteString("return true\n")
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
	// Append a bounded prefix before formatting a field; exhausted fields never
	// invoke their display hooks. Scalars only use fmt, never arbitrary Stringers.
	op.out.WriteString("if h1 <= 0 { return \"\" }; result := \"\"\nappendPart := func(part string) { if len(part)>h1 { part=part[:h1] }; result+=part; h1-=len(part) }\n")
	for _, field := range op.fields {
		if field.declaration.Policy == "omit" {
			continue
		}
		expression := "h0." + field.member.Name()
		if field.declaration.Policy == "keep" {
			basic, ok := field.member.Type().Underlying().(*types.Basic)
			if !ok || basic.Kind() == types.UnsafePointer || basic.Kind() == types.Invalid {
				return op.fail("display keep requires a scalar; use a budget-aware display hook")
			}
			// Convert named scalars before formatting, bypassing arbitrary methods.
			scalar := op.host.TypeName(types.Typ[basic.Kind()])
			if basic.Info()&types.IsString != 0 {
				expression = "string(" + expression + ")"
			} else {
				qualifier := op.host.Qualifier(types.NewPackage("fmt", "fmt"))
				expression = qualifier + ".Sprint(" + scalar + "(" + expression + "))"
			}
		} else {
			function, sig, err := op.hook(field.declaration.Policy, []types.Type{field.member.Type(), types.Typ[types.Int]}, 1)
			if err != nil {
				return err
			}
			if !types.Identical(sig.Results().At(0).Type(), types.Typ[types.String]) {
				return op.fail("display hook must return string")
			}
			expression = function + "(" + expression + ",h1)"
		}
		fmt.Fprintf(&op.out, "if h1 > 0 { appendPart(%q); if h1 > 0 { appendPart(%s) } }\n", field.declaration.Name+"=", expression)
		op.out.WriteString("if h1 > 0 { appendPart(\";\") }\n")
	}
	op.out.WriteString("return result\n")
	return nil
}

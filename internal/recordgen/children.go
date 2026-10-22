package recordgen

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
	"go/types"
)

func (op *operation) children() {
	transform := op.plan.Kind == goplan.Transform
	if transform {
		op.out.WriteString("result := h0\n")
	}
	for i, field := range op.plan.Fields {
		source, target := "h0."+field.Member.Name(), "result."+field.Member.Name()
		if field.Policy == goplan.Apply {
			if transform {
				fmt.Fprintf(&op.out, "%s = h1(%s)\n", target, source)
			} else if op.plan.Short {
				fmt.Fprintf(&op.out, "if !h1(%s) { return false }\n", source)
			} else {
				fmt.Fprintf(&op.out, "h1(%s)\n", source)
			}
			continue
		}
		if transform {
			fmt.Fprintf(&op.out, "if %s != nil {\n%s = make(%s,len(%s))\n", source, target, op.host.TypeName(field.Member.Type()), source)
			fmt.Fprintf(&op.out, "for hi%d,hvalue%d := range %s { %s[hi%d] = h1(hvalue%d) }\n}\n", i, i, source, target, i, i)
		} else if op.plan.Short {
			fmt.Fprintf(&op.out, "for _,hvalue%d := range %s { if !h1(hvalue%d) { return false } }\n", i, source, i)
		} else {
			fmt.Fprintf(&op.out, "for _,hvalue%d := range %s { h1(hvalue%d) }\n", i, source, i)
		}
	}
	if transform {
		op.out.WriteString("return result\n")
	} else if op.plan.Short {
		op.out.WriteString("return true\n")
	}
}

func (op *operation) display() {
	op.out.WriteString("if h1 <= 0 { return \"\" }; result := \"\"\n")
	op.out.WriteString("appendPart := func(part string) { if len(part)>h1 { part=part[:h1] }; result+=part; h1-=len(part) }\n")
	for _, field := range op.plan.Fields {
		expression := "h0." + field.Member.Name()
		if field.Policy == goplan.ScalarDisplay {
			if field.Scalar.Info()&types.IsString != 0 {
				expression = "string(" + expression + ")"
			} else {
				qualifier := op.host.Qualifier(types.NewPackage("fmt", "fmt"))
				expression = qualifier + ".Sprint(" + op.host.TypeName(types.Typ[field.Scalar.Kind()]) + "(" + expression + "))"
			}
		} else {
			expression = field.Hook + "(" + expression + ",h1)"
		}
		fmt.Fprintf(&op.out, "if h1 > 0 { appendPart(%q); if h1 > 0 { appendPart(%s) } }\n", field.Name+"=", expression)
		op.out.WriteString("if h1 > 0 { appendPart(\";\") }\n")
	}
	op.out.WriteString("return result\n")
}

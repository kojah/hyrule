package recordgen

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
	"strings"
)

func (op *operation) freeze() {
	op.out.WriteString("result := h0\n")
	for _, field := range op.plan.Fields {
		source, target := "h0."+field.Member.Name(), "result."+field.Member.Name()
		switch field.Policy {
		case goplan.Clone:
			fmt.Fprintf(&op.out, "if %s != nil { %s = make(%s,len(%s)); copy(%s,%s) }\n", source, target, op.host.TypeName(field.Member.Type()), source, target, source)
		case goplan.CallHook:
			fmt.Fprintf(&op.out, "%s = %s(%s)\n", target, field.Hook, source)
		}
	}
	op.out.WriteString("return result\n")
}

func projection(field goplan.DerivedField, input string) string {
	value := input + "." + field.Member.Name()
	switch field.Policy {
	case goplan.ProjectParameter:
		return fmt.Sprintf("h%d", field.Parameter)
	case goplan.CallHook:
		return field.Hook + "(" + value + ")"
	default:
		return value
	}
}

func (op *operation) keyFields() string {
	fields := make([]string, len(op.plan.Fields))
	for i, field := range op.plan.Fields {
		fields[i] = field.Destination.Name() + ":" + projection(field, "h0")
	}
	return strings.Join(fields, ",")
}

func (op *operation) equal() {
	for i, field := range op.plan.Fields {
		fmt.Fprintf(&op.out, "hl%d,hr%d := %s,%s\nif hl%d != hr%d { return false }\n", i, i, projection(field, "h0"), projection(field, "h1"), i, i)
	}
	op.out.WriteString("return true\n")
}

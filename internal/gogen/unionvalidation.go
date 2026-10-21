package gogen

import (
	"fmt"
	"go/types"
	"strings"
)

func (e *emitter) unionFor(typ types.Type) *boundUnion {
	for _, union := range e.unions {
		if types.Identical(typ, union.typ) {
			return union
		}
	}
	return nil
}
func (e *emitter) validator(union *boundUnion) string {
	return fmt.Sprintf("hyrule%sUnion%dValid", e.host.Function, union.schema.ID)
}

// Direct active fields with another declared union type are checked recursively.
// Go rejects cyclic value-struct layouts; pointer/container payloads are opaque
// host values and do not acquire a recursive union representation here.
func (e *emitter) activeValidation(variant *boundVariant, value string) string {
	var checks []string
	for _, field := range variant.fields {
		if nested := e.unionFor(field.Type()); nested != nil {
			checks = append(checks, e.validator(nested)+"("+value+"."+field.Name()+")")
		}
	}
	return strings.Join(checks, " && ")
}

func (e *emitter) emitValidators() {
	for _, union := range e.unions {
		fmt.Fprintf(&e.out, "func %s(value %s) bool {\nswitch value.%s {\n", e.validator(union), e.host.typeName(union.typ), union.tag.Name())
		for _, variant := range union.variants {
			checked := e.activeValidation(variant, "value")
			if checked == "" {
				checked = "true"
			}
			fmt.Fprintf(&e.out, "case %s: return %s\n", variant.tag.Name(), checked)
		}
		e.out.WriteString("}\nreturn false\n}\n")
	}
}

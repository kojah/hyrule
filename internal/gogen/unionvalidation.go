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

func (e *emitter) recordValidator(record *boundRecord) string {
	return fmt.Sprintf("hyrule%sRecord%dValid", e.host.Function, record.schema.ID)
}

func (e *emitter) validation(typ types.Type, value string) string {
	if union := e.unionFor(typ); union != nil {
		return e.validator(union) + "(" + value + ")"
	}
	if record := e.recordFor(typ); record != nil {
		if e.recordChecks(record, "value") != "" {
			return e.recordValidator(record) + "(" + value + ")"
		}
	}
	return ""
}

func (e *emitter) recordChecks(record *boundRecord, value string) string {
	var checks []string
	for _, field := range record.fields {
		if validation := e.validation(field.Type(), value+"."+field.Name()); validation != "" {
			checks = append(checks, validation)
		}
	}
	return strings.Join(checks, " && ")
}

// Direct active fields with another declared union type are checked recursively.
// Go rejects cyclic value-struct layouts; pointer/container payloads are opaque
// host values and do not acquire a recursive union representation here.
func (e *emitter) activeValidation(variant *boundVariant, value string) string {
	var checks []string
	for _, field := range variant.fields {
		if validation := e.validation(field.Type(), value+"."+field.Name()); validation != "" {
			checks = append(checks, validation)
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
	for _, record := range e.records {
		checked := e.recordChecks(record, "value")
		if checked == "" {
			continue
		}
		fmt.Fprintf(&e.out, "func %s(value %s) bool { return %s }\n", e.recordValidator(record), e.host.typeName(record.typ), checked)
	}
}

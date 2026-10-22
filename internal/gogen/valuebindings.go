package gogen

import (
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/valuegen"
	"github.com/kojah/lockdoc/tools/hyrule/internal/valueir"
)

func (e *emitter) emitValue(value *valueir.Value) string {
	renderer := valuegen.Emitter{Bindings: e, Out: &e.out, Serial: &e.serial}
	return renderer.Value(value)
}

func (e *emitter) ValueType(id matchir.TypeID) string        { return e.host.TypeName(e.tables.Types[id]) }
func (e *emitter) ValueOperand(value matchir.Operand) string { return e.operand(value) }
func (e *emitter) ValueHook(id matchir.HookID) string        { return e.tables.Hooks[id] }
func (e *emitter) ValueMember(id valueir.MemberID) string    { return e.tables.Members[id] }
func (e *emitter) ValueRecordFields(id matchir.RecordID) []string {
	fields := e.records[id].Fields
	names := make([]string, len(fields))
	for i, field := range fields {
		names[i] = field.Name()
	}
	return names
}
func (e *emitter) ValueUnionFields(id matchir.UnionID, variant matchir.VariantID) (string, string, []string) {
	union := e.unions[id]
	member := union.Variants[variant]
	names := make([]string, len(member.Fields))
	for i, field := range member.Fields {
		names[i] = field.Name()
	}
	return union.Tag.Name(), member.Tag.Name(), names
}
func (e *emitter) ValueValidation(id matchir.TypeID, value string) string {
	return e.validation(e.tables.Types[id], value)
}
func (e *emitter) ValueFailureType() string { return e.host.TypeName(sigOutput(e)) }

func (e *emitter) emitResult(result *valueir.Result) string {
	renderer := valuegen.Emitter{Bindings: e, Out: &e.out, Serial: &e.serial}
	return renderer.Result(result, "")
}

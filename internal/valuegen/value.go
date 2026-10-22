// Package valuegen emits Go for resolved value plans. It neither resolves names
// nor checks source-language constructs; those responsibilities precede emission.
package valuegen

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/valueir"
	"io"
	"strings"
)

// Bindings maps resolved identities to Go storage and type spellings.
type Bindings interface {
	ValueType(matchir.TypeID) string
	ValueOperand(matchir.Operand) string
	ValueHook(matchir.HookID) string
	ValueMember(valueir.MemberID) string
	ValueRecordFields(matchir.RecordID) []string
	ValueUnionFields(matchir.UnionID, matchir.VariantID) (tag, variant string, fields []string)
	ValueValidation(matchir.TypeID, string) string
	ValueFailureType() string
}

// Emitter writes statement prerequisites and returns the final Go expression.
// Serial belongs to the containing function and prevents local-name collisions.
type Emitter struct {
	Bindings Bindings
	Out      io.Writer
	Serial   *int
}

// Value emits an already resolved computation in its original evaluation order.
func (e *Emitter) Value(value *valueir.Value) string {
	switch value.Kind {
	case valueir.Operand:
		return e.Bindings.ValueOperand(value.Operand)
	case valueir.Project:
		return e.Value(value.Base) + "." + e.Bindings.ValueMember(value.Member)
	case valueir.Update:
		return e.update(value)
	}
	arguments := make([]string, len(value.Arguments))
	for _, argument := range value.Arguments {
		text := e.Value(argument.Value)
		if argument.Materialize {
			text = e.temporary(argument.Type, text, argument.Validate)
		}
		arguments[argument.Position] = text
	}
	switch value.Kind {
	case valueir.Invoke:
		return e.Bindings.ValueHook(value.Hook) + "(" + strings.Join(arguments, ", ") + ")"
	case valueir.Collection:
		return e.Bindings.ValueType(value.Type) + "{" + strings.Join(arguments, ", ") + "}"
	case valueir.Product:
		return e.aggregate(value.Type, e.Bindings.ValueRecordFields(value.Record.ID), arguments, nil)
	case valueir.Tagged:
		tag, variant, fields := e.Bindings.ValueUnionFields(value.Union.ID, value.Variant)
		return e.aggregate(value.Type, fields, arguments, []string{tag + ": " + variant})
	default:
		panic("unhandled resolved value operation")
	}
}

func (e *Emitter) aggregate(typ matchir.TypeID, fields, values, prefix []string) string {
	for i, name := range fields {
		prefix = append(prefix, name+": "+values[i])
	}
	return e.Bindings.ValueType(typ) + "{" + strings.Join(prefix, ", ") + "}"
}

func (e *Emitter) temporary(typ matchir.TypeID, value string, validate bool) string {
	*e.Serial++
	name := fmt.Sprintf("hv%d", *e.Serial)
	fmt.Fprintf(e.Out, "var %s %s = %s\n", name, e.Bindings.ValueType(typ), value)
	if validate {
		if check := e.Bindings.ValueValidation(typ, name); check != "" {
			fmt.Fprintf(e.Out, "if !%s { var zero %s; return zero,false }\n", check, e.Bindings.ValueFailureType())
		}
	}
	return name
}

func (e *Emitter) update(value *valueir.Value) string {
	base := e.temporary(value.Type, e.Value(value.Base), true)
	fields := e.Bindings.ValueRecordFields(value.Record.ID)
	for _, argument := range value.Arguments {
		text := e.Value(argument.Value)
		fmt.Fprintf(e.Out, "%s.%s = %s\n", base, fields[argument.Position], text)
	}
	return base
}

// Package valuegen emits Go for resolved value plans. It neither resolves names
// nor checks source-language constructs; those responsibilities precede emission.
package valuegen

import (
	"fmt"
	"github.com/kojah/hyrule/internal/matchir"
	"github.com/kojah/hyrule/internal/valueir"
	"go/token"
	"io"
	"strings"
)

// Bindings maps resolved identities to Go storage and type spellings.
type Bindings interface {
	ValueType(matchir.TypeID) string
	ValueOperand(matchir.Operand) string
	ValueUsed(matchir.ValueID) bool
	ValueHook(matchir.HookID) string
	ValueMember(valueir.MemberID) string
	ValueRecordFields(matchir.RecordID) []string
	ValueUnionFields(matchir.UnionID, matchir.VariantID) (tag, variant string, fields []string)
	ValueValidation(matchir.TypeID, string) string
	ValueFailure() string
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
	case valueir.Compare, valueir.Arithmetic:
		return e.operator(value)
	case valueir.Logic:
		return e.logic(value)
	case valueir.Conditional:
		return e.conditional(value)
	}
	size := len(value.Arguments)
	if value.Kind == valueir.Product {
		size = len(e.Bindings.ValueRecordFields(value.Record.ID))
	}
	arguments := e.arguments(value.Arguments, size)
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

// aggregate emits a keyed composite literal; an omitted derived-record field
// has no value and keeps its Go zero value.
func (e *Emitter) aggregate(typ matchir.TypeID, fields, values, prefix []string) string {
	for i, name := range fields {
		if values[i] != "" {
			prefix = append(prefix, name+": "+values[i])
		}
	}
	return e.Bindings.ValueType(typ) + "{" + strings.Join(prefix, ", ") + "}"
}

func (e *Emitter) temporary(typ matchir.TypeID, value string, validate, inferred bool) string {
	if inferred && token.IsIdentifier(value) && (!validate || e.Bindings.ValueValidation(typ, value) == "") {
		return value
	}
	*e.Serial++
	name := fmt.Sprintf("hv%d", *e.Serial)
	if inferred {
		fmt.Fprintf(e.Out, "%s := %s\n", name, value)
	} else {
		fmt.Fprintf(e.Out, "var %s %s = %s\n", name, e.Bindings.ValueType(typ), value)
	}
	if validate {
		if check := e.Bindings.ValueValidation(typ, name); check != "" {
			fmt.Fprintf(e.Out, "if !%s { %s }\n", check, e.Bindings.ValueFailure())
		}
	}
	return name
}

func (e *Emitter) update(value *valueir.Value) string {
	base := e.temporary(value.Type, e.Value(value.Base), true, value.Type == value.Base.Type)
	fields := e.Bindings.ValueRecordFields(value.Record.ID)
	for _, argument := range value.Arguments {
		text := e.Value(argument.Value)
		fmt.Fprintf(e.Out, "%s.%s = %s\n", base, fields[argument.Position], text)
	}
	return base
}

// stableValue can be delayed within one construction without observing a new
// read or changing callback/panic order. Calls, projections and arithmetic keep
// their existing evaluation points; established captures are immutable values.
func stableValue(value *valueir.Value) bool {
	switch value.Kind {
	case valueir.Operand:
		return true
	case valueir.Product, valueir.Tagged, valueir.Collection:
		for _, argument := range value.Arguments {
			if !stableValue(argument.Value) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

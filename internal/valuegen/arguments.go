package valuegen

import (
	"fmt"
	"strings"

	"github.com/kojah/hyrule/internal/valueir"
)

type argumentCode struct {
	argument       valueir.Argument
	prelude, value string
	materialize    bool
}

// arguments keeps written evaluation order even when a later argument emits
// statements or named fields are assembled in declaration order. Immutable
// captures and literals may remain expressions; callbacks and reads may not
// cross those boundaries.
func (e *Emitter) arguments(arguments []valueir.Argument, size int) []string {
	codes := make([]argumentCode, len(arguments))
	reordered := false
	for index, argument := range arguments {
		if index > 0 && argument.Position < arguments[index-1].Position {
			reordered = true
		}
		var prelude strings.Builder
		saved := e.Out
		e.Out = &prelude
		value := e.Value(argument.Value)
		e.Out = saved
		codes[index] = argumentCode{argument: argument, prelude: prelude.String(), value: value}
	}
	laterStatements := false
	for index := len(codes) - 1; index >= 0; index-- {
		code := &codes[index]
		validation := code.argument.Validate && e.Bindings.ValueValidation(code.argument.Type, "value") != ""
		code.materialize = validation || !stableValue(code.argument.Value) && (reordered || code.argument.Materialize || laterStatements)
		laterStatements = laterStatements || code.materialize || code.prelude != ""
	}
	values := make([]string, size)
	for _, code := range codes {
		fmt.Fprint(e.Out, code.prelude)
		value := code.value
		if code.materialize {
			value = e.temporary(code.argument.Type, value, code.argument.Validate, code.argument.Type == code.argument.Value.Type)
		}
		values[code.argument.Position] = value
	}
	return values
}

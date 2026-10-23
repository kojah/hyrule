package valuegen

import (
	"fmt"
	"strings"

	"github.com/kojah/hyrule/internal/matchir"
	"github.com/kojah/hyrule/internal/valueir"
)

// ComparisonSpelling is the Go operator for a portable comparison.
func ComparisonSpelling(operator matchir.Comparison) string {
	return [...]string{"==", "!=", "<", "<=", ">", ">="}[operator]
}

// operator emits comparisons and arithmetic. Both operands are always
// evaluated, left before right, so their prerequisites may precede the result.
func (e *Emitter) operator(value *valueir.Value) string {
	left := e.Value(value.Arguments[0].Value)
	var prelude strings.Builder
	saved := e.Out
	e.Out = &prelude
	right := e.Value(value.Arguments[1].Value)
	e.Out = saved
	if prelude.Len() > 0 && !stableValue(value.Arguments[0].Value) {
		left = e.temporary(value.Arguments[0].Value.Type, left, false, true)
	}
	fmt.Fprint(e.Out, prelude.String())
	if value.Kind == valueir.Compare {
		return "(" + left + " " + ComparisonSpelling(value.Comparison) + " " + right + ")"
	}
	return "(" + left + " " + [...]string{"+", "-", "*", "/", "%"}[value.Arithmetic] + " " + right + ")"
}

// logic preserves short-circuit evaluation: the second operand's prerequisite
// statements run only inside the branch where Go would evaluate it.
func (e *Emitter) logic(value *valueir.Value) string {
	if value.Logic == valueir.Not {
		return "!(" + e.Value(value.Arguments[0].Value) + ")"
	}

	var left, right strings.Builder
	saved := e.Out
	e.Out = &left
	first := e.Value(value.Arguments[0].Value)
	e.Out = &right
	second := e.Value(value.Arguments[1].Value)
	e.Out = saved
	fmt.Fprint(e.Out, left.String())
	if right.Len() == 0 {
		operator := " && "
		if value.Logic == valueir.Or {
			operator = " || "
		}
		return "(" + first + operator + second + ")"
	}
	name := e.local()
	fmt.Fprintf(e.Out, "%s := %s\n", name, first)
	if value.Logic == valueir.And {
		fmt.Fprintf(e.Out, "if %s {\n", name)
	} else {
		fmt.Fprintf(e.Out, "if !%s {\n", name)
	}
	fmt.Fprintf(e.Out, "%s%s = %s\n}\n", right.String(), name, second)
	return name
}

// conditional evaluates exactly one branch after its condition.
func (e *Emitter) conditional(value *valueir.Value) string {
	condition := e.Value(value.Arguments[0].Value)
	name := e.local()
	fmt.Fprintf(e.Out, "var %s %s\nif %s {\n", name, e.Bindings.ValueType(value.Type), condition)
	fmt.Fprintf(e.Out, "%s = %s\n} else {\n", name, e.Value(value.Arguments[1].Value))
	fmt.Fprintf(e.Out, "%s = %s\n}\n", name, e.Value(value.Arguments[2].Value))
	return name
}

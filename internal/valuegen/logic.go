package valuegen

import (
	"fmt"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/valueir"
)

// ComparisonSpelling is the Go operator for a portable comparison.
func ComparisonSpelling(operator matchir.Comparison) string {
	return [...]string{"==", "!=", "<", "<=", ">", ">="}[operator]
}

// operator emits comparisons and arithmetic. Both operands are always
// evaluated, left before right, so their prerequisites may precede the result.
func (e *Emitter) operator(value *valueir.Value) string {
	left, right := e.Value(value.Arguments[0].Value), e.Value(value.Arguments[1].Value)
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
	name := e.local()
	fmt.Fprintf(e.Out, "var %s bool = %s\n", name, e.Value(value.Arguments[0].Value))
	if value.Logic == valueir.And {
		fmt.Fprintf(e.Out, "if %s {\n", name)
	} else {
		fmt.Fprintf(e.Out, "if !%s {\n", name)
	}
	fmt.Fprintf(e.Out, "%s = %s\n}\n", name, e.Value(value.Arguments[1].Value))
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

// branch continues with one result; an unanswered (declining) branch leaves
// the zero output and its enclosing choice flag unset.
func (e *Emitter) branch(result *valueir.Result, answered string) string {
	condition := e.Value(result.Value)
	name := e.local()
	fmt.Fprintf(e.Out, "var %s %s\nif %s {\n", name, e.Bindings.ValueType(result.Type), condition)
	fmt.Fprintf(e.Out, "%s = %s\n} else {\n", name, e.Result(result.Alternatives[0], answered))
	fmt.Fprintf(e.Out, "%s = %s\n}\n", name, e.Result(result.Alternatives[1], answered))
	return name
}

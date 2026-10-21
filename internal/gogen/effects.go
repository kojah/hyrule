package gogen

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// Effect lists are typed construction, not execution. Each element is evaluated
// once in written order; the generated function never applies the returned plan.
func sigOutput(e *emitter) types.Type { return e.host.Signature.Results().At(0).Type() }

func (e *emitter) typedValue(x *syntax.Expr, expected types.Type) (string, error) {
	if x.Kind != syntax.Effects {
		return e.value(x, "build")
	}
	slice, ok := expected.Underlying().(*types.Slice)
	if !ok {
		return "", e.fail(x, "effects requires a slice result or slice builder parameter")
	}
	var elements []string
	for _, child := range x.Args {
		value, err := e.typedValue(child, slice.Elem())
		if err != nil {
			return "", err
		}
		e.serial++
		name := fmt.Sprintf("hv%d", e.serial)
		fmt.Fprintf(&e.out, "var %s %s = %s\n", name, e.host.typeName(slice.Elem()), value)
		if union := e.unionFor(slice.Elem()); union != nil {
			fmt.Fprintf(&e.out, "if !%s(%s) { var zero %s; return zero,false }\n", e.validator(union), name, e.host.typeName(sigOutput(e)))
		}
		elements = append(elements, name)
	}
	return e.host.typeName(expected) + "{" + strings.Join(elements, ",") + "}", nil
}

func (e *emitter) resultValue(x *syntax.Expr, expected types.Type) (string, error) {
	if x.Kind == syntax.Query {
		return e.queryValue(x, expected)
	}
	return e.typedValue(x, expected)
}

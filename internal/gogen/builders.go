package gogen

import (
	"fmt"
	"go/types"

	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// valueArguments preserves evaluation in source order even when named arguments
// are passed in Go parameter order. Typed temporaries preserve constant/nil
// assignment contexts and each nested builder is evaluated exactly once.
func (e *emitter) valueArguments(x *syntax.Expr, signature *types.Signature, kind string) ([]string, error) {
	indices, err := e.builderPositions(x, signature, kind)
	if err != nil {
		return nil, err
	}
	arguments := make([]string, len(x.Args))
	ordered := requiresWrittenOrder(x)
	for written, argument := range x.Args {
		parameter := indices[written]
		value, err := e.typedValue(argument, signature.Params().At(parameter).Type())
		if err != nil {
			return nil, err
		}
		if ordered && (argument.Kind == syntax.Call || argument.Kind == syntax.Effects) {
			e.serial++
			name := fmt.Sprintf("hv%d", e.serial)
			fmt.Fprintf(&e.out, "var %s %s = %s\n", name, e.host.TypeName(signature.Params().At(parameter).Type()), value)
			value = name
		}
		arguments[parameter] = value
	}
	return arguments, nil
}

func (e *emitter) builderPositions(x *syntax.Expr, signature *types.Signature, kind string) ([]int, error) {
	named := hasNamedArguments(x)
	indices := make([]int, len(x.Args))
	if !named {
		for index := range indices {
			indices[index] = index
		}
		return indices, nil
	}
	if kind != "build" {
		return nil, e.fail(x, "named arguments are supported for builders and matchers, not guards")
	}
	names := map[string]int{}
	for index := 0; index < signature.Params().Len(); index++ {
		name := signature.Params().At(index).Name()
		if name == "" || name == "_" {
			return nil, e.fail(x, "named builder arguments require usable Go parameter names")
		}
		if _, exists := names[name]; exists {
			return nil, e.fail(x, "named builder arguments require unique Go parameter names")
		}
		names[name] = index
	}
	seen := map[string]struct{}{}
	for written, argument := range x.Args {
		if argument.Field == "" {
			return nil, e.fail(argument, "cannot mix named and positional builder arguments")
		}
		parameter, exists := names[argument.Field]
		if !exists {
			return nil, e.fail(argument, "unknown builder argument "+argument.Field)
		}
		if _, duplicate := seen[argument.Field]; duplicate {
			return nil, e.fail(argument, "duplicate builder argument "+argument.Field)
		}
		seen[argument.Field] = struct{}{}
		indices[written] = parameter
	}
	if len(seen) != signature.Params().Len() {
		return nil, e.fail(x, "missing named builder arguments")
	}
	return indices, nil
}

// Positional parents must sequence earlier siblings before named children or
// structural values emit temporaries. Other positional trees retain their output.
func requiresWrittenOrder(expression *syntax.Expr) bool {
	if expression.Kind == syntax.List || expression.Kind == syntax.Update {
		return true
	}
	for _, argument := range expression.Args {
		if argument.Field != "" || argument.Kind == syntax.Effects || requiresWrittenOrder(argument) {
			return true
		}
	}
	return false
}

func hasNamedArguments(expression *syntax.Expr) bool {
	for _, argument := range expression.Args {
		if argument.Field != "" {
			return true
		}
	}
	return false
}

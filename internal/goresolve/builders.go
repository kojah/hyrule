package goresolve

import (
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
	"go/types"
)

func (e *resolver) builderPositions(x *semantic.Expr, signature *types.Signature, kind string) ([]int, error) {
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
func requiresWrittenOrder(expression *semantic.Expr) bool {
	if expression.Kind == semantic.List || expression.Kind == semantic.Update {
		return true
	}
	for _, argument := range expression.Args {
		if argument.Field != "" || argument.Kind == semantic.Effects || requiresWrittenOrder(argument) {
			return true
		}
	}
	return false
}

func hasNamedArguments(expression *semantic.Expr) bool {
	for _, argument := range expression.Args {
		if argument.Field != "" {
			return true
		}
	}
	return false
}

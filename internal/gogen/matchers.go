package gogen

import (
	"go/types"

	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

// matcher resolves child positions once for both contract checking and emission.
// Named fields refer to Go result names; the final success bool is never a child.
func (e *emitter) matcher(x *semantic.Expr, input types.Type) (string, *types.Signature, []int, error) {
	var name string
	var sig *types.Signature
	var err error
	if record := e.recordNames[x.Name]; record != nil && record.declaration.View {
		name, sig, err = e.viewMatcher(x, record, input)
	} else {
		name, sig, err = e.matchSignature(x)
	}
	if err != nil {
		return "", nil, nil, err
	}
	n := sig.Results().Len()
	if sig.Params().Len() != 1 || n < 1 || !types.Identical(sig.Results().At(n-1).Type(), types.Typ[types.Bool]) {
		return "", nil, nil, e.fail(x, "matcher requires one input and results followed by bool")
	}
	if !types.AssignableTo(input, sig.Params().At(0).Type()) {
		return "", nil, nil, e.fail(x, "matcher input type mismatch")
	}
	children := n - 1
	if e.capturedView(x) != nil {
		children--
	}
	indices := make([]int, len(x.Args))
	named := len(x.Args) > 0 && x.Args[0].Field != ""
	if !named && len(x.Args) != children && (e.constructors[x.Name] == nil && e.recordNames[x.Name] == nil || len(x.Args) != 0) {
		return "", nil, nil, e.fail(x, "matcher requires one result per pattern argument followed by bool")
	}
	seen := map[string]struct{}{}
	for i, arg := range x.Args {
		if (arg.Field != "") != named {
			return "", nil, nil, e.fail(arg, "cannot mix named and positional matcher children")
		}
		if !named {
			indices[i] = i
			continue
		}
		if _, exists := seen[arg.Field]; exists {
			return "", nil, nil, e.fail(arg, "duplicate matcher field "+arg.Field)
		}
		seen[arg.Field] = struct{}{}
		indices[i] = -1
		for result := 0; result < children; result++ {
			if sig.Results().At(result).Name() == arg.Field {
				indices[i] = result
				break
			}
		}
		if indices[i] < 0 {
			return "", nil, nil, e.fail(arg, "unknown matcher field "+arg.Field)
		}
	}
	return name, sig, indices, nil
}

// checkPattern joins capture contracts at each OR, before later siblings can
// hide a missing binding. It never invokes a host hook or constructs effects.
func (e *emitter) checkPattern(x *semantic.Expr, input types.Type) error {
	if x.Bind != "" {
		captured := input
		if view := e.capturedView(x); view != nil {
			captured = view.typ
		}
		if err := e.bind(x, x.Bind, 0, captured); err != nil {
			return err
		}
	}
	switch x.Kind {
	case semantic.Variable:
		return e.bind(x, x.Name, 0, input)
	case semantic.Symbol:
		return e.checkConstant(x, input)
	case semantic.Call:
		_, sig, indices, err := e.matcher(x, input)
		if err != nil {
			return err
		}
		for i, arg := range x.Args {
			if err := e.checkPattern(arg, sig.Results().At(indices[i]).Type()); err != nil {
				return err
			}
		}
	case semantic.Alternation:
		incoming := e.captures
		var joined map[string]capture
		for _, alternative := range x.Args {
			e.captures = cloneCaptures(incoming)
			if err := e.checkPattern(alternative, input); err != nil {
				return err
			}
			if joined == nil {
				joined = e.captures
				continue
			}
			if !sameCaptures(joined, e.captures) {
				return e.fail(alternative, "or alternatives must bind identical capture names and types")
			}
		}
		e.captures = joined
	}
	return nil
}

func cloneCaptures(source map[string]capture) map[string]capture {
	result := make(map[string]capture, len(source))
	for name, value := range source {
		result[name] = value
	}
	return result
}

func sameCaptures(left, right map[string]capture) bool {
	if len(left) != len(right) {
		return false
	}
	for name, value := range left {
		other, ok := right[name]
		if !ok || !types.Identical(value.typ, other.typ) {
			return false
		}
	}
	return true
}

func (e *emitter) matchSignature(x *semantic.Expr) (string, *types.Signature, error) {
	if record := e.recordNames[x.Name]; record != nil {
		return x.Name, record.signature, nil
	}
	if constructor := e.constructors[x.Name]; constructor != nil {
		return x.Name, constructor.signature, nil
	}
	return e.binding(x, "match")
}

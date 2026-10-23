package goresolve

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/kojah/hyrule/internal/semantic"
)

// matcher resolves child positions once for both contract checking and emission.
// Named fields refer to Go result names; the final success bool is never a child.
func (e *resolver) matcher(x *semantic.Expr, input types.Type) (string, *types.Signature, []int, error) {
	name, sig, err := e.matcherHook(x, input)
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
	if view, _ := e.capturedView(x, input); view != nil {
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

// matcherHook selects a list extraction, a declared or derived view, or a
// declared matcher, constructor or record schema.
func (e *resolver) matcherHook(x *semantic.Expr, input types.Type) (string, *types.Signature, error) {
	if x.Kind == semantic.List {
		return e.listMatcher(x, input)
	}
	if record := e.recordNames[x.Name]; record != nil && record.declaration.View {
		return e.viewMatcher(x, record, input)
	}
	implicit, err := e.implicitView(x, input)
	if err != nil {
		return "", nil, err
	}
	if implicit != nil {
		return e.viewMatcher(x, implicit, input)
	}
	return e.matchSignature(x)
}

// checkPattern joins capture contracts at each OR, before later siblings can
// hide a missing binding. It never invokes a host hook or constructs effects.
func (e *resolver) checkPattern(x *semantic.Expr, input types.Type) error {
	x, err := e.constantPattern(x, input)
	if err != nil {
		return err
	}
	if x.Bind != "" {
		captured := input
		if view, err := e.capturedView(x, input); err != nil {
			return err
		} else if view != nil {
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
	case semantic.Call, semantic.List:
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

func (e *resolver) matchSignature(x *semantic.Expr) (string, *types.Signature, error) {
	if record := e.recordNames[x.Name]; record != nil {
		return x.Name, record.signature, nil
	}
	if constructor := e.constructors[x.Name]; constructor != nil {
		return x.Name, constructor.signature, nil
	}
	return e.binding(x, "match")
}

// listMatcher emits a pure fixed-arity extraction for a list pattern. Go slices
// use len and indexing. Other sequences expose Len() int and At(int) E, plus
// Sub(int, int) of their own type for a rest pattern. A length mismatch is an
// ordinary failed match; the rest suffix aliases the input without copying.
func (e *resolver) listMatcher(x *semantic.Expr, input types.Type) (string, *types.Signature, error) {
	fixed := len(x.Args)
	if x.Rest {
		fixed--
	}
	length, at, suffix := "len(hinput)", "hinput[%d]", "hinput[%d:]"
	var element types.Type
	if slice, ok := input.Underlying().(*types.Slice); ok {
		element = slice.Elem()
	} else {
		element = e.sequenceElement(input, x.Rest)
		if element == nil {
			message := "list pattern requires a slice or a type with Len() int and At(int) methods"
			if x.Rest {
				message += ", and Sub(int, int) of the same type for a rest pattern"
			}
			return "", nil, e.fail(x, message)
		}
		length, at, suffix = "hinput.Len()", "hinput.At(%d)", "hinput.Sub(%d, hinput.Len())"
	}
	var parameters, values []string
	var results []*types.Var
	for index := range fixed {
		name := fmt.Sprintf("h%d", index)
		parameters = append(parameters, name+" "+e.host.TypeName(element))
		values = append(values, fmt.Sprintf(at, index))
		results = append(results, types.NewVar(token.NoPos, e.host.Package, name, element))
	}
	comparison := "!="
	if x.Rest {
		comparison = "<"
		parameters = append(parameters, "hrest "+e.host.TypeName(input))
		values = append(values, fmt.Sprintf(suffix, fixed))
		results = append(results, types.NewVar(token.NoPos, e.host.Package, "hrest", input))
	}
	parameters = append(parameters, "hok bool")
	values = append(values, "true")
	results = append(results, types.NewVar(token.NoPos, e.host.Package, "matched", types.Typ[types.Bool]))
	body := fmt.Sprintf("(func(hinput %s) (%s) { if %s %s %d { return }; return %s })",
		e.host.TypeName(input), strings.Join(parameters, ", "), length, comparison, fixed, strings.Join(values, ", "))
	parameter := types.NewTuple(types.NewVar(token.NoPos, e.host.Package, "input", input))
	return body, types.NewSignatureType(nil, nil, nil, parameter, types.NewTuple(results...), false), nil
}

// sequenceElement returns E for a value-receiver Len() int and At(int) E,
// requiring Sub(int, int) T as well when rest is set. Host sequences promise
// that these methods are pure reads, like any other matcher.
func (e *resolver) sequenceElement(input types.Type, rest bool) types.Type {
	method := func(name string) *types.Signature {
		object, _, _ := types.LookupFieldOrMethod(input, false, e.host.Package, name)
		if function, ok := object.(*types.Func); ok {
			if signature := function.Type().(*types.Signature); !signature.Variadic() && signature.TypeParams().Len() == 0 {
				return signature
			}
		}
		return nil
	}
	integer := func(tuple *types.Tuple, count int) bool {
		if tuple.Len() != count {
			return false
		}
		for index := range count {
			if !types.Identical(tuple.At(index).Type(), types.Typ[types.Int]) {
				return false
			}
		}
		return true
	}
	length, at := method("Len"), method("At")
	if length == nil || at == nil || length.Params().Len() != 0 || !integer(length.Results(), 1) ||
		!integer(at.Params(), 1) || at.Results().Len() != 1 {
		return nil
	}
	if rest {
		sub := method("Sub")
		if sub == nil || !integer(sub.Params(), 2) || sub.Results().Len() != 1 || !types.Identical(sub.Results().At(0).Type(), input) {
			return nil
		}
	}
	return at.Results().At(0).Type()
}

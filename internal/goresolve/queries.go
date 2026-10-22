package goresolve

import (
	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
	"github.com/kojah/lockdoc/tools/hyrule/internal/valueir"
	"go/types"
	"strings"
)

func (e *resolver) checkDecision(x *semantic.Expr, union *boundUnion) error {
	seen := map[string]struct{}{}
	for _, arm := range x.Arms {
		pattern := arm.Pattern
		variant := e.constructors[pattern.Name]
		if pattern.Kind != semantic.Call || pattern.Bind != "" || variant == nil || variant.union != union {
			return e.fail(pattern, "decision arm must name a variant of "+union.declaration.Name)
		}
		if _, exists := seen[pattern.Name]; exists {
			return e.fail(pattern, "duplicate decision variant "+pattern.Name)
		}
		seen[pattern.Name] = struct{}{}
		if _, _, _, err := e.matcher(pattern, union.typ); err != nil {
			return err
		}
		names := map[string]struct{}{}
		if x.Name != "" {
			names[x.Name] = struct{}{}
		}
		for name := range e.captures {
			names[name] = struct{}{}
		}
		for _, arg := range pattern.Args {
			if arg.Kind == semantic.Wildcard {
				continue
			}
			_, exists := names[arg.Name]
			if arg.Kind != semantic.Variable || exists || strings.Contains(arg.Name, ".") {
				return e.fail(arg, "decision fields require fresh captures or wildcards; refinements are not exhaustive")
			}
			names[arg.Name] = struct{}{}
		}
	}
	var missing []string
	for _, variant := range union.variants {
		if _, exists := seen[variant.declaration.Name]; !exists {
			missing = append(missing, variant.declaration.Name)
		}
	}
	if len(missing) != 0 && x.Kind != semantic.Selection {
		return e.fail(x, "non-exhaustive query decision: missing "+strings.Join(missing, ", "))
	}
	if x.Kind == semantic.Selection && len(x.Arms) > 1 {
		return e.checkSelectionCaptures(x, union)
	}
	return nil
}

// A selected OR expands into switch arms after one query. It never expands the
// query itself, so state and budget charges occur once for the whole selection.
func selectionOutcomes(x *semantic.Expr) *semantic.Expr {
	if x.Kind != semantic.Selection || len(x.Arms) != 1 {
		return x
	}
	pattern := x.Arms[0].Pattern
	if pattern.Kind != semantic.Alternation || pattern.Bind != "" || pattern.Field != "" {
		return x
	}
	copy := *x
	copy.Arms = nil
	for _, pattern := range x.Arms[0].Pattern.Args {
		copy.Arms = append(copy.Arms, semantic.DecisionArm{Pattern: pattern, Result: x.Arms[0].Result})
	}
	return &copy
}

// Every selected variant exposes the same typed scope to the shared result.
// Field names and positions may differ; capture names and Go types must agree.
func (e *resolver) checkSelectionCaptures(x *semantic.Expr, union *boundUnion) error {
	var common map[string]types.Type
	for _, arm := range x.Arms {
		pattern := arm.Pattern
		variant := e.constructors[pattern.Name]
		_, _, indices, err := e.matcher(pattern, union.typ)
		if err != nil {
			return err
		}
		captures := map[string]types.Type{}
		for i, arg := range pattern.Args {
			if arg.Kind == semantic.Variable {
				captures[arg.Name] = variant.fields[indices[i]].Type()
			}
		}
		if common == nil {
			common = captures
			continue
		}
		compatible := len(common) == len(captures)
		for name, typ := range common {
			other, found := captures[name]
			if !found || !types.Identical(typ, other) {
				compatible = false
			}
		}
		if !compatible {
			return e.fail(pattern, "selected alternatives must bind the same captures with identical types")
		}
	}
	return nil
}

func (e *resolver) resolveQuery(x *semantic.Expr, expected types.Type, inChoice bool) (*valueir.Result, error) {
	outer := e.captures
	e.captures = cloneCaptures(outer)
	defer func() { e.captures = outer }()
	if _, exists := e.captures[x.Name]; x.Name != "" && exists {
		return nil, e.fail(x, "query capture shadows $"+x.Name)
	}
	q, typ, err := e.resolveQueryCall(x)
	if err != nil {
		return nil, err
	}
	union := e.unionFor(typ)
	if union != nil {
		q.Union = union.schema
	}
	result := &valueir.Result{Kind: valueir.Query, Type: e.tables.typeID(expected), Query: q}
	if x.BindResult {
		if len(x.Arms) != 1 {
			return nil, e.fail(x, "whole-result query binding cannot have else")
		}
		return e.resolveWholeQuery(x, expected, inChoice, result, typ)
	}

	boolean := q.TupleSize == 1 && types.Identical(typ.Underlying(), types.Typ[types.Bool])
	x, err = e.expandOutcomePatterns(x, union, boolean)
	if err != nil {
		return nil, err
	}
	if boolean {
		if err = e.checkBooleanDecision(x); err != nil {
			return nil, err
		}
	} else {
		if union == nil {
			return nil, e.fail(x.Args[0], "query result must have a declared union")
		}
		if err = e.checkDecision(x, union); err != nil {
			return nil, err
		}
	}
	if x.Name != "" {
		e.captures[x.Name] = capture{q.Capture, typ}
	}
	if err := e.resolveOutcomes(x, q, typ, expected, inChoice, boolean); err != nil {
		return nil, err
	}
	return result, nil
}

func (e *resolver) checkBooleanDecision(x *semantic.Expr) error {
	seen := map[string]bool{}
	for _, arm := range x.Arms {
		p := arm.Pattern
		if p.Kind != semantic.Literal || (p.Name != "true" && p.Name != "false") || p.Bind != "" || p.Field != "" {
			return e.fail(p, "Boolean decision arms require true and false")
		}
		if seen[p.Name] {
			return e.fail(p, "duplicate Boolean decision arm "+p.Name)
		}
		seen[p.Name] = true
	}
	if x.Kind != semantic.Selection && (!seen["true"] || !seen["false"]) {
		return e.fail(x, "non-exhaustive Boolean query decision: require true and false")
	}
	return nil
}

// resolveQueryCall fixes the host call signature, positional inputs and result
// identity before any outcome introduces payload captures.
func (e *resolver) resolveQueryCall(x *semantic.Expr) (*valueir.QueryPlan, types.Type, error) {
	call := x.Args[0]
	if call.Kind != semantic.Call || call.Bind != "" {
		return nil, nil, e.fail(call, "query requires a query hook call")
	}
	expanded, err := e.forwardingCall(call, "query")
	if err != nil {
		return nil, nil, err
	}
	var name string
	var signature *types.Signature
	if expanded != call {
		call = expanded
		name, signature, err = e.host.Hook(call.Name)
	} else {
		name, signature, err = e.binding(call, "query")
	}
	if err != nil {
		return nil, nil, err
	}
	if signature.Results().Len() < 1 || signature.Params().Len() != len(call.Args) {
		return nil, nil, e.fail(call, "query requires results and one parameter per argument")
	}
	typ := signature.Results().At(0).Type()
	if signature.Results().Len() > 1 {
		typ = hostapi.Tuple(e.host.Package, signature.Results())
	}
	q := &valueir.QueryPlan{Hook: e.tables.hookID(name), Type: e.tables.typeID(typ), TupleSize: signature.Results().Len()}
	for i, arg := range call.Args {
		if arg.Field != "" || arg.Bind != "" || (arg.Kind != semantic.Variable && arg.Kind != semantic.Literal && arg.Kind != semantic.Symbol) {
			return nil, nil, e.fail(arg, "query arguments must be positional captures, constants or literals")
		}
		value, err := e.resolveValue(arg, "build", signature.Params().At(i).Type())
		if err != nil {
			return nil, nil, err
		}
		q.Arguments = append(q.Arguments, valueir.Argument{Value: value, Type: e.tables.typeID(signature.Params().At(i).Type()), Position: i, Materialize: true})
	}
	e.serial++
	q.Capture = matchir.ValueID(e.serial)
	return q, typ, nil
}

func (e *resolver) resolveWholeQuery(x *semantic.Expr, expected types.Type, inChoice bool, result *valueir.Result, typ types.Type) (*valueir.Result, error) {
	q := result.Query
	var err error
	pattern := x.Arms[0].Pattern
	if _, exists := e.captures[pattern.Name]; exists || strings.Contains(pattern.Name, ".") || pattern.Bind != "" || pattern.Field != "" {
		return nil, e.fail(pattern, "whole-result query binding requires a fresh capture")
	}
	e.captures[pattern.Name] = capture{q.Capture, typ}
	q.Next, err = e.resolveResult(x.Arms[0].Result, expected, inChoice)
	return result, err
}

// OR arms share one continuation, so every alternative must establish the same
// typed scope. Validate the group before expanding it into ordinary cases.
func (e *resolver) expandOutcomePatterns(x *semantic.Expr, union *boundUnion, boolean bool) (*semantic.Expr, error) {
	copy := *x
	copy.Arms = nil
	for _, arm := range x.Arms {
		if arm.Pattern.Kind != semantic.Alternation {
			copy.Arms = append(copy.Arms, arm)
			continue
		}
		group := *x
		group.Kind = semantic.Selection
		group.Arms = []semantic.DecisionArm{arm}
		expanded := selectionOutcomes(&group)
		var err error
		if boolean {
			err = e.checkBooleanDecision(expanded)
		} else if union != nil {
			err = e.checkDecision(expanded, union)
		} else {
			return nil, e.fail(arm.Pattern, "query result must have a declared union")
		}
		if err != nil {
			return nil, err
		}
		copy.Arms = append(copy.Arms, expanded.Arms...)
	}
	return &copy, nil
}

func (e *resolver) resolveOutcomes(x *semantic.Expr, q *valueir.QueryPlan, typ, expected types.Type, inChoice, boolean bool) error {
	var err error
	base := e.captures
	for _, arm := range x.Arms {
		e.captures = cloneCaptures(base)
		outcome := valueir.Outcome{Boolean: arm.Pattern.Name == "true"}
		if !boolean {
			variant := e.constructors[arm.Pattern.Name]
			outcome.Variant = variant.id
			_, _, indices, err := e.matcher(arm.Pattern, typ)
			if err != nil {
				return err
			}
			for i, arg := range arm.Pattern.Args {
				if arg.Kind == semantic.Wildcard {
					continue
				}
				e.serial++
				id := matchir.ValueID(e.serial)
				field := variant.fields[indices[i]]
				e.captures[arg.Name] = capture{id, field.Type()}
				outcome.Captures = append(outcome.Captures, valueir.FieldCapture{Field: matchir.FieldID(indices[i]), Value: id})
			}
		}
		outcome.Result, err = e.resolveResult(arm.Result, expected, inChoice)
		if err != nil {
			return err
		}
		// A declining outcome leaves the choice unanswered and the output zero.
		// The query tag has already been validated, so no switch arm is needed.
		if outcome.Result.Kind != valueir.Decline {
			q.Cases = append(q.Cases, outcome)
		}
	}
	return nil
}

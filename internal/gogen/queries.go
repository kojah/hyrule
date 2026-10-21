package gogen

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// Queries are committed construction continuations, never matching predicates.
// They cannot be shared with another rule or retried by its failure edges. Each
// reached occurrence executes once. Ordinary decisions are exhaustive; a select
// leaves other valid outcomes unanswered within its ordered alternatives.
func (e *emitter) queryValue(x *syntax.Expr, expected types.Type) (string, error) {
	return e.queryResult(x, expected, "")
}

func (e *emitter) queryResult(x *syntax.Expr, expected types.Type, answered string) (string, error) {
	x = selectionOutcomes(x)
	outer := e.captures
	e.captures = cloneCaptures(outer)
	defer func() { e.captures = outer }()
	if _, exists := e.captures[x.Name]; x.Name != "" && exists {
		return "", e.fail(x, "query capture shadows $"+x.Name)
	}
	call := x.Args[0]
	if call.Kind != syntax.Call || call.Bind != "" {
		return "", e.fail(call, "query requires a query hook call")
	}
	expanded, err := e.forwardingCall(call, "query")
	if err != nil {
		return "", err
	}
	var name string
	var sig *types.Signature
	if expanded != call {
		call = expanded
		name, sig, err = e.host.Hook(call.Name)
	} else {
		name, sig, err = e.binding(call, "query")
	}
	if err != nil {
		return "", err
	}
	if sig.Results().Len() < 1 || sig.Params().Len() != len(call.Args) {
		return "", e.fail(call, "query requires results and one parameter per argument")
	}
	if x.Sequential && x.Arms[0].Pattern.Kind == syntax.Variable {
		return e.bindQueryResult(x, call, name, sig, expected, answered)
	}
	if sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type().Underlying(), types.Typ[types.Bool]) {
		return e.booleanQuery(x, call, name, sig, expected, answered)
	}
	resultType := sig.Results().At(0).Type()
	if sig.Results().Len() > 1 {
		resultType = hostapi.Tuple(e.host.Package, sig.Results())
	}
	union := e.unionFor(resultType)
	if union == nil {
		return "", e.fail(call, "query result must have a declared union")
	}
	if err := e.checkDecision(x, union); err != nil {
		return "", err
	}
	arguments, err := e.queryArguments(call, sig)
	if err != nil {
		return "", err
	}
	id := e.emitQueryCall(name, sig, arguments, resultType)
	result := valueName(id)
	// An anonymous query introduces no source-level capture. In particular,
	// nested shorthand queries cannot collide through a synthetic name.
	if x.Name != "" {
		e.captures[x.Name] = capture{id, union.typ}
	}
	fmt.Fprintf(&e.out, "if !%s(%s) { var zero %s; return zero,false }\n", e.validator(union), result, e.host.TypeName(sigOutput(e)))
	e.serial++
	output := fmt.Sprintf("hv%d", e.serial)
	fmt.Fprintf(&e.out, "var %s %s\nswitch %s.%s {\n", output, e.host.TypeName(expected), result, union.tag.Name())
	base := e.captures
	for _, arm := range x.Arms {
		e.captures = cloneCaptures(base)
		variant := e.constructors[arm.Pattern.Name]
		fmt.Fprintf(&e.out, "case %s:\n", variant.tag.Name())
		if err := e.bindOutcome(arm.Pattern, variant, result); err != nil {
			return "", err
		}
		value, err := e.answerValue(arm.Result, expected, answered)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&e.out, "%s = %s\n", output, value)
	}
	e.out.WriteString("}\n")
	return output, nil
}

// A whole-result binding consumes every result without choosing a success tag.
// Declared union values still cross the same validation boundary as decisions.
func (e *emitter) bindQueryResult(
	x, call *syntax.Expr, name string, sig *types.Signature, expected types.Type, answered string,
) (string, error) {
	pattern := x.Arms[0].Pattern
	if _, exists := e.captures[pattern.Name]; exists || strings.Contains(pattern.Name, ".") || pattern.Bind != "" || pattern.Field != "" {
		return "", e.fail(pattern, "whole-result query binding requires a fresh capture")
	}
	resultType := sig.Results().At(0).Type()
	if sig.Results().Len() > 1 {
		resultType = hostapi.Tuple(e.host.Package, sig.Results())
	}
	arguments, err := e.queryArguments(call, sig)
	if err != nil {
		return "", err
	}
	id := e.emitQueryCall(name, sig, arguments, resultType)
	fmt.Fprintf(&e.out, "_ = %s\n", valueName(id))
	if union := e.unionFor(resultType); union != nil {
		fmt.Fprintf(&e.out, "if !%s(%s) { var zero %s; return zero,false }\n", e.validator(union), valueName(id), e.host.TypeName(sigOutput(e)))
	}
	e.captures[pattern.Name] = capture{id, resultType}
	return e.answerValue(x.Arms[0].Result, expected, answered)
}

func (e *emitter) emitQueryCall(name string, sig *types.Signature, arguments []string, resultType types.Type) matchir.ValueID {
	e.serial++
	id := matchir.ValueID(e.serial)
	result := valueName(id)
	if sig.Results().Len() == 1 {
		fmt.Fprintf(&e.out, "%s := %s(%s)\n", result, name, strings.Join(arguments, ","))
		return id
	}
	vars, fields := []string{}, []string{}
	for i := 0; i < sig.Results().Len(); i++ {
		v := fmt.Sprintf("%spart%d", result, i)
		vars = append(vars, v)
		fields = append(fields, fmt.Sprintf("V%d:%s", i, v))
	}
	fmt.Fprintf(&e.out, "%s := %s(%s)\n%s := %s{%s}\n",
		strings.Join(vars, ","), name, strings.Join(arguments, ","), result, e.host.TypeName(resultType), strings.Join(fields, ","))
	return id
}

func (e *emitter) queryArguments(call *syntax.Expr, sig *types.Signature) ([]string, error) {
	arguments := make([]string, len(call.Args))
	for i, arg := range call.Args {
		if arg.Field != "" || arg.Bind != "" || (arg.Kind != syntax.Variable && arg.Kind != syntax.Literal && arg.Kind != syntax.Symbol) {
			return nil, e.fail(arg, "query arguments must be positional captures, constants or literals")
		}
		value, err := e.value(arg, "build")
		if err != nil {
			return nil, err
		}
		e.serial++
		arguments[i] = fmt.Sprintf("hv%d", e.serial)
		fmt.Fprintf(&e.out, "var %s %s = %s\n", arguments[i], e.host.TypeName(sig.Params().At(i).Type()), value)
	}
	return arguments, nil
}

// Each arm handles a complete variant, and payloads stay inside that arm.
// Ordinary decisions require all variants. A selection deliberately handles chosen
// variants, but cannot refine them or turn captures into equality tests.
func (e *emitter) checkDecision(x *syntax.Expr, union *boundUnion) error {
	seen := map[string]struct{}{}
	for _, arm := range x.Arms {
		pattern := arm.Pattern
		variant := e.constructors[pattern.Name]
		if pattern.Kind != syntax.Call || pattern.Bind != "" || variant == nil || variant.union != union {
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
			if arg.Kind == syntax.Wildcard {
				continue
			}
			_, exists := names[arg.Name]
			if arg.Kind != syntax.Variable || exists || strings.Contains(arg.Name, ".") {
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
	if len(missing) != 0 && x.Kind != syntax.Selection {
		return e.fail(x, "non-exhaustive query decision: missing "+strings.Join(missing, ", "))
	}
	if x.Kind == syntax.Selection && len(x.Arms) > 1 {
		return e.checkSelectionCaptures(x, union)
	}
	return nil
}

// A selected OR expands into switch arms after one query. It never expands the
// query itself, so state and budget charges occur once for the whole selection.
func selectionOutcomes(x *syntax.Expr) *syntax.Expr {
	if x.Kind != syntax.Selection || len(x.Arms) != 1 {
		return x
	}
	pattern := x.Arms[0].Pattern
	if pattern.Kind != syntax.Alternation || pattern.Bind != "" || pattern.Field != "" {
		return x
	}
	copy := *x
	copy.Arms = nil
	for _, pattern := range x.Arms[0].Pattern.Args {
		copy.Arms = append(copy.Arms, syntax.DecisionArm{Pattern: pattern, Result: x.Arms[0].Result})
	}
	return &copy
}

// Every selected variant exposes the same typed scope to the shared result.
// Field names and positions may differ; capture names and Go types must agree.
func (e *emitter) checkSelectionCaptures(x *syntax.Expr, union *boundUnion) error {
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
			if arg.Kind == syntax.Variable {
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

func (e *emitter) bindOutcome(pattern *syntax.Expr, variant *boundVariant, input string) error {
	_, _, indices, err := e.matcher(pattern, variant.union.typ)
	if err != nil {
		return err
	}
	for i, arg := range pattern.Args {
		if arg.Kind == syntax.Wildcard {
			continue
		}
		field := variant.fields[indices[i]]
		e.serial++
		id := matchir.ValueID(e.serial)
		fmt.Fprintf(&e.out, "%s := %s.%s\n_ = %s\n", valueName(id), input, field.Name(), valueName(id))
		e.captures[arg.Name] = capture{id, field.Type()}
	}
	return nil
}

// Boolean outcomes are closed by the Go type itself; no schema or tag aliases
// are needed. A named capture retains its exact (possibly named) Boolean type.
func (e *emitter) booleanQuery(x, call *syntax.Expr, name string, sig *types.Signature, expected types.Type, answered string) (string, error) {
	seen := map[string]bool{}
	for _, arm := range x.Arms {
		p := arm.Pattern
		if p.Kind != syntax.Literal || (p.Name != "true" && p.Name != "false") || p.Bind != "" || p.Field != "" {
			return "", e.fail(p, "Boolean decision arms require true and false")
		}
		if seen[p.Name] {
			return "", e.fail(p, "duplicate Boolean decision arm "+p.Name)
		}
		seen[p.Name] = true
	}
	if x.Kind != syntax.Selection && (!seen["true"] || !seen["false"]) {
		return "", e.fail(x, "non-exhaustive Boolean query decision: require true and false")
	}
	args, err := e.queryArguments(call, sig)
	if err != nil {
		return "", err
	}
	e.serial++
	id := matchir.ValueID(e.serial)
	result := valueName(id)
	fmt.Fprintf(&e.out, "%s := %s(%s)\n", result, name, strings.Join(args, ","))
	if x.Name != "" {
		e.captures[x.Name] = capture{id, sig.Results().At(0).Type()}
	}
	e.serial++
	output := fmt.Sprintf("hv%d", e.serial)
	fmt.Fprintf(&e.out, "var %s %s\nswitch %s {\n", output, e.host.TypeName(expected), result)
	base := e.captures
	for _, arm := range x.Arms {
		e.captures = cloneCaptures(base)
		fmt.Fprintf(&e.out, "case %s:\n", arm.Pattern.Name)
		value, err := e.answerValue(arm.Result, expected, answered)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&e.out, "%s = %s\n", output, value)
	}
	e.out.WriteString("}\n")
	return output, nil
}

// Ordered alternatives keep their own answer flag, never a matching failure
// edge. Each query and its result construction stay inside a lazy branch. The
// final pure answer makes the continuation total without manufacturing Option
// wrappers or letting an unanswered query retry a different rule.
func (e *emitter) alternativesValue(x *syntax.Expr, expected types.Type) (string, error) {
	outer := e.captures
	defer func() { e.captures = outer }()
	e.serial++
	output := fmt.Sprintf("hv%d", e.serial)
	answered := output + "answered"
	fmt.Fprintf(&e.out, "var %s %s\n%s := false\n", output, e.host.TypeName(expected), answered)
	for i, alternative := range x.Args {
		e.captures = cloneCaptures(outer)
		last := i == len(x.Args)-1
		if last && alternative.Kind != syntax.Pure {
			return "", e.fail(alternative, "ordered alternatives require a final pure fallback")
		}
		fmt.Fprintf(&e.out, "if !%s {\n", answered)
		if alternative.Kind == syntax.Pure {
			if !last {
				return "", e.fail(alternative, "pure must be the final ordered alternative")
			}
			value, err := e.resultValue(alternative, expected)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&e.out, "%s = %s\n", output, value)
		} else {
			if alternative.Kind != syntax.Selection && alternative.Kind != syntax.Do {
				return "", e.fail(alternative, "ordered alternative requires query ... select, do, or a final pure fallback")
			}
			var value string
			var err error
			if alternative.Kind == syntax.Selection {
				value, err = e.queryResult(alternative, expected, answered)
			} else {
				value, err = e.answerValue(alternative, expected, answered)
			}
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&e.out, "if %s { %s = %s }\n", answered, output, value)
		}
		e.out.WriteString("}\n")
	}
	return output, nil
}

// An answer flag belongs to the nearest ordered choice. Nested queries propagate
// it without declaring success until a terminal result is constructed. Decline
// leaves it false; effects already performed by queries are never rolled back.
func (e *emitter) answerValue(x *syntax.Expr, expected types.Type, answered string) (string, error) {
	if answered == "" {
		return e.resultValue(x, expected)
	}
	switch x.Kind {
	case syntax.Do:
		return e.answerValue(x.Args[0], expected, answered)
	case syntax.Query:
		return e.queryResult(x, expected, answered)
	case syntax.Decline:
		e.serial++
		zero := fmt.Sprintf("hv%d", e.serial)
		fmt.Fprintf(&e.out, "var %s %s\n", zero, e.host.TypeName(expected))
		return zero, nil
	default:
		value, err := e.resultValue(x, expected)
		if err == nil {
			fmt.Fprintf(&e.out, "%s = true\n", answered)
		}
		return value, err
	}
}

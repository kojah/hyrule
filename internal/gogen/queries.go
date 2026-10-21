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
// reached occurrence executes once, followed by one exhaustive outcome branch.
func (e *emitter) queryValue(x *syntax.Expr, expected types.Type) (string, error) {
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
	if sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type().Underlying(), types.Typ[types.Bool]) {
		return e.booleanQuery(x, call, name, sig, expected)
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
	e.serial++
	id := matchir.ValueID(e.serial)
	result := valueName(id)
	// An anonymous query introduces no source-level capture. In particular,
	// nested shorthand queries cannot collide through a synthetic name.
	if x.Name != "" {
		e.captures[x.Name] = capture{id, union.typ}
	}
	if sig.Results().Len() == 1 {
		fmt.Fprintf(&e.out, "%s := %s(%s)\n", result, name, strings.Join(arguments, ","))
	} else {
		vars, fields := []string{}, []string{}
		for i := 0; i < sig.Results().Len(); i++ {
			v := fmt.Sprintf("%spart%d", result, i)
			vars = append(vars, v)
			fields = append(fields, fmt.Sprintf("V%d:%s", i, v))
		}
		fmt.Fprintf(&e.out, "%s := %s(%s)\n%s := %s{%s}\n",
			strings.Join(vars, ","), name, strings.Join(arguments, ","), result, e.host.TypeName(resultType), strings.Join(fields, ","))
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
		value, err := e.resultValue(arm.Result, expected)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&e.out, "%s = %s\n", output, value)
	}
	e.out.WriteString("}\n")
	return output, nil
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

// Each variant has exactly one unrefined arm. No wildcard can conceal a future
// outcome, and repeated/outer captures cannot turn a total arm into an equality
// test. Payloads are available only inside the selected arm.
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
	if len(missing) != 0 {
		return e.fail(x, "non-exhaustive query decision: missing "+strings.Join(missing, ", "))
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
func (e *emitter) booleanQuery(x, call *syntax.Expr, name string, sig *types.Signature, expected types.Type) (string, error) {
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
	if !seen["true"] || !seen["false"] {
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
		value, err := e.resultValue(arm.Result, expected)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&e.out, "%s = %s\n", output, value)
	}
	e.out.WriteString("}\n")
	return output, nil
}

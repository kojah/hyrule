package frontend

import (
	"fmt"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// functionFiles lowers function parameters and arm subjects to the existing
// ordered-rule boundary. Syntax trees remain unchanged for formatting.
func functionFiles(files []*syntax.File) ([]*syntax.File, error) {
	var shared *syntax.API
	for _, f := range files {
		for _, item := range f.Items {
			if item.Function != nil && item.Function.API != nil {
				shared = item.Function.API
			}
		}
	}
	result := make([]*syntax.File, len(files))
	for i, file := range files {
		out := &syntax.File{Trailing: file.Trailing}
		result[i] = out
		for _, item := range file.Items {
			if item.Function == nil {
				out.Items = append(out.Items, item)
				continue
			}
			fn := item.Function
			api := fn.API
			if api == nil {
				api = shared
			}
			if fn.API != nil {
				out.Items = append(out.Items, syntax.Item{API: fn.API})
			}
			native := api != nil && api.Mode != ""
			if fn.API != nil && fn.API.Mode == "total" && fn.API.Fallback == nil {
				return nil, &syntax.Diagnostic{Position: fn.Position, Message: "total fn requires a fallback"}
			}
			seen := map[string]bool{}
			for _, p := range fn.Parameters {
				if p.Name == "_" || seen[p.Name] {
					return nil, &syntax.Diagnostic{Position: fn.Position, Message: "fn parameters require unique names"}
				}
				seen[p.Name] = true
			}
			rules := fn.Rules
			if fn.Body != nil {
				rules = []*syntax.Rule{{Position: fn.Position, Name: fmt.Sprintf("%s_L%d", fn.Name, fn.Line), Family: fn.Name,
					Match: &syntax.Expr{Position: fn.Position, Kind: syntax.Wildcard}, Build: fn.Body}}
			}
			for _, source := range rules {
				rule := *source
				pattern, parameters, err := functionPattern(fn, source, native)
				if err != nil {
					return nil, err
				}
				rule.Match = pattern
				rule.Clauses = append(parameters, rule.Clauses...)
				out.Items = append(out.Items, syntax.Item{Rule: &rule})
			}
		}
	}
	return result, nil
}

func functionPattern(fn *syntax.Function, rule *syntax.Rule, native bool) (*syntax.Expr, []syntax.Clause, error) {
	pattern := rule.Match
	subject := fn.Subject
	if subject == "" && len(fn.Parameters) == 0 {
		subject = "inputs"
	}
	if subject == "" && len(fn.Parameters) > 0 {
		subject = fn.Parameters[0].Name
	}
	if !native {
		if len(fn.Parameters) == 0 && subject == "inputs" {
			return pattern, nil, nil
		}
		if len(fn.Parameters) != 1 || subject != fn.Parameters[0].Name {
			return nil, nil, &syntax.Diagnostic{Position: fn.Position, Message: "use partial fn or total fn for multiple explicit parameters"}
		}
		if !functionParameterUsed(rule, fn.Parameters[0].Name) {
			return pattern, nil, nil
		}
		return parameterPattern(pattern, fn.Parameters[0].Name), nil, nil
	}
	root := pattern
	if subject != "inputs" {
		root = &syntax.Expr{Position: pattern.Position, Kind: syntax.Call, Name: "inputs"}
		found := false
		for _, param := range fn.Parameters {
			child := &syntax.Expr{Position: pattern.Position, Kind: syntax.Wildcard}
			if param.Name == subject {
				child = pattern
				found = true
			}
			root.Args = append(root.Args, child)
		}
		if !found {
			return nil, nil, &syntax.Diagnostic{Position: fn.Position, Message: "match subject must name a parameter or inputs"}
		}
	}
	if len(fn.Parameters) == 0 {
		return root, nil, nil
	}
	// Bind parameter aliases from the native input product independently of
	// which named fields an arm matches. The host adapter resolves slot storage.
	const input = "#functionInputs"
	var parameters []syntax.Clause
	for i, param := range fn.Parameters {
		if !functionParameterUsed(rule, param.Name) {
			continue
		}
		parameters = append(parameters, syntax.Clause{Name: param.Name, Parameter: true,
			Value: &syntax.Expr{Position: fn.Position, Kind: syntax.InputParameter, ParameterIndex: i,
				Args: []*syntax.Expr{{Position: fn.Position, Kind: syntax.Variable, Name: input}}}})
	}
	if len(parameters) > 0 {
		root = parameterPattern(root, input)
	}
	return root, parameters, nil
}

func parameterPattern(pattern *syntax.Expr, name string) *syntax.Expr {
	if pattern.Bind == name || (pattern.Bare || pattern.Kind == syntax.Variable) && pattern.Name == name {
		return pattern
	}
	// A one-arm pattern group preserves an existing whole capture as well.
	return &syntax.Expr{Position: pattern.Position, Kind: syntax.Alternation, Bind: name, Field: pattern.Field, Args: []*syntax.Expr{pattern}}
}

// Parameter aliases are introduced only when used. In embedded Go, textual
// occurrence is conservative: the Go checker later removes unused arguments.
func functionParameterUsed(rule *syntax.Rule, name string) bool {
	var used func(*syntax.Expr) bool
	used = func(x *syntax.Expr) bool {
		if x == nil {
			return false
		}
		if x.Kind == syntax.InlineGo && strings.Contains(x.Name, name) {
			return true
		}
		if (x.Kind == syntax.Variable || x.Bare) && (x.Name == name || strings.HasPrefix(x.Name, name+".")) {
			return true
		}
		for _, a := range x.Args {
			if used(a) {
				return true
			}
		}
		for _, a := range x.Arms {
			if used(a.Result) {
				return true
			}
		}
		return false
	}
	if used(rule.Build) {
		return true
	}
	for _, c := range rule.Clauses {
		if used(c.Value) {
			return true
		}
	}
	for _, c := range rule.Where {
		if used(c.Value) {
			return true
		}
	}
	return false
}

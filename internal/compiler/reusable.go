package compiler

import (
	"iter"
	"sort"

	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// Reuse is bounded independently of OR variants: ordinary nested matchers can
// duplicate exponentially too. Limits apply before each expanded node is made.
const maxPatternNodes = 10000
const maxPatternDepth = 64

type patternArgument struct {
	expression *syntax.Expr
	scope      map[string]patternArgument
}

type patternExpansion struct {
	emitter   *Program
	remaining int
}

func (e *Program) reuseRules(rules iter.Seq[*syntax.Rule]) ([]*syntax.Rule, error) {
	names := make([]string, 0, len(e.patterns))
	for name := range e.patterns {
		names = append(names, name)
	}
	sort.Strings(names)
	checked := map[string]struct{}{}
	for _, name := range names {
		if err := e.validateDefinition(name, checked, map[string]struct{}{}); err != nil {
			return nil, err
		}
	}
	// Invalid unused declarations must not become dormant traps for later users.
	for _, name := range names {
		expansion := patternExpansion{emitter: e, remaining: maxPatternNodes}
		e.rule = name
		if _, err := expansion.expand(e.patterns[name].Body, nil, 0); err != nil {
			return nil, err
		}
	}
	var result []*syntax.Rule
	for rule := range rules {
		e.rule = rule.Name
		expansion := patternExpansion{emitter: e, remaining: maxPatternNodes}
		match, err := expansion.expand(rule.Match, nil, 0)
		if err != nil {
			return nil, err
		}
		copy := *rule
		copy.Match = match
		result = append(result, &copy)
	}
	return result, nil
}

func (expansion *patternExpansion) expand(x *syntax.Expr, scope map[string]patternArgument, depth int) (*syntax.Expr, error) {
	e := expansion.emitter
	if depth >= maxPatternDepth {
		return nil, e.fail(x, "expanded pattern nesting exceeds 64")
	}
	if expansion.remaining == 0 {
		return nil, e.fail(x, "pattern expansion exceeds 10000 nodes")
	}
	expansion.remaining--
	if x.Kind == syntax.Variable {
		if argument, ok := scope[x.Name]; ok {
			result, err := expansion.expand(argument.expression, argument.scope, depth+1)
			if err != nil {
				return nil, err
			}
			result.Field = x.Field
			return result, nil
		}
	}
	result, err := expansion.expandShape(x, scope, depth)
	if err != nil {
		return nil, err
	}
	return expansion.bindExpanded(x, result, scope, depth)
}

func (expansion *patternExpansion) expandShape(x *syntax.Expr, scope map[string]patternArgument, depth int) (*syntax.Expr, error) {
	e := expansion.emitter
	var result *syntax.Expr
	if definition := e.patterns[x.Name]; x.Kind == syntax.Call && definition != nil {
		if len(x.Args) != len(definition.Parameters) {
			return nil, e.fail(x, "reusable pattern arity mismatch for "+x.Name)
		}
		arguments := make(map[string]patternArgument, len(x.Args))
		for index, actual := range x.Args {
			if actual.Field != "" {
				return nil, e.fail(actual, "reusable pattern arguments are positional")
			}
			arguments[definition.Parameters[index]] = patternArgument{expression: actual, scope: scope}
		}
		var err error
		result, err = expansion.expand(definition.Body, arguments, depth+1)
		if err != nil {
			return nil, err
		}
		result.Field = x.Field
	} else {
		copy := *x
		copy.Bind = ""
		copy.Args = nil
		result = &copy
		for _, child := range x.Args {
			expanded, err := expansion.expand(child, scope, depth+1)
			if err != nil {
				return nil, err
			}
			result.Args = append(result.Args, expanded)
		}
	}
	return result, nil
}

func (expansion *patternExpansion) bindExpanded(x, result *syntax.Expr, scope map[string]patternArgument, depth int) (*syntax.Expr, error) {
	e := expansion.emitter
	if x.Bind == "" {
		return result, nil
	}
	name := x.Bind
	if actual, ok := scope[name]; ok {
		resolved, err := expansion.expand(actual.expression, actual.scope, depth+1)
		if err != nil {
			return nil, err
		}
		switch resolved.Kind {
		case syntax.Wildcard:
			return result, nil
		case syntax.Variable:
			name = resolved.Name
		default:
			return nil, e.fail(x, "whole-input pattern parameter requires a capture or wildcard")
		}
	}
	if result.Bind != "" {
		return &syntax.Expr{Position: x.Position, Kind: syntax.Alternation, Bind: name, Field: x.Field, Args: []*syntax.Expr{result}}, nil
	}
	result.Bind = name
	return result, nil
}

package goresolve

import (
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

func (e *resolver) decisions(rules []*semantic.Rule, input types.Type) (*matchir.Node, error) {
	union := e.unionFor(input)
	for _, declared := range e.unions {
		if declared.declaration.Exhaustive && declared != union {
			return nil, &semantic.Diagnostic{Position: declared.declaration.Position, Message: "exhaustive union must be the target input type"}
		}
	}
	if union != nil {
		if err := e.checkCoverage(union, rules); err != nil {
			return nil, err
		}
		dispatch := &matchir.Node{Kind: matchir.Dispatch, Union: union.schema, Input: 0, Fallback: &matchir.Node{Kind: matchir.Fail}}
		for _, variant := range union.variants {
			var selected []*semantic.Rule
			for _, rule := range rules {
				root := rootPattern(rule.Match)
				constructor := e.constructors[root.Name]
				if root.Kind == semantic.Call && constructor != nil && constructor.union == union && constructor != variant {
					continue
				}
				selected = append(selected, rule)
			}
			next, err := e.orderedDecisions(selected, input)
			if err != nil {
				return nil, err
			}
			dispatch.Cases = append(dispatch.Cases, matchir.VariantCase{Variant: variant.id, Next: next})
		}
		return dispatch, nil
	}
	return e.orderedDecisions(rules, input)
}

func rootPattern(expression *semantic.Expr) *semantic.Expr {
	for expression.Kind == semantic.Alternation && len(expression.Args) == 1 {
		expression = expression.Args[0]
	}
	return expression
}

func (e *resolver) checkCoverage(union *boundUnion, rules []*semantic.Rule) error {
	if !union.declaration.Exhaustive {
		return nil
	}
	covered := map[string]struct{}{}
	for _, rule := range rules {
		if len(rule.Clauses) != 0 {
			continue
		}
		root := rootPattern(rule.Match)
		if root.Kind == semantic.Wildcard || root.Kind == semantic.Variable {
			return nil
		}
		variant := e.constructors[root.Name]
		if root.Kind != semantic.Call || variant == nil || variant.union != union {
			continue
		}
		seen := map[string]struct{}{}
		if rule.Match.Bind != "" {
			seen[rule.Match.Bind] = struct{}{}
		}
		unrestricted := true
		for _, field := range root.Args {
			if field.Kind == semantic.Wildcard {
				continue
			}
			if field.Kind != semantic.Variable {
				unrestricted = false
				break
			}
			if _, repeated := seen[field.Name]; repeated {
				unrestricted = false
				break
			}
			seen[field.Name] = struct{}{}
		}
		if unrestricted {
			covered[variant.declaration.Name] = struct{}{}
		}
	}
	var missing []string
	for _, variant := range union.variants {
		if _, known := covered[variant.declaration.Name]; !known {
			missing = append(missing, variant.declaration.Name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	message := "non-exhaustive union " + union.declaration.Name + ": missing unconditional variants " + strings.Join(missing, ", ")
	return &semantic.Diagnostic{Position: union.declaration.Position, Message: message}
}

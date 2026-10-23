package goresolve

import (
	"go/types"

	"github.com/kojah/hyrule/internal/matchir"
	"github.com/kojah/hyrule/internal/semantic"
)

func (e *resolver) decisions(rules []*semantic.Rule, input types.Type) (*matchir.Node, error) {
	union := e.unionFor(input)
	for _, declared := range e.unions {
		if declared.declaration.Exhaustive && declared != union {
			return nil, &semantic.Diagnostic{Position: declared.declaration.Position, Message: "exhaustive union must be the target input type"}
		}
	}
	if union != nil {
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

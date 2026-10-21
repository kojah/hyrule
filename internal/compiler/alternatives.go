package compiler

import (
	"iter"

	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// maxAlternatives bounds code growth before allocating expanded pattern trees.
const maxAlternatives = 64

// expandRules gives OR the semantics of adjacent source rules: a failed guard
// retries the next alternative, and builders run only after all guards succeed.
func (e *Program) expandRules(rules iter.Seq[*syntax.Rule], check func(*syntax.Rule) error) ([]*syntax.Rule, error) {
	var expanded []*syntax.Rule
	for rule := range rules {
		e.rule = rule.Name
		if _, err := e.variantCount(rule.Match); err != nil {
			return nil, err
		}
		if err := check(rule); err != nil {
			return nil, err
		}
		for _, pattern := range expandPattern(rule.Match) {
			variant := *rule
			variant.Match = pattern
			expanded = append(expanded, &variant)
		}
	}
	return expanded, nil
}

func (e *Program) variantCount(x *syntax.Expr) (int, error) {
	count := 1
	if x.Kind == syntax.Alternation {
		count = 0
	}
	for _, child := range x.Args {
		n, err := e.variantCount(child)
		if err != nil {
			return 0, err
		}
		if x.Kind == syntax.Alternation {
			if count > maxAlternatives-n {
				return 0, e.fail(x, "or expansion exceeds 64 alternatives per rule")
			}
			count += n
		} else {
			if count > maxAlternatives/n {
				return 0, e.fail(x, "or expansion exceeds 64 alternatives per rule")
			}
			count *= n
		}
	}
	return count, nil
}

func expandPattern(x *syntax.Expr) []*syntax.Expr {
	if x.Kind == syntax.Alternation {
		var result []*syntax.Expr
		for _, child := range x.Args {
			for _, variant := range expandPattern(child) {
				copy := *variant
				// Preserve an outer whole-input capture without replacing an inner one.
				if x.Bind != "" && copy.Bind != "" {
					copy = syntax.Expr{Position: x.Position, Kind: syntax.Alternation, Bind: x.Bind, Args: []*syntax.Expr{variant}}
				} else if x.Bind != "" {
					copy.Bind = x.Bind
				}
				copy.Field = x.Field
				result = append(result, &copy)
			}
		}
		return result
	}
	copy := *x
	copy.Args = nil
	variants := []*syntax.Expr{&copy}
	for _, child := range x.Args {
		choices := expandPattern(child)
		next := make([]*syntax.Expr, 0, len(variants)*len(choices))
		for _, partial := range variants {
			for _, choice := range choices {
				composed := *partial
				composed.Args = append(append([]*syntax.Expr(nil), partial.Args...), choice)
				next = append(next, &composed)
			}
		}
		variants = next
	}
	return variants
}

package compiler

import (
	"iter"

	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

// maxAlternatives bounds code growth before allocating expanded pattern trees.
const maxAlternatives = 64

// expandRules gives OR the semantics of adjacent source rules: a failed guard
// retries the next alternative, and builders run only after all guards succeed.
func (e *Program) expandRules(rules iter.Seq[*semantic.Rule], check func(*semantic.Rule) error) ([]*semantic.Rule, error) {
	var expanded []*semantic.Rule
	for rule := range rules {
		e.rule = rule.Name
		if _, err := e.variantCount(rule.Match); err != nil {
			return nil, err
		}
		if err := check(rule); err != nil {
			return nil, err
		}
		for _, alternative := range e.expandPattern(rule.Match) {
			variant := *rule
			variant.Match = alternative.pattern
			variant.Choices = alternative.choices
			expanded = append(expanded, &variant)
		}
	}
	return expanded, nil
}

func (e *Program) variantCount(x *semantic.Expr) (int, error) {
	count := 1
	if x.Kind == semantic.Alternation {
		count = 0
	}
	for _, child := range x.Args {
		n, err := e.variantCount(child)
		if err != nil {
			return 0, err
		}
		if x.Kind == semantic.Alternation {
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

// alternative is one OR-free expansion and the written alternatives it takes.
type alternative struct {
	pattern *semantic.Expr
	choices []semantic.Position
}

func (e *Program) expandPattern(x *semantic.Expr) []alternative {
	if x.Kind == semantic.Alternation {
		var result []alternative
		written := e.written[x]
		for index, child := range x.Args {
			for _, variant := range e.expandPattern(child) {
				copy := *variant.pattern
				// Preserve an outer whole-input capture without replacing an inner one.
				if x.Bind != "" && copy.Bind != "" {
					copy = semantic.Expr{Position: x.Position, Kind: semantic.Alternation, Bind: x.Bind, Args: []*semantic.Expr{variant.pattern}}
				} else if x.Bind != "" {
					copy.Bind = x.Bind
				}
				copy.Field = x.Field
				choices := variant.choices
				if written != nil {
					choices = append([]semantic.Position{written[index]}, choices...)
				}
				result = append(result, alternative{&copy, choices})
			}
		}
		return result
	}
	copy := *x
	copy.Args = nil
	variants := []alternative{{pattern: &copy}}
	for _, child := range x.Args {
		choices := e.expandPattern(child)
		next := make([]alternative, 0, len(variants)*len(choices))
		for _, partial := range variants {
			for _, choice := range choices {
				composed := *partial.pattern
				composed.Args = append(append([]*semantic.Expr(nil), partial.pattern.Args...), choice.pattern)
				taken := append(append([]semantic.Position(nil), partial.choices...), choice.choices...)
				next = append(next, alternative{&composed, taken})
			}
		}
		variants = next
	}
	return variants
}

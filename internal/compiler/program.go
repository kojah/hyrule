// Package compiler owns language-independent rule ordering, reusable patterns
// and OR expansion. Target adapters resolve hooks and validate typed captures;
// matchir owns the resulting ordered decision graph and pure-prefix sharing.
package compiler

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
	"slices"
)

// Program holds ordered source rules and their binding declarations.
type Program struct {
	Bindings map[string]*syntax.Binding
	patterns map[string]*syntax.Pattern
	rules    []*syntax.Rule
	rule     string
}

// Collect validates names independently of any target-language hook discovery.
func Collect(files []*syntax.File) (*Program, error) {
	program := &Program{Bindings: map[string]*syntax.Binding{}, patterns: map[string]*syntax.Pattern{}}
	names := map[string]bool{}
	for _, file := range files {
		for _, item := range file.Items {
			if b := item.Binding; b != nil {
				if b.Name == "or" {
					return nil, &syntax.Diagnostic{Position: b.Position, Message: "or is reserved for alternatives"}
				}
				if program.Bindings[b.Name] != nil {
					return nil, &syntax.Diagnostic{Position: b.Position, Message: "duplicate binding " + b.Name}
				}
				program.Bindings[b.Name] = b
			} else if pattern := item.Pattern; pattern != nil {
				if program.patterns[pattern.Name] != nil {
					return nil, &syntax.Diagnostic{Position: pattern.Position, Message: "duplicate pattern " + pattern.Name}
				}
				program.patterns[pattern.Name] = pattern
			} else {
				if names[item.Rule.Name] {
					return nil, &syntax.Diagnostic{Position: item.Rule.Position, Message: "duplicate rule " + item.Rule.Name}
				}
				names[item.Rule.Name] = true
				program.rules = append(program.rules, item.Rule)
			}
		}
	}
	if len(program.rules) == 0 {
		return nil, fmt.Errorf("ruleset is empty")
	}
	return program, nil
}

// Expand checks reusable definitions, then preserves the priority of every
// expanded OR alternative. The adapter validates capture types before expansion.
func (p *Program) Expand(check func(*syntax.Rule) error) ([]*syntax.Rule, error) {
	rules, err := p.reuseRules(slices.Values(p.rules))
	if err != nil {
		return nil, err
	}
	return p.expandRules(slices.Values(rules), check)
}
func (p *Program) fail(x *syntax.Expr, message string) error {
	return &syntax.Diagnostic{Position: x.Position, Rule: p.rule, Message: message}
}

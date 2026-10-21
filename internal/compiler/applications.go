package compiler

import "github.com/kojah/lockdoc/tools/hyrule/internal/syntax"

// Resolve bare outer names only after all target declarations are available.
// Constants retain symbol semantics. Nested bare names never become calls:
// nested application requires explicit parentheses, even for nullary operations.
func (program *Program) resolveApplications() {
	for _, rule := range program.rules {
		program.resolveApplication(rule.Match)
		program.resolveApplication(rule.Build)
		for _, clause := range rule.Clauses {
			program.resolveApplication(clause.Value)
		}
	}
	for _, pattern := range program.patterns {
		program.resolveApplication(pattern.Body)
	}
}

func (program *Program) resolveApplication(expression *syntax.Expr) {
	if expression.Unparenthesized && expression.Kind == syntax.Symbol && program.operationName(expression.Name) {
		expression.Kind = syntax.Call
	}
	for _, child := range expression.Args {
		program.resolveApplication(child)
	}
	for _, arm := range expression.Arms {
		program.resolveApplication(arm.Pattern)
		program.resolveApplication(arm.Result)
	}
}

func (program *Program) operationName(name string) bool {
	if binding := program.Bindings[name]; binding != nil {
		return binding.Kind != "const"
	}
	return program.Constructors[name] != nil || program.RecordNames[name] != nil || program.patterns[name] != nil
}

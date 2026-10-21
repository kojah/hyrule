package gogen

import (
	"go/types"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// decisionTables belongs to the Go adapter. The shared IR knows only its opaque
// IDs, never these Go types, hook spellings, syntax expressions or capture maps.
type decisionTables struct {
	types       []types.Type
	hooks       []string
	literals    []string
	expressions []decisionExpression
}
type decisionExpression struct {
	rule       *syntax.Rule
	expression *syntax.Expr
	captures   map[string]capture
}

func (t *decisionTables) typeID(typ types.Type) matchir.TypeID {
	for index, existing := range t.types {
		if types.Identical(typ, existing) {
			return matchir.TypeID(index)
		}
	}
	t.types = append(t.types, typ)
	return matchir.TypeID(len(t.types) - 1)
}
func (t *decisionTables) hookID(name string) matchir.HookID {
	for index, existing := range t.hooks {
		if existing == name {
			return matchir.HookID(index)
		}
	}
	t.hooks = append(t.hooks, name)
	return matchir.HookID(len(t.hooks) - 1)
}
func (t *decisionTables) literalID(text string) matchir.LiteralID {
	for index, existing := range t.literals {
		if existing == text {
			return matchir.LiteralID(index)
		}
	}
	t.literals = append(t.literals, text)
	return matchir.LiteralID(len(t.literals) - 1)
}
func (e *emitter) expressionID(rule *syntax.Rule, expression *syntax.Expr) matchir.ExprID {
	e.tables.expressions = append(e.tables.expressions, decisionExpression{rule, expression, cloneCaptures(e.captures)})
	return matchir.ExprID(len(e.tables.expressions) - 1)
}

// decisions adapts resolved Go hook signatures to the neutral matching graph.
// The graph owns extraction identity, pure-prefix sharing and fallback edges.
func (e *emitter) decisions(rules []*syntax.Rule, input types.Type) (*matchir.Node, error) {
	e.graph = matchir.New(e.tables.typeID(input))
	for _, rule := range rules {
		e.rule = rule.Name
		e.captures = map[string]capture{}
		e.cursor = e.graph.Begin(matchir.Origin{Rule: rule.Name, File: rule.File, Line: rule.Line, Column: rule.Column})
		if err := e.pattern(rule.Match, e.graph.Input(), input); err != nil {
			return nil, err
		}
		for _, condition := range rule.Guards {
			if _, err := e.value(condition, "guard"); err != nil {
				return nil, err
			}
			e.cursor.Guard(e.expressionID(rule, condition))
		}
		if _, err := e.value(rule.Build, "build"); err != nil {
			return nil, err
		}
		e.cursor.Build(e.expressionID(rule, rule.Build))
	}
	e.cursor = nil
	return e.graph.Root(), nil
}

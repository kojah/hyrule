package goresolve

import (
	"github.com/kojah/lockdoc/tools/hyrule/internal/valueir"
	"go/types"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

// decisionTables belongs to the Go adapter. The shared IR knows only its opaque
// IDs, never these Go types, hook spellings, semantic expressions or capture maps.
type decisionTables struct {
	types       []types.Type
	members     []string
	hooks       []string
	literals    []string
	expressions []decisionExpression
	constants   []*boundConstant
}
type decisionExpression struct {
	value  *valueir.Value
	result *valueir.Result
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
func (e *resolver) storeValue(value *valueir.Value) matchir.ExprID {
	e.tables.expressions = append(e.tables.expressions, decisionExpression{value: value})
	return matchir.ExprID(len(e.tables.expressions) - 1)
}
func (e *resolver) storeResult(result *valueir.Result) matchir.ExprID {
	e.tables.expressions = append(e.tables.expressions, decisionExpression{result: result})
	return matchir.ExprID(len(e.tables.expressions) - 1)
}

// decisions adapts resolved Go hook signatures to the neutral matching graph.
// The graph owns extraction identity, pure-prefix sharing and fallback edges.
func (e *resolver) orderedDecisions(rules []*semantic.Rule, input types.Type) (*matchir.Node, error) {
	e.graph = matchir.New(e.tables.typeID(input))
	for _, rule := range rules {
		e.rule = rule.Name
		e.captures = map[string]capture{}
		e.cursor = e.graph.Begin(matchir.Origin{Rule: rule.Name, File: rule.File, Line: rule.Line, Column: rule.Column})
		if err := e.pattern(rule.Match, e.graph.Input(), input); err != nil {
			return nil, err
		}
		for _, clause := range rule.Clauses {
			if clause.Name != "" {
				if err := e.local(clause); err != nil {
					return nil, err
				}
				continue
			}
			condition, err := e.prepareValue(clause.Value, types.Typ[types.Bool])
			if err != nil {
				return nil, err
			}
			if condition.Kind == semantic.Comparison {
				if err := e.compare(condition); err != nil {
					return nil, err
				}
				continue
			}
			value, err := e.resolveValue(condition, "guard", types.Typ[types.Bool])
			if err != nil {
				return nil, err
			}
			e.cursor.Guard(e.storeValue(value))
		}
		prepared := *rule
		var err error
		prepared.Build, err = e.prepareValue(rule.Build, e.host.Signature.Results().At(0).Type())
		if err != nil {
			return nil, err
		}
		result, err := e.resolveResult(prepared.Build, sigOutput(e), false)
		if err != nil {
			return nil, err
		}
		e.cursor.Build(e.storeResult(result))
	}
	e.cursor = nil
	return e.graph.Root(), nil
}

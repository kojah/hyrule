package gogen

import (
	"fmt"
	"strings"

	"github.com/kojah/hyrule/internal/matchir"
)

// emitMatch scopes typed extractions to their successful matching continuation.
func (e *emitter) emitMatch(node *matchir.Node) error {
	if view := e.viewFor(node); view != nil {
		return e.emitViewMatch(node, view)
	}
	outputs := make([]string, 0, len(node.Outputs)+1)
	for _, output := range node.Outputs {
		name := "_"
		if e.used[output] {
			name = valueName(output)
		}
		outputs = append(outputs, name)
	}
	outputs = append(outputs, "hok")
	fmt.Fprintf(&e.out, "{\n%s := %s(%s)\n", strings.Join(outputs, ", "), e.tables.Hooks[node.Hook], valueName(node.Input))
	for _, output := range node.Outputs {
		if e.used[output] {
			fmt.Fprintf(&e.out, "_ = %s\n", valueName(output))
		}
	}
	e.out.WriteString("if hok {\n")
	if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
		return err
	}
	e.out.WriteString("}\n}\n")
	return nil
}

// emitGuard keeps its expression and ordered continuation in one lexical scope.
func (e *emitter) emitGuard(node *matchir.Node) error {
	e.out.WriteString("{\n")
	condition := e.decisionValue(node, "guard")
	fmt.Fprintf(&e.out, "if %s {\n", condition)
	if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
		return err
	}
	e.out.WriteString("}\n}\n")
	return nil
}

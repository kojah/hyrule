package gogen

import (
	"fmt"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
)

func valueName(value matchir.ValueID) string {
	if value == 0 {
		return "harg0"
	}
	return fmt.Sprintf("hv%d", value)
}
func (e *emitter) operand(operand matchir.Operand) string {
	if operand.Kind == matchir.LiteralOperand {
		return e.tables.literals[operand.Literal]
	}
	return valueName(operand.Value)
}
func (e *emitter) decisionValue(node *matchir.Node, kind string) (string, error) {
	expression := e.tables.expressions[node.Expression]
	e.rule = expression.rule.Name
	e.captures = expression.captures
	return e.value(expression.expression, kind)
}

// Structured Go branches stop at their common fallback. This backend owns all
// source spelling, calls and locals; the shared graph is never interpreted.
func (e *emitter) emitDecisions(node, stop *matchir.Node) error {
	for node != nil && node != stop && node.Kind != matchir.Fail {
		switch node.Kind {
		case matchir.Match:
			outputs := make([]string, 0, len(node.Outputs)+1)
			for _, output := range node.Outputs {
				outputs = append(outputs, valueName(output))
			}
			outputs = append(outputs, "hok")
			fmt.Fprintf(&e.out, "{\n%s := %s(%s)\n", strings.Join(outputs, ", "), e.tables.hooks[node.Hook], valueName(node.Input))
			for _, output := range node.Outputs {
				fmt.Fprintf(&e.out, "_ = %s\n", valueName(output))
			}
			e.out.WriteString("if hok {\n")
			if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
				return err
			}
			e.out.WriteString("}\n}\n")
		case matchir.Test:
			fmt.Fprintf(&e.out, "if %s == %s {\n", e.operand(node.Left), e.operand(node.Right))
			if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
				return err
			}
			e.out.WriteString("}\n")
		case matchir.Guard:
			e.out.WriteString("{\n")
			condition, err := e.decisionValue(node, "guard")
			if err != nil {
				return err
			}
			fmt.Fprintf(&e.out, "if %s {\n", condition)
			if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
				return err
			}
			e.out.WriteString("}\n}\n")
		case matchir.Build:
			return e.emitBuild(node, stop)
		}
		node = node.Fallback
	}
	return nil
}

func decisionReturns(node *matchir.Node) bool {
	for node != nil && node.Kind != matchir.Fail {
		if node.Kind == matchir.Build {
			return true
		}
		node = node.Fallback
	}
	return false
}

func (e *emitter) emitBuild(node, stop *matchir.Node) error {
	if node.Fallback != nil && node.Fallback != stop && node.Fallback.Kind != matchir.Fail {
		e.out.WriteString("if false { // Unreachable rules retain Go type validation.\n")
		if err := e.emitDecisions(node.Fallback, stop); err != nil {
			return err
		}
		e.out.WriteString("}\n")
	}
	fmt.Fprintf(&e.out, "// Rule %s (%s:%d).\n", node.Origin.Rule, node.Origin.File, node.Origin.Line)
	build, err := e.decisionValue(node, "build")
	if err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "return %s, true\n", build)
	return nil
}

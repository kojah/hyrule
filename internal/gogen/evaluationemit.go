package gogen

import (
	"fmt"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
)

// emitEvaluation scopes immutable locals to the ordered continuation. Arithmetic
// uses ordinary Go operators; no overflow check or failure branch is inserted.
func (e *emitter) emitEvaluation(node *matchir.Node) error {
	e.out.WriteString("{\n")
	value, err := e.evaluationValue(node.Evaluation)
	if err != nil {
		return err
	}
	name := valueName(node.Outputs[0])
	typ := e.tables.types[node.Evaluation.Type]
	fmt.Fprintf(&e.out, "var %s %s = %s\n_ = %s\n", name, e.host.typeName(typ), value, name)
	if validation := e.validation(typ, name); validation != "" {
		fmt.Fprintf(&e.out, "if !%s { var zero %s; return zero,false }\n", validation, e.host.typeName(e.host.Signature.Results().At(0).Type()))
	}
	if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
		return err
	}
	e.out.WriteString("}\n")
	return nil
}

func (e *emitter) evaluationValue(expression *matchir.Evaluation) (string, error) {
	switch expression.Kind {
	case matchir.OperandEvaluation:
		return e.operand(expression.Operand), nil
	case matchir.ForeignEvaluation:
		value := e.tables.expressions[expression.Expression]
		e.rule, e.captures = value.rule.Name, value.captures
		return e.valueExpected(value.expression, "build", e.tables.types[expression.Type])
	case matchir.ArithmeticEvaluation:
		operands := make([]string, len(expression.Arguments))
		for index, argument := range expression.Arguments {
			value, err := e.evaluationValue(argument)
			if err != nil {
				return "", err
			}
			e.serial++
			operands[index] = fmt.Sprintf("hv%d", e.serial)
			fmt.Fprintf(&e.out, "var %s %s = %s\n", operands[index], e.host.typeName(e.tables.types[expression.Type]), value)
		}
		operator := [...]string{"+", "-", "*", "/", "%"}[expression.Arithmetic]
		return "(" + operands[0] + " " + operator + " " + operands[1] + ")", nil
	default:
		return "", fmt.Errorf("unsupported evaluation kind %d", expression.Kind)
	}
}

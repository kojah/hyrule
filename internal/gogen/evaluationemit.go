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
	typ := e.tables.Types[node.Evaluation.Type]
	fmt.Fprintf(&e.out, "var %s %s = %s\n_ = %s\n", name, e.host.TypeName(typ), value, name)
	if validation := e.validation(typ, name); validation != "" {
		fmt.Fprintf(&e.out, "if !%s { var zero %s; return zero,false }\n", validation, e.host.TypeName(e.host.Signature.Results().At(0).Type()))
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
		value := e.tables.Expressions[expression.Expression]
		return e.emitValue(value.Value), nil
	case matchir.ArithmeticEvaluation:
		operands := make([]string, len(expression.Arguments))
		for index, argument := range expression.Arguments {
			value, err := e.evaluationValue(argument)
			if err != nil {
				return "", err
			}
			e.serial++
			operands[index] = fmt.Sprintf("hv%d", e.serial)
			fmt.Fprintf(&e.out, "var %s %s = %s\n", operands[index], e.host.TypeName(e.tables.Types[expression.Type]), value)
		}
		operator := [...]string{"+", "-", "*", "/", "%"}[expression.Arithmetic]
		return "(" + operands[0] + " " + operator + " " + operands[1] + ")", nil
	default:
		return "", fmt.Errorf("unsupported evaluation kind %d", expression.Kind)
	}
}

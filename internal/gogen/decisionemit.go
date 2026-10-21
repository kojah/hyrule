package gogen

import (
	"fmt"

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
	if operand.Kind == matchir.ConstantOperand {
		return e.tables.constants[operand.Constant].object.Name()
	}
	return valueName(operand.Value)
}
func (e *emitter) decisionValue(node *matchir.Node, kind string) (string, error) {
	expression := e.tables.expressions[node.Expression]
	e.rule = expression.rule.Name
	e.captures = expression.captures
	if kind == "build" {
		return e.resultValue(expression.expression, sigOutput(e))
	}
	return e.value(expression.expression, kind)
}

// Structured Go branches stop at their common fallback. This backend owns all
// source spelling, calls and locals; the shared graph is never interpreted.
func (e *emitter) emitDecisions(node, stop *matchir.Node) error {
	for node != nil && node != stop && node.Kind != matchir.Fail {
		switch node.Kind {
		case matchir.Dispatch:
			if err := e.emitDispatch(node); err != nil {
				return err
			}
		case matchir.Variant:
			terminal := e.variantReturns(node)
			if terminal {
				if err := e.emitDeadSuffix(node.Fallback, stop); err != nil {
					return err
				}
			}
			if err := e.emitVariant(node); err != nil {
				return err
			}
			if terminal {
				return nil
			}
		case matchir.Match:
			if err := e.emitMatch(node); err != nil {
				return err
			}
		case matchir.Record:
			if err := e.emitRecord(node); err != nil {
				return err
			}
		case matchir.Evaluate:
			if err := e.emitEvaluation(node); err != nil {
				return err
			}
		case matchir.Test:
			fmt.Fprintf(&e.out, "if %s == %s {\n", e.operand(node.Left), e.operand(node.Right))
			if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
				return err
			}
			e.out.WriteString("}\n")
		case matchir.Compare:
			fmt.Fprintf(&e.out, "if %s %s %s {\n", e.operand(node.Left), comparisonSpelling(node.Comparison), e.operand(node.Right))
			if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
				return err
			}
			e.out.WriteString("}\n")
		case matchir.Guard:
			if err := e.emitGuard(node); err != nil {
				return err
			}
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
	if err := e.emitDeadSuffix(node.Fallback, stop); err != nil {
		return err
	}
	fmt.Fprintf(&e.out, "// Rule %s (%s:%d).\n", node.Origin.Rule, node.Origin.File, node.Origin.Line)
	var build string
	var err error
	if node.Construction != nil {
		build, err = e.constructionValue(node.Construction)
	} else {
		build, err = e.decisionValue(node, "build")
	}
	if err != nil {
		return err
	}
	if validation := e.validation(e.host.Signature.Results().At(0).Type(), "hresult"); validation != "" {
		fmt.Fprintf(&e.out, "hresult := %s\nif !%s { var zero %s; return zero,false }\nreturn hresult,true\n",
			build, validation, e.host.typeName(e.host.Signature.Results().At(0).Type()))
	} else {
		fmt.Fprintf(&e.out, "return %s, true\n", build)
	}
	return nil
}

func (e *emitter) emitDeadSuffix(node, stop *matchir.Node) error {
	if node == nil || node == stop || node.Kind == matchir.Fail {
		return nil
	}
	e.out.WriteString("if false { // Unreachable rules retain Go type validation.\n")
	if err := e.emitDecisions(node, stop); err != nil {
		return err
	}
	e.out.WriteString("}\n")
	return nil
}

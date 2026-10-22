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
		return e.tables.Literals[operand.Literal]
	}
	if operand.Kind == matchir.ConstantOperand {
		return e.tables.Constants[operand.Constant].Name()
	}
	return valueName(operand.Value)
}
func (e *emitter) decisionValue(node *matchir.Node, kind string) string {
	expression := e.tables.Expressions[node.Expression]
	if kind == "build" {
		return e.emitResult(expression.Result)
	}
	return e.emitValue(expression.Value)
}

// Structured Go branches stop at their common fallback. This backend owns all
// source spelling, calls and locals; the shared graph is never interpreted.
func (e *emitter) emitDecisions(node, stop *matchir.Node) error {
	for node != nil && node != stop && node.Kind != matchir.Fail {
		terminal := node.Kind == matchir.Variant && e.variantReturns(node) ||
			(node.Kind == matchir.Record || node.Kind == matchir.Evaluate) && continuationReturns(node.Next, node.Fallback)
		if terminal {
			if err := e.emitDeadSuffix(node.Fallback, stop); err != nil {
				return err
			}
		}
		if err := e.emitDecision(node, stop); err != nil {
			return err
		}
		if node.Kind == matchir.Build {
			return nil
		}
		if terminal {
			return nil
		}
		node = node.Fallback
	}
	return nil
}

// Infallible extractions/evaluations enter their continuation unconditionally.
// Recognizing that terminal path avoids emitting unreachable trailing failure
// returns, while unreachable rules still receive Go type validation above it.
func decisionReturns(node *matchir.Node) bool { return continuationReturns(node, nil) }

func continuationReturns(node, stop *matchir.Node) bool {
	for node != nil && node != stop && node.Kind != matchir.Fail {
		if node.Kind == matchir.Build {
			return true
		}
		if (node.Kind == matchir.Record || node.Kind == matchir.Evaluate) && continuationReturns(node.Next, node.Fallback) {
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
	build := e.decisionValue(node, "build")
	if validation := e.validation(e.host.Signature.Results().At(0).Type(), "hresult"); validation != "" {
		fmt.Fprintf(&e.out, "hresult := %s\nif !%s { var zero %s; return zero,false }\nreturn hresult,true\n",
			build, validation, e.host.TypeName(e.host.Signature.Results().At(0).Type()))
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

func (e *emitter) emitRecord(node *matchir.Node) error {
	record := e.records[node.Record.ID]
	e.out.WriteString("{\n")
	for index, field := range record.Fields {
		fmt.Fprintf(&e.out, "%s := %s.%s\n_ = %s\n", valueName(node.Outputs[index]), valueName(node.Input), field.Name(), valueName(node.Outputs[index]))
	}
	if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
		return err
	}
	e.out.WriteString("}\n")
	return nil
}

// emitDecision handles one resolved operation; traversal and shared fallback
// placement stay in emitDecisions.
func (e *emitter) emitDecision(node, stop *matchir.Node) error {
	switch node.Kind {
	case matchir.Dispatch:
		if err := e.emitDispatch(node); err != nil {
			return err
		}
	case matchir.Variant:
		if err := e.emitVariant(node); err != nil {
			return err
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
	return nil
}

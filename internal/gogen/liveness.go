package gogen

import (
	"github.com/kojah/hyrule/internal/goplan"
	"github.com/kojah/hyrule/internal/matchir"
	"github.com/kojah/hyrule/internal/valueir"
)

// liveValues tracks read operands, not extraction outputs. Unused structural
// fields need no Go local; effectful calls and evaluations are still emitted.
func liveValues(root *matchir.Node, expressions []goplan.Expression) map[matchir.ValueID]bool {
	used := map[matchir.ValueID]bool{}
	operand := func(op matchir.Operand) {
		if op.Kind == matchir.ValueOperand {
			used[op.Value] = true
		}
	}
	var value func(*valueir.Value)
	value = func(v *valueir.Value) {
		if v == nil {
			return
		}
		if v.Kind == valueir.Operand {
			operand(v.Operand)
		}
		value(v.Base)
		for _, arg := range v.Arguments {
			value(arg.Value)
		}
	}
	var result func(*valueir.Result)
	result = func(r *valueir.Result) {
		if r == nil {
			return
		}
		value(r.Value)
		for _, alternative := range r.Alternatives {
			result(alternative)
		}
		if q := r.Query; q != nil {
			value(q.Pure)
			for _, arg := range q.Arguments {
				value(arg.Value)
			}
			result(q.Next)
			for _, arm := range q.Cases {
				result(arm.Result)
			}
		}
	}
	for _, expression := range expressions {
		value(expression.Value)
		result(expression.Result)
	}
	var evaluation func(*matchir.Evaluation)
	evaluation = func(v *matchir.Evaluation) {
		if v == nil {
			return
		}
		if v.Kind == matchir.OperandEvaluation {
			operand(v.Operand)
		}
		for _, arg := range v.Arguments {
			evaluation(arg)
		}
	}
	seen := map[*matchir.Node]bool{}
	var walk func(*matchir.Node)
	walk = func(n *matchir.Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		switch n.Kind {
		case matchir.Match, matchir.Record, matchir.Variant, matchir.Dispatch:
			used[n.Input] = true
		case matchir.Test, matchir.Compare:
			operand(n.Left)
			operand(n.Right)
		case matchir.Evaluate:
			evaluation(n.Evaluation)
		}
		walk(n.Next)
		walk(n.Fallback)
		for _, branch := range n.Cases {
			walk(branch.Next)
		}
	}
	walk(root)
	return used
}

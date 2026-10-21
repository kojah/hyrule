package syntax

import (
	"strings"
	"text/scanner"
)

// A do block retains its surface wrapper while each binding becomes an ordinary
// committed query. The first arm continues the block; else arms exit it. This
// keeps capture scoping and outcome validation on the existing compiler path.
func (p *parser) doExpression(depth int) *Expr {
	result := &Expr{Position: p.position, Kind: Do}
	p.expect("do")
	p.expect("{")
	var steps []*Expr
	for p.text != "pure" && p.text != "}" && p.token != scanner.EOF && p.failure == nil {
		if depth+len(steps)+1 >= maxDepth {
			p.fail(p.position, "do sequencing exceeds nesting limit")
			break
		}
		pattern := p.outerExpression(depth + len(steps) + 1)
		p.expect("<")
		p.expect("-")
		position := p.position
		p.expect("query")
		call := p.outerExpression(depth + len(steps) + 1)
		step := &Expr{Position: position, Kind: Query, Args: []*Expr{call}, Sequential: true,
			Arms: []DecisionArm{{Pattern: pattern}}}
		if p.text == "else" {
			if pattern.Kind == Variable {
				p.fail(pattern.Position, "whole-result query binding cannot have else")
			}
			p.next()
			p.doElse(step, depth+len(steps)+1)
		}
		p.expect(";")
		steps = append(steps, step)
	}
	if p.text != "pure" {
		p.fail(p.position, "do requires a final pure result")
	}
	terminal := p.resultOperand(depth + len(steps) + 1)
	p.expect(";")
	p.expect("}")
	for i := len(steps) - 1; i >= 0; i-- {
		steps[i].Arms[0].Result = terminal
		terminal = steps[i]
	}
	result.Args = []*Expr{terminal}
	return result
}

// Expression else is deliberately Boolean-only: the complementary literal is
// explicit after lowering, so tagged domains retain named, exhaustive arms.
func (p *parser) doElse(step *Expr, depth int) {
	if p.text != "{" {
		pattern := step.Arms[0].Pattern
		if pattern.Kind != Literal || (pattern.Name != "true" && pattern.Name != "false") || pattern.Bind != "" {
			p.fail(pattern.Position, "single-expression else requires a true or false do binding")
			return
		}
		other := "true"
		if pattern.Name == "true" {
			other = "false"
		}
		step.ExpressionElse = true
		step.Arms = append(step.Arms, DecisionArm{
			Pattern: &Expr{Position: pattern.Position, Kind: Literal, Name: other},
			Result:  p.resultExpression(depth),
		})
		return
	}
	p.expect("{")
	for p.text != "}" && p.token != scanner.EOF && p.failure == nil {
		other := p.outerExpression(depth)
		p.expect("=")
		p.expect(">")
		answer := p.resultExpression(depth)
		p.expect(";")
		step.Arms = append(step.Arms, DecisionArm{Pattern: other, Result: answer})
	}
	p.expect("}")
}

func renderDo(expression *Expr, indent int) string {
	pad := strings.Repeat(" ", indent)
	var out strings.Builder
	out.WriteString("do {")
	step := expression.Args[0]
	for step.Kind == Query && step.Sequential {
		prefix := renderLayout(step.Arms[0].Pattern, indent+2) + " <- query "
		out.WriteString("\n" + pad + "  " + prefix + renderLayout(step.Args[0], indent+2+len(prefix)))
		if step.ExpressionElse {
			out.WriteString("\n" + pad + "    else " + renderLayout(step.Arms[1].Result, indent+9))
		} else if len(step.Arms) > 1 {
			out.WriteString("\n" + pad + "    else {")
			for _, arm := range step.Arms[1:] {
				out.WriteString("\n" + pad + "      " + render(arm.Pattern) + " => " + renderLayout(arm.Result, indent+6) + ";")
			}
			out.WriteString("\n" + pad + "    }")
		}
		out.WriteString(";")
		step = step.Arms[0].Result
	}
	out.WriteString("\n" + pad + "  " + renderLayout(step, indent+2) + ";\n" + pad + "}")
	return out.String()
}

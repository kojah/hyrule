package syntax

import (
	"strings"
	"text/scanner"
)

// Result expressions execute only after a rule commits. Queries own an explicit
// exhaustive decision, so an unanswered proof cannot silently retry another rule.
func (p *parser) effects(depth int) *Expr {
	position := p.position
	p.expect("effects")
	return p.effectsBody(position, depth)
}

func (p *parser) effectsBody(position Position, depth int) *Expr {
	result := &Expr{Position: position, Kind: Effects}
	p.expect("[")
	for p.text != "]" && p.token != scanner.EOF && p.failure == nil {
		result.Args = append(result.Args, p.expression(depth+1))
		if p.text != "]" {
			p.expect(",")
		}
	}
	p.expect("]")
	return result
}

func (p *parser) query(depth int) *Expr {
	position := p.position
	p.expect("query")
	return p.queryBody(position, depth)
}

func (p *parser) queryBody(position Position, depth int) *Expr {
	result := &Expr{Position: position, Kind: Query}
	p.expect("$")
	result.Name = p.identifier()
	p.expect("=")
	result.Args = []*Expr{p.outerExpression(depth + 1)}
	p.expect("decide")
	p.expect("$")
	if name := p.identifier(); name != result.Name {
		p.fail(result.Position, "decide must handle the immediately preceding query capture")
	}
	p.expect("{")
	for p.text != "}" && p.token != scanner.EOF && p.failure == nil {
		pattern := p.outerExpression(depth + 1)
		p.expect("=")
		p.expect(">")
		value := p.outerExpression(depth + 1)
		p.expect(";")
		result.Arms = append(result.Arms, DecisionArm{Pattern: pattern, Result: value})
	}
	p.expect("}")
	return result
}

func renderResult(expression *Expr, indent int) string {
	if expression.Kind == Effects {
		parts := make([]string, len(expression.Args))
		for i, arg := range expression.Args {
			parts[i] = renderLayout(arg, indent+2)
		}
		return "effects [" + strings.Join(parts, ", ") + "]"
	}
	pad := strings.Repeat(" ", indent)
	var out strings.Builder
	out.WriteString("query $" + expression.Name + " = " + renderLayout(expression.Args[0], indent) + "\n" + pad + "decide $" + expression.Name + " {")
	for _, arm := range expression.Arms {
		out.WriteString("\n" + pad + "  " + render(arm.Pattern) + " => " + renderLayout(arm.Result, indent+2) + ";")
	}
	out.WriteString("\n" + pad + "}")
	return out.String()
}

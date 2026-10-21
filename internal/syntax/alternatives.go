package syntax

import "strings"

// Alternatives are committed results, separate from speculative pattern OR.
// Parsing a flat chain does not consume nesting depth for each alternative.
func (p *parser) resultExpression(depth int) *Expr {
	first := p.resultOperand(depth)
	if p.text != "<" || p.failure != nil {
		return first
	}
	result := &Expr{Position: first.Position, Kind: OrderedAlternatives, Args: []*Expr{first}}
	for p.text == "<" && p.failure == nil {
		p.expect("<")
		p.expect("|")
		p.expect(">")
		result.Args = append(result.Args, p.resultOperand(depth))
	}
	return result
}

func (p *parser) resultOperand(depth int) *Expr {
	if p.text != "pure" {
		return p.outerExpression(depth)
	}
	position := p.position
	p.next()
	return &Expr{Position: position, Kind: Pure, Args: []*Expr{p.outerExpression(depth + 1)}}
}

func renderAlternatives(expression *Expr, indent int) string {
	switch expression.Kind {
	case Pure:
		return "pure " + renderLayout(expression.Args[0], indent+5)
	case Selection:
		arm := expression.Arms[0]
		prefix := "query "
		if expression.Name != "" {
			prefix += "$" + expression.Name + " = "
		}
		return prefix + renderLayout(expression.Args[0], indent+len(prefix)) +
			"\n" + strings.Repeat(" ", indent+2) + "select " + render(arm.Pattern) + " => " + renderLayout(arm.Result, indent+2)
	default:
		parts := make([]string, len(expression.Args))
		for i, alternative := range expression.Args {
			parts[i] = renderLayout(alternative, indent+4)
		}
		return strings.Join(parts, "\n"+strings.Repeat(" ", indent)+"<|> ")
	}
}

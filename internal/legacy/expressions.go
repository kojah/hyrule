package legacy

import "text/scanner"

func (p *parser) listExpression(depth int) *Expr {
	result := &Expr{Position: p.position, Kind: List}
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

// guardExpression preserves the existing hook-call form and adds one typed
// comparison. Repeated guards retain the language's ordered short circuiting.
func (p *parser) guardExpression() *Expr {
	left := p.outerExpression(0)
	operator := p.text
	if operator != "<" && operator != ">" && operator != "!" && (operator != "=" || p.scanner.Peek() != '=') {
		return left
	}
	p.next()
	if p.text == "=" {
		operator += "="
		p.next()
	} else if operator == "!" || operator == "=" {
		p.fail(p.position, "expected comparison operator")
	}
	return &Expr{Position: left.Position, Kind: Comparison, Name: operator, Args: []*Expr{left, p.expression(0)}}
}

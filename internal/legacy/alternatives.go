package legacy

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

package syntax

func (p *parser) patternDeclaration(position Position, comments []string) *Pattern {
	pattern := &Pattern{Position: position, Name: p.identifier(), Comments: comments}
	p.rule = pattern.Name
	p.expect("(")
	for p.text != ")" && p.failure == nil {
		p.expect("$")
		pattern.Parameters = append(pattern.Parameters, p.identifier())
		if p.text != "," {
			break
		}
		p.next()
	}
	p.expect(")")
	p.expect("=")
	pattern.Body = p.expression(0)
	pattern.Comments = append(pattern.Comments, p.comments...)
	p.comments = nil
	p.expect(";")
	p.rule = ""
	return pattern
}

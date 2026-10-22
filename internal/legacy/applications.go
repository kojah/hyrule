package legacy

import "text/scanner"

// outerExpression permits application only where a declaration, clause, or
// decision supplies its terminator. Children still use expression/argument, so
// omitted parentheses cannot silently change nested application grouping.
func (p *parser) outerExpression(depth int) *Expr {
	result := p.expression(depth)
	if result.Kind != Symbol || p.failure != nil {
		return result
	}
	result.Unparenthesized = true
	for !p.applicationBoundary() && p.failure == nil {
		result.Args = append(result.Args, p.argument(depth+1))
	}
	if len(result.Args) != 0 || result.Name == "or" || result.Name == "update" {
		result.Kind = Call
		p.applicationKind(result)
	}
	// A bare outer name may be a constant or a nullary operation. Resolution
	// happens after collecting declarations from every rules file in the target.
	return result
}

func (p *parser) applicationBoundary() bool {
	if p.token == scanner.EOF {
		return true
	}
	switch p.text {
	case ";", "=", "&", "when", "let", "else", "decide", "select", "{", "}", ")", "]", ",", "<", ">", "!":
		return true
	}
	return false
}

func (p *parser) applicationKind(result *Expr) {
	if result.Name == "update" {
		result.Kind = Update
	}
	if result.Name != "or" {
		return
	}
	result.Kind = Alternation
	if len(result.Args) < 2 {
		p.fail(result.Position, "or requires at least two alternatives")
	}
	for _, arg := range result.Args {
		if arg.Field != "" {
			p.fail(arg.Position, "or alternatives cannot have field names")
		}
	}
}

package legacy

import (
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
	if p.text == "$" {
		p.next()
		result.Name = p.identifier()
		p.expect("=")
	}
	result.Args = []*Expr{p.outerExpression(depth + 1)}
	if result.Name != "" && p.text != "select" {
		p.expect("decide")
		p.expect("$")
		if name := p.identifier(); name != result.Name {
			p.fail(result.Position, "decide must handle the immediately preceding query capture")
		}
	}
	if p.text == "select" {
		p.next()
		result.Kind = Selection
		pattern := p.outerExpression(depth + 1)
		p.expect("=")
		p.expect(">")
		result.Arms = []DecisionArm{{Pattern: pattern, Result: p.outerExpression(depth + 1)}}
		return result
	}
	p.expect("{")
	for p.text != "}" && p.token != scanner.EOF && p.failure == nil {
		pattern := p.outerExpression(depth + 1)
		p.expect("=")
		p.expect(">")
		value := p.resultExpression(depth + 1)
		p.expect(";")
		result.Arms = append(result.Arms, DecisionArm{Pattern: pattern, Result: value})
	}
	p.expect("}")
	return result
}

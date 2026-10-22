package syntax

func (p *parser) caseExpression(start token, base int) *Expr {
	call := p.expression(base, false, orRank)
	p.expect("of")
	outerGroup := p.grouped
	p.grouped = 0
	defer func() { p.grouped = outerGroup }()
	level := p.block(start.indent)
	result := &Expr{Position: start.Position, Kind: Query, Args: []*Expr{call}}
	result.Arms = p.arms(level)
	return result
}

func (p *parser) arms(level int) []DecisionArm {
	var result []DecisionArm
	for !p.end() && p.peek().indent == level {
		pattern := p.expression(level, true, 0)
		p.expect("->")
		answer := p.expression(level, false, 0)
		result = append(result, DecisionArm{Pattern: pattern, Result: answer})
		if !p.at("\n") {
			break
		}
		before := p.index
		comments := len(p.comments)
		p.skipLines()
		if p.peek().indent != level {
			p.index = before
			p.comments = p.comments[:comments]
			break
		}
	}
	if len(result) == 0 {
		p.fail("expected at least one outcome arm")
	}
	return result
}

func (p *parser) doBlock(start token) *Expr {
	result := &Expr{Position: start.Position, Kind: Do}
	outerGroup := p.grouped
	p.grouped = 0
	defer func() { p.grouped = outerGroup }()
	level := p.block(start.indent)
	var steps []*Expr
	var terminal *Expr
	for !p.end() && p.peek().indent == level {
		// Both bind patterns and terminal values share application syntax. A '|'
		// inside parentheses is parsed as a pattern only when the line binds.
		bind := p.hasOnLine("<-")
		value := p.expression(level, bind, 0)
		if !p.take("<-") {
			terminal = value
			break
		}
		if len(steps)+p.depth >= maxDepth {
			p.fail("do sequencing exceeds nesting limit")
			break
		}
		call := p.expression(level, false, orRank)
		step := &Expr{Position: value.Position, Kind: Query, Sequential: true, Args: []*Expr{call}, Arms: []DecisionArm{{Pattern: value}}}
		p.skipLines()
		if !p.end() && p.peek().indent > level && p.take("else") {
			elseIndent := p.tokens[p.index-1].indent
			if p.at("\n") {
				armLevel := p.block(elseIndent)
				step.Arms = append(step.Arms, p.arms(armLevel)...)
			} else {
				var other *Expr
				if p.hasOnLine("->") {
					other = p.expression(elseIndent, true, 0)
					p.expect("->")
				} else {
					if value.Kind != Literal || (value.Name != "true" && value.Name != "false") {
						p.fail("single-expression else requires a true or false do binding")
					}
					name := "true"
					if value.Name == "true" {
						name = "false"
					}
					other = &Expr{Position: value.Position, Kind: Literal, Name: name}
				}
				answer := p.expression(elseIndent, false, 0)
				step.Arms = append(step.Arms, DecisionArm{Pattern: other, Result: answer})
				p.skipLines()
			}
		}
		p.skipLines()
		steps = append(steps, step)
	}
	if terminal == nil {
		p.fail("do requires a final result")
		terminal = &Expr{Position: start.Position}
	}
	if terminal.Kind != Do && terminal.Kind != Query && terminal.Kind != OrderedAlternatives && terminal.Kind != Decline && terminal.Kind != Conditional {
		terminal = &Expr{Position: terminal.Position, Kind: Pure, Args: []*Expr{terminal}}
	}
	for i := len(steps) - 1; i >= 0; i-- {
		steps[i].Arms[0].Result = terminal
		terminal = steps[i]
	}
	result.Args = []*Expr{terminal}
	return result
}
func (p *parser) hasOnLine(operator string) bool {
	nesting := 0
	for i := p.index; i < len(p.tokens); i++ {
		switch p.tokens[i].text {
		case "(", "[":
			nesting++
		case ")", "]":
			nesting--
		case operator:
			if nesting == 0 {
				return true
			}
		case "\n":
			if nesting == 0 {
				return false
			}
		}
	}
	return false
}

// conditional parses if/then/else. Then and else may start indented lines
// below the if, which the ordinary continuation leaves visible for do-else.
func (p *parser) conditional(start token, base int) *Expr {
	condition := p.expression(base, false, orRank)
	p.keywordLine(start.indent, "then")
	p.expect("then")
	then := p.expression(base, false, 0)
	p.keywordLine(start.indent, "else")
	p.expect("else")
	otherwise := p.expression(base, false, 0)
	return &Expr{Position: start.Position, Kind: Conditional, Args: []*Expr{condition, then, otherwise}}
}

// keywordLine skips line breaks before keyword when it starts a line indented
// past the construct's own line.
func (p *parser) keywordLine(indent int, keyword string) {
	if !p.at("\n") {
		return
	}
	index, comments := p.index, len(p.comments)
	p.skipLines()
	if t := p.peek(); t.text == keyword && t.indent > indent {
		return
	}
	p.index, p.comments = index, p.comments[:comments]
}

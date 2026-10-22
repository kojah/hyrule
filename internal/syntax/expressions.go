package syntax

import (
	"strconv"
	"strings"
	"text/scanner"
)

func itoa(value int) string { return strconv.Itoa(value) }

func precedence(op string) int {
	switch op {
	case "<|>":
		return 1
	case "==", "!=", "<", ">", "<=", ">=":
		return 2
	case "+", "-":
		return 3
	case "*", "/", "%":
		return 4
	}
	return 0
}
func arithmetic(op string) string {
	switch op {
	case "+":
		return "add"
	case "-":
		return "sub"
	case "*":
		return "mul"
	case "/":
		return "div"
	case "%":
		return "rem"
	}
	return op
}

func (p *parser) expression(base int, pattern bool, min int) *Expr {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		p.fail("expression nesting exceeds limit")
		return &Expr{}
	}
	p.continuation(base)
	left := p.application(base, pattern)
	for !p.end() {
		p.continuation(base)
		op := p.peek()
		rank := precedence(op.text)
		if pattern || rank == 0 || rank < min {
			break
		}
		p.next()
		right := p.expression(base, false, rank+1)
		kind, name := Call, arithmetic(op.text)
		if rank == 2 {
			kind = Comparison
		}
		if rank == 1 {
			if left.Kind == OrderedAlternatives {
				left.Args = append(left.Args, right)
				continue
			}
			kind = OrderedAlternatives
		}
		left = &Expr{Position: op.Position, Kind: kind, Name: name, Args: []*Expr{left, right}}
	}
	return left
}

func (p *parser) application(base int, pattern bool) *Expr {
	left := p.atom(base, pattern)
	for !p.end() {
		p.continuation(base)
		if p.at("with") && !pattern {
			p.next()
			p.expect("(")
			args := p.named(base, false)
			left = &Expr{Position: left.Position, Kind: Update, Name: "update", Args: append([]*Expr{left}, args...)}
			break
		}
		if !p.atomStart() {
			break
		}
		if !applicable(left) {
			p.fail("only a named constructor or host function can be applied")
			break
		}
		left.Kind = Call
		left.Bare = false
		if p.at("(") && p.namedAhead() {
			p.next()
			left.Args = append(left.Args, p.named(base, pattern)...)
		} else if p.at("(") && p.emptyAhead() {
			p.next()
			p.skipLines()
			p.expect(")")
		} else {
			left.Args = append(left.Args, p.atom(base, pattern))
		}
	}
	return left
}
func (p *parser) atomStart() bool {
	t := p.peek()
	switch t.text {
	case "of", "else", "with", "let", "do", "case", "decline":
		return false
	}
	return t.kind == scanner.Ident || t.kind == scanner.RawString || t.kind == scanner.Int || t.kind == scanner.Float || t.kind == scanner.String ||
		t.text == "(" || t.text == "[" || t.text == "^"
}
func (p *parser) emptyAhead() bool {
	i := p.index + 1
	for i < len(p.tokens) && (p.tokens[i].text == "\n" || p.tokens[i].kind == scanner.Comment) {
		i++
	}
	return i < len(p.tokens) && p.tokens[i].text == ")"
}
func (p *parser) namedAhead() bool {
	i := p.index + 1
	for i < len(p.tokens) && (p.tokens[i].text == "\n" || p.tokens[i].kind == scanner.Comment) {
		i++
	}
	if i >= len(p.tokens) || (p.tokens[i].kind != scanner.Ident && p.tokens[i].kind != scanner.RawString) {
		return false
	}
	i++
	for i < len(p.tokens) && (p.tokens[i].text == "\n" || p.tokens[i].kind == scanner.Comment) {
		i++
	}
	return i < len(p.tokens) && p.tokens[i].text == ":"
}

func (p *parser) atom(base int, pattern bool) *Expr {
	p.continuation(base)
	t := p.peek()
	x := &Expr{Position: t.Position}
	switch {
	case !pattern && p.take("do"):
		return p.doBlock(t)
	case !pattern && p.take("case"):
		return p.caseExpression(t, base)
	case !pattern && p.take("decline"):
		x.Kind = Decline
	case p.take("("):
		p.grouped++
		p.skipLines()
		x = p.expression(base, pattern, 0)
		if pattern && p.at("|") {
			alts := []*Expr{x}
			for p.take("|") {
				alts = append(alts, p.expression(base, true, 0))
			}
			x = &Expr{Position: t.Position, Kind: Alternation, Name: "or", Args: alts}
		}
		p.skipLines()
		p.expect(")")
		p.grouped--
	case p.take("["):
		return p.list(t, base, pattern)
	case p.take("^"):
		x.Kind = Symbol
		x.Name = p.path()
	case p.take("_"):
		x.Kind = Wildcard
	case t.kind == scanner.String || t.kind == scanner.Int || t.kind == scanner.Float || t.text == "true" || t.text == "false" || t.text == "nil":
		x.Kind = Literal
		x.Name = p.next().text
	case p.take("-"):
		if p.peek().kind != scanner.Int && p.peek().kind != scanner.Float {
			p.fail("minus requires a numeric literal or an infix left operand")
		}
		x.Kind = Literal
		x.Name = "-" + p.next().text
	case t.kind == scanner.Ident || t.kind == scanner.RawString:
		return p.nameExpression(t, base, pattern)
	default:
		p.fail("expected expression")
	}
	return x
}

func (p *parser) named(base int, pattern bool) []*Expr {
	p.grouped++
	defer func() { p.grouped-- }()
	p.skipLines()
	var args []*Expr
	for !p.end() && !p.at(")") {
		name := p.name()
		p.expect(":")
		x := p.expression(base, pattern, 0)
		x.Field = name
		args = append(args, x)
		p.skipLines()
		if !p.take(",") {
			break
		}
		p.skipLines()
	}
	p.expect(")")
	return args
}

// identifierText is used by the formatter to retain keyword captures.
func identifierText(name string) string {
	switch name {
	case "do", "case", "of", "else", "decline", "with", "let", "otherwise", "true", "false", "nil":
		return "`" + name + "`"
	}
	return strings.TrimSpace(name)
}

func applicable(x *Expr) bool {
	return x.Kind == Call || x.Kind == Symbol && x.Bare || x.Kind == Variable && x.Quoted
}

func (p *parser) list(t token, base int, pattern bool) *Expr {
	x := &Expr{Position: t.Position}

	if pattern {
		p.fail("list patterns are not supported")
	}
	x.Kind = List
	p.grouped++
	p.skipLines()
	for !p.end() && !p.at("]") {
		x.Args = append(x.Args, p.expression(base, false, 0))
		p.skipLines()
		if !p.take(",") {
			break
		}
		p.skipLines()
	}
	p.expect("]")
	p.grouped--

	return x
}

func (p *parser) nameExpression(t token, base int, pattern bool) *Expr {
	x := &Expr{Position: t.Position}

	x.Kind = Symbol
	x.Name = p.path()
	x.Bare = true
	if t.kind == scanner.RawString {
		x.Kind = Variable
		x.Quoted = true
		x.Bare = false
	}
	if p.at("(") && p.emptyAhead() {
		p.next()
		p.skipLines()
		p.expect(")")
		x.Kind = Call
		x.Bare = false
	}
	if p.take("@") {
		p.depth++
		if p.depth > maxDepth {
			p.fail("expression nesting exceeds limit")
			p.depth--
			return x
		}
		bound := p.atom(base, pattern)
		p.depth--
		bound.Bind = x.Name
		return bound
	}
	return x
}

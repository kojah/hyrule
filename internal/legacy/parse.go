package legacy

import (
	"fmt"
	"io"
	"text/scanner"
)

const maxDepth = 64
const maxTokens = 100000

type parser struct {
	scanner  scanner.Scanner
	token    rune
	text     string
	position Position
	comments []string
	failure  error
	count    int
	rule     string
}

// Parse accepts a complete rules file. Nested patterns and total tokens are
// bounded; unsupported syntax is an error, never an ignored rule.
func Parse(name string, input io.Reader) (*File, error) {
	p := &parser{}
	p.scanner.Init(input)
	p.scanner.Filename = name
	p.scanner.Mode = scanner.ScanIdents | scanner.ScanInts | scanner.ScanFloats | scanner.ScanStrings | scanner.ScanRawStrings | scanner.ScanComments
	p.scanner.Error = func(scan *scanner.Scanner, message string) {
		if p.failure == nil {
			p.failure = &Diagnostic{Position: Position{File: name, Line: scan.Line, Column: scan.Column}, Message: message}
		}
	}
	p.next()
	file := &File{}
	for p.token != scanner.EOF && p.failure == nil {
		comments := p.comments
		p.comments = nil
		pos, kind := p.position, p.text
		p.next()
		switch kind {
		case "match", "build", "guard", "query", "const":
			file.Items = append(file.Items, p.bindings(pos, kind, comments)...)
		case "union", "exhaustive":
			if kind == "exhaustive" {
				p.expect("union")
			}
			union := p.unionDeclaration(pos, comments)
			union.Exhaustive = kind == "exhaustive"
			file.Items = append(file.Items, Item{Union: union})
		case "record", "view":
			record := p.recordDeclaration(pos, comments)
			record.View = kind == "view"
			file.Items = append(file.Items, Item{Record: record})
		case "derive":
			file.Items = append(file.Items, Item{Derivation: p.derivation(pos, comments)})
		case "signature":
			file.Items = append(file.Items, Item{API: p.apiDeclaration(pos, comments)})
		case "pattern":
			pattern := p.patternDeclaration(pos, comments)
			file.Items = append(file.Items, Item{Pattern: pattern})
		case "rule":
			file.Items = append(file.Items, Item{Rule: p.ruleDeclaration(pos, comments)})
		default:
			p.fail(pos, "expected match, build, guard, query, const, record, union, pattern or rule declaration")
		}
	}
	file.Trailing = p.comments
	if p.failure != nil {
		return nil, p.failure
	}
	return file, nil
}

func (p *parser) next() {
	if p.failure != nil {
		p.token, p.text = scanner.EOF, ""
		return
	}
	for {
		p.token = p.scanner.Scan()
		p.text = p.scanner.TokenText()
		position := p.scanner.Position
		p.position = Position{File: position.Filename, Line: position.Line, Column: position.Column}
		p.count++
		if p.count > maxTokens {
			p.fail(p.position, "rule file exceeds token limit")
			return
		}
		if p.token != scanner.Comment {
			return
		}
		p.comments = append(p.comments, p.text)
	}
}

func (p *parser) fail(position Position, message string) {
	if p.failure == nil {
		p.failure = &Diagnostic{Position: position, Rule: p.rule, Message: message}
	}
}

func (p *parser) expect(text string) {
	if p.text != text {
		p.fail(p.position, fmt.Sprintf("expected %q, got %q", text, p.text))
		return
	}
	p.next()
}

func (p *parser) identifier() string {
	name := p.text
	if p.token != scanner.Ident || name == "_" {
		p.fail(p.position, "expected identifier")
		return ""
	}
	p.next()
	return name
}

func (p *parser) expression(depth int) *Expr {
	result := &Expr{Position: p.position}
	if p.failure != nil {
		return result
	}
	if depth >= maxDepth {
		p.fail(p.position, "expression nesting exceeds limit")
		return result
	}
	switch {
	case p.text == "do":
		return p.doExpression(depth)
	case p.text == "decline":
		result.Kind = Decline
		p.next()
	case p.text == "effects":
		return p.effects(depth)
	case p.text == "query":
		return p.query(depth)
	case p.text == "[":
		return p.listExpression(depth)
	case p.text == "(":
		p.next()
		result.Kind, result.Name = Call, p.hookName()
		for p.text != ")" && p.token != scanner.EOF && p.failure == nil {
			result.Args = append(result.Args, p.argument(depth+1))
		}
		p.expect(")")
		p.applicationKind(result)
	case p.text == "$":
		p.next()
		result.Kind, result.Name = Variable, p.capturePath()
		if p.text == ":" {
			p.next()
			bound := p.expression(depth + 1)
			if bound.Kind != Call && bound.Kind != Alternation {
				p.fail(bound.Position, "a named operation must bind a call pattern")
			}
			bound.Bind = result.Name
			return bound
		}
	case p.text == "_":
		result.Kind = Wildcard
		p.next()
	case p.token == scanner.Int || p.token == scanner.Float || p.token == scanner.String || p.token == scanner.RawString ||
		p.text == "true" || p.text == "false" || p.text == "nil":
		result.Kind, result.Name = Literal, p.text
		p.next()
	case p.text == "-":
		p.next()
		if p.token != scanner.Int && p.token != scanner.Float {
			p.fail(p.position, "expected numeric literal after minus")
		}
		result.Kind, result.Name = Literal, "-"+p.text
		p.next()
	case p.token == scanner.Ident:
		result.Kind, result.Name = Symbol, p.hookName()
	default:
		p.fail(p.position, "expected operation, $capture, _, or literal")
	}
	return result
}

// argument recognizes labels without changing the expression language's literals.
func (p *parser) argument(depth int) *Expr {
	if depth >= maxDepth {
		return p.expression(depth)
	}
	if p.token != scanner.Ident {
		return p.expression(depth)
	}
	position, name := p.position, p.text
	p.next()
	if p.text == ":" && name != "_" {
		p.next()
		value := p.expression(depth)
		value.Field = name
		return value
	}
	if name == "effects" {
		return p.effectsBody(position, depth)
	}
	if name == "query" {
		return p.queryBody(position, depth)
	}
	if name == "_" {
		return &Expr{Position: position, Kind: Wildcard}
	}
	if name == "true" || name == "false" || name == "nil" {
		return &Expr{Position: position, Kind: Literal, Name: name}
	}
	return &Expr{Position: position, Kind: Symbol, Name: name}
}

func (p *parser) ruleDeclaration(pos Position, comments []string) *Rule {
	rule := &Rule{Position: pos, Name: p.identifier(), Comments: comments}
	p.rule = rule.Name
	p.expect(":")
	rule.Match = p.outerExpression(0)
	for p.text == "&" || p.text == "when" || p.text == "let" {
		if p.text == "let" {
			p.next()
			p.expect("$")
			name := p.identifier()
			p.expect("=")
			rule.Clauses = append(rule.Clauses, Clause{Name: name, Value: p.guardExpression()})
			continue
		} else if p.text == "when" {
			p.next()
		} else {
			p.expect("&")
			p.expect("&")
		}
		rule.Clauses = append(rule.Clauses, Clause{Value: p.guardExpression()})
	}
	p.expect("=")
	p.expect(">")
	rule.Build = p.resultExpression(0)
	// Comments inside a rule are retained ahead of it when formatted.
	rule.Comments = append(rule.Comments, p.comments...)
	p.comments = nil
	p.expect(";")
	p.rule = ""
	return rule
}

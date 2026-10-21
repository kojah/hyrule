package syntax

import (
	"fmt"
	"io"
	"strings"
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
			p.failure = &Diagnostic{Position: Position{name, scan.Line, scan.Column}, Message: message}
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
		case "match", "build", "guard":
			binding := &Binding{Position: pos, Kind: kind, Name: p.identifier(), Comments: comments}
			p.expect("=")
			binding.GoName = p.identifier()
			p.expect(";")
			file.Items = append(file.Items, Item{Binding: binding})
		case "rule":
			rule := &Rule{Position: pos, Name: p.identifier(), Comments: comments}
			p.rule = rule.Name
			p.expect(":")
			rule.Match = p.expression(0)
			if p.text == "&" {
				p.expect("&")
				p.expect("&")
				rule.Guard = p.expression(0)
			}
			p.expect("=")
			p.expect(">")
			rule.Build = p.expression(0)
			// Comments inside a rule are retained ahead of it when formatted.
			rule.Comments = append(rule.Comments, p.comments...)
			p.comments = nil
			p.expect(";")
			file.Items = append(file.Items, Item{Rule: rule})
			p.rule = ""
		default:
			p.fail(pos, "expected match, build, guard or rule declaration")
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
		p.position = Position{position.Filename, position.Line, position.Column}
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
	case p.text == "(":
		p.next()
		result.Kind, result.Name = Call, p.identifier()
		for p.text != ")" && p.token != scanner.EOF && p.failure == nil {
			result.Args = append(result.Args, p.expression(depth+1))
		}
		p.expect(")")
	case p.text == "$":
		p.next()
		result.Kind, result.Name = Variable, p.identifier()
		if p.text == ":" {
			p.next()
			bound := p.expression(depth + 1)
			if bound.Kind != Call {
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
	default:
		p.fail(p.position, "expected operation, $capture, _, or literal")
	}
	return result
}

// Format canonicalizes valid rules without resolving host bindings. Comments
// are preserved; comments inside an expression move before their declaration.
func Format(name string, input []byte) ([]byte, error) {
	file, err := Parse(name, strings.NewReader(string(input)))
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	comments := func(values []string) {
		for _, value := range values {
			out.WriteString(value + "\n")
		}
	}
	for index, item := range file.Items {
		if item.Binding != nil {
			binding := item.Binding
			comments(binding.Comments)
			fmt.Fprintf(&out, "%s %s = %s;\n", binding.Kind, binding.Name, binding.GoName)
			continue
		}
		if index != 0 {
			out.WriteByte('\n')
		}
		rule := item.Rule
		comments(rule.Comments)
		fmt.Fprintf(&out, "rule %s:\n  %s", rule.Name, render(rule.Match))
		if rule.Guard != nil {
			fmt.Fprintf(&out, "\n  && %s", render(rule.Guard))
		}
		fmt.Fprintf(&out, "\n  => %s;\n", render(rule.Build))
	}
	comments(file.Trailing)
	return []byte(out.String()), nil
}

func render(expression *Expr) string {
	switch expression.Kind {
	case Wildcard:
		return "_"
	case Variable:
		return "$" + expression.Name
	case Literal:
		return expression.Name
	default:
		parts := []string{expression.Name}
		for _, argument := range expression.Args {
			parts = append(parts, render(argument))
		}
		prefix := ""
		if expression.Bind != "" {
			prefix = "$" + expression.Bind + ":"
		}
		return prefix + "(" + strings.Join(parts, " ") + ")"
	}
}

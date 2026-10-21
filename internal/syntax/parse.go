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
		case "signature":
			file.Items = append(file.Items, Item{API: p.apiDeclaration(pos, comments)})
		case "pattern":
			pattern := p.patternDeclaration(pos, comments)
			file.Items = append(file.Items, Item{Pattern: pattern})
		case "rule":
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
			file.Items = append(file.Items, Item{Rule: rule})
			p.rule = ""
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
		if item.API != nil {
			comments(item.API.Comments)
			out.WriteString(formatAPI(item.API))
			continue
		}
		if item.Binding != nil {
			binding := item.Binding
			comments(binding.Comments)
			out.WriteString(formatBinding(file.Items, index))
			continue
		}
		if item.Union != nil {
			comments(item.Union.Comments)
			out.WriteString(formatUnion(item.Union))
			continue
		}
		if item.Record != nil {
			comments(item.Record.Comments)
			out.WriteString(formatRecord(item.Record))
			continue
		}
		if item.Pattern != nil {
			pattern := item.Pattern
			comments(pattern.Comments)
			parameters := make([]string, len(pattern.Parameters))
			for i, name := range pattern.Parameters {
				parameters[i] = "$" + name
			}
			fmt.Fprintf(&out, "pattern %s(%s) =\n  %s;\n", pattern.Name, strings.Join(parameters, ", "), renderLayout(pattern.Body, 2))
			continue
		}
		if index != 0 {
			out.WriteByte('\n')
		}
		rule := item.Rule
		comments(rule.Comments)
		fmt.Fprintf(&out, "rule %s:\n  %s", rule.Name, renderLayout(rule.Match, 2))
		for _, clause := range rule.Clauses {
			if clause.Name != "" {
				fmt.Fprintf(&out, "\n  let $%s = %s", clause.Name, renderLayout(clause.Value, 9+len(clause.Name)))
			} else {
				fmt.Fprintf(&out, "\n  && %s", renderLayout(clause.Value, 5))
			}
		}
		fmt.Fprintf(&out, "\n  => %s;\n", renderLayout(rule.Build, 5))
	}
	comments(file.Trailing)
	return []byte(out.String()), nil
}

func render(expression *Expr) string {
	switch expression.Kind {
	case Effects, Query, Selection, OrderedAlternatives, Pure:
		return renderResult(expression, 0)
	case Wildcard:
		return "_"
	case Variable:
		return "$" + expression.Name
	case Literal, Symbol:
		return expression.Name
	case Comparison:
		return render(expression.Args[0]) + " " + expression.Name + " " + render(expression.Args[1])
	case List:
		items := make([]string, len(expression.Args))
		for index, item := range expression.Args {
			items[index] = render(item)
		}
		return "[" + strings.Join(items, ", ") + "]"
	default:
		parts := []string{expression.Name}
		for _, argument := range expression.Args {
			child := render(argument)
			if argument.Field != "" {
				child = argument.Field + ":" + child
			}
			parts = append(parts, child)
		}
		prefix := ""
		if expression.Bind != "" {
			prefix = "$" + expression.Bind + ":"
		}
		if expression.Unparenthesized {
			return prefix + strings.Join(parts, " ")
		}
		return prefix + "(" + strings.Join(parts, " ") + ")"
	}
}

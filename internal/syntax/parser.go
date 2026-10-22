package syntax

import (
	"fmt"
	"io"
	"strings"
	"text/scanner"
)

type parser struct {
	tokens   []token
	index    int
	failure  error
	comments []string
	grouped  int
	depth    int
	family   string
}

// Parse reads equation syntax only. Legacy files must be explicitly migrated.
func Parse(name string, input io.Reader) (*File, error) {
	tokens, err := lex(name, input)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	file := &File{}
	for p.skipLines(); !p.end() && p.failure == nil; p.skipLines() {
		if p.peek().indent != 0 {
			p.fail("top-level declaration must start in column one")
			break
		}
		comments := p.comments
		p.comments = nil
		p.declaration(file, comments)
	}
	file.Trailing = p.comments
	if p.failure != nil {
		return nil, p.failure
	}
	return file, nil
}

func (p *parser) peek() token {
	for p.index < len(p.tokens)-1 && p.tokens[p.index].kind == scanner.Comment {
		p.comments = append(p.comments, p.tokens[p.index].text)
		p.index++
	}
	return p.tokens[p.index]
}
func (p *parser) next() token {
	t := p.peek()
	if p.index < len(p.tokens)-1 {
		p.index++
	}
	return t
}
func (p *parser) end() bool        { return p.peek().kind == scanner.EOF || p.failure != nil }
func (p *parser) at(s string) bool { return p.peek().text == s }
func (p *parser) take(s string) bool {
	if p.at(s) {
		p.next()
		return true
	}
	return false
}
func (p *parser) expect(s string) {
	if !p.take(s) {
		p.fail(fmt.Sprintf("expected %q, got %q", s, p.peek().text))
	}
}
func (p *parser) fail(message string) {
	if p.failure == nil {
		p.failure = &Diagnostic{Position: p.peek().Position, Message: message}
	}
}
func (p *parser) skipLines() {
	for p.take("\n") {
	}
}
func (p *parser) name() string {
	t := p.peek()
	if t.kind != scanner.Ident && t.kind != scanner.RawString {
		p.fail("expected identifier")
		return ""
	}
	p.next()
	return strings.Trim(t.text, "`")
}
func (p *parser) path() string {
	name := p.name()
	for p.take(".") {
		name += "." + p.name()
	}
	return name
}
func (p *parser) storage() string {
	if p.peek().kind == scanner.Int {
		return p.next().text
	}
	return p.path()
}
func (p *parser) lineEnd() {
	if !p.end() && !p.at("\n") {
		p.fail("expected end of declaration")
	}
	p.skipLines()
}
func (p *parser) block(parent int) int {
	p.expect("\n")
	p.skipLines()
	if p.end() || p.peek().indent <= parent {
		p.fail("expected an indented block")
	}
	return p.peek().indent
}

// continuation ignores newlines inside groups and more deeply indented
// expression continuations. Structural boundaries stay visible to the caller.
func (p *parser) continuation(base int) {
	if !p.at("\n") {
		return
	}
	index := p.index
	comments := len(p.comments)
	p.skipLines()
	t := p.peek()
	if p.grouped > 0 || (t.indent > base && t.text != "else") || (t.text == "<|>" && t.indent == base) {
		return
	}
	p.index = index
	p.comments = p.comments[:comments]
}

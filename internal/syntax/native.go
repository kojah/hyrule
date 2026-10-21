package syntax

import (
	"strings"
	"text/scanner"
)

func (p *parser) capturePath() string {
	name := p.identifier()
	for p.text == "." && p.failure == nil {
		p.next()
		name += "." + p.identifier()
	}
	return name
}

func (p *parser) goType() string {
	prefix := ""
	if p.text == "*" {
		prefix = "*"
		p.next()
	}
	name := p.identifier()
	if p.text == "." {
		p.next()
		name += "." + p.identifier()
	}
	return prefix + name
}

func (p *parser) storageName() string {
	if p.token == scanner.Int {
		name := p.text
		p.next()
		return name
	}
	return p.identifier()
}

func (p *parser) apiDeclaration(pos Position, comments []string) *API {
	api := &API{Position: pos, Mode: p.identifier(), Comments: comments}
	if api.Mode == "total" {
		p.expect("fallback")
		api.Fallback = p.outerExpression(0)
	} else if api.Mode != "partial" {
		p.fail(pos, "signature requires partial or total fallback")
	}
	p.expect(";")
	return api
}

func formatAPI(api *API) string {
	value := "signature " + api.Mode
	if api.Fallback != nil {
		value += " fallback " + render(api.Fallback)
	}
	return strings.TrimSpace(value) + ";\n"
}

func (p *parser) hookName() string {
	name := p.identifier()
	if p.text == "." {
		p.next()
		name += "." + p.identifier()
	}
	return name
}

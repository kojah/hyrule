package syntax

import (
	"strings"
	"text/scanner"
)

// parameters reads a comma-separated list of optionally named Go types, which
// may continue on more deeply indented lines.
func (p *parser) parameters() []Param {
	var params []Param
	for !p.end() {
		p.continuation(0)
		var param Param
		if t := p.peek(); (t.kind == scanner.Ident || t.kind == scanner.RawString) &&
			p.index+1 < len(p.tokens) && p.tokens[p.index+1].text == ":" {
			param.Name = p.name()
			p.expect(":")
		}
		param.Type = p.goType(0)
		params = append(params, param)
		if !p.take(",") {
			break
		}
	}
	return params
}

// goType reads pointer, slice, map, named and instantiated generic Go types.
// The result is canonical Go syntax; the host resolves its names after parsing.
func (p *parser) goType(depth int) string {
	if depth > maxDepth {
		p.fail("type nesting exceeds limit")
		return ""
	}
	switch {
	case p.take("*"):
		return "*" + p.goType(depth+1)
	case p.take("["):
		p.expect("]")
		return "[]" + p.goType(depth+1)
	case p.at("map") && p.index+1 < len(p.tokens) && p.tokens[p.index+1].text == "[":
		p.next()
		p.next()
		key := p.goType(depth + 1)
		p.expect("]")
		return "map[" + key + "]" + p.goType(depth+1)
	}
	name := p.path()
	if !p.take("[") {
		return name
	}
	arguments := []string{p.goType(depth + 1)}
	for p.take(",") {
		arguments = append(arguments, p.goType(depth+1))
	}
	p.expect("]")
	return name + "[" + strings.Join(arguments, ", ") + "]"
}

func paramsText(params []Param) string {
	parts := make([]string, len(params))
	for i, param := range params {
		parts[i] = param.Type
		if param.Name != "" {
			parts[i] = identifierText(param.Name) + ": " + param.Type
		}
	}
	return strings.Join(parts, ", ")
}

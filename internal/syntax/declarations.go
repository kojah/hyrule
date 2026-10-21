package syntax

import (
	"fmt"
	"strings"
)

// bindings expands exact-name groups before typed hook resolution. Each name
// retains its own position so duplicate and missing-hook errors stay precise.
func (p *parser) bindings(position Position, kind string, comments []string) []Item {
	binding := &Binding{Position: position, Kind: kind, Name: p.identifier(), Comments: comments}
	binding.GoName = binding.Name
	if p.text == "(" {
		if kind != "query" && kind != "build" && kind != "guard" {
			p.fail(position, "only query, build and guard bindings support argument forwarding")
		}
		p.next()
		for p.text != ")" && p.failure == nil {
			p.expect("$")
			binding.Parameters = append(binding.Parameters, p.identifier())
			if p.text != "," {
				break
			}
			p.next()
		}
		p.expect(")")
		p.expect("=")
		binding.Body = p.outerExpression(0)
		if binding.Body.Kind != Call || binding.Body.Bind != "" {
			p.fail(position, "forwarding binding requires a function application")
		}
		for _, arg := range binding.Body.Args {
			if arg.Bind != "" || arg.Field != "" || (arg.Kind != Variable && arg.Kind != Literal && arg.Kind != Symbol) {
				p.fail(arg.Position, "forwarding arguments require positional captures, constants or literals")
			}
		}
		binding.GoName = binding.Body.Name
		p.expect(";")
		return []Item{{Binding: binding}}
	}
	binding.Implicit = p.text != "="
	if !binding.Implicit {
		p.next()
		binding.GoName = p.hookName()
	}
	items := []Item{{Binding: binding}}
	for binding.Implicit && p.text == "," && p.failure == nil {
		p.next()
		namePosition := p.position
		name := p.identifier()
		items = append(items, Item{Binding: &Binding{
			Position: namePosition, Kind: kind, Name: name, GoName: name,
			Implicit: true, Continued: true,
		}})
	}
	binding.Comments = append(binding.Comments, p.comments...)
	p.comments = nil
	p.expect(";")
	return items
}

func (p *parser) patternDeclaration(position Position, comments []string) *Pattern {
	pattern := &Pattern{Position: position, Name: p.identifier(), Comments: comments}
	p.rule = pattern.Name
	p.expect("(")
	for p.text != ")" && p.failure == nil {
		p.expect("$")
		pattern.Parameters = append(pattern.Parameters, p.identifier())
		if p.text != "," {
			break
		}
		p.next()
	}
	p.expect(")")
	p.expect("=")
	pattern.Body = p.outerExpression(0)
	pattern.Comments = append(pattern.Comments, p.comments...)
	p.comments = nil
	p.expect(";")
	p.rule = ""
	return pattern
}

// formatBinding preserves grouping without changing the resolved declarations.
func formatBinding(items []Item, index int) string {
	binding := items[index].Binding
	text := fmt.Sprintf("%s %s", binding.Kind, binding.Name)
	if binding.Continued {
		text = ", " + binding.Name
	}
	if binding.Body != nil {
		names := make([]string, len(binding.Parameters))
		for i, name := range binding.Parameters {
			names[i] = "$" + name
		}
		text += "(" + strings.Join(names, ", ") + ") = " + render(binding.Body)
	} else if !binding.Implicit {
		text += " = " + binding.GoName
	}
	if index+1 == len(items) || items[index+1].Binding == nil || !items[index+1].Binding.Continued {
		text += ";\n"
	}
	return text
}

package syntax

import (
	"fmt"
	"hash/fnv"
	"strings"
)

func (p *parser) function(comments []string) *Function {
	fn := &Function{Position: p.peek().Position, Comments: comments}
	api := &API{Position: fn.Position, Function: true}
	if p.at("partial") || p.at("total") {
		api.Mode = p.next().text
	}
	p.expect("fn")
	fn.Name = p.name()
	hasParameters := p.take("(")
	if hasParameters {
		p.skipLines()
	}
	if hasParameters && !p.at(")") {
		for !p.end() {
			p.skipLines()
			param := Param{Name: p.name()}
			if p.take(":") {
				param.Type = p.goType(0)
			}
			fn.Parameters = append(fn.Parameters, param)
			if !p.take(",") {
				break
			}
		}
	}
	if hasParameters {
		p.skipLines()
		p.expect(")")
	}
	p.keywordLine(0, "->")
	if hasParameters && len(fn.Parameters) == 0 {
		p.fail("omit the parameter list to infer it from Go")
	}
	if p.take("->") {
		if !hasParameters {
			p.fail("result annotation requires typed parameters")
		}
		api.Outputs = p.parameters()
		for _, param := range fn.Parameters {
			if param.Type == "" {
				p.fail("annotated fn requires every parameter type")
			}
		}
		api.Inputs = fn.Parameters
	} else {
		for _, param := range fn.Parameters {
			if param.Type != "" {
				p.fail("annotated fn requires a result type")
			}
		}
	}
	if api.Mode != "" || len(api.Inputs) > 0 {
		fn.API = api
	}
	p.lineEnd()
	if p.end() || p.peek().indent == 0 {
		return fn
	}
	level := p.peek().indent
	if p.take("fallback") {
		if api.Mode != "total" {
			p.fail("fallback requires total fn")
		}
		api.Fallback = p.expression(level, false, 0)
		p.lineEnd()
	}
	if p.end() || p.peek().indent == 0 {
		return fn
	}
	if p.peek().indent != level {
		p.fail("expected fn body")
		return fn
	}
	if !p.take("match") {
		fn.Body = p.expression(level, false, 0)
		p.lineEnd()
		return fn
	}
	fn.Subject = p.name()
	armLevel := p.block(level)
	for !p.end() && p.peek().indent == armLevel {
		r := &Rule{Position: p.peek().Position, Family: fn.Name, Comments: p.comments}
		p.comments = nil
		if p.take("@") {
			r.Name = p.name()
			p.lineEnd()
		}
		if r.Name == "" {
			hash := fnv.New32a()
			_, _ = hash.Write([]byte(r.File))
			r.Name = fmt.Sprintf("%s_L%d_F%x", fn.Name, r.Line, hash.Sum32())
		}
		r.Match = p.expression(armLevel, true, 0)
		p.continuation(armLevel)
		if p.take("when") {
			r.Clauses = p.qualifiers(armLevel)
		}
		p.expect("->")
		r.Build = p.expression(armLevel, false, 0)
		if p.take("where") {
			r.Where = p.whereBindings()
		}
		fn.Rules = append(fn.Rules, r)
		p.lineEnd()
	}
	if len(fn.Rules) == 0 {
		p.fail("match requires at least one arm")
	}
	return fn
}

func printFunction(out *strings.Builder, fn *Function) {
	printComments(out, fn.Comments)
	if fn.API != nil && fn.API.Mode != "" {
		out.WriteString(fn.API.Mode + " ")
	}
	out.WriteString("fn " + identifierText(fn.Name))
	if len(fn.Parameters) > 0 {
		out.WriteString("(")
	}
	for i, param := range fn.Parameters {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(identifierText(param.Name))
		if param.Type != "" {
			out.WriteString(": " + param.Type)
		}
	}
	if len(fn.Parameters) > 0 {
		out.WriteString(")")
	}
	if fn.API != nil && len(fn.API.Outputs) > 0 {
		out.WriteString(" -> " + paramsText(fn.API.Outputs))
	}
	out.WriteByte('\n')
	if fn.API != nil && fn.API.Fallback != nil {
		out.WriteString("  fallback " + render(fn.API.Fallback, 2) + "\n")
	}
	if fn.Body != nil {
		out.WriteString("  " + render(fn.Body, 2) + "\n")
	}
	if fn.Subject == "" {
		return
	}
	out.WriteString("  match " + identifierText(fn.Subject) + "\n")
	for _, r := range fn.Rules {
		for _, comment := range r.Comments {
			out.WriteString("    " + comment + "\n")
		}
		if r.Name != "" {
			out.WriteString("    @" + identifierText(r.Name) + "\n")
		}
		out.WriteString("    " + render(r.Match, 4))
		for i, c := range r.Clauses {
			if i == 0 {
				out.WriteString("\n      when ")
			} else {
				out.WriteString("\n      , ")
			}
			if c.Name != "" {
				out.WriteString("let " + identifierText(c.Name) + " = ")
			}
			if c.Pattern != nil {
				out.WriteString(render(c.Pattern, 6) + " <- ")
			}
			out.WriteString(render(c.Value, 6))
		}
		out.WriteString(" ->\n      " + render(r.Build, 6) + "\n")
		if len(r.Where) > 0 {
			out.WriteString("      where\n")
			for _, c := range r.Where {
				out.WriteString("        " + identifierText(c.Name) + " = " + render(c.Value, 8) + "\n")
			}
		}
	}
}

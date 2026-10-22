package syntax

import (
	"fmt"
	"hash/fnv"
)

func (p *parser) declaration(file *File, comments []string) {
	pos := p.peek().Position
	switch {
	case p.take("host"):
		p.hostDeclarations(file, comments)
	case p.at("type") || p.at("view") || p.at("exhaustive"):
		p.schema(file, comments)
	case p.take("signature"):
		api := &API{Position: pos, Mode: p.name(), Comments: comments}
		if api.Mode == "total" {
			p.expect("=")
			api.Fallback = p.expression(0, false, 0)
		} else if api.Mode != "partial" {
			p.fail("expected partial or total signature")
		}
		file.Items = append(file.Items, Item{API: api})
		p.lineEnd()
	case p.take("pattern"):
		pattern := &Pattern{Position: pos, Name: p.name(), Comments: comments}
		for !p.end() && !p.at("=") {
			pattern.Parameters = append(pattern.Parameters, p.name())
		}
		p.expect("=")
		pattern.Body = p.expression(0, true, 0)
		file.Items = append(file.Items, Item{Pattern: pattern})
		p.lineEnd()
	case p.take("derive"):
		p.derive(file, pos, comments)
	default:
		p.equation(file, comments)
	}
}

func (p *parser) schema(file *File, comments []string) {
	pos := p.peek().Position
	exhaustive := p.take("exhaustive")
	view := p.take("view")
	if !view {
		p.expect("type")
	}
	name := p.name()
	p.expect("=")
	var tuple []string
	host := ""
	if p.take("(") {
		tuple = append(tuple, p.hostType())
		for p.take(",") {
			tuple = append(tuple, p.hostType())
		}
		p.expect(")")
	} else {
		if p.take("*") {
			host = "*"
		}
		host += p.path()
	}
	// Empty products need no artificial field or empty block.
	p.lineEnd()
	if p.end() || p.peek().indent == 0 {
		if exhaustive || len(tuple) > 0 {
			p.fail("union schema requires a tag and variants")
			return
		}
		file.Items = append(file.Items, Item{Record: &Record{Position: pos, Name: name, GoType: host, View: view, Comments: comments}})
		return
	}
	level := p.peek().indent
	if p.take("tag") {
		union := &Union{Position: pos, Name: name, GoType: host, Types: tuple, Tag: p.storage(), Exhaustive: exhaustive, Comments: comments}
		if view {
			p.fail("view cannot declare a union")
		}
		p.unionVariants(union, level)
		file.Items = append(file.Items, Item{Union: union})
		return
	}
	if exhaustive || len(tuple) > 0 {
		p.fail("union schema requires tag before variants")
	}
	record := &Record{Position: pos, Name: name, GoType: host, View: view, Comments: comments}
	for !p.end() && p.peek().indent == level {
		record.Fields = append(record.Fields, p.field())
		p.lineEnd()
	}
	file.Items = append(file.Items, Item{Record: record})
}

func (p *parser) field() UnionField {
	field := UnionField{Position: p.peek().Position, Name: p.name()}
	field.GoName = field.Name
	field.Implicit = !p.take(":")
	if !field.Implicit {
		field.GoName = p.storage()
	}
	return field
}

func (p *parser) derive(file *File, pos Position, comments []string) {
	derive := &Derivation{Position: pos, Record: p.name(), Comments: comments}
	level := p.block(0)
	for !p.end() && p.peek().indent == level {
		op := &DerivedOperation{Position: p.peek().Position, Kind: p.name(), Function: p.name()}
		p.expect(":")
		op.Signature = p.name()
		p.lineEnd()
		if !p.end() && p.peek().indent > level {
			fields := p.peek().indent
			for !p.end() && p.peek().indent == fields {
				field := DerivedField{Position: p.peek().Position, Name: p.name(), Policy: "keep"}
				if p.take(":") {
					prefix := ""
					if p.take("@") {
						prefix = "$"
					}
					field.Policy = prefix + p.path()
				}
				op.Fields = append(op.Fields, field)
				p.lineEnd()
			}
		}
		derive.Operations = append(derive.Operations, op)
	}
	file.Items = append(file.Items, Item{Derivation: derive})
}

func (p *parser) equation(file *File, comments []string) {
	pos := p.peek().Position
	label := ""
	if p.take("@") {
		label = p.name()
		p.lineEnd()
	}
	family := p.name()
	if p.family != "" && p.family != family {
		p.fail("equations in a file must use one family")
	}
	p.family = family
	match := p.expression(0, true, 0)
	if label == "" {
		hash := fnv.New32a()
		_, _ = hash.Write([]byte(pos.File))
		label = fmt.Sprintf("%s_L%d_F%x", family, pos.Line, hash.Sum32())
	}
	ordinal := 0
	var rules []*Rule
	for !p.end() {
		p.continuation(0)
		rule := &Rule{Position: pos, Name: label, Family: family, Match: match, Comments: comments}
		if ordinal > 0 {
			rule.Name += "_" + itoa(ordinal+1)
			rule.Comments = nil
			rule.Continued = true
		}
		if p.take("|") {
			rule.Clauses = p.qualifiers()
		} else if ordinal > 0 {
			break
		}
		p.expect("=")
		rule.Build = p.expression(0, false, 0)
		file.Items = append(file.Items, Item{Rule: rule})
		rules = append(rules, rule)
		ordinal++
		p.continuation(0)
		if !p.at("|") {
			break
		}
	}
	if p.take("where") {
		where := p.whereBindings()
		for _, rule := range rules {
			rule.Where = where
		}
	}
	p.lineEnd()
}

// qualifiers reads one guard's comma-separated lets, pattern guards and guards.
func (p *parser) qualifiers() []Clause {
	var clauses []Clause
	for !p.end() {
		p.continuation(0)
		clause := Clause{}
		if p.take("let") {
			clause.Name = p.name()
			p.expect("=")
		} else if p.clauseBinds() {
			clause.Pattern = p.expression(0, true, 0)
			p.expect("<-")
		}
		clause.Value = p.expression(0, false, 0)
		if clause.Name != "" || clause.Pattern != nil {
			clauses = append(clauses, clause)
		} else if !clause.Value.Bare || clause.Value.Name != "otherwise" {
			// A top-level conjunction is a sequence of ordinary guards.
			clauses = append(clauses, conjuncts(clause.Value)...)
		}
		if !p.take(",") {
			break
		}
	}
	return clauses
}

// conjuncts splits a top-level && into ordered guards; nested ones remain.
func conjuncts(x *Expr) []Clause {
	if x.Kind == Logic && x.Name == "&&" {
		return append(conjuncts(x.Args[0]), conjuncts(x.Args[1])...)
	}
	return []Clause{{Value: x}}
}

// clauseBinds reports whether the guard qualifier at the cursor is a pattern
// guard: a <- before the qualifier's terminating comma or equals sign.
func (p *parser) clauseBinds() bool {
	nesting := 0
	for i := p.index; i < len(p.tokens); i++ {
		switch p.tokens[i].text {
		case "(", "[":
			nesting++
		case ")", "]":
			nesting--
		case "<-":
			if nesting == 0 {
				return true
			}
		case ",", "=":
			if nesting == 0 {
				return false
			}
		}
	}
	return false
}

// whereBindings reads one inline binding or an indented block of them.
func (p *parser) whereBindings() []Clause {
	var where []Clause
	if !p.at("\n") {
		name := p.name()
		p.expect("=")
		return append(where, Clause{Name: name, Value: p.expression(0, false, 0)})
	}
	level := p.block(0)
	for !p.end() && p.peek().indent == level {
		name := p.name()
		p.expect("=")
		where = append(where, Clause{Name: name, Value: p.expression(level, false, 0)})
		if !p.at("\n") {
			break
		}
		before, comments := p.index, len(p.comments)
		p.skipLines()
		if p.end() || p.peek().indent != level {
			p.index, p.comments = before, p.comments[:comments]
			break
		}
	}
	return where
}

func (p *parser) hostType() string {
	prefix := ""
	if p.take("*") {
		prefix = "*"
	}
	return prefix + p.path()
}

func (p *parser) unionVariants(union *Union, level int) {
	p.lineEnd()
	for !p.end() && p.peek().indent == level {
		variant := &Variant{Position: p.peek().Position, Name: p.name()}
		variant.GoTag = variant.Name
		variant.OmitFields = !p.take("(")
		if !variant.OmitFields {
			p.grouped++
			p.skipLines()
			for !p.end() && !p.at(")") {
				variant.Fields = append(variant.Fields, p.field())
				p.skipLines()
				if !p.take(",") {
					break
				}
				p.skipLines()
			}
			p.expect(")")
			p.grouped--
		}
		variant.Implicit = !p.take("=")
		if !variant.Implicit {
			variant.GoTag = p.path()
		}
		union.Variants = append(union.Variants, variant)
		p.lineEnd()
	}
}

func (p *parser) hostDeclarations(file *File, comments []string) {

	level := p.block(0)
	for !p.end() && p.peek().indent == level {
		comments = append(comments, p.comments...)
		p.comments = nil
		binding := &Binding{Position: p.peek().Position, Kind: "value", Name: p.name(), Comments: comments}
		comments = nil
		if p.take("?") {
			binding.Kind = "match"
		} else if p.take("!") {
			binding.Kind = "query"
		}
		for !p.end() && !p.at("=") && !p.at("\n") {
			binding.Parameters = append(binding.Parameters, p.name())
		}
		binding.GoName = binding.Name
		binding.Implicit = !p.take("=")
		if binding.Implicit && len(binding.Parameters) > 0 {
			p.fail("forwarding parameters require an explicit body")
		}
		if !binding.Implicit {
			p.continuation(level)
			if len(binding.Parameters) > 0 {
				binding.Body = p.expression(level, false, 0)
				binding.GoName = binding.Body.Name
			} else {
				binding.GoName = p.path()
			}
		}
		file.Items = append(file.Items, Item{Binding: binding})
		p.lineEnd()
	}
}

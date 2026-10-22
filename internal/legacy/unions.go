package legacy

import (
	"text/scanner"
)

func (p *parser) unionDeclaration(position Position, comments []string) *Union {
	union := &Union{Position: position, Name: p.identifier(), Comments: comments}
	p.rule = union.Name
	p.expect("=")
	if p.text == "(" {
		p.next()
		for p.failure == nil {
			union.Types = append(union.Types, p.goType())
			if p.text != "," {
				break
			}
			p.next()
		}
		p.expect(")")
	} else {
		union.GoType = p.identifier()
	}
	p.expect("tag")
	union.Tag = p.storageName()
	p.expect("{")
	for p.text != "}" && p.token != scanner.EOF && p.failure == nil {
		variant := &Variant{Position: p.position, Name: p.identifier()}
		variant.GoTag = variant.Name
		variant.Implicit = p.text != "="
		if !variant.Implicit {
			p.next()
			variant.GoTag = p.identifier()
		}
		variant.OmitFields = p.text == ";"
		if !variant.OmitFields {
			p.expect("{")
			for p.text != "}" && p.token != scanner.EOF && p.failure == nil {
				variant.Fields = append(variant.Fields, p.storageField())
			}
			p.expect("}")
		}
		p.expect(";")
		union.Variants = append(union.Variants, variant)
	}
	p.expect("}")
	p.expect(";")
	union.Comments = append(union.Comments, p.comments...)
	p.comments = nil
	p.rule = ""
	return union
}

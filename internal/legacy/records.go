package legacy

import (
	"text/scanner"
)

func (p *parser) recordDeclaration(position Position, comments []string) *Record {
	record := &Record{Position: position, Name: p.identifier(), Comments: comments}
	p.rule = record.Name
	p.expect("=")
	record.GoType = p.goType()
	p.expect("{")
	for p.text != "}" && p.token != scanner.EOF && p.failure == nil {
		record.Fields = append(record.Fields, p.storageField())
	}
	p.expect("}")
	p.expect(";")
	record.Comments = append(record.Comments, p.comments...)
	p.comments = nil
	p.rule = ""
	return record
}

func (p *parser) storageField() UnionField {
	field := UnionField{Position: p.position, Name: p.identifier()}
	field.GoName = field.Name
	field.Implicit = p.text != ":"
	if !field.Implicit {
		p.next()
		field.GoName = p.storageName()
	}
	p.expect(";")
	return field
}

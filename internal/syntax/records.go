package syntax

import (
	"fmt"
	"strings"
	"text/scanner"
)

func (p *parser) recordDeclaration(position Position, comments []string) *Record {
	record := &Record{Position: position, Name: p.identifier(), Comments: comments}
	p.rule = record.Name
	p.expect("=")
	record.GoType = p.identifier()
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

func formatRecord(record *Record) string {
	var output strings.Builder
	fmt.Fprintf(&output, "record %s = %s {\n", record.Name, record.GoType)
	for _, field := range record.Fields {
		output.WriteString("  " + formatStorageField(field))
	}
	output.WriteString("};\n")
	return output.String()
}

func (p *parser) storageField() UnionField {
	field := UnionField{Position: p.position, Name: p.identifier()}
	field.GoName = field.Name
	field.Implicit = p.text != ":"
	if !field.Implicit {
		p.next()
		field.GoName = p.identifier()
	}
	p.expect(";")
	return field
}

func formatStorageField(field UnionField) string {
	if field.Implicit {
		return field.Name + ";\n"
	}
	return field.Name + ": " + field.GoName + ";\n"
}

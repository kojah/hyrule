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
		field := UnionField{Position: p.position, Name: p.identifier()}
		p.expect(":")
		field.GoName = p.identifier()
		p.expect(";")
		record.Fields = append(record.Fields, field)
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
		fmt.Fprintf(&output, "  %s: %s;\n", field.Name, field.GoName)
	}
	output.WriteString("};\n")
	return output.String()
}

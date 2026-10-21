package syntax

import (
	"fmt"
	"strings"
	"text/scanner"
)

func (p *parser) unionDeclaration(position Position, comments []string) *Union {
	union := &Union{Position: position, Name: p.identifier(), Comments: comments}
	p.rule = union.Name
	p.expect("=")
	union.GoType = p.identifier()
	p.expect("tag")
	union.Tag = p.identifier()
	p.expect("{")
	for p.text != "}" && p.token != scanner.EOF && p.failure == nil {
		variant := &Variant{Position: p.position, Name: p.identifier()}
		p.expect("=")
		variant.GoTag = p.identifier()
		p.expect("{")
		for p.text != "}" && p.token != scanner.EOF && p.failure == nil {
			field := UnionField{Position: p.position, Name: p.identifier()}
			p.expect(":")
			field.GoName = p.identifier()
			p.expect(";")
			variant.Fields = append(variant.Fields, field)
		}
		p.expect("}")
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

func formatUnion(union *Union) string {
	var output strings.Builder
	if union.Exhaustive {
		output.WriteString("exhaustive ")
	}
	fmt.Fprintf(&output, "union %s = %s tag %s {\n", union.Name, union.GoType, union.Tag)
	for _, variant := range union.Variants {
		fmt.Fprintf(&output, "  %s = %s {", variant.Name, variant.GoTag)
		if len(variant.Fields) > 0 {
			output.WriteByte('\n')
		}
		for _, field := range variant.Fields {
			fmt.Fprintf(&output, "    %s: %s;\n", field.Name, field.GoName)
		}
		if len(variant.Fields) > 0 {
			output.WriteString("  ")
		}
		output.WriteString("};\n")
	}
	output.WriteString("};\n")
	return output.String()
}

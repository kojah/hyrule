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

func formatUnion(union *Union) string {
	var output strings.Builder
	if union.Exhaustive {
		output.WriteString("exhaustive ")
	}
	storage := union.GoType
	if len(union.Types) > 0 {
		storage = "(" + strings.Join(union.Types, ", ") + ")"
	}
	fmt.Fprintf(&output, "union %s = %s tag %s {\n", union.Name, storage, union.Tag)
	for _, variant := range union.Variants {
		fmt.Fprintf(&output, "  %s", variant.Name)
		if !variant.Implicit {
			fmt.Fprintf(&output, " = %s", variant.GoTag)
		}
		if variant.OmitFields {
			output.WriteString(";\n")
			continue
		}
		output.WriteString(" {")
		if len(variant.Fields) > 0 {
			output.WriteByte('\n')
		}
		for _, field := range variant.Fields {
			output.WriteString("    " + formatStorageField(field))
		}
		if len(variant.Fields) > 0 {
			output.WriteString("  ")
		}
		output.WriteString("};\n")
	}
	output.WriteString("};\n")
	return output.String()
}

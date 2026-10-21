package syntax

import (
	"fmt"
	"strings"
	"text/scanner"
)

// Derivation requests explicit operations over one complete record schema.
// Field policies are language-level names or host hooks resolved by a backend.
type Derivation struct {
	Position
	Record     string
	Operations []*DerivedOperation
	Comments   []string
}

// DerivedOperation names a generated function and its explicit host signature.
type DerivedOperation struct {
	Position
	Kind, Function, Signature string
	Fields                    []DerivedField
}

// DerivedField assigns a policy to one logical schema field, including omission.
type DerivedField struct {
	Position
	Name, Policy string
}

func (p *parser) derivation(position Position, comments []string) *Derivation {
	result := &Derivation{Position: position, Record: p.identifier(), Comments: comments}
	p.expect("{")
	for p.text != "}" && p.token != scanner.EOF && p.failure == nil {
		operation := &DerivedOperation{Position: p.position, Kind: p.identifier(), Function: p.identifier()}
		p.expect("=")
		operation.Signature = p.identifier()
		p.expect("{")
		for p.text != "}" && p.token != scanner.EOF && p.failure == nil {
			field := DerivedField{Position: p.position, Name: p.identifier(), Policy: "keep"}
			if p.text == ":" {
				p.next()
				field.Policy = p.hookName()
			}
			p.expect(";")
			operation.Fields = append(operation.Fields, field)
		}
		p.expect("}")
		p.expect(";")
		result.Operations = append(result.Operations, operation)
	}
	p.expect("}")
	p.expect(";")
	result.Comments = append(result.Comments, p.comments...)
	p.comments = nil
	return result
}

func formatDerivation(derive *Derivation) string {
	var out strings.Builder
	fmt.Fprintf(&out, "derive %s {\n", derive.Record)
	for _, operation := range derive.Operations {
		fmt.Fprintf(&out, "  %s %s = %s {\n", operation.Kind, operation.Function, operation.Signature)
		for _, field := range operation.Fields {
			fmt.Fprintf(&out, "    %s: %s;\n", field.Name, field.Policy)
		}
		out.WriteString("  };\n")
	}
	out.WriteString("};\n")
	return out.String()
}

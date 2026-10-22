package legacy

import (
	"text/scanner"
)

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
				if p.text == "$" {
					p.next()
					field.Policy = "$" + p.identifier()
				} else {
					field.Policy = p.hookName()
				}
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

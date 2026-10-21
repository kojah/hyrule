package compiler

import "github.com/kojah/lockdoc/tools/hyrule/internal/syntax"

// ValidateDerivations accounts for every field before target type resolution.
// Explicit omission belongs to identity/child policies, never silent schema drift.
func (p *Program) ValidateDerivations(function string) error {
	names := map[string]bool{function: true}
	for _, derive := range p.Derivations {
		record := p.RecordNames[derive.Record]
		if record == nil || record.View {
			return &syntax.Diagnostic{Position: derive.Position, Message: "derive requires a complete record schema: " + derive.Record}
		}
		fields := map[string]bool{}
		for _, field := range record.Fields {
			fields[field.Name] = true
		}
		for _, op := range derive.Operations {
			switch op.Kind {
			case "freeze", "key", "equal", "visit", "transform", "display", "store", "load":
			default:
				return &syntax.Diagnostic{Position: op.Position, Message: "unknown derivation operation " + op.Kind}
			}
			if names[op.Function] {
				return &syntax.Diagnostic{Position: op.Position, Message: "duplicate generated function " + op.Function}
			}
			names[op.Function] = true
			if op.Kind == "load" {
				if len(op.Fields) != 0 {
					return &syntax.Diagnostic{Position: op.Position, Message: "load derives its layout from the store signature and takes no field policies"}
				}
				continue
			}
			seen := map[string]bool{}
			for _, field := range op.Fields {
				if !fields[field.Name] || seen[field.Name] {
					return &syntax.Diagnostic{Position: field.Position, Message: "unknown or duplicate derived field " + field.Name}
				}
				seen[field.Name] = true
			}
			for _, field := range record.Fields {
				if !seen[field.Name] {
					return &syntax.Diagnostic{Position: op.Position, Message: "derivation omits field policy for " + field.Name}
				}
			}
		}
	}
	return nil
}

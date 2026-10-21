package compiler

import "github.com/kojah/lockdoc/tools/hyrule/internal/syntax"

func (program *Program) collectUnions() error {
	names := map[string]struct{}{}
	for _, union := range program.Unions {
		if _, duplicate := names[union.Name]; duplicate {
			return &syntax.Diagnostic{Position: union.Position, Message: "duplicate union " + union.Name}
		}
		names[union.Name] = struct{}{}
		if len(union.Variants) == 0 {
			return &syntax.Diagnostic{Position: union.Position, Message: "union requires at least one variant"}
		}
		for _, variant := range union.Variants {
			if reservedOperation(variant.Name) || program.Constructors[variant.Name] != nil || program.Bindings[variant.Name] != nil || program.patterns[variant.Name] != nil {
				return &syntax.Diagnostic{Position: variant.Position, Message: "duplicate or colliding constructor " + variant.Name}
			}
			program.Constructors[variant.Name] = variant
		}
	}
	return nil
}

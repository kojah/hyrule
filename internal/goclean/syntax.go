package goclean

import "go/ast"

// fieldNames identifies Go spelling that denotes fields rather than locals.
// Parser object resolution alone cannot distinguish a keyed struct field from
// a map key. Explicit maps and arrays retain expression keys; named literals keep their
// field spelling unless the embedded-Go boundary has proved otherwise.
func fieldNames(function *ast.FuncDecl) map[*ast.Ident]bool {
	protected := map[*ast.Ident]bool{}
	ast.Inspect(function, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch value := node.(type) {

		case *ast.CompositeLit:
			switch value.Type.(type) {
			case *ast.MapType, *ast.ArrayType:
				return true
			}
			for _, element := range value.Elts {
				if pair, ok := element.(*ast.KeyValueExpr); ok {
					if name, ok := pair.Key.(*ast.Ident); ok {
						protected[name] = true
					}
				}
			}
		case *ast.SelectorExpr:
			protected[value.Sel] = true
		case *ast.StructType:
			for _, field := range value.Fields.List {
				for _, name := range field.Names {
					protected[name] = true
				}
			}
		}
		return true
	})
	return protected
}

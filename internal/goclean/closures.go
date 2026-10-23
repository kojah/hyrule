package goclean

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"

	"golang.org/x/tools/go/ast/astutil"
)

// inlineResults removes expression-only embedded-Go wrappers whose result has
// exactly the declared type. Parameter copies stay when their address, methods,
// or nested closures could make storage identity observable.
func inlineResults(function *ast.FuncDecl) {
	astutil.Apply(function.Body, func(cursor *astutil.Cursor) bool {
		call, ok := cursor.Node().(*ast.CallExpr)
		if !ok {
			_, closure := cursor.Node().(*ast.FuncLit)
			return !closure
		}
		closure, ok := call.Fun.(*ast.FuncLit)
		if !ok || len(closure.Body.List) != 1 || closure.Type.Results == nil || len(closure.Type.Results.List) != 1 {
			return true
		}
		returned, ok := closure.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(returned.Results) != 1 {
			return false
		}
		parameters := map[*ast.Object]ast.Expr{}
		types := map[*ast.Object]ast.Expr{}
		index := 0
		for _, field := range closure.Type.Params.List {
			for _, name := range field.Names {
				if index >= len(call.Args) {
					return false
				}
				argument, ok := call.Args[index].(*ast.Ident)
				if !ok || argument.Obj == nil {
					return false
				}
				parameters[name.Obj] = argument
				types[name.Obj] = field.Type
				index++
			}
		}
		if index != len(call.Args) {
			return false
		}
		expression := returned.Results[0]
		var actual ast.Expr
		switch value := expression.(type) {
		case *ast.CompositeLit:
			actual = value.Type
		case *ast.Ident:
			actual = types[value.Obj]
		}
		if actual == nil || !sameType(actual, closure.Type.Results.List[0].Type) {
			return false
		}
		safe := true
		fields := map[*ast.Ident]bool{}
		ast.Inspect(expression, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.CompositeLit:
				switch value.Type.(type) {
				case *ast.MapType, *ast.ArrayType:
					return true
				}
				_, explicit := value.Type.(*ast.StructType)
				for _, element := range value.Elts {
					if pair, ok := element.(*ast.KeyValueExpr); ok {
						if key, ok := pair.Key.(*ast.Ident); ok {
							fields[key] = true
							// A named literal could be a map. With only syntax, a key sharing
							// a parameter's name cannot safely be classified as a field.
							if !explicit && parameters[key.Obj] != nil {
								safe = false
							}
						}
					}
				}
			case *ast.FuncLit:
				safe = false
			case *ast.UnaryExpr:
				if value.Op == token.AND {
					safe = false
				}
			case *ast.CallExpr:
				if selector, ok := value.Fun.(*ast.SelectorExpr); ok {
					ast.Inspect(selector.X, func(child ast.Node) bool {
						if name, ok := child.(*ast.Ident); ok && parameters[name.Obj] != nil {
							safe = false
						}
						return true
					})
				}
			}
			return safe
		})
		if !safe {
			return false
		}
		replacement := astutil.Apply(expression, func(inner *astutil.Cursor) bool {
			if id, ok := inner.Node().(*ast.Ident); ok {
				if parameters[id.Obj] != nil && !fields[id] {
					copy := *parameters[id.Obj].(*ast.Ident)
					copy.NamePos = id.NamePos
					inner.Replace(&copy)
				} else {
					// All Hyrule captures are explicit wrapper parameters. Remaining
					// names belong to the host, even if parser resolution linked one
					// to a coincidentally named private emitter local outside the wrapper.
					id.Obj = nil
				}
			}
			return true
		}, nil).(ast.Expr)
		cursor.Replace(replacement)
		return false
	}, nil)
}

func sameType(left, right ast.Expr) bool {
	var a, b bytes.Buffer
	if format.Node(&a, token.NewFileSet(), left) != nil || format.Node(&b, token.NewFileSet(), right) != nil {
		return false
	}
	return a.String() == b.String()
}

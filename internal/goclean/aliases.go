package goclean

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/ast/astutil"
)

// aliases substitutes copies of immutable local values only. Field reads keep
// their snapshot position; mutable or address-exposed values keep their storage.
func aliases(function *ast.FuncDecl) {
	changed := map[*ast.Object]bool{}
	protected := fieldNames(function)
	var root func(ast.Expr) *ast.Object
	root = func(expression ast.Expr) *ast.Object {
		switch value := expression.(type) {
		case *ast.Ident:
			return value.Obj
		case *ast.SelectorExpr:
			return root(value.X)
		case *ast.IndexExpr:
			return root(value.X)
		case *ast.ParenExpr:
			return root(value.X)
		}
		return nil
	}
	ast.Inspect(function, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.AssignStmt:
			if value.Tok != token.DEFINE {
				for _, lhs := range value.Lhs {
					changed[root(lhs)] = true
				}
			}
		case *ast.IncDecStmt:
			changed[root(value.X)] = true
		case *ast.UnaryExpr:
			if value.Op == token.AND {
				changed[root(value.X)] = true
			}
		}
		return true
	})
	substitutions := map[*ast.Object]*ast.Ident{}
	discarded := map[ast.Stmt]bool{}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || assignment.Tok != token.DEFINE || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			return true
		}
		name, nok := assignment.Lhs[0].(*ast.Ident)
		source, sok := assignment.Rhs[0].(*ast.Ident)
		if !nok || !sok || name.Obj == nil || source.Obj == nil || changed[name.Obj] || changed[source.Obj] {
			return true
		}
		if !private(name.Name) {
			return true
		}
		substitutions[name.Obj] = source
		discarded[assignment] = true
		return true
	})
	var remove func([]ast.Stmt) []ast.Stmt
	remove = func(list []ast.Stmt) []ast.Stmt {
		var kept []ast.Stmt
		for _, statement := range list {
			if !discarded[statement] {
				children(statement, remove)
				kept = append(kept, statement)
			}
		}
		return kept
	}
	function.Body.List = remove(function.Body.List)
	astutil.Apply(function.Body, func(cursor *astutil.Cursor) bool {
		if _, closure := cursor.Node().(*ast.FuncLit); closure {
			return false
		}
		id, ok := cursor.Node().(*ast.Ident)
		if !ok || protected[id] {
			return true
		}
		target := id
		for substitutions[target.Obj] != nil {
			target = substitutions[target.Obj]
		}
		if target != id {
			copy := *target
			copy.NamePos = id.NamePos
			cursor.Replace(&copy)
		}
		return true
	}, nil)
}

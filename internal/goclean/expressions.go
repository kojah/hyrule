package goclean

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/ast/astutil"
)

// expressions leaves embedded Go bodies intact. Go's printer restores any
// parentheses required by operator precedence after generated grouping is gone.
func expressions(function *ast.FuncDecl) {
	astutil.Apply(function.Body, func(cursor *astutil.Cursor) bool {
		if _, ok := cursor.Node().(*ast.FuncLit); ok {
			return false
		}
		return true
	}, func(cursor *astutil.Cursor) bool {
		switch node := cursor.Node().(type) {
		case *ast.ParenExpr:
			switch node.X.(type) {
			case *ast.BinaryExpr, *ast.UnaryExpr, *ast.Ident, *ast.BasicLit:
				cursor.Replace(node.X)
			}
		case *ast.CompositeLit:
			if collection, ok := node.Type.(*ast.ArrayType); ok {
				for _, element := range node.Elts {
					if pair, ok := element.(*ast.KeyValueExpr); ok {
						element = pair.Value
					}
					if literal, ok := element.(*ast.CompositeLit); ok && literal.Type != nil && sameType(literal.Type, collection.Elt) {
						literal.Type = nil
					}
				}
			}
		case *ast.IfStmt:
			node.Cond = booleanCondition(node.Cond)
		}
		return true
	})
}

func booleanCondition(expression ast.Expr) ast.Expr {
	binary, ok := expression.(*ast.BinaryExpr)
	if !ok {
		return expression
	}
	if binary.Op == token.LAND || binary.Op == token.LOR {
		binary.X = booleanCondition(binary.X)
		binary.Y = booleanCondition(binary.Y)
		return binary
	}
	if binary.Op != token.EQL && binary.Op != token.NEQ {
		return expression
	}
	literal, ok := binary.Y.(*ast.Ident)
	if !ok || literal.Obj != nil || literal.Name != "true" && literal.Name != "false" {
		return expression
	}
	if (literal.Name == "true") == (binary.Op == token.EQL) {
		return binary.X
	}
	return &ast.UnaryExpr{OpPos: binary.Pos(), Op: token.NOT, X: binary.X}
}

package goclean

import (
	"go/ast"
	"go/token"
)

func statements(list []ast.Stmt) []ast.Stmt {
	var result []ast.Stmt
	for _, statement := range list {
		children(statement, statements)
		if block, ok := statement.(*ast.BlockStmt); ok {
			result = append(result, block.List...)
			continue
		}
		if branch, ok := statement.(*ast.IfStmt); ok {
			for branch.Init == nil && branch.Else == nil && len(branch.Body.List) == 1 {
				inner, ok := branch.Body.List[0].(*ast.IfStmt)
				if !ok || inner.Init != nil || inner.Else != nil {
					break
				}
				branch.Cond = &ast.BinaryExpr{X: branch.Cond, Op: token.LAND, Y: inner.Cond}
				branch.Body = inner.Body
			}
		}
		result = append(result, statement)
	}
	return result
}

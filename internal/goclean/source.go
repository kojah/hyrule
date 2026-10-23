// Package goclean renders generated Go with hygienic names and local structural
// simplifications. It never moves computations across statements or callbacks.
package goclean

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"golang.org/x/tools/go/ast/astutil"
)

// Source cleans a generated file. Suggestions associate private emitter names
// with source captures; reserved includes host package and import identifiers.
func Source(source []byte, suggestions map[string]string, reserved []string) ([]byte, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "generated.go", source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		unpackInputs(function)
		aliases(function)
		removePlaceholders(function)
		function.Body.List = statements(function.Body.List)
		scalarResults(function)
		function.Body.List = statements(function.Body.List)
		inlineResults(function)
		expressions(function)
		rename(function, suggestions, reserved)
	}
	var output bytes.Buffer
	if err := format.Node(&output, files, file); err != nil {
		return nil, err
	}
	return format.Source(layout(output.Bytes()))
}

func removePlaceholders(function *ast.FuncDecl) {

	uses := map[*ast.Object]bool{}
	protected := fieldNames(function)
	astutil.Apply(function, func(cursor *astutil.Cursor) bool {
		if _, closure := cursor.Node().(*ast.FuncLit); closure {
			return false
		}
		id, ok := cursor.Node().(*ast.Ident)
		if !ok || id.Obj == nil || protected[id] {
			return true
		}
		switch parent := cursor.Parent().(type) {
		case *ast.Field:
			for _, name := range parent.Names {
				if name == id {
					return true
				}
			}
		case *ast.ValueSpec:
			for _, name := range parent.Names {
				if name == id {
					return true
				}
			}
		case *ast.AssignStmt:
			if placeholder(parent, id.Obj) {
				return true
			}
			for _, lhs := range parent.Lhs {
				if lhs == id {
					return true
				}
			}
		}
		uses[id.Obj] = true
		return true
	}, nil)
	var clean func([]ast.Stmt) []ast.Stmt
	clean = func(list []ast.Stmt) []ast.Stmt {
		var result []ast.Stmt
		for _, statement := range list {
			if assignment, ok := statement.(*ast.AssignStmt); ok && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 {
				blank, lhs := assignment.Lhs[0].(*ast.Ident)
				id, rhs := assignment.Rhs[0].(*ast.Ident)
				if lhs && rhs && blank.Name == "_" && id.Obj != nil && (uses[id.Obj] || parameter(id.Obj)) {
					continue
				}
			}
			children(statement, clean)
			result = append(result, statement)
		}
		return result
	}
	function.Body.List = clean(function.Body.List)
}

// children visits statement lists, deliberately excluding embedded Go closures.
func children(statement ast.Stmt, visit func([]ast.Stmt) []ast.Stmt) {
	switch node := statement.(type) {
	case *ast.BlockStmt:
		node.List = visit(node.List)
	case *ast.IfStmt:
		node.Body.List = visit(node.Body.List)
		if node.Else != nil {
			children(node.Else, visit)
		}
	case *ast.SwitchStmt:
		node.Body.List = visit(node.Body.List)
	case *ast.TypeSwitchStmt:
		node.Body.List = visit(node.Body.List)
	case *ast.CaseClause:
		node.Body = visit(node.Body)
	}
}

func parameter(object *ast.Object) bool {
	_, ok := object.Decl.(*ast.Field)
	return ok
}

package goclean

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// unpackInputs removes the compiler's parameter product when every use is a
// field projection. Parameters are already values, so no read or call moves.
func unpackInputs(function *ast.FuncDecl) {
	if len(function.Body.List) == 0 {
		return
	}
	assignment, ok := function.Body.List[0].(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return
	}
	name, ok := assignment.Lhs[0].(*ast.Ident)
	if !ok || name.Name != "harg0" {
		return
	}
	literal, ok := assignment.Rhs[0].(*ast.CompositeLit)
	if !ok {
		return
	}
	fields := map[string]ast.Expr{}
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok {
			return
		}
		value, ok := pair.Value.(*ast.Ident)
		if !ok || !strings.HasPrefix(value.Name, "hparam") {
			return
		}
		fields[key.Name] = value
	}
	valid := true
	for _, statement := range function.Body.List[1:] {
		if placeholder(statement, name.Obj) {
			continue
		}
		astutil.Apply(statement, func(cursor *astutil.Cursor) bool {
			if _, closure := cursor.Node().(*ast.FuncLit); closure {
				return false
			}
			id, ok := cursor.Node().(*ast.Ident)
			if !ok || id.Obj != name.Obj {
				return true
			}
			selector, ok := cursor.Parent().(*ast.SelectorExpr)
			if !ok || selector.X != id || fields[selector.Sel.Name] == nil {
				valid = false
			}
			return true
		}, nil)
	}
	if !valid {
		return
	}
	var body []ast.Stmt
	for _, statement := range function.Body.List[1:] {
		if placeholder(statement, name.Obj) {
			continue
		}
		astutil.Apply(statement, func(cursor *astutil.Cursor) bool {
			if _, closure := cursor.Node().(*ast.FuncLit); closure {
				return false
			}
			selector, ok := cursor.Node().(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := selector.X.(*ast.Ident)
			if ok && id.Obj == name.Obj {
				value := *fields[selector.Sel.Name].(*ast.Ident)
				value.NamePos = selector.Pos()
				cursor.Replace(&value)
				return false
			}
			return true
		}, nil)
		body = append(body, statement)
	}
	function.Body.List = body
}

func placeholder(statement ast.Stmt, object *ast.Object) bool {
	assignment, ok := statement.(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return false
	}
	lhs, lok := assignment.Lhs[0].(*ast.Ident)
	rhs, rok := assignment.Rhs[0].(*ast.Ident)
	return lok && rok && lhs.Name == "_" && rhs.Obj == object
}

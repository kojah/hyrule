package goclean

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/ast/astutil"
)

type resultProduct struct {
	fields []*ast.Field
	locals []*ast.Ident
}

// scalarResults removes result carriers only when they never escape: every read
// must project one field and every whole write must be a matching literal.
// Explicit declarations and simultaneous assignments preserve conversions and
// evaluation order, including typed nils and multi-result branches.
func scalarResults(function *ast.FuncDecl) {
	candidates := map[*ast.Object]*resultProduct{}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch declaration := node.(type) {
		case *ast.ValueSpec:
			if len(declaration.Names) == 1 {
				addProduct(candidates, declaration.Names[0], declaration.Type)
			}
		case *ast.AssignStmt:
			if declaration.Tok == token.DEFINE && len(declaration.Lhs) == 1 && len(declaration.Rhs) == 1 {
				if name, ok := declaration.Lhs[0].(*ast.Ident); ok {
					if literal, ok := declaration.Rhs[0].(*ast.CompositeLit); ok {
						addProduct(candidates, name, literal.Type)
					}
				}
			}
		}
		return true
	})
	invalid := map[*ast.Object]bool{}
	links := map[*ast.Object][]*ast.Object{}
	protected := fieldNames(function)
	astutil.Apply(function.Body, func(cursor *astutil.Cursor) bool {
		if _, closure := cursor.Node().(*ast.FuncLit); closure {
			return false
		}
		id, ok := cursor.Node().(*ast.Ident)
		if !ok || protected[id] || candidates[id.Obj] == nil {
			return true
		}
		product := candidates[id.Obj]
		switch parent := cursor.Parent().(type) {
		case *ast.SelectorExpr:
			if parent.X == id && product.index(parent.Sel.Name) >= 0 {
				return true
			}
		case *ast.ValueSpec:
			if len(parent.Names) == 1 && len(parent.Values) == 1 && parent.Values[0] == id {
				target := parent.Names[0].Obj
				if candidates[target] != nil && candidates[target].compatible(product) {
					links[id.Obj] = append(links[id.Obj], target)
					links[target] = append(links[target], id.Obj)
					return true
				}
			}
			if len(parent.Names) == 1 && parent.Names[0] == id && (len(parent.Values) == 0 || len(parent.Values) == 1 && product.values(parent.Values[0], candidates) != nil) {
				return true
			}

		case *ast.AssignStmt:
			if len(parent.Lhs) == 1 && len(parent.Rhs) == 1 && parent.Rhs[0] == id {
				if target, ok := parent.Lhs[0].(*ast.Ident); ok && candidates[target.Obj] != nil && candidates[target.Obj].compatible(product) {
					links[id.Obj] = append(links[id.Obj], target.Obj)
					links[target.Obj] = append(links[target.Obj], id.Obj)
					return true
				}
			}
			if len(parent.Lhs) == 1 && parent.Lhs[0] == id && len(parent.Rhs) == 1 && product.values(parent.Rhs[0], candidates) != nil {
				return true
			}
		}
		invalid[id.Obj] = true
		return true
	}, nil)

	// A copy connects both representations: if either escapes, retain the pair.
	queue := []*ast.Object{}
	for object := range invalid {
		queue = append(queue, object)
	}
	for len(queue) > 0 {
		object := queue[0]
		queue = queue[1:]
		for _, linked := range links[object] {
			if !invalid[linked] {
				invalid[linked] = true
				queue = append(queue, linked)
			}
		}
	}
	for object := range invalid {
		delete(candidates, object)
	}
	astutil.Apply(function.Body, func(cursor *astutil.Cursor) bool {
		if _, closure := cursor.Node().(*ast.FuncLit); closure {
			return false
		}
		switch node := cursor.Node().(type) {
		case *ast.SelectorExpr:
			if id, ok := node.X.(*ast.Ident); ok && candidates[id.Obj] != nil {
				product := candidates[id.Obj]
				copy := *product.locals[product.index(node.Sel.Name)]
				copy.NamePos = node.Pos()
				cursor.Replace(&copy)
				return false
			}
		case *ast.DeclStmt:
			group, ok := node.Decl.(*ast.GenDecl)
			if !ok || len(group.Specs) != 1 {
				break
			}
			spec, ok := group.Specs[0].(*ast.ValueSpec)
			if !ok || len(spec.Names) != 1 {
				break
			}
			if product := candidates[spec.Names[0].Obj]; product != nil {
				var values []ast.Expr
				if len(spec.Values) == 1 {
					values = product.values(spec.Values[0], candidates)
				}
				cursor.Replace(product.declare(node.Pos(), values))
			}
		case *ast.AssignStmt:
			if len(node.Lhs) != 1 || len(node.Rhs) != 1 {
				break
			}
			id, ok := node.Lhs[0].(*ast.Ident)
			if !ok || candidates[id.Obj] == nil {
				break
			}
			product := candidates[id.Obj]
			values := product.values(node.Rhs[0], candidates)
			if node.Tok == token.DEFINE {
				cursor.Replace(product.declare(node.Pos(), values))
			} else {
				node.Lhs = product.targets(node.Pos())
				node.Rhs = values
			}
		}
		return true
	}, nil)
}

func addProduct(products map[*ast.Object]*resultProduct, name *ast.Ident, expression ast.Expr) {
	if name.Obj == nil {
		return
	}
	record, ok := expression.(*ast.StructType)
	if !ok || len(record.Fields.List) == 0 {
		return
	}
	for index, field := range record.Fields.List {
		if len(field.Names) != 1 || field.Names[0].Name != fmt.Sprintf("V%d", index) {
			return
		}
	}
	product := &resultProduct{fields: record.Fields.List}
	for index := range product.fields {
		local := ast.NewIdent(fmt.Sprintf("%sfield%d", name.Name, index))
		local.Obj = ast.NewObj(ast.Var, local.Name)
		product.locals = append(product.locals, local)
	}
	products[name.Obj] = product
}
func (product *resultProduct) index(name string) int {
	for index, field := range product.fields {
		if field.Names[0].Name == name {
			return index
		}
	}
	return -1
}
func (product *resultProduct) values(expression ast.Expr, candidates map[*ast.Object]*resultProduct) []ast.Expr {
	if id, ok := expression.(*ast.Ident); ok {
		if source := candidates[id.Obj]; source != nil && product.compatible(source) {
			return source.targets(id.Pos())
		}
	}

	literal, ok := expression.(*ast.CompositeLit)
	if !ok || len(literal.Elts) != len(product.fields) {
		return nil
	}
	values := make([]ast.Expr, len(literal.Elts))
	for index, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return nil
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok || product.index(key.Name) != index {
			return nil
		}
		values[index] = pair.Value
	}
	return values
}
func (product *resultProduct) targets(position token.Pos) []ast.Expr {
	result := make([]ast.Expr, len(product.locals))
	for index, local := range product.locals {
		copy := *local
		copy.NamePos = position
		result[index] = &copy
	}
	return result
}
func (product *resultProduct) declare(position token.Pos, values []ast.Expr) ast.Stmt {
	var statements []ast.Stmt
	for index, field := range product.fields {
		local := *product.locals[index]
		local.NamePos = position
		spec := &ast.ValueSpec{Names: []*ast.Ident{&local}, Type: field.Type}
		local.Obj.Decl = spec
		statements = append(statements, &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, TokPos: position, Specs: []ast.Spec{spec}}})
	}
	if len(values) > 0 {
		statements = append(statements, &ast.AssignStmt{Lhs: product.targets(position), Tok: token.ASSIGN, TokPos: position, Rhs: values})
	}
	return &ast.BlockStmt{List: statements}
}

func (product *resultProduct) compatible(other *resultProduct) bool {
	if len(product.fields) != len(other.fields) {
		return false
	}
	for index, field := range product.fields {
		if !sameType(field.Type, other.fields[index].Type) {
			return false
		}
	}
	return true
}

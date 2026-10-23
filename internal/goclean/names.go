package goclean

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode"
)

func private(name string) bool {
	for _, prefix := range []string{"harg", "hparam", "hv", "hs"} {
		if suffix, ok := strings.CutPrefix(name, prefix); ok {
			suffix = strings.TrimSuffix(suffix, "answered")
			suffix = strings.Replace(suffix, "field", "part", 1)
			if head, part, found := strings.Cut(suffix, "part"); found {
				if _, err := strconv.Atoi(part); err != nil {
					continue
				}
				suffix = head
			}
			if _, err := strconv.Atoi(suffix); err == nil {
				return true
			}
		}
	}
	return name == "hok" || name == "hvalue" || name == "hresult"
}

func spelling(name string) string {
	if !token.IsIdentifier(name) || name == "_" {
		return "value"
	}
	letters := []rune(name)
	// Keep initialisms natural: HTTPRequest -> httpRequest, ID -> id.
	for index := range letters {
		if !unicode.IsUpper(letters[index]) {
			break
		}
		if index > 0 && index+1 < len(letters) && unicode.IsLower(letters[index+1]) {
			break
		}
		letters[index] = unicode.ToLower(letters[index])
	}
	result := string(letters)
	if !token.IsIdentifier(result) {
		result += "Value"
	}
	return result
}

func rename(function *ast.FuncDecl, suggestions map[string]string, reserved []string) {
	occupied := map[string]bool{}
	for _, name := range reserved {
		occupied[name] = true
	}
	hints := map[*ast.Object]string{}
	ast.Inspect(function, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		if id, ok := node.(*ast.Ident); ok && !private(id.Name) {
			occupied[id.Name] = true
		}
		if assignment, ok := node.(*ast.AssignStmt); ok && assignment.Tok == token.DEFINE && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 {
			if id, ok := assignment.Lhs[0].(*ast.Ident); ok && id.Obj != nil {
				hints[id.Obj] = expressionName(assignment.Rhs[0])
			}
		}
		if spec, ok := node.(*ast.ValueSpec); ok && len(spec.Names) == 1 {
			if len(spec.Values) == 1 {
				hints[spec.Names[0].Obj] = expressionName(spec.Values[0])
			} else {
				hints[spec.Names[0].Obj] = expressionName(spec.Type)
			}
		}
		return true
	})

	// Updates retain their copied storage and advertise that role in the name.
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || assignment.Tok != token.ASSIGN {
			return true
		}
		for _, lhs := range assignment.Lhs {
			selector, ok := lhs.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			id, ok := selector.X.(*ast.Ident)
			if !ok || id.Obj == nil {
				continue
			}
			declaration, ok := id.Obj.Decl.(*ast.AssignStmt)
			if !ok || len(declaration.Rhs) != 1 {
				continue
			}
			source, ok := declaration.Rhs[0].(*ast.Ident)
			if !ok {
				continue
			}
			hint := suggestions[source.Name]
			if hint == "" {
				hint = hints[source.Obj]
			}
			if hint != "" {
				letters := []rune(hint)
				letters[0] = unicode.ToUpper(letters[0])
				hints[id.Obj] = "updated" + string(letters)
			}
		}
		return true
	})
	state := &nameState{suggestions: suggestions, hints: hints, names: map[*ast.Object]string{}, protected: fieldNames(function)}
	ast.Walk(&nameVisitor{state: state, scope: &nameScope{used: occupied}}, function)
}

type nameScope struct {
	parent *nameScope
	used   map[string]bool
}

func (scope *nameScope) contains(name string) bool {
	for current := scope; current != nil; current = current.parent {
		if current.used[name] {
			return true
		}
	}
	return false
}

type nameState struct {
	suggestions map[string]string
	hints       map[*ast.Object]string
	names       map[*ast.Object]string
	protected   map[*ast.Ident]bool
}
type nameVisitor struct {
	state *nameState
	scope *nameScope
}

func (visitor *nameVisitor) Visit(node ast.Node) ast.Visitor {
	if _, closure := node.(*ast.FuncLit); closure {
		return nil
	}
	if node == nil {
		return nil
	}
	switch node.(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.FuncLit:
		visitor = &nameVisitor{state: visitor.state, scope: &nameScope{parent: visitor.scope, used: map[string]bool{}}}
	}
	id, ok := node.(*ast.Ident)
	if !ok || id.Obj == nil || visitor.state.protected[id] || !private(id.Name) {
		return visitor
	}
	state := visitor.state
	name, known := state.names[id.Obj]
	if !known {
		hint := state.suggestions[id.Name]
		if strings.HasSuffix(id.Name, "answered") {
			hint = "answered"
		}
		if hint == "" {
			hint = state.hints[id.Obj]
		}
		if hint == "" {
			switch {
			case id.Name == "hok":
				hint = "matched"
			case strings.HasSuffix(id.Name, "answered"):
				hint = "answered"
			case strings.HasPrefix(id.Name, "harg"), strings.HasPrefix(id.Name, "hparam"):
				hint = "input"
			default:
				hint = "result"
			}
		}
		base := spelling(hint)
		name = base
		for suffix := 2; visitor.scope.contains(name); suffix++ {
			name = base + strconv.Itoa(suffix)
		}
		visitor.scope.used[name] = true
		state.names[id.Obj] = name
	}
	id.Name = name
	return visitor
}

func expressionName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.SelectorExpr:
		if parent, ok := value.X.(*ast.SelectorExpr); ok && (value.Sel.Name == "Value" || value.Sel.Name == "Kind") {
			return parent.Sel.Name + value.Sel.Name
		}
		return value.Sel.Name
	case *ast.ArrayType:
		return expressionName(value.Elt) + "Values"
	case *ast.StarExpr:
		return expressionName(value.X)
	case *ast.Ident:
		switch value.Name {
		case "bool", "true", "false":
			return "condition"
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "string", "float32", "float64":
			return "value"
		}
		if !private(value.Name) {
			return value.Name
		}
	case *ast.CompositeLit:
		return expressionName(value.Type)
	case *ast.CallExpr:
		if name := expressionName(value.Fun); name != "" {
			return name + "Result"
		}
	case *ast.ParenExpr:
		return expressionName(value.X)
	case *ast.BinaryExpr:
		return "combined"
	}
	return ""
}

package hostapi

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// Inline is a type-checked function literal and the lexical captures it uses.
// Parameters are in invocation order. Its body obeys the host purity contract.
type Inline struct {
	Function   string
	Parameters []string
	Result     types.Type
}

// InlineGo checks an expression or a block with return against the host package.
// Blocks need a contextual result type; expressions can infer their own type.
// The temporary scope never mutates the loaded package or its capture objects.
func (h *Host) InlineGo(body string, captures []*types.Var, expected types.Type) (*Inline, error) {
	expression, expressionErr := parser.ParseExpr(body)
	block := expressionErr != nil
	if block && expected == nil {
		return nil, fmt.Errorf("a statement block requires a contextual result type; use a typed Go function expression when inference is needed")
	}
	scope := newInlineScope(h, body, captures)
	var params []string
	for _, capture := range captures {
		params = append(params, capture.Name()+" "+scope.typeName(capture.Type()))
	}
	output := ""
	if expected != nil {
		output = scope.typeName(expected)
	}
	statements := body
	if !block {
		var text bytes.Buffer
		if err := format.Node(&text, token.NewFileSet(), expression); err != nil {
			return nil, err
		}
		if expected == nil {
			statements = "_ = " + text.String()
		} else {
			statements = "return " + text.String()
		}
	}
	source := "func(" + strings.Join(params, ", ") + ") " + output + " {\n" + statements + "\n}"
	fs := token.NewFileSet()
	parsed, err := parser.ParseExprFrom(fs, "inline.go", source, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("invalid Go: %w", err)
	}
	function := parsed.(*ast.FuncLit)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	if err := types.CheckExpr(fs, scope.pkg, token.NoPos, function, info); err != nil {
		return nil, fmt.Errorf("type check: %w", err)
	}
	result := expected
	if !block && expected == nil {
		expression = function.Body.List[0].(*ast.AssignStmt).Rhs[0]
		result = types.Default(info.TypeOf(expression))
		if result == nil || result == types.Typ[types.UntypedNil] {
			return nil, fmt.Errorf("expression needs a contextual result type")
		}
		if _, tuple := result.(*types.Tuple); tuple {
			return nil, fmt.Errorf("go! requires one result")
		}
		function.Body.List = []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{expression}}}
	}
	used := map[types.Object]bool{}
	for _, object := range info.Uses {
		used[object] = true
	}
	inline := &Inline{Result: result}
	var fields []*ast.Field
	for i, field := range function.Type.Params.List {
		if !used[info.Defs[field.Names[0]]] {
			continue
		}
		inline.Parameters = append(inline.Parameters, captures[i].Name())
		field.Type, err = parser.ParseExpr(h.TypeName(captures[i].Type()))
		if err != nil {
			return nil, fmt.Errorf("capture type: %w", err)
		}
		fields = append(fields, field)
	}
	function.Type.Params.List = fields
	resultType, err := parser.ParseExpr(h.TypeName(result))
	if err != nil {
		return nil, fmt.Errorf("result type: %w", err)
	}
	function.Type.Results = &ast.FieldList{List: []*ast.Field{{Type: resultType}}}
	scope.qualify(h, info)
	var text bytes.Buffer
	if err := format.Node(&text, fs, function); err != nil {
		return nil, fmt.Errorf("format inline Go: %w", err)
	}
	inline.Function = text.String()
	return inline, nil
}

// Package hostapi resolves Go host types, hooks and signature stubs for Hyrule.
package hostapi

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"strings"
)

// Stub recovers the signature from a binding file before generated code exists.
// Signature types must be declared directly as function types in this file.
func Stub(path, signature, function string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, path, data, 0)
	if err != nil {
		return nil, err
	}
	var ft *ast.FuncType
	ast.Inspect(file, func(n ast.Node) bool {
		if spec, ok := n.(*ast.TypeSpec); ok && spec.Name.Name == signature {
			ft, _ = spec.Type.(*ast.FuncType)
		}
		return true
	})
	if ft == nil {
		return nil, fmt.Errorf("%s: %s must be a declared function type", path, signature)
	}
	fields, err := signatureFields(ft)
	if err != nil {
		return nil, err
	}
	ft.Params = &ast.FieldList{List: fields}
	var sig bytes.Buffer
	if err = format.Node(&sig, fs, ft); err != nil {
		return nil, err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "package %s\n", file.Name.Name)
	// Keep only imports referenced by the signature, never host implementation imports.
	used := map[string]bool{}
	ast.Inspect(ft, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	for _, im := range file.Imports {
		path := strings.Trim(im.Path.Value, "\"")
		name := path[strings.LastIndex(path, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		if name == "." {
			return nil, fmt.Errorf("dot imports are not supported in binding files")
		}
		if used[name] {
			fmt.Fprintf(&out, "import %s %s\n", name, im.Path.Value)
		}
	}
	fmt.Fprintf(&out, "func %s%s { panic(\"Hyrule validation stub\") }\n", function, strings.TrimPrefix(sig.String(), "func"))
	return []byte(out.String()), nil
}

func signatureFields(ft *ast.FuncType) ([]*ast.Field, error) {
	fields := []*ast.Field{}
	if ft.Params != nil {
		for _, f := range ft.Params.List {
			count := len(f.Names)
			if count == 0 {
				count = 1
			}
			for range count {
				fields = append(fields, &ast.Field{Type: f.Type})
			}
		}
	}
	if len(fields) < 1 {
		return nil, fmt.Errorf("signature requires at least one parameter")
	}
	for i, f := range fields {
		f.Names = []*ast.Ident{ast.NewIdent(fmt.Sprintf("harg%d", i))}
	}

	return fields, nil
}

// Host contains the package and the selected generated function signature.
type Host struct {
	Package     *types.Package
	Signature   *types.Signature
	Function    string
	Imports     map[string]string
	ImportNames map[string]*types.Package
}

// TypeName renders a type and records its generated imports.
func (h *Host) TypeName(t types.Type) string {
	return types.TypeString(t, h.Qualifier)
}

// Hook resolves a non-generic function, explicit receiver method, or legacy host method.
func (h *Host) Hook(name string) (string, *types.Signature, error) {
	obj := h.Package.Scope().Lookup(name)
	if label, member, qualified := strings.Cut(name, "."); qualified {
		pkg := h.ImportNames[label]
		if receiver, ok := h.Package.Scope().Lookup(label).(*types.TypeName); ok {
			if pkg != nil {
				return "", nil, fmt.Errorf("ambiguous receiver and import %s", label)
			}
			return h.methodExpression(receiver.Type(), member)
		}
		if pkg == nil {
			return "", nil, fmt.Errorf("unknown import %s", label)
		}
		fn, ok := pkg.Scope().Lookup(member).(*types.Func)
		if !ok || !fn.Exported() {
			return "", nil, fmt.Errorf("unknown exported function %s", name)
		}
		sig := fn.Type().(*types.Signature)
		if sig.Variadic() || sig.TypeParams().Len() != 0 {
			return "", nil, fmt.Errorf("hook %s: generic and variadic functions unsupported", name)
		}
		return h.Qualifier(pkg) + "." + member, sig, nil
	}
	var method types.Object
	if h.Signature != nil && h.Signature.Params().Len() == 2 {
		method, _, _ = types.LookupFieldOrMethod(h.Signature.Params().At(1).Type(), true, h.Package, name)
	}
	if obj != nil && method != nil {
		return "", nil, fmt.Errorf("ambiguous hook %s: package function and host method", name)
	}
	call := name
	if method != nil {
		obj = method
		call = "harg1." + name
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return "", nil, fmt.Errorf("unknown function hook %s", name)
	}
	sig := fn.Type().(*types.Signature)
	if sig.Variadic() || sig.TypeParams().Len() != 0 {
		return "", nil, fmt.Errorf("hook %s: generic and variadic functions unsupported", name)
	}
	return call, sig, nil
}

// Qualifier assigns stable, collision-free generated import names.
func (h *Host) Qualifier(p *types.Package) string {
	if p == h.Package {
		return ""
	}
	if name := h.Imports[p.Path()]; name != "" {
		return name
	}
	name := fmt.Sprintf("hyruleImport%d", len(h.Imports))
	h.Imports[p.Path()] = name
	return name
}

// UsedQualifiers finds actual selector roots, excluding comments and literals.
func UsedQualifiers(source string) (map[string]bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "generated.go", "package generated\n"+source, 0)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if selector, ok := n.(*ast.SelectorExpr); ok {
			if root, ok := selector.X.(*ast.Ident); ok {
				used[root.Name] = true
			}
		}
		return true
	})
	return used, nil
}

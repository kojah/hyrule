package hostapi

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
)

// FunctionStub is one generated API. Type is the Go function type inferred
// from the rules; when empty, the binding file must declare Signature.
type FunctionStub struct{ Signature, Function, Type string }

// Stubs declares the configured rule functions in one pre-generation overlay.
// Actual generated code is checked separately; no stub is published or executed.
func Stubs(path string, functions []FunctionStub) ([]byte, error) {
	fs := token.NewFileSet()
	var combined *ast.File
	imports := map[string]*ast.ImportSpec{}
	names := map[string]bool{}
	for _, function := range functions {
		if names[function.Function] {
			return nil, fmt.Errorf("duplicate generated function %s", function.Function)
		}
		names[function.Function] = true
		source, err := Stub(path, function.Signature, function.Function, function.Type)
		if err != nil {
			return nil, err
		}
		file, err := parser.ParseFile(fs, "stub.go", source, 0)
		if err != nil {
			return nil, err
		}
		if combined == nil {
			combined = &ast.File{Name: file.Name}
		}
		for _, declaration := range file.Decls {
			if gen, ok := declaration.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
				for _, spec := range gen.Specs {
					im := spec.(*ast.ImportSpec)
					if old := imports[im.Path.Value]; old != nil && old.Name.Name != im.Name.Name {
						return nil, fmt.Errorf("conflicting stub import %s", im.Path.Value)
					}
					imports[im.Path.Value] = im
				}
			} else {
				combined.Decls = append(combined.Decls, declaration)
			}
		}
	}
	if combined == nil {
		return nil, fmt.Errorf("no generated signatures")
	}
	paths := make([]string, 0, len(imports))
	for path := range imports {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var declarations []ast.Decl
	for _, path := range paths {
		declarations = append(declarations, &ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{imports[path]}})
	}
	combined.Decls = append(declarations, combined.Decls...)
	var out bytes.Buffer
	if err := format.Node(&out, fs, combined); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

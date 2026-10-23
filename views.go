package hyrule

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"
	"golang.org/x/tools/go/packages"
)

// viewDirective opts a method into pattern projections from its doc comment.
const viewDirective = "//hyrule:view"

// accessors returns the method projections patterns may use, keyed by
// "import/path.Type.Method". Entries come from the configured .views files,
// for packages the repository does not own, and from //hyrule:view method
// comments in loaded source. Every .views entry must name a real method
// without parameters and with one result.
func (c *Config) accessors(loaded map[string]*packages.Package) (map[string]bool, error) {
	result := map[string]bool{}
	all := map[string]*packages.Package{}
	for _, root := range loaded {
		packages.Visit([]*packages.Package{root}, nil, func(pkg *packages.Package) {
			if all[pkg.PkgPath] == nil {
				all[pkg.PkgPath] = pkg
				directiveAccessors(pkg, result)
			}
		})
	}
	for _, path := range c.Views {
		entries, err := readViews(filepath.Join(c.root, path))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			key, err := checkAccessor(all, entry)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, entry.line, err)
			}
			result[key] = true
		}
	}
	return result, nil
}

func directiveAccessors(pkg *packages.Package, result map[string]bool) {
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Doc == nil || len(function.Recv.List) != 1 {
				continue
			}
			for _, comment := range function.Doc.List {
				if strings.TrimSpace(comment.Text) == viewDirective {
					result[pkg.PkgPath+"."+receiverName(function.Recv.List[0].Type)+"."+function.Name.Name] = true
				}
			}
		}
	}
}

func receiverName(expression ast.Expr) string {
	switch receiver := expression.(type) {
	case *ast.StarExpr:
		return receiverName(receiver.X)
	case *ast.IndexExpr:
		return receiverName(receiver.X)
	case *ast.Ident:
		return receiver.Name
	}
	return ""
}

type viewEntry struct {
	pkg, typ, method string
	line             int
}

// readViews parses a .views file: an unindented import path followed by
// indented Type.Method lines. Blank lines and // comments are ignored.
func readViews(path string) ([]viewEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []viewEntry
	current := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if !strings.HasPrefix(text, " ") {
			current = trimmed
			continue
		}
		typ, method, ok := strings.Cut(trimmed, ".")
		if current == "" || !ok || typ == "" || method == "" || strings.ContainsAny(method, ". ") {
			return nil, fmt.Errorf("%s:%d: expected an import path line, then indented Type.Method lines", path, line)
		}
		entries = append(entries, viewEntry{pkg: current, typ: typ, method: method, line: line})
	}
	return entries, scanner.Err()
}

// checkAccessor resolves an entry to the key of the method's declaring
// receiver, so promoted methods share one identity with their declaration.
func checkAccessor(all map[string]*packages.Package, entry viewEntry) (string, error) {
	pkg := all[entry.pkg]
	if pkg == nil || pkg.Types == nil {
		return "", fmt.Errorf("accessor package %s is not loaded by any target", entry.pkg)
	}
	named, ok := pkg.Types.Scope().Lookup(entry.typ).(*types.TypeName)
	if !ok {
		return "", fmt.Errorf("unknown accessor type %s.%s", entry.pkg, entry.typ)
	}
	receiver := named.Type()
	if !types.IsInterface(receiver) {
		receiver = types.NewPointer(receiver)
	}
	object, _, _ := types.LookupFieldOrMethod(receiver, true, pkg.Types, entry.method)
	function, ok := object.(*types.Func)
	if !ok {
		return "", fmt.Errorf("unknown accessor method %s.%s.%s", entry.pkg, entry.typ, entry.method)
	}
	signature := function.Type().(*types.Signature)
	if signature.Params().Len() != 0 || signature.Results().Len() != 1 || signature.Variadic() {
		return "", fmt.Errorf("accessor %s.%s.%s must take no arguments and return one value", entry.pkg, entry.typ, entry.method)
	}
	return hostapi.MethodKey(function), nil
}

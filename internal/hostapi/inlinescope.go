package hostapi

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// inlineScope supplies existing host objects, imports and private type aliases
// to Go's checker. Function parameters establish capture scope and shadowing.
type inlineScope struct {
	pkg      *types.Package
	reserved map[string]bool
	serial   int
}

func newInlineScope(h *Host, body string, captures []*types.Var) *inlineScope {
	scope := &inlineScope{pkg: types.NewPackage(h.Package.Path(), h.Package.Name()), reserved: map[string]bool{}}
	for _, name := range h.Package.Scope().Names() {
		scope.pkg.Scope().Insert(h.Package.Scope().Lookup(name))
		scope.reserved[name] = true
	}
	for _, word := range strings.FieldsFunc(body, func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r > 127)
	}) {
		scope.reserved[word] = true
	}
	for _, capture := range captures {
		scope.reserved[capture.Name()] = true
	}
	names := make([]string, 0, len(h.ImportNames))
	for name := range h.ImportNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		scope.reserved[name] = true
		if scope.pkg.Scope().Lookup(name) == nil {
			scope.pkg.Scope().Insert(types.NewPkgName(token.NoPos, scope.pkg, name, h.ImportNames[name]))
		}
	}
	return scope
}

func (s *inlineScope) fresh() string {
	for {
		name := fmt.Sprintf("hyruleInline%d", s.serial)
		s.serial++
		if !s.reserved[name] {
			s.reserved[name] = true
			return name
		}
	}
}

func (s *inlineScope) typeName(typ types.Type) string {
	name := s.fresh()
	object := types.NewTypeName(token.NoPos, s.pkg, name, typ)
	s.pkg.Scope().Insert(object)
	return name
}

// qualify rewrites resolved package identifiers, never selector fields or
// textual lookalikes in strings. Local names that could shadow generated
// import aliases are renamed by object identity.
func (s *inlineScope) qualify(h *Host, info *types.Info) {
	renamed := map[types.Object]string{}
	// Map iteration must not affect generated names.
	objects := make([]*ast.Ident, 0, len(info.Defs))
	for id := range info.Defs {
		objects = append(objects, id)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Pos() < objects[j].Pos() })
	for _, id := range objects {
		object := info.Defs[id]
		if object == nil || !strings.HasPrefix(id.Name, "hyruleImport") {
			continue
		}
		if field, ok := object.(*types.Var); ok && field.IsField() {
			continue
		}
		renamed[object] = s.fresh()
		id.Name = renamed[object]
	}
	uses := make([]*ast.Ident, 0, len(info.Uses))
	for id := range info.Uses {
		uses = append(uses, id)
	}
	sort.Slice(uses, func(i, j int) bool { return uses[i].Pos() < uses[j].Pos() })
	for _, id := range uses {
		object := info.Uses[id]
		if name := renamed[object]; name != "" {
			id.Name = name
			continue
		}
		if imported, ok := object.(*types.PkgName); ok {
			id.Name = h.Qualifier(imported.Imported())
		}
	}
}

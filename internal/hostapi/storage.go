package hostapi

import (
	"fmt"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// Type resolves non-generic local or imported named types and pointers. Import
// names are taken from the binding file, so aliases retain their Go meaning.
func (h *Host) Type(name string) (types.Type, error) {
	if strings.HasPrefix(name, "*") {
		elem, err := h.Type(name[1:])
		if err != nil {
			return nil, err
		}
		return types.NewPointer(elem), nil
	}
	scope := h.Package.Scope()
	if pkg, member, ok := strings.Cut(name, "."); ok {
		found := h.ImportNames[pkg]
		if found == nil {
			return nil, fmt.Errorf("unknown imported type %s", name)
		}
		scope = found.Scope()
		name = member
	}
	obj := scope.Lookup(name)
	if obj == nil {
		obj = types.Universe.Lookup(name)
	}
	typ, ok := obj.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("unknown Go type %s", name)
	}
	return typ.Type(), nil
}

// Tuple models native Go result lists as an anonymous value struct internally.
// It is emitted only in generated code; host callers keep their native API.
func Tuple(pkg *types.Package, tuple *types.Tuple) *types.Struct {
	fields := make([]*types.Var, tuple.Len())
	for i := range fields {
		fields[i] = types.NewVar(token.NoPos, pkg, fmt.Sprintf("V%d", i), tuple.At(i).Type())
	}
	return types.NewStruct(fields, nil)
}

// IndexField resolves a zero-based native tuple slot without accepting Go text.
func IndexField(index string, structure *types.Struct) string {
	i, err := strconv.Atoi(index)
	if err != nil || i < 0 || i >= structure.NumFields() {
		return ""
	}
	return structure.Field(i).Name()
}

// Project resolves a read-only field path. Methods are deliberately excluded;
// projections cannot conceal calls, budget consumption, or state changes.
func (h *Host) Project(typ types.Type, path []string) (types.Type, error) {
	for _, name := range path {
		field, _, _ := types.LookupFieldOrMethod(typ, true, h.Package, name)
		member, ok := field.(*types.Var)
		if !ok || !member.IsField() || (!member.Exported() && member.Pkg() != h.Package) {
			return nil, fmt.Errorf("missing or inaccessible field %s", name)
		}
		typ = member.Type()
	}
	return typ, nil
}

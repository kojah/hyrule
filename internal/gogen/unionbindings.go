package gogen

import (
	"go/constant"
	"go/token"
	"go/types"

	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

type boundUnion struct {
	declaration *syntax.Union
	schema      *matchir.UnionSchema
	typ         types.Type
	tag         *types.Var
	variants    []*boundVariant
}
type boundVariant struct {
	union       *boundUnion
	declaration *syntax.Variant
	id          matchir.VariantID
	signature   *types.Signature
	tag         *types.Const
	fields      []*types.Var
}

func (e *emitter) bindUnions(declarations []*syntax.Union) error {
	e.constructors = map[string]*boundVariant{}
	for _, declaration := range declarations {
		var storageType types.Type
		var structure *types.Struct
		if len(declaration.Types) > 0 {
			vars := make([]*types.Var, len(declaration.Types))
			for i, name := range declaration.Types {
				typ, err := e.host.Type(name)
				if err != nil {
					return err
				}
				vars[i] = types.NewVar(token.NoPos, e.host.Package, "", typ)
			}
			structure = hostapi.Tuple(e.host.Package, types.NewTuple(vars...))
			storageType = structure
		} else {
			object := e.host.Package.Scope().Lookup(declaration.GoType)
			typename, known := object.(*types.TypeName)
			if !known {
				return &syntax.Diagnostic{Position: declaration.Position, Message: "union requires a local Go type " + declaration.GoType}
			}
			if genericStorage(typename.Type()) {
				return &syntax.Diagnostic{Position: declaration.Position, Message: "union storage requires a non-generic Go type"}
			}
			storageType = typename.Type()
			structure, known = storageType.Underlying().(*types.Struct)
			if !known {
				return &syntax.Diagnostic{Position: declaration.Position, Message: "union storage must be a Go struct"}
			}
		}
		for _, existing := range e.unions {
			if types.Identical(existing.typ, storageType) {
				return &syntax.Diagnostic{Position: declaration.Position, Message: "Go type already bound to union " + existing.declaration.Name}
			}
		}
		tagName := declaration.Tag
		if len(declaration.Types) > 0 {
			tagName = hostapi.IndexField(tagName, structure)
		}
		tag := directField(structure, tagName)
		if tag == nil {
			return &syntax.Diagnostic{Position: declaration.Position, Message: "missing direct union tag field " + declaration.Tag}
		}
		if basic, known := tag.Type().Underlying().(*types.Basic); !known || basic.Info()&(types.IsInteger|types.IsString|types.IsBoolean) == 0 {
			return &syntax.Diagnostic{Position: declaration.Position, Message: "union tag must have integer, string or Boolean type"}
		}
		union := &boundUnion{declaration: declaration, typ: storageType, tag: tag,
			schema: &matchir.UnionSchema{ID: matchir.UnionID(len(e.unions)), Name: declaration.Name, Type: e.tables.typeID(storageType)}}
		for _, variant := range declaration.Variants {
			if err := e.bindVariant(union, structure, variant); err != nil {
				return err
			}
		}
		e.unions = append(e.unions, union)
	}
	return nil
}

func (e *emitter) bindVariant(union *boundUnion, structure *types.Struct, declaration *syntax.Variant) error {
	failure := func(message string) error {
		return &syntax.Diagnostic{Position: declaration.Position, Message: message}
	}
	tag, known := e.host.Package.Scope().Lookup(declaration.GoTag).(*types.Const)
	if declaration.GoTag == "true" || declaration.GoTag == "false" {
		if basic, ok := union.tag.Type().Underlying().(*types.Basic); ok && basic.Info()&types.IsBoolean != 0 {
			tag = types.NewConst(token.NoPos, nil, declaration.GoTag, union.tag.Type(), types.Universe.Lookup(declaration.GoTag).(*types.Const).Val())
			known = true
		}
	}
	if !known || !types.Identical(tag.Type(), union.tag.Type()) {
		return failure("variant tag requires a Go constant with the union tag's exact type")
	}
	for _, existing := range union.variants {
		if constant.Compare(existing.tag.Val(), token.EQL, tag.Val()) {
			return failure("duplicate variant tag value " + declaration.GoTag)
		}
	}
	variant := &boundVariant{union: union, declaration: declaration, id: matchir.VariantID(len(union.variants)), tag: tag}
	schema := matchir.VariantSchema{ID: variant.id, Name: declaration.Name}
	seen, storage := map[string]struct{}{}, map[string]struct{}{}
	var results []*types.Var
	for index, field := range declaration.Fields {
		if _, duplicate := seen[field.Name]; duplicate {
			return failure("duplicate union field " + field.Name)
		}
		if _, duplicate := storage[field.GoName]; duplicate {
			return failure("duplicate union storage field " + field.GoName)
		}
		seen[field.Name], storage[field.GoName] = struct{}{}, struct{}{}
		name := field.GoName
		if len(union.declaration.Types) > 0 {
			name = hostapi.IndexField(name, structure)
		}
		member := directField(structure, name)
		if member == nil || member == union.tag {
			return failure("missing or invalid direct variant field " + field.GoName)
		}
		variant.fields = append(variant.fields, member)
		results = append(results, types.NewVar(token.NoPos, e.host.Package, field.Name, member.Type()))
		schema.Fields = append(schema.Fields, matchir.FieldSchema{ID: matchir.FieldID(index), Name: field.Name, Type: e.tables.typeID(member.Type())})
	}
	results = append(results, types.NewVar(token.NoPos, e.host.Package, "matched", types.Typ[types.Bool]))
	input := types.NewTuple(types.NewVar(token.NoPos, e.host.Package, "input", union.typ))
	variant.signature = types.NewSignatureType(nil, nil, nil, input, types.NewTuple(results...), false)
	union.variants = append(union.variants, variant)
	union.schema.Variants = append(union.schema.Variants, schema)
	e.constructors[declaration.Name] = variant
	return nil
}

func directField(structure *types.Struct, name string) *types.Var {
	if name == "_" {
		return nil
	}
	for index := 0; index < structure.NumFields(); index++ {
		if field := structure.Field(index); field.Name() == name {
			return field
		}
	}
	return nil
}

func genericStorage(typ types.Type) bool {
	switch concrete := typ.(type) {
	case *types.Named:
		return concrete.TypeParams().Len() != 0
	case *types.Alias:
		return concrete.TypeParams().Len() != 0
	default:
		return false
	}
}

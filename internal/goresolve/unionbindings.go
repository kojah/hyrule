package goresolve

import (
	"go/constant"
	"go/token"
	"go/types"

	"github.com/kojah/hyrule/internal/hostapi"
	"github.com/kojah/hyrule/internal/matchir"
	"github.com/kojah/hyrule/internal/semantic"
)

type boundUnion struct {
	declaration *semantic.Union
	schema      *matchir.UnionSchema
	typ         types.Type
	tag         *types.Var
	variants    []*boundVariant
}
type boundVariant struct {
	union       *boundUnion
	declaration *semantic.Variant
	id          matchir.VariantID
	signature   *types.Signature
	tag         *types.Const
	fields      []*types.Var
}

func (e *resolver) bindUnions(declarations []*semantic.Union) error {
	e.constructors = map[string]*boundVariant{}
	for _, declaration := range declarations {
		if err := e.bindUnion(declaration); err != nil {
			return err
		}
	}
	return nil
}

func (e *resolver) bindVariant(union *boundUnion, structure *types.Struct, declaration *semantic.Variant) error {
	failure := func(message string) error {
		return &semantic.Diagnostic{Position: declaration.Position, Message: message}
	}
	tag, known := e.host.Package.Scope().Lookup(declaration.HostTag).(*types.Const)
	if declaration.HostTag == "true" || declaration.HostTag == "false" {
		if basic, ok := union.tag.Type().Underlying().(*types.Basic); ok && basic.Info()&types.IsBoolean != 0 {
			tag = types.NewConst(token.NoPos, nil, declaration.HostTag, union.tag.Type(), types.Universe.Lookup(declaration.HostTag).(*types.Const).Val())
			known = true
		}
	}
	if !known || !types.Identical(tag.Type(), union.tag.Type()) {
		return failure("variant tag requires a Go constant with the union tag's exact type")
	}
	for _, existing := range union.variants {
		if constant.Compare(existing.tag.Val(), token.EQL, tag.Val()) {
			return failure("duplicate variant tag value " + declaration.HostTag)
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
		if _, duplicate := storage[field.HostName]; duplicate {
			return failure("duplicate union storage field " + field.HostName)
		}
		seen[field.Name], storage[field.HostName] = struct{}{}, struct{}{}
		name := field.HostName
		if len(union.declaration.Types) > 0 {
			name = hostapi.IndexField(name, structure)
		}
		member := directField(structure, name)
		if member == nil || member == union.tag {
			return failure("missing or invalid direct variant field " + field.HostName)
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

func (e *resolver) constructorSignature(variant *boundVariant) *types.Signature {
	params := make([]*types.Var, len(variant.fields))
	for index := range params {
		params[index] = variant.signature.Results().At(index)
	}
	result := types.NewTuple(types.NewVar(0, e.host.Package, "", variant.union.typ))
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), result, false)
}

func (e *resolver) bindUnion(declaration *semantic.Union) error {
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
		object := e.host.Package.Scope().Lookup(declaration.HostType)
		typename, known := object.(*types.TypeName)
		if !known {
			return &semantic.Diagnostic{Position: declaration.Position, Message: "union requires a local Go type " + declaration.HostType}
		}
		if genericStorage(typename.Type()) {
			return &semantic.Diagnostic{Position: declaration.Position, Message: "union storage requires a non-generic Go type"}
		}
		storageType = typename.Type()
		structure, known = storageType.Underlying().(*types.Struct)
		if !known {
			return &semantic.Diagnostic{Position: declaration.Position, Message: "union storage must be a Go struct"}
		}
	}
	for _, existing := range e.unions {
		if types.Identical(existing.typ, storageType) {
			return &semantic.Diagnostic{Position: declaration.Position, Message: "Go type already bound to union " + existing.declaration.Name}
		}
	}
	tagName := declaration.Tag
	if len(declaration.Types) > 0 {
		tagName = hostapi.IndexField(tagName, structure)
	}
	tag := directField(structure, tagName)
	if tag == nil {
		return &semantic.Diagnostic{Position: declaration.Position, Message: "missing direct union tag field " + declaration.Tag}
	}
	if basic, known := tag.Type().Underlying().(*types.Basic); !known || basic.Info()&(types.IsInteger|types.IsString|types.IsBoolean) == 0 {
		return &semantic.Diagnostic{Position: declaration.Position, Message: "union tag must have integer, string or Boolean type"}
	}
	union := &boundUnion{declaration: declaration, typ: storageType, tag: tag,
		schema: &matchir.UnionSchema{ID: matchir.UnionID(len(e.unions)), Name: declaration.Name, Type: e.tables.typeID(storageType)}}
	for _, variant := range declaration.Variants {
		if err := e.bindVariant(union, structure, variant); err != nil {
			return err
		}
	}
	e.unions = append(e.unions, union)
	return nil
}

// unionFor returns the union bound to typ, if any.
func (e *resolver) unionFor(typ types.Type) *boundUnion {
	for _, union := range e.unions {
		if types.Identical(typ, union.typ) {
			return union
		}
	}
	return nil
}

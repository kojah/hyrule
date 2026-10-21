package gogen

import (
	"go/token"
	"go/types"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

type boundRecord struct {
	declaration *syntax.Record
	schema      *matchir.RecordSchema
	typ         types.Type
	fields      []*types.Var
	signature   *types.Signature
}

func (e *emitter) bindRecords(declarations []*syntax.Record) error {
	e.recordNames = map[string]*boundRecord{}
	for _, declaration := range declarations {
		if err := e.bindRecord(declaration); err != nil {
			return err
		}
	}
	return nil
}

func (e *emitter) bindRecord(declaration *syntax.Record) error {
	failure := func(message string) error {
		return &syntax.Diagnostic{Position: declaration.Position, Message: message}
	}
	object, known := e.host.Package.Scope().Lookup(declaration.GoType).(*types.TypeName)
	if !known || genericStorage(object.Type()) {
		return failure("record requires a local non-generic Go type " + declaration.GoType)
	}
	structure, known := object.Type().Underlying().(*types.Struct)
	if !known {
		return failure("record storage must be a Go struct")
	}
	if e.unionFor(object.Type()) != nil || e.recordFor(object.Type()) != nil {
		return failure("Go type already bound to a record or union")
	}
	record := &boundRecord{declaration: declaration, typ: object.Type(),
		schema: &matchir.RecordSchema{ID: matchir.RecordID(len(e.records)), Name: declaration.Name, Type: e.tables.typeID(object.Type())}}
	names, storage := map[string]bool{}, map[string]bool{}
	var results []*types.Var
	for index, field := range declaration.Fields {
		if names[field.Name] || storage[field.GoName] {
			return failure("duplicate record field or storage field " + field.Name)
		}
		names[field.Name], storage[field.GoName] = true, true
		member := directField(structure, field.GoName)
		if member == nil || !member.Exported() && member.Pkg() != e.host.Package {
			return failure("missing or inaccessible direct record field " + field.GoName)
		}
		record.fields = append(record.fields, member)
		results = append(results, types.NewVar(token.NoPos, e.host.Package, field.Name, member.Type()))
		record.schema.Fields = append(record.schema.Fields, matchir.FieldSchema{ID: matchir.FieldID(index), Name: field.Name, Type: e.tables.typeID(member.Type())})
	}
	for index := 0; index < structure.NumFields(); index++ {
		if field := structure.Field(index); field.Name() != "_" && !storage[field.Name()] {
			return failure("record schema omits Go field " + field.Name())
		}
	}
	results = append(results, types.NewVar(token.NoPos, e.host.Package, "matched", types.Typ[types.Bool]))
	input := types.NewTuple(types.NewVar(token.NoPos, e.host.Package, "input", record.typ))
	record.signature = types.NewSignatureType(nil, nil, nil, input, types.NewTuple(results...), false)
	e.records = append(e.records, record)
	e.recordNames[declaration.Name] = record
	return nil
}

func (e *emitter) recordFor(typ types.Type) *boundRecord {
	for _, record := range e.records {
		if types.Identical(record.typ, typ) {
			return record
		}
	}
	return nil
}

func (e *emitter) recordSignature(record *boundRecord) *types.Signature {
	params := make([]*types.Var, len(record.fields))
	for index := range params {
		params[index] = record.signature.Results().At(index)
	}
	result := types.NewTuple(types.NewVar(token.NoPos, e.host.Package, "", record.typ))
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), result, false)
}

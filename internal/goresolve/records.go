package goresolve

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

type boundRecord struct {
	declaration *semantic.Record
	schema      *matchir.RecordSchema
	typ         types.Type
	fields      []*types.Var
	signature   *types.Signature
}

func (e *resolver) bindRecords(declarations []*semantic.Record) error {
	e.recordNames = map[string]*boundRecord{}
	for _, declaration := range declarations {
		if err := e.bindRecord(declaration); err != nil {
			return err
		}
	}
	return nil
}

func (e *resolver) bindRecord(declaration *semantic.Record) error {
	failure := func(message string) error {
		return &semantic.Diagnostic{Position: declaration.Position, Message: message}
	}
	typ, err := e.host.Type(declaration.HostType)
	if err != nil || genericStorage(typ) {
		return failure("record requires a non-generic Go type " + declaration.HostType)
	}
	storageType := typ
	if pointer, ok := storageType.Underlying().(*types.Pointer); ok && declaration.View {
		storageType = pointer.Elem()
	}
	if genericStorage(storageType) {
		return failure("view storage requires a non-generic Go type")
	}
	structure, known := storageType.Underlying().(*types.Struct)
	if !known {
		return failure("record storage must be a Go struct (or pointer for a view)")
	}
	if !declaration.View && (e.unionFor(typ) != nil || e.recordFor(typ) != nil) {
		return failure("Go type already bound to a record or union")
	}
	record := &boundRecord{declaration: declaration, typ: typ,
		schema: &matchir.RecordSchema{ID: matchir.RecordID(len(e.records)), Name: declaration.Name, Type: e.tables.typeID(typ)}}
	names, storage := map[string]bool{}, map[string]bool{}
	var results []*types.Var
	for index, field := range declaration.Fields {
		if names[field.Name] || storage[field.HostName] {
			return failure("duplicate record field or storage field " + field.Name)
		}
		names[field.Name], storage[field.HostName] = true, true
		member := directField(structure, field.HostName)
		if member == nil || !member.Exported() && member.Pkg() != e.host.Package {
			return failure("missing or inaccessible direct record field " + field.HostName)
		}
		record.fields = append(record.fields, member)
		results = append(results, types.NewVar(token.NoPos, e.host.Package, field.Name, member.Type()))
		record.schema.Fields = append(record.schema.Fields, matchir.FieldSchema{ID: matchir.FieldID(index), Name: field.Name, Type: e.tables.typeID(member.Type())})
	}
	for index := 0; !declaration.View && index < structure.NumFields(); index++ {
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

func (e *resolver) recordFor(typ types.Type) *boundRecord {
	for _, record := range e.records {
		if !record.declaration.View && types.Identical(record.typ, typ) {
			return record
		}
	}
	return nil
}

func (e *resolver) recordSignature(record *boundRecord) *types.Signature {
	params := make([]*types.Var, len(record.fields))
	for index := range params {
		params[index] = record.signature.Results().At(index)
	}
	result := types.NewTuple(types.NewVar(token.NoPos, e.host.Package, "", record.typ))
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), result, false)
}

// viewMatcher emits a read-only structural extraction. Interface inputs are
// checked with a Go type assertion; nil pointers never expose fields. There is
// no host callback to hide state changes inside the shared matching graph.
func (e *resolver) viewMatcher(x *semantic.Expr, record *boundRecord, input types.Type) (string, *types.Signature, error) {
	var parameters, values, assignments []string
	for i, field := range record.fields {
		name := fmt.Sprintf("hr%d", i)
		parameters = append(parameters, name+" "+e.host.TypeName(field.Type()))
		values = append(values, name)
		assignments = append(assignments, name+" = hvalue."+field.Name())
	}
	var results []*types.Var
	for i := 0; i < record.signature.Results().Len()-1; i++ {
		results = append(results, record.signature.Results().At(i))
	}
	if x.Bind != "" {
		parameters = append(parameters, "hwhole "+e.host.TypeName(record.typ))
		values = append(values, "hvalue")
		results = append(results, types.NewVar(token.NoPos, e.host.Package, "hwhole", record.typ))
	}
	results = append(results, types.NewVar(token.NoPos, e.host.Package, "matched", types.Typ[types.Bool]))
	parameters = append(parameters, "hok bool")
	var extraction string
	if types.Identical(input, record.typ) {
		extraction = "hvalue := hinput\n"
	} else if iface, ok := input.Underlying().(*types.Interface); ok && types.AssertableTo(iface, record.typ) {
		extraction = "hvalue, hok := hinput.(" + e.host.TypeName(record.typ) + ")\nif !hok { return }\n"
	} else {
		return "", nil, e.fail(x, "view input must be its storage type or a compatible interface")
	}
	if _, pointer := record.typ.Underlying().(*types.Pointer); pointer {
		extraction += "if hvalue == nil { hok = false; return }\n"
	}
	extraction += "_ = hvalue\n"
	values = append(values, "true")
	body := fmt.Sprintf("(func(hinput %s) (%s) { %s%s; return %s })",
		e.host.TypeName(input), strings.Join(parameters, ","), extraction, strings.Join(assignments, ";"), strings.Join(values, ","))
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(types.NewVar(token.NoPos, e.host.Package, "input", input)), types.NewTuple(results...), false)
	return body, sig, nil
}

// A whole view capture is the successfully asserted storage value, rather than
// its wider input interface. It has the same identity and is usable by hooks
// requiring that concrete Go type. The extra matcher output is not a field.
func (e *resolver) capturedView(x *semantic.Expr) *boundRecord {
	if x.Kind != semantic.Call || x.Bind == "" {
		return nil
	}
	if record := e.recordNames[x.Name]; record != nil && record.declaration.View {
		return record
	}
	return nil
}

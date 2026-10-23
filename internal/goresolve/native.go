package goresolve

import (
	"fmt"
	"github.com/kojah/hyrule/internal/valueir"
	"go/token"
	"go/types"
	"strings"

	"github.com/kojah/hyrule/internal/hostapi"
	"github.com/kojah/hyrule/internal/matchir"
	"github.com/kojah/hyrule/internal/semantic"
)

// Native APIs reuse the ordered recognizer internally. Only the generated
// boundary packs values: callers and host hooks retain their original Go APIs.
// A total boundary must explicitly say what failed recognition means.
type nativeSignature struct {
	declaration   *semantic.API
	fallback      *valueir.Result
	original      *types.Signature
	function      string
	input, output *types.Struct
}

func nativeAPI(h *Host, api *semantic.API) (*nativeSignature, error) {
	sig := h.Signature
	if api != nil && api.Fallback != nil && containsQuery(api.Fallback) {
		return nil, &semantic.Diagnostic{Position: api.Position, Message: "signature fallback cannot run a query"}
	}
	if sig.Variadic() || sig.TypeParams().Len() != 0 || sig.Params().Len() < 1 {
		return nil, fmt.Errorf("signature requires non-generic, non-variadic parameters")
	}
	if !api.Native() {
		if sig.Params().Len() > 2 || sig.Results().Len() != 2 || !types.Identical(sig.Results().At(1).Type(), types.Typ[types.Bool]) {
			return nil, fmt.Errorf("signature must be func(Input[, Host]) (Output, bool); use partial fn or total fn with a fallback for a native API")
		}
		return nil, nil
	}
	count := sig.Results().Len()
	if api.Mode == "partial" {
		if count < 2 || !types.Identical(sig.Results().At(count-1).Type(), types.Typ[types.Bool]) {
			return nil, fmt.Errorf("partial signature requires results followed by bool")
		}
		count--
	}
	if count < 1 {
		return nil, fmt.Errorf("native signature requires at least one result")
	}
	results := make([]*types.Var, count)
	for i := range results {
		results[i] = sig.Results().At(i)
	}
	// Parameters are a distinct product from query outcomes, even when their
	// Go element types coincide. Declaring an outcome domain must not restrict
	// ordinary function arguments before any rule or query runs.
	inputFields := make([]*types.Var, sig.Params().Len())
	for i := range inputFields {
		inputFields[i] = types.NewField(token.NoPos, h.Package, fmt.Sprintf("P%d", i), sig.Params().At(i).Type(), false)
	}
	native := &nativeSignature{declaration: api, original: sig, function: h.Function,
		input: types.NewStruct(inputFields, nil), output: hostapi.Tuple(h.Package, types.NewTuple(results...))}
	h.Function = "hyrule" + h.Function + "Recognize"
	input := types.NewTuple(types.NewVar(token.NoPos, h.Package, "input", native.input))
	output := types.NewTuple(types.NewVar(token.NoPos, h.Package, "output", native.output), types.NewVar(token.NoPos, h.Package, "matched", types.Typ[types.Bool]))
	h.Signature = types.NewSignatureType(nil, nil, nil, input, output, false)
	return native, nil
}

func (e *resolver) bindNativeRecords(native *nativeSignature) {
	for _, item := range []struct {
		name     string
		storage  *types.Struct
		original *types.Tuple
	}{
		{"inputs", native.input, native.original.Params()}, {"outputs", native.output, native.original.Results()},
	} {
		record := &boundRecord{declaration: &semantic.Record{Name: item.name}, typ: item.storage,
			schema: &matchir.RecordSchema{ID: matchir.RecordID(len(e.records)), Name: item.name, Type: e.tables.typeID(item.storage)}}
		results := make([]*types.Var, item.storage.NumFields(), item.storage.NumFields()+1)
		for i := range results {
			field := item.storage.Field(i)
			name := item.original.At(i).Name()
			if name == "" || name == "_" {
				name = fmt.Sprintf("value%d", i)
			}
			record.fields = append(record.fields, field)
			results[i] = types.NewVar(token.NoPos, e.host.Package, name, field.Type())
			record.schema.Fields = append(record.schema.Fields, matchir.FieldSchema{ID: matchir.FieldID(i), Name: name, Type: e.tables.typeID(field.Type())})
		}
		results = append(results, types.NewVar(token.NoPos, e.host.Package, "matched", types.Typ[types.Bool]))
		input := types.NewTuple(types.NewVar(token.NoPos, e.host.Package, "input", record.typ))
		record.signature = types.NewSignatureType(nil, nil, nil, input, types.NewTuple(results...), false)
		e.records = append(e.records, record)
		e.recordNames[item.name] = record
	}
}

// forwardingCall substitutes only values, never evaluating a nested host call.
// Free captures are explicit lexical inputs from the rule that invokes it.
func (e *resolver) forwardingCall(x *semantic.Expr, kind string) (*semantic.Expr, error) {
	b := e.bindings[x.Name]
	if b == nil || b.Body == nil {
		return x, nil
	}
	if !bindingKindMatches(b.Kind, kind) || len(x.Args) != len(b.Parameters) {
		return nil, e.fail(x, "forwarding binding arity or kind mismatch")
	}
	params := map[string]*semantic.Expr{}
	for i, name := range b.Parameters {
		if x.Args[i].Field != "" {
			return nil, e.fail(x.Args[i], "forwarding arguments must be positional")
		}
		if params[name] != nil {
			return nil, e.fail(x, "duplicate forwarding parameter "+name)
		}
		params[name] = x.Args[i]
	}
	call := *b.Body
	call.Args = nil
	for _, arg := range b.Body.Args {
		copy := *arg
		if arg.Kind == semantic.Variable {
			base, path, _ := strings.Cut(arg.Name, ".")
			if actual := params[base]; actual != nil {
				if actual.Kind != semantic.Variable && actual.Kind != semantic.Literal && actual.Kind != semantic.Symbol {
					return nil, e.fail(actual, "forwarded arguments require captures, constants or literals")
				}
				copy = *actual
				if path != "" {
					if copy.Kind != semantic.Variable {
						return nil, e.fail(actual, "field projection requires a capture")
					}
					copy.Name += "." + path
				}
			}
		}
		call.Args = append(call.Args, &copy)
	}
	return &call, nil
}

func containsQuery(x *semantic.Expr) bool {
	if x.Kind == semantic.Query || x.Kind == semantic.Selection || x.Kind == semantic.Do || x.Kind == semantic.Decline {
		return true
	}
	for _, child := range x.Args {
		if containsQuery(child) {
			return true
		}
	}
	for _, arm := range x.Arms {
		if containsQuery(arm.Result) {
			return true
		}
	}
	return false
}

package gogen

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// Native APIs reuse the ordered recognizer internally. Only the generated
// boundary packs values: callers and host hooks retain their original Go APIs.
// A total boundary must explicitly say what failed recognition means.
type nativeSignature struct {
	declaration   *syntax.API
	original      *types.Signature
	function      string
	input, output *types.Struct
}

func nativeAPI(h *Host, api *syntax.API) (*nativeSignature, error) {
	sig := h.Signature
	if api != nil && api.Fallback != nil && containsQuery(api.Fallback) {
		return nil, &syntax.Diagnostic{Position: api.Position, Message: "signature fallback cannot run a query"}
	}
	if sig.Variadic() || sig.TypeParams().Len() != 0 || sig.Params().Len() < 1 {
		return nil, fmt.Errorf("signature requires non-generic, non-variadic parameters")
	}
	if api == nil {
		if sig.Params().Len() > 2 || sig.Results().Len() != 2 || !types.Identical(sig.Results().At(1).Type(), types.Typ[types.Bool]) {
			return nil, fmt.Errorf("signature must be func(Input[, Host]) (Output, bool); use signature partial or total fallback for a native API")
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
		inputFields[i] = types.NewVar(token.NoPos, h.Package, fmt.Sprintf("P%d", i), sig.Params().At(i).Type())
	}
	native := &nativeSignature{declaration: api, original: sig, function: h.Function,
		input: types.NewStruct(inputFields, nil), output: hostapi.Tuple(h.Package, types.NewTuple(results...))}
	h.Function = "hyrule" + h.Function + "Recognize"
	input := types.NewTuple(types.NewVar(token.NoPos, h.Package, "input", native.input))
	output := types.NewTuple(types.NewVar(token.NoPos, h.Package, "output", native.output), types.NewVar(token.NoPos, h.Package, "matched", types.Typ[types.Bool]))
	h.Signature = types.NewSignatureType(nil, nil, nil, input, output, false)
	return native, nil
}

func (e *emitter) bindNativeRecords(native *nativeSignature) {
	for _, item := range []struct {
		name     string
		storage  *types.Struct
		original *types.Tuple
	}{
		{"inputs", native.input, native.original.Params()}, {"outputs", native.output, native.original.Results()},
	} {
		record := &boundRecord{declaration: &syntax.Record{Name: item.name}, typ: item.storage,
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

func (e *emitter) emitNativeAPI(native *nativeSignature) error {
	var params, fields, outputs []string
	for i := 0; i < native.original.Params().Len(); i++ {
		name := fmt.Sprintf("hparam%d", i)
		params = append(params, name+" "+e.host.TypeName(native.original.Params().At(i).Type()))
		fields = append(fields, native.input.Field(i).Name()+":"+name)
	}
	for i := 0; i < native.original.Results().Len(); i++ {
		outputs = append(outputs, e.host.TypeName(native.original.Results().At(i).Type()))
	}
	fmt.Fprintf(&e.out, "func %s(%s) (%s) {\n", native.function, strings.Join(params, ","), strings.Join(outputs, ","))
	fmt.Fprintf(&e.out, "hvalue, hok := %s(%s{%s})\n", e.host.Function, e.host.TypeName(native.input), strings.Join(fields, ","))
	if native.declaration.Mode == "total" {
		e.captures = map[string]capture{}
		e.rule = "signature fallback"
		fmt.Fprintln(&e.out, "if !hok {")
		fallback, err := e.resultValue(native.declaration.Fallback, native.output)
		if err != nil {
			return err
		}
		fmt.Fprintf(&e.out, "hvalue = %s\n}\n", fallback)
	}
	var returns []string
	for i := 0; i < native.output.NumFields(); i++ {
		returns = append(returns, fmt.Sprintf("hvalue.V%d", i))
	}
	if native.declaration.Mode == "partial" {
		returns = append(returns, "hok")
	}
	fmt.Fprintf(&e.out, "return %s\n}\n", strings.Join(returns, ","))
	return nil
}

// forwardingCall substitutes only values, never evaluating a nested host call.
// Free captures are explicit lexical inputs from the rule that invokes it.
func (e *emitter) forwardingCall(x *syntax.Expr, kind string) (*syntax.Expr, error) {
	b := e.bindings[x.Name]
	if b == nil || b.Body == nil {
		return x, nil
	}
	if b.Kind != kind || len(x.Args) != len(b.Parameters) {
		return nil, e.fail(x, "forwarding binding arity or kind mismatch")
	}
	params := map[string]*syntax.Expr{}
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
		if arg.Kind == syntax.Variable {
			base, path, _ := strings.Cut(arg.Name, ".")
			if actual := params[base]; actual != nil {
				if actual.Kind != syntax.Variable && actual.Kind != syntax.Literal && actual.Kind != syntax.Symbol {
					return nil, e.fail(actual, "forwarded arguments require captures, constants or literals")
				}
				copy = *actual
				if path != "" {
					if copy.Kind != syntax.Variable {
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

func containsQuery(x *syntax.Expr) bool {
	if x.Kind == syntax.Query || x.Kind == syntax.Selection || x.Kind == syntax.Do || x.Kind == syntax.Decline {
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

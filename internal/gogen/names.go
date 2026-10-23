package gogen

import (
	"fmt"
	"github.com/kojah/hyrule/internal/valueir"
	"go/types"
	"unicode"

	"github.com/kojah/hyrule/internal/goplan"
	"github.com/kojah/hyrule/internal/matchir"
)

// nameSuggestions uses declared parameters and captures before type-derived
// defaults. Allocation IDs never become part of the public output spelling.
func nameSuggestions(program *goplan.Program) map[string]string {
	names := map[string]string{}
	for id, name := range program.Hints {
		names[valueName(id)] = name
	}
	for id, name := range program.Names {
		names[valueName(id)] = name
	}
	signature := program.Host.Signature
	for index := range signature.Params().Len() {
		parameter := signature.Params().At(index)
		key := fmt.Sprintf("harg%d", index)
		if parameter.Name() != "" && parameter.Name() != "_" {
			names[key] = parameter.Name()
		}
		if names[key] == "" {
			names[key] = typeHint(parameter.Type())
		}
	}
	if program.Native != nil {
		for index := range program.Native.Original.Params().Len() {
			parameter := program.Native.Original.Params().At(index)
			hint := parameter.Name()
			if hint == "" || hint == "_" {
				hint = typeHint(parameter.Type())
				if root := program.Root; root != nil && root.Kind == matchir.Record && root.Input == 0 && index < len(root.Outputs) {
					if captured := program.Names[root.Outputs[index]]; captured != "" {
						hint = captured
					}
				}
			}
			names[fmt.Sprintf("hparam%d", index)] = hint
		}
	}

	var resultHints func(*valueir.Result)
	resultHints = func(result *valueir.Result) {
		if result == nil {
			return
		}
		for _, alternative := range result.Alternatives {
			resultHints(alternative)
		}
		if query := result.Query; query != nil {
			if query.Union != nil && names[valueName(query.Capture)] == "" {
				names[valueName(query.Capture)] = query.Union.Name
			}
			if query.TupleSize > 1 {
				product := program.Tables.Types[query.Type].Underlying().(*types.Struct)
				for index := range product.NumFields() {
					names[fmt.Sprintf("%spart%d", valueName(query.Capture), index)] = typeHint(product.Field(index).Type())
				}
			}
			resultHints(query.Next)
			for _, outcome := range query.Cases {
				resultHints(outcome.Result)
			}
		}
	}
	for _, expression := range program.Tables.Expressions {
		resultHints(expression.Result)
	}
	if program.Native != nil {
		resultHints(program.Native.Fallback)
	}
	return names
}

func typeHint(typ types.Type) string {
	if pointer, ok := typ.(*types.Pointer); ok {
		return typeHint(pointer.Elem())
	}
	if named, ok := typ.(*types.Named); ok {
		return named.Obj().Name()
	}
	if alias, ok := typ.(*types.Alias); ok {
		return alias.Obj().Name()
	}
	switch typ.Underlying().(type) {
	case *types.Slice, *types.Array:
		return "values"
	case *types.Interface:
		return "input"
	default:
		return "value"
	}
}

func schemaTitle(name string) string {
	letters := []rune(name)
	if len(letters) > 0 {
		letters[0] = unicode.ToUpper(letters[0])
	}
	return string(letters)
}

// assignValidatorNames reserves host declarations and disambiguates only helpers
// sharing a base spelling. Unrelated schemas never renumber existing helpers.
func (e *emitter) assignValidatorNames() {
	occupied := map[string]bool{}
	for _, name := range e.host.Package.Scope().Names() {
		occupied[name] = true
	}
	e.helperNames = map[string]string{}
	assign := func(key, schema string) {
		base := "hyrule" + e.host.Function + "Valid" + schemaTitle(schema)
		name := base
		for suffix := 2; occupied[name]; suffix++ {
			name = fmt.Sprintf("%s%d", base, suffix)
		}
		occupied[name] = true
		e.helperNames[key] = name
	}
	for _, union := range e.unions {
		assign(fmt.Sprintf("union:%d", union.Schema.ID), union.Schema.Name)
	}
	for _, record := range e.records {
		assign(fmt.Sprintf("record:%d", record.Schema.ID), record.Schema.Name)
	}
}

package goresolve

import (
	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
)

// publish drops all source declarations, scopes and symbol lookup maps. The
// returned graph and values refer only to resolved IDs and immutable bindings.
func (e *resolver) publish(root *matchir.Node, native *nativeSignature) *goplan.Program {
	result := &goplan.Program{Host: *e.host, Root: root, Serial: e.serial,
		Tables: goplan.Tables{Types: e.tables.types, Members: e.tables.members, Hooks: e.tables.hooks, Literals: e.tables.literals}}
	for _, expression := range e.tables.expressions {
		result.Tables.Expressions = append(result.Tables.Expressions, goplan.Expression{Value: expression.value, Result: expression.result})
	}
	for _, constant := range e.tables.constants {
		result.Tables.Constants = append(result.Tables.Constants, constant.object)
	}
	for _, record := range e.records {
		result.Records = append(result.Records, &goplan.Record{Schema: record.schema, Type: record.typ, Fields: record.fields, View: record.declaration.View})
	}
	for _, union := range e.unions {
		resolved := &goplan.Union{Schema: union.schema, Type: union.typ, Tag: union.tag}
		for _, variant := range union.variants {
			resolved.Variants = append(resolved.Variants, &goplan.Variant{Union: resolved, ID: variant.id, Tag: variant.tag, Fields: variant.fields})
		}
		result.Unions = append(result.Unions, resolved)
	}
	if native != nil {
		result.Native = &goplan.Native{Original: native.original, Function: native.function,
			Input: native.input, Output: native.output, Total: native.declaration.Mode == "total",
			Fallback: native.fallback}
	}
	return result
}

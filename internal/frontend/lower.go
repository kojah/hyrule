// Package frontend translates the current surface AST into an owned semantic
// model. Formatting and shorthand resolution stop at this boundary. A new
// frontend can supply the same model without depending on the current parser.
package frontend

import (
	"slices"

	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// Lower resolves surface application shorthand across all input declarations
// and copies every semantic value. The input remains usable by the formatter.
func Lower(files []*syntax.File) []*semantic.File {
	operations := map[string]bool{}
	for _, file := range files {
		for _, item := range file.Items {
			switch {
			case item.Binding != nil:
				operations[item.Binding.Name] = item.Binding.Kind != "const"
			case item.Pattern != nil:
				operations[item.Pattern.Name] = true
			case item.Record != nil:
				operations[item.Record.Name] = true
			case item.Union != nil:
				for _, variant := range item.Union.Variants {
					operations[variant.Name] = true
				}
			case item.API != nil:
				operations["inputs"], operations["outputs"] = true, true
			}
		}
	}
	lower := lowering{operations: operations}
	result := make([]*semantic.File, len(files))
	for i, file := range files {
		result[i] = &semantic.File{}
		for _, item := range file.Items {
			result[i].Items = append(result[i].Items, lower.item(item))
		}
	}
	return result
}

type lowering struct{ operations map[string]bool }

func position(p syntax.Position) semantic.Position {
	return semantic.Position{File: p.File, Line: p.Line, Column: p.Column}
}

func (l lowering) expression(x *syntax.Expr) *semantic.Expr {
	if x == nil {
		return nil
	}
	result := &semantic.Expr{Position: position(x.Position), Kind: expressionKind(x.Kind), Name: x.Name, Bind: x.Bind, Field: x.Field,
		BindResult: x.Sequential && len(x.Arms) > 0 && x.Arms[0].Pattern.Kind == syntax.Variable}
	if x.Unparenthesized && x.Kind == syntax.Symbol && l.operations[x.Name] {
		result.Kind = semantic.Call
	}
	for _, arg := range x.Args {
		result.Args = append(result.Args, l.expression(arg))
	}
	for _, arm := range x.Arms {
		result.Arms = append(result.Arms, semantic.DecisionArm{Pattern: l.expression(arm.Pattern), Result: l.expression(arm.Result)})
	}
	return result
}

func expressionKind(kind syntax.Kind) semantic.Kind {
	switch kind {
	case syntax.Call:
		return semantic.Call
	case syntax.Variable:
		return semantic.Variable
	case syntax.Wildcard:
		return semantic.Wildcard
	case syntax.Literal:
		return semantic.Literal
	case syntax.Alternation:
		return semantic.Alternation
	case syntax.Effects:
		return semantic.Effects
	case syntax.Query:
		return semantic.Query
	case syntax.Symbol:
		return semantic.Symbol
	case syntax.Comparison:
		return semantic.Comparison
	case syntax.List:
		return semantic.List
	case syntax.Update:
		return semantic.Update
	case syntax.OrderedAlternatives:
		return semantic.OrderedAlternatives
	case syntax.Selection:
		return semantic.Selection
	case syntax.Pure:
		return semantic.Pure
	case syntax.Do:
		return semantic.Do
	case syntax.Decline:
		return semantic.Decline
	default:
		panic("unhandled parser expression kind")
	}
}

func (l lowering) item(item syntax.Item) semantic.Item {
	var out semantic.Item
	if x := item.Binding; x != nil {
		out.Binding = &semantic.Binding{Position: position(x.Position), Kind: x.Kind, Name: x.Name, HostName: x.GoName,
			Parameters: slices.Clone(x.Parameters), Body: l.expression(x.Body)}
	}
	if x := item.Pattern; x != nil {
		out.Pattern = &semantic.Pattern{Position: position(x.Position), Name: x.Name, Parameters: slices.Clone(x.Parameters), Body: l.expression(x.Body)}
	}
	if x := item.Rule; x != nil {
		out.Rule = &semantic.Rule{Position: position(x.Position), Name: x.Name, Match: l.expression(x.Match), Build: l.expression(x.Build)}
		for _, clause := range x.Clauses {
			out.Rule.Clauses = append(out.Rule.Clauses, semantic.Clause{Name: clause.Name, Value: l.expression(clause.Value)})
		}
	}
	if x := item.Record; x != nil {
		out.Record = &semantic.Record{Position: position(x.Position), Name: x.Name, HostType: x.GoType, View: x.View, Fields: fields(x.Fields)}
	}
	if x := item.Union; x != nil {
		out.Union = &semantic.Union{Position: position(x.Position), Name: x.Name, HostType: x.GoType, Tag: x.Tag,
			Types: slices.Clone(x.Types), Exhaustive: x.Exhaustive}
		for _, v := range x.Variants {
			out.Union.Variants = append(out.Union.Variants, &semantic.Variant{Position: position(v.Position), Name: v.Name, HostTag: v.GoTag, Fields: fields(v.Fields)})
		}
	}
	if x := item.API; x != nil {
		out.API = &semantic.API{Position: position(x.Position), Mode: x.Mode, Fallback: l.expression(x.Fallback)}
	}
	if x := item.Derivation; x != nil {
		out.Derivation = &semantic.Derivation{Position: position(x.Position), Record: x.Record}
		for _, op := range x.Operations {
			next := &semantic.DerivedOperation{Position: position(op.Position), Kind: op.Kind, Function: op.Function, Signature: op.Signature}
			for _, f := range op.Fields {
				next.Fields = append(next.Fields, semantic.DerivedField{Position: position(f.Position), Name: f.Name, Policy: f.Policy})
			}
			out.Derivation.Operations = append(out.Derivation.Operations, next)
		}
	}
	return out
}

func fields(input []syntax.UnionField) []semantic.UnionField {
	result := make([]semantic.UnionField, len(input))
	for i, f := range input {
		result[i] = semantic.UnionField{Position: position(f.Position), Name: f.Name, HostName: f.GoName}
	}
	return result
}

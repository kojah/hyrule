// Package frontend translates the current surface AST into an owned semantic
// model. Formatting and shorthand resolution stop at this boundary. A new
// frontend can supply the same model without depending on the current parser.
package frontend

import (
	"maps"
	"slices"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// Lower resolves names and lexical scopes across all input declarations
// and copies every semantic value. The input remains usable by the formatter.
// Equation where bindings are substituted at each use.
func Lower(files []*syntax.File) ([]*semantic.File, error) {
	constructors := map[string]bool{}
	names := map[string]bool{}
	for _, file := range files {
		for _, item := range file.Items {
			switch {
			case item.Binding != nil:
				names[item.Binding.Name] = true
				constructors[item.Binding.Name] = item.Binding.Kind == "match"
			case item.Pattern != nil:
				constructors[item.Pattern.Name] = true
			case item.Record != nil:
				constructors[item.Record.Name] = true
			case item.Union != nil:
				for _, variant := range item.Union.Variants {
					constructors[variant.Name] = true
				}
			case item.API != nil:
				constructors["inputs"], constructors["outputs"] = true, true
			}
		}
	}
	lower := lowering{constructors: constructors, names: names, captures: map[string]bool{}, failure: new(error)}
	result := make([]*semantic.File, len(files))
	for i, file := range files {
		result[i] = &semantic.File{}
		for _, item := range file.Items {
			result[i].Items = append(result[i].Items, lower.item(item))
		}
	}
	return result, *lower.failure
}

// lowering is copied for nested scopes; where state and the first failure are
// shared by every copy lowering one rule.
type lowering struct {
	constructors, names, captures map[string]bool
	where                         *whereScope
	failure                       *error
}

func position(p syntax.Position) semantic.Position {
	return semantic.Position{File: p.File, Line: p.Line, Column: p.Column}
}

func (l lowering) expression(x *syntax.Expr) *semantic.Expr {
	if x == nil {
		return nil
	}
	// A named case consumes the preceding query result rather than invoking it again.
	if x.Kind == syntax.Do && len(x.Args) == 1 {
		step := x.Args[0]
		if step.Kind == syntax.Query && step.Sequential && len(step.Arms) == 1 {
			capture := step.Arms[0].Pattern
			decision := step.Arms[0].Result
			wholeCapture := capture.Kind == syntax.Variable || capture.Bare && !l.constructors[capture.Name]
			if wholeCapture && decision.Kind == syntax.Query && !decision.Sequential && len(decision.Args) == 1 &&
				decision.Args[0].Name == capture.Name && len(decision.Args[0].Args) == 0 {
				merged := *decision
				merged.Name = capture.Name
				merged.Args = step.Args
				scoped := l
				scoped.captures = maps.Clone(l.captures)
				scoped.captures[capture.Name] = true
				return scoped.expression(&merged)
			}
		}
	}
	if substituted, ok := l.substitute(x); ok {
		return substituted
	}
	result := &semantic.Expr{Position: position(x.Position), Kind: expressionKind(x.Kind), Name: x.Name, Bind: x.Bind, Field: x.Field,
		BindResult: x.Sequential && len(x.Arms) > 0 && x.Arms[0].Pattern.Kind == syntax.Variable}
	if x.Bare {
		base, _, _ := strings.Cut(x.Name, ".")
		switch {
		case l.captures[base]:
			result.Kind = semantic.Variable
		case l.constructors[x.Name]:
			result.Kind = semantic.Call
		case l.names[x.Name]:
			result.Kind = semantic.Symbol
		default:
			result.Kind = semantic.Variable
		}
	}
	for _, arg := range x.Args {
		result.Args = append(result.Args, l.expression(arg))
	}
	for _, arm := range x.Arms {
		branch := l
		branch.captures = maps.Clone(l.captures)
		branch.capture(arm.Pattern)
		result.Arms = append(result.Arms, semantic.DecisionArm{Pattern: branch.expression(arm.Pattern), Result: branch.expression(arm.Result)})
	}
	if x.Sequential && len(result.Arms) > 0 {
		result.BindResult = result.Arms[0].Pattern.Kind == semantic.Variable
	}
	if result.Kind == semantic.OrderedAlternatives && len(result.Args) > 0 {
		last := len(result.Args) - 1
		value := result.Args[last]
		if value.Kind != semantic.Pure && value.Kind != semantic.Query && value.Kind != semantic.Do &&
			value.Kind != semantic.Decline && value.Kind != semantic.OrderedAlternatives {
			result.Args[last] = &semantic.Expr{Position: value.Position, Kind: semantic.Pure, Args: []*semantic.Expr{value}}
		}
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
	case syntax.Logic:
		return semantic.Logic
	case syntax.Conditional:
		return semantic.Conditional
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
		l.captures = map[string]bool{}
		for _, name := range x.Parameters {
			l.captures[name] = true
		}
		out.Pattern = &semantic.Pattern{Position: position(x.Position), Name: x.Name, Parameters: slices.Clone(x.Parameters), Body: l.expression(x.Body)}
	}
	if x := item.Rule; x != nil {
		l.captures = map[string]bool{}
		l.capture(x.Match)
		out.Rule = &semantic.Rule{Position: position(x.Position), Name: x.Name, Family: x.Family, Match: l.expression(x.Match)}
		l.where = l.bindWhere(x)
		for _, clause := range x.Clauses {
			lowered := semantic.Clause{Name: clause.Name, Value: l.expression(clause.Value)}
			if clause.Pattern != nil {
				l.capture(clause.Pattern)
				lowered.Pattern = l.expression(clause.Pattern)
			}
			out.Rule.Clauses = append(out.Rule.Clauses, lowered)
			if clause.Name != "" {
				l.captures[clause.Name] = true
			}
		}
		out.Rule.Build = l.expression(x.Build)
		l.where = nil
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
	return out
}

func fields(input []syntax.UnionField) []semantic.UnionField {
	result := make([]semantic.UnionField, len(input))
	for i, f := range input {
		result[i] = semantic.UnionField{Position: position(f.Position), Name: f.Name, HostName: f.GoName}
	}
	return result
}

func (l lowering) capture(x *syntax.Expr) {
	if x == nil {
		return
	}
	if x.Bind != "" {
		l.captures[x.Bind] = true
	}
	if x.Kind == syntax.Variable || x.Bare && !l.constructors[x.Name] {
		l.captures[x.Name] = true
	}
	for _, arg := range x.Args {
		l.capture(arg)
	}
}

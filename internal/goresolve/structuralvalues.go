package goresolve

import (
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
	"github.com/kojah/lockdoc/tools/hyrule/internal/valueir"
	"go/types"
)

func (e *resolver) updateRecord(expression *semantic.Expr) (*boundRecord, capture, error) {
	if len(expression.Args) == 0 || expression.Args[0].Kind != semantic.Variable || expression.Args[0].Field != "" {
		return nil, capture{}, e.fail(expression, "update requires an explicit captured record base")
	}
	base, known := e.captures[expression.Args[0].Name]
	if !known {
		return nil, capture{}, e.fail(expression, "unbound update base")
	}
	record := e.recordFor(base.typ)
	if record == nil {
		return nil, capture{}, e.fail(expression, "update base requires a declared record schema")
	}
	return record, base, nil
}

func (e *resolver) updateFields(expression *semantic.Expr, record *boundRecord) ([]int, error) {
	indices := make([]int, len(expression.Args)-1)
	seen := map[string]bool{}
	for index, argument := range expression.Args[1:] {
		if argument.Field == "" || seen[argument.Field] {
			return nil, e.fail(argument, "update requires unique named fields")
		}
		seen[argument.Field] = true
		indices[index] = -1
		for position, field := range record.schema.Fields {
			if field.Name == argument.Field {
				indices[index] = position
				break
			}
		}
		if indices[index] < 0 {
			return nil, e.fail(argument, "unknown update field "+argument.Field)
		}
	}
	return indices, nil
}

func sigOutput(e *resolver) types.Type { return e.host.Signature.Results().At(0).Type() }

// resolveResult commits control-flow meanings before emission. The inChoice
// context is lexical: declining a query never becomes a matching fallback.
func (e *resolver) resolveResult(x *semantic.Expr, expected types.Type, inChoice bool) (*valueir.Result, error) {
	result := &valueir.Result{Type: e.tables.typeID(expected)}
	switch x.Kind {
	case semantic.Do:
		return e.resolveResult(x.Args[0], expected, inChoice)
	case semantic.Decline:
		if !inChoice {
			return nil, e.fail(x, "decline requires an enclosing ordered alternative")
		}
		result.Kind = valueir.Decline
		return result, nil
	case semantic.OrderedAlternatives:
		result.Kind = valueir.Choice
		for i, alternative := range x.Args {
			last := i == len(x.Args)-1
			if last && alternative.Kind != semantic.Pure {
				return nil, e.fail(alternative, "ordered alternatives require a final pure fallback")
			}
			if !last && alternative.Kind == semantic.Pure {
				return nil, e.fail(alternative, "pure must be the final ordered alternative")
			}
			if alternative.Kind != semantic.Pure && alternative.Kind != semantic.Selection && alternative.Kind != semantic.Do &&
				alternative.Kind != semantic.Query && alternative.Kind != semantic.Conditional {
				return nil, e.fail(alternative, "ordered alternative requires case, do, if, or a final pure fallback")
			}
			var branch *valueir.Result
			var err error
			if alternative.Kind == semantic.Selection {
				branch, err = e.resolveQuery(alternative, expected, true)
			} else {
				branch, err = e.resolveResult(alternative, expected, true)
			}
			if err != nil {
				return nil, err
			}
			result.Alternatives = append(result.Alternatives, branch)
		}
		return result, nil
	case semantic.Selection:
		return nil, e.fail(x, "select requires ordered alternatives with a final pure fallback")
	case semantic.Query:
		return e.resolveQuery(x, expected, inChoice)
	case semantic.Conditional:
		return e.resolveBranch(x, expected, inChoice)
	case semantic.Pure:
		if containsQuery(x.Args[0]) {
			return nil, e.fail(x, "pure cannot execute a query")
		}
		x = x.Args[0]
	}
	value, err := e.resolveValue(x, "build", expected)
	if err != nil {
		return nil, err
	}
	result.Kind = valueir.Answer
	result.Value = value
	return result, nil
}

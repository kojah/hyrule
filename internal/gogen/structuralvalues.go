package gogen

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

func (e *emitter) listValue(expression *syntax.Expr, expected types.Type) (string, error) {
	if expected == nil {
		return "", e.fail(expression, "list construction requires a typed slice context")
	}
	collection, known := expected.Underlying().(*types.Slice)
	if !known {
		return "", e.fail(expression, "list construction requires a slice type")
	}
	items := make([]string, len(expression.Args))
	for index, item := range expression.Args {
		value, err := e.valueExpected(item, "build", collection.Elem())
		if err != nil {
			return "", err
		}
		items[index] = e.structuralTemporary(collection.Elem(), value)
	}
	return e.host.TypeName(expected) + "{" + strings.Join(items, ", ") + "}", nil
}

func (e *emitter) listConstructionValue(list *matchir.ListConstruction) (string, error) {
	items := make([]string, len(list.Items))
	for index, item := range list.Items {
		expression := e.tables.expressions[item]
		e.rule, e.captures = expression.rule.Name, expression.captures
		value, err := e.valueExpected(expression.expression, "build", e.tables.types[list.Element])
		if err != nil {
			return "", err
		}
		items[index] = e.structuralTemporary(e.tables.types[list.Element], value)
	}
	return e.host.TypeName(e.tables.types[list.Type]) + "{" + strings.Join(items, ", ") + "}", nil
}

// structuralTemporary sequences one field or element and validates declared
// value schemas before the surrounding result can be published.
func (e *emitter) structuralTemporary(typ types.Type, value string) string {
	e.serial++
	name := fmt.Sprintf("hv%d", e.serial)
	fmt.Fprintf(&e.out, "var %s %s = %s\n", name, e.host.TypeName(typ), value)
	if validation := e.validation(typ, name); validation != "" {
		fmt.Fprintf(&e.out, "if !%s { var zero %s; return zero,false }\n", validation, e.host.TypeName(e.host.Signature.Results().At(0).Type()))
	}
	return name
}

func (e *emitter) updateRecord(expression *syntax.Expr) (*boundRecord, capture, error) {
	if len(expression.Args) == 0 || expression.Args[0].Kind != syntax.Variable || expression.Args[0].Field != "" {
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

func (e *emitter) updateFields(expression *syntax.Expr, record *boundRecord) ([]int, error) {
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

func (e *emitter) updateValue(expression *syntax.Expr) (string, error) {
	record, base, err := e.updateRecord(expression)
	if err != nil {
		return "", err
	}
	indices, err := e.updateFields(expression, record)
	if err != nil {
		return "", err
	}
	name := e.structuralTemporary(record.typ, valueName(base.id))
	for index, argument := range expression.Args[1:] {
		field := record.fields[indices[index]]
		value, err := e.valueExpected(argument, "build", field.Type())
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&e.out, "%s.%s = %s\n", name, field.Name(), value)
	}
	return name, nil
}

func (e *emitter) updateConstruction(rule *syntax.Rule, expression *syntax.Expr) (*matchir.Construction, error) {
	record, base, err := e.updateRecord(expression)
	if err != nil {
		return nil, err
	}
	indices, err := e.updateFields(expression, record)
	if err != nil {
		return nil, err
	}
	update := &matchir.RecordUpdate{Record: record.schema, Base: base.id}
	for index, argument := range expression.Args[1:] {
		update.Fields = append(update.Fields, matchir.FieldExpression{Field: matchir.FieldID(indices[index]), Expression: e.expressionID(rule, argument)})
	}
	return &matchir.Construction{Update: update}, nil
}

func (e *emitter) updateConstructionValue(update *matchir.RecordUpdate) (string, error) {
	record := e.records[update.Record.ID]
	name := e.structuralTemporary(record.typ, valueName(update.Base))
	for _, field := range update.Fields {
		expression := e.tables.expressions[field.Expression]
		e.rule, e.captures = expression.rule.Name, expression.captures
		value, err := e.valueExpected(expression.expression, "build", record.fields[field.Field].Type())
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&e.out, "%s.%s = %s\n", name, record.fields[field.Field].Name(), value)
	}
	return name, nil
}

// Effect lists are typed construction, not execution. Each element is evaluated
// once in written order; the generated function never applies the returned plan.
func sigOutput(e *emitter) types.Type { return e.host.Signature.Results().At(0).Type() }

func (e *emitter) typedValue(x *syntax.Expr, expected types.Type) (string, error) {
	if x.Kind != syntax.Effects {
		return e.valueExpected(x, "build", expected)
	}
	slice, ok := expected.Underlying().(*types.Slice)
	if !ok {
		return "", e.fail(x, "effects requires a slice result or slice builder parameter")
	}
	var elements []string
	for _, child := range x.Args {
		value, err := e.typedValue(child, slice.Elem())
		if err != nil {
			return "", err
		}
		e.serial++
		name := fmt.Sprintf("hv%d", e.serial)
		fmt.Fprintf(&e.out, "var %s %s = %s\n", name, e.host.TypeName(slice.Elem()), value)
		if union := e.unionFor(slice.Elem()); union != nil {
			fmt.Fprintf(&e.out, "if !%s(%s) { var zero %s; return zero,false }\n", e.validator(union), name, e.host.TypeName(sigOutput(e)))
		}
		elements = append(elements, name)
	}
	return e.host.TypeName(expected) + "{" + strings.Join(elements, ",") + "}", nil
}

func (e *emitter) resultValue(x *syntax.Expr, expected types.Type) (string, error) {
	if x.Kind == syntax.Do {
		return e.resultValue(x.Args[0], expected)
	}
	if x.Kind == syntax.Decline {
		return "", e.fail(x, "decline requires an enclosing ordered alternative")
	}
	if x.Kind == syntax.OrderedAlternatives {
		return e.alternativesValue(x, expected)
	}
	if x.Kind == syntax.Selection {
		return "", e.fail(x, "select requires ordered alternatives with a final pure fallback")
	}
	if x.Kind == syntax.Pure {
		if containsQuery(x.Args[0]) {
			return "", e.fail(x, "pure cannot execute a query")
		}
		return e.typedValue(x.Args[0], expected)
	}
	if x.Kind == syntax.Query {
		return e.queryValue(x, expected)
	}
	return e.typedValue(x, expected)
}

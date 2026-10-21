package gogen

import (
	"fmt"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

func (e *emitter) recordValue(expression *syntax.Expr, record *boundRecord) (string, error) {
	if len(expression.Args) != len(record.fields) && !hasNamedArguments(expression) {
		return "", e.fail(expression, "record construction requires every declared field")
	}
	arguments, err := e.valueArguments(expression, e.recordSignature(record), "build")
	if err != nil {
		return "", err
	}
	return e.recordLiteral(record, arguments), nil
}

func (e *emitter) recordLiteral(record *boundRecord, arguments []string) string {
	fields := make([]string, len(arguments))
	for index, value := range arguments {
		fields[index] = record.fields[index].Name() + ": " + value
	}
	return e.host.typeName(record.typ) + "{" + strings.Join(fields, ", ") + "}"
}

func (e *emitter) recordConstruction(rule *syntax.Rule, expression *syntax.Expr, record *boundRecord) (*matchir.Construction, error) {
	positions, err := e.builderPositions(expression, e.recordSignature(record), "build")
	if err != nil {
		return nil, err
	}
	result := &matchir.Construction{Record: record.schema}
	for written, argument := range expression.Args {
		result.Fields = append(result.Fields, matchir.FieldExpression{Field: matchir.FieldID(positions[written]), Expression: e.expressionID(rule, argument)})
	}
	return result, nil
}

func (e *emitter) recordConstructionValue(construction *matchir.Construction) (string, error) {
	record := e.records[construction.Record.ID]
	arguments := make([]string, len(record.fields))
	for _, field := range construction.Fields {
		expression := e.tables.expressions[field.Expression]
		e.rule, e.captures = expression.rule.Name, expression.captures
		value, err := e.valueExpected(expression.expression, "build", record.fields[field.Field].Type())
		if err != nil {
			return "", err
		}
		if expression.expression.Kind == syntax.Call {
			e.serial++
			temporary := fmt.Sprintf("hv%d", e.serial)
			fmt.Fprintf(&e.out, "var %s %s = %s\n", temporary, e.host.typeName(record.fields[field.Field].Type()), value)
			value = temporary
		}
		arguments[field.Field] = value
	}
	return e.recordLiteral(record, arguments), nil
}

func (e *emitter) emitRecord(node *matchir.Node) error {
	record := e.records[node.Record.ID]
	e.out.WriteString("{\n")
	for index, field := range record.fields {
		fmt.Fprintf(&e.out, "%s := %s.%s\n_ = %s\n", valueName(node.Outputs[index]), valueName(node.Input), field.Name(), valueName(node.Outputs[index]))
	}
	if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
		return err
	}
	e.out.WriteString("}\n")
	return nil
}

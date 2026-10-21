package gogen

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

func (e *emitter) constructorValue(expression *syntax.Expr, variant *boundVariant) (string, error) {
	if len(expression.Args) != len(variant.fields) && !hasNamedArguments(expression) {
		return "", e.fail(expression, "union constructor requires every declared field")
	}
	signature := e.constructorSignature(variant)
	arguments, err := e.valueArguments(expression, signature, "build")
	if err != nil {
		return "", err
	}
	fields := []string{variant.union.tag.Name() + ": " + variant.tag.Name()}
	for index, value := range arguments {
		fields = append(fields, variant.fields[index].Name()+": "+value)
	}
	return e.host.typeName(variant.union.typ) + "{" + strings.Join(fields, ", ") + "}", nil
}

func (e *emitter) constructorSignature(variant *boundVariant) *types.Signature {
	params := make([]*types.Var, len(variant.fields))
	for index := range params {
		params[index] = variant.signature.Results().At(index)
	}
	result := types.NewTuple(types.NewVar(0, e.host.Package, "", variant.union.typ))
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), result, false)
}

func (e *emitter) construction(rule *syntax.Rule, expression *syntax.Expr) (*matchir.Construction, error) {
	variant := e.constructors[expression.Name]
	if expression.Kind != syntax.Call || variant == nil {
		return nil, nil
	}
	positions, err := e.builderPositions(expression, e.constructorSignature(variant), "build")
	if err != nil {
		return nil, err
	}
	result := &matchir.Construction{Union: variant.union.schema, Variant: variant.id}
	for written, argument := range expression.Args {
		field := matchir.FieldExpression{Field: matchir.FieldID(positions[written]), Expression: e.expressionID(rule, argument)}
		result.Fields = append(result.Fields, field)
	}
	return result, nil
}

func (e *emitter) constructionValue(construction *matchir.Construction) (string, error) {
	union := e.unions[construction.Union.ID]
	variant := union.variants[construction.Variant]
	arguments := make([]string, len(variant.fields))
	for _, field := range construction.Fields {
		expression := e.tables.expressions[field.Expression]
		e.rule, e.captures = expression.rule.Name, expression.captures
		value, err := e.typedValue(expression.expression, variant.fields[field.Field].Type())
		if err != nil {
			return "", err
		}
		if expression.expression.Kind == syntax.Call || expression.expression.Kind == syntax.Effects {
			e.serial++
			temporary := fmt.Sprintf("hv%d", e.serial)
			fmt.Fprintf(&e.out, "var %s %s = %s\n", temporary, e.host.typeName(variant.fields[field.Field].Type()), value)
			value = temporary
		}
		arguments[field.Field] = value
	}
	fields := []string{union.tag.Name() + ": " + variant.tag.Name()}
	for index, value := range arguments {
		fields = append(fields, variant.fields[index].Name()+": "+value)
	}
	return e.host.typeName(union.typ) + "{" + strings.Join(fields, ", ") + "}", nil
}

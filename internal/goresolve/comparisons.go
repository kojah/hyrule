package goresolve

import (
	"go/token"
	"go/types"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

func (e *resolver) comparisonOperand(expression *semantic.Expr) (matchir.Operand, types.Type, error) {
	switch expression.Kind {
	case semantic.Variable:
		value, known := e.captures[expression.Name]
		if !known {
			return matchir.Operand{}, nil, e.fail(expression, "unbound capture $"+expression.Name)
		}
		return matchir.Value(value.id), value.typ, nil
	case semantic.Symbol:
		value, err := e.constant(expression)
		if err != nil {
			return matchir.Operand{}, nil, err
		}
		return matchir.Constant(value.schema.ID), value.object.Type(), nil
	case semantic.Literal:
		value, err := types.Eval(token.NewFileSet(), e.host.Package, token.NoPos, expression.Name)
		if err != nil {
			return matchir.Operand{}, nil, e.fail(expression, "invalid comparison literal")
		}
		return matchir.Literal(e.tables.literalID(expression.Name)), value.Type, nil
	default:
		return matchir.Operand{}, nil, e.fail(expression, "comparison operands require captures, constants or literals")
	}
}

func (e *resolver) compare(expression *semantic.Expr) error {
	left, leftType, err := e.comparisonOperand(expression.Args[0])
	if err != nil {
		return err
	}
	right, rightType, err := e.comparisonOperand(expression.Args[1])
	if err != nil {
		return err
	}
	if !types.AssignableTo(leftType, rightType) && !types.AssignableTo(rightType, leftType) {
		return e.fail(expression, "comparison requires compatible operand types")
	}
	operator, ordered := matchir.Equal, false
	switch expression.Name {
	case "==":
	case "!=":
		operator = matchir.NotEqual
	case "<":
		operator, ordered = matchir.Less, true
	case "<=":
		operator, ordered = matchir.LessEqual, true
	case ">":
		operator, ordered = matchir.Greater, true
	case ">=":
		operator, ordered = matchir.GreaterEqual, true
	default:
		return e.fail(expression, "unknown comparison operator")
	}
	if ordered {
		for _, typ := range []types.Type{leftType, rightType} {
			basic, known := typ.Underlying().(*types.Basic)
			if !known || basic.Info()&types.IsOrdered == 0 {
				return e.fail(expression, "ordered comparison requires ordered scalar types")
			}
		}
	} else if !safeEquality(leftType) || !safeEquality(rightType) || !types.Comparable(leftType) && !types.Comparable(rightType) {
		return e.fail(expression, "equality requires safe comparable operands")
	}
	e.cursor.Compare(operator, left, right)
	return nil
}

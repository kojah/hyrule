package gogen

import (
	"go/types"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

type boundConstant struct {
	object *types.Const
	schema matchir.ConstantSchema
}

func (e *emitter) bindConstant(binding *semantic.Binding) error {
	object, known := e.host.Package.Scope().Lookup(binding.HostName).(*types.Const)
	if !known {
		return &semantic.Diagnostic{Position: binding.Position, Message: "constant binding requires a Go constant " + binding.HostName}
	}
	for _, existing := range e.tables.constants {
		if existing.object == object {
			e.constants[binding.Name] = existing
			return nil
		}
	}
	value := &boundConstant{object: object, schema: matchir.ConstantSchema{ID: matchir.ConstantID(len(e.tables.constants)), Type: e.tables.typeID(object.Type())}}
	e.constants[binding.Name] = value
	e.tables.constants = append(e.tables.constants, value)
	return nil
}

func (e *emitter) constant(expression *semantic.Expr) (*boundConstant, error) {
	value := e.constants[expression.Name]
	if value == nil {
		return nil, e.fail(expression, "unknown constant "+expression.Name)
	}
	return value, nil
}

func (e *emitter) checkConstant(expression *semantic.Expr, input types.Type) error {
	value, err := e.constant(expression)
	if err != nil {
		return err
	}
	if !types.Comparable(input) || !types.AssignableTo(value.object.Type(), input) {
		return e.fail(expression, "constant pattern type mismatch")
	}
	return nil
}

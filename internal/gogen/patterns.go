package gogen

import (
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

func (e *emitter) bind(x *syntax.Expr, name string, value matchir.ValueID, t types.Type) error {
	if strings.Contains(name, ".") {
		return e.fail(x, "field projections cannot bind captures")
	}
	if old, ok := e.captures[name]; ok {
		if !types.Comparable(t) || !types.Identical(old.typ, t) {
			return e.fail(x, "repeated capture requires identical comparable types")
		}
		if !safeEquality(t) {
			return e.fail(x, "repeated capture may contain an interface; use an explicit equality guard")
		}
		if e.cursor != nil {
			e.cursor.Test(matchir.Value(old.id), matchir.Value(value))
		}
	} else {
		e.captures[name] = capture{value, t}
	}
	return nil
}

// Interfaces, including those embedded in comparable aggregates, can panic on ==.
func safeEquality(t types.Type) bool {
	switch t := t.Underlying().(type) {
	case *types.Interface:
		return false
	case *types.Array:
		return safeEquality(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !safeEquality(t.Field(i).Type()) {
				return false
			}
		}
	}
	return true
}
func (e *emitter) pattern(x *syntax.Expr, value matchir.ValueID, t types.Type) error {
	if x.Kind == syntax.Variable && strings.Contains(x.Name, ".") {
		return e.fail(x, "field projections are read-only values, not capture patterns")
	}
	if x.Bind != "" {
		if err := e.bind(x, x.Bind, value, t); err != nil {
			return err
		}
	}
	switch x.Kind {
	case syntax.Alternation:
		if len(x.Args) != 1 {
			return e.fail(x, "unexpanded alternatives")
		}
		return e.pattern(x.Args[0], value, t)
	case syntax.Wildcard:
		return nil
	case syntax.Variable:
		return e.bind(x, x.Name, value, t)
	case syntax.Literal:
		e.cursor.Test(matchir.Value(value), matchir.Literal(e.tables.literalID(x.Name)))
		return nil
	case syntax.Symbol:
		if err := e.checkConstant(x, t); err != nil {
			return err
		}
		e.cursor.Test(matchir.Value(value), matchir.Constant(e.constants[x.Name].schema.ID))
		return nil
	case syntax.Call:
		name, sig, indices, err := e.matcher(x, t)
		if err != nil {
			return err
		}
		outputTypes := make([]matchir.TypeID, sig.Results().Len()-1)
		for index := range outputTypes {
			outputTypes[index] = e.tables.typeID(sig.Results().At(index).Type())
		}
		var outputs []matchir.ValueID
		if record := e.recordNames[x.Name]; record != nil && !record.declaration.View {
			outputs = e.cursor.Record(record.schema, value)
		} else if constructor := e.constructors[x.Name]; constructor != nil {
			outputs = e.cursor.Variant(constructor.union.schema, constructor.id, value)
		} else {
			outputs = e.cursor.Match(e.tables.hookID(name), value, outputTypes)
		}
		for _, id := range outputs {
			e.serial = max(e.serial, int(id))
		}
		for child, arg := range x.Args {
			index := indices[child]
			if err := e.pattern(arg, outputs[index], sig.Results().At(index).Type()); err != nil {
				return err
			}
		}
		return nil
	default:
		return e.fail(x, "invalid pattern")
	}
}

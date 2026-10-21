package gogen

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

func (e *emitter) bind(x *syntax.Expr, name, value string, t types.Type) error {
	if old, ok := e.captures[name]; ok {
		if !types.Comparable(t) || !types.Identical(old.typ, t) {
			return e.fail(x, "repeated capture requires identical comparable types")
		}
		if !safeEquality(t) {
			return e.fail(x, "repeated capture may contain an interface; use an explicit equality guard")
		}
		fmt.Fprintf(&e.out, "if %s != %s { return zero, false }\n", old.name, value)
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
func (e *emitter) pattern(x *syntax.Expr, value string, t types.Type) error {
	if x.Bind != "" {
		if err := e.bind(x, x.Bind, value, t); err != nil {
			return err
		}
	}
	switch x.Kind {
	case syntax.Alternation:
		// Expansion retains a single-child wrapper only for nested whole captures.
		if len(x.Args) != 1 {
			return e.fail(x, "unexpanded alternatives")
		}
		return e.pattern(x.Args[0], value, t)
	case syntax.Wildcard:
		return nil
	case syntax.Variable:
		return e.bind(x, x.Name, value, t)
	case syntax.Literal:
		fmt.Fprintf(&e.out, "if %s != %s { return zero, false }\n", value, x.Name)
		return nil
	case syntax.Call:
		name, sig, indices, err := e.matcher(x, t)
		if err != nil {
			return err
		}
		n := sig.Results().Len()
		vars := []string{}
		for range n {
			e.serial++
			vars = append(vars, fmt.Sprintf("hv%d", e.serial))
		}
		fmt.Fprintf(&e.out, "%s := %s(%s)\nif !%s { return zero, false }\n", strings.Join(vars, ", "), name, value, vars[n-1])
		selected := make([]bool, n-1)
		for _, index := range indices {
			selected[index] = true
		}
		for index, variable := range vars[:n-1] {
			if !selected[index] {
				fmt.Fprintf(&e.out, "_ = %s\n", variable)
			}
		}
		for child, arg := range x.Args {
			i := indices[child]
			fmt.Fprintf(&e.out, "_ = %s\n", vars[i])
			if err = e.pattern(arg, vars[i], sig.Results().At(i).Type()); err != nil {
				return err
			}
		}
		return nil
	default:
		return e.fail(x, "invalid pattern")
	}
}

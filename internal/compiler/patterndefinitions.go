package compiler

import "github.com/kojah/lockdoc/tools/hyrule/internal/semantic"

// validateDefinition checks the entire declaration graph, including unused
// definitions, without expansion. Every capture is an explicit parameter.
func (e *Program) validateDefinition(name string, checked, active map[string]struct{}) error {
	definition := e.patterns[name]
	e.rule = name
	if _, ok := active[name]; ok {
		return e.fail(definition.Body, "recursive pattern "+name)
	}
	if _, ok := checked[name]; ok {
		return nil
	}
	if len(active) >= maxPatternDepth {
		return e.fail(definition.Body, "pattern dependency nesting exceeds 64")
	}
	if name == "or" {
		return e.fail(definition.Body, "or is reserved for alternatives")
	}
	if name == "update" {
		return e.fail(definition.Body, "update is reserved for record updates")
	}
	if _, intrinsic := Arithmetic(name); intrinsic {
		return e.fail(definition.Body, "arithmetic operation is reserved: "+name)
	}
	for _, binding := range e.Bindings {
		if binding.Name == name || binding.HostName == name {
			return e.fail(definition.Body, "pattern collides with Go hook "+name)
		}
	}
	parameters := map[string]struct{}{}
	for _, parameter := range definition.Parameters {
		if _, ok := parameters[parameter]; ok {
			return e.fail(definition.Body, "duplicate pattern parameter "+parameter)
		}
		parameters[parameter] = struct{}{}
	}
	active[name] = struct{}{}
	used := map[string]struct{}{}
	if err := e.validatePatternBody(definition.Body, parameters, used, func(callee string) error {
		err := e.validateDefinition(callee, checked, active)
		e.rule = name
		return err
	}); err != nil {
		return err
	}
	for _, parameter := range definition.Parameters {
		if _, ok := used[parameter]; !ok {
			return e.fail(definition.Body, "unused pattern parameter "+parameter)
		}
	}
	delete(active, name)
	checked[name] = struct{}{}
	return nil
}

func (e *Program) validatePatternBody(x *semantic.Expr, parameters, used map[string]struct{}, visit func(string) error) error {
	for _, capture := range []string{x.Bind, captureName(x)} {
		if capture != "" {
			used[capture] = struct{}{}
			if _, ok := parameters[capture]; !ok {
				return e.fail(x, "free capture $"+capture+" in reusable pattern")
			}
		}
	}
	if x.Kind == semantic.Call {
		if definition := e.patterns[x.Name]; definition != nil {
			if len(x.Args) != len(definition.Parameters) {
				return e.fail(x, "reusable pattern arity mismatch for "+x.Name)
			}
			if err := visit(x.Name); err != nil {
				return err
			}
		} else if binding := e.Bindings[x.Name]; e.RecordNames[x.Name] == nil && e.Constructors[x.Name] == nil && (binding == nil || binding.Kind != "match") {
			return e.fail(x, "unknown match pattern "+x.Name)
		}
	}
	if x.Kind == semantic.Symbol {
		if binding := e.Bindings[x.Name]; binding == nil || binding.Kind != "const" && binding.Kind != "value" {
			return e.fail(x, "unknown constant "+x.Name)
		}
	}
	for _, child := range x.Args {
		if err := e.validatePatternBody(child, parameters, used, visit); err != nil {
			return err
		}
	}
	return nil
}

func captureName(x *semantic.Expr) string {
	if x.Kind == semantic.Variable {
		return x.Name
	}
	return ""
}

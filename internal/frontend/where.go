package frontend

import (
	"maps"
	"strings"

	"github.com/kojah/hyrule/internal/semantic"
	"github.com/kojah/hyrule/internal/syntax"
)

// whereScope holds one equation's where bindings. Bodies are lowered at each
// use in the equation's pattern scope, so a binding is evaluated only where a
// guard or branch actually reaches it, like Haskell's lazy where.
type whereScope struct {
	bodies   map[string]*syntax.Expr
	captures map[string]bool
	active   map[string]bool
}

func (l lowering) bindWhere(rule *syntax.Rule) *whereScope {
	if len(rule.Where) == 0 {
		return nil
	}
	scope := &whereScope{bodies: map[string]*syntax.Expr{}, captures: maps.Clone(l.captures), active: map[string]bool{}}
	for _, binding := range rule.Where {
		switch {
		case scope.bodies[binding.Name] != nil:
			l.fail(binding.Value, "duplicate where binding "+binding.Name)
		case l.captures[binding.Name] || l.constructors[binding.Name] || l.names[binding.Name]:
			l.fail(binding.Value, "where binding "+binding.Name+" collides with a capture or declaration")
		}
		scope.bodies[binding.Name] = binding.Value
	}
	for _, binding := range rule.Where {
		l.checkWhereBody(binding.Name, binding.Value, scope)
	}
	return scope
}

// substitute replaces an unshadowed bare where name with its lowered body.
func (l lowering) substitute(x *syntax.Expr) (*semantic.Expr, bool) {
	if l.where == nil || !x.Bare {
		return nil, false
	}
	base, path, projected := strings.Cut(x.Name, ".")
	body := l.where.bodies[base]
	if body == nil || l.captures[base] {
		return nil, false
	}
	if projected {
		l.fail(x, "where binding "+base+" cannot be projected as "+base+"."+path+"; bind it with let")
		return &semantic.Expr{Position: position(x.Position), Kind: semantic.Variable, Name: x.Name}, true
	}
	if l.where.active[base] {
		l.fail(x, "where binding "+base+" refers to itself")
		return &semantic.Expr{Position: position(x.Position), Kind: semantic.Variable, Name: x.Name}, true
	}
	l.where.active[base] = true
	scoped := l
	scoped.captures = l.where.captures
	lowered := scoped.expression(body)
	l.where.active[base] = false
	copy := *lowered
	copy.Field = x.Field
	return &copy, true
}

// checkWhereBody rejects names that are neither equation captures, other where
// bindings, declarations nor bound inside the body itself. Guard locals and
// do-bound names are not in a where binding's scope.
func (l lowering) checkWhereBody(name string, body *syntax.Expr, scope *whereScope) {
	bound := map[string]bool{}
	binders(body, bound)
	var visit func(*syntax.Expr)
	visit = func(x *syntax.Expr) {
		if x == nil {
			return
		}
		if x.Kind == syntax.Variable || x.Bare && len(x.Args) == 0 {
			base, _, _ := strings.Cut(x.Name, ".")
			if !scope.captures[base] && scope.bodies[base] == nil && !l.constructors[base] && !l.names[base] && !bound[base] {
				l.fail(x, "where binding "+name+" refers to "+base+", which is not an equation capture")
			}
		}
		for _, arg := range x.Args {
			visit(arg)
		}
		for _, arm := range x.Arms {
			visit(arm.Result)
		}
	}
	visit(body)
}

// binders collects names introduced inside an expression by outcome patterns,
// whole captures and do bindings.
func binders(x *syntax.Expr, bound map[string]bool) {
	if x == nil {
		return
	}
	if x.Bind != "" {
		bound[x.Bind] = true
	}
	for _, arm := range x.Arms {
		collectPatternNames(arm.Pattern, bound)
		binders(arm.Result, bound)
	}
	for _, arg := range x.Args {
		binders(arg, bound)
	}
}

func collectPatternNames(x *syntax.Expr, bound map[string]bool) {
	if x == nil {
		return
	}
	if x.Bind != "" {
		bound[x.Bind] = true
	}
	if x.Kind == syntax.Variable || x.Bare {
		bound[x.Name] = true
	}
	for _, arg := range x.Args {
		collectPatternNames(arg, bound)
	}
}

func (l lowering) fail(x *syntax.Expr, message string) {
	if *l.failure == nil {
		*l.failure = &semantic.Diagnostic{Position: position(x.Position), Message: message}
	}
}

// Package goresolve resolves Go hook/storage contracts and constructs typed
// matching and value plans. It never emits the dispatcher or result statements.
package goresolve

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/compiler"
	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
	"go/types"
)

// Host supplies already loaded Go signatures and storage types.
type Host = hostapi.Host
type capture struct {
	id  matchir.ValueID
	typ types.Type
}
type resolver struct {
	host         *Host
	bindings     map[string]*semantic.Binding
	captures     map[string]capture
	rule         string
	serial       int
	cursor       *matchir.Cursor
	graph        *matchir.Graph
	tables       decisionTables
	unions       []*boundUnion
	constructors map[string]*boundVariant
	constants    map[string]*boundConstant
	records      []*boundRecord
	recordNames  map[string]*boundRecord
}

func Resolve(h Host, program *compiler.Program) (*goplan.Program, error) {
	h.Imports = map[string]string{}
	if h.Signature == nil {
		if program.API != nil || len(program.Bindings) != 0 || len(program.Unions) != 0 || program.HasRules() {
			return nil, fmt.Errorf("schema target accepts only records and derivations")
		}
		e := resolver{host: &h}
		if err := e.bindRecords(program.Records); err != nil {
			return nil, err
		}
		return e.publish(nil, nil), nil
	}
	native, err := nativeAPI(&h, program.API)
	if err != nil {
		return nil, err
	}
	sig := h.Signature
	e := resolver{host: &h, bindings: program.Bindings, constants: map[string]*boundConstant{}}
	if err := e.bindUnions(program.Unions); err != nil {
		return nil, err
	}
	if err := e.bindRecords(program.Records); err != nil {
		return nil, err
	}
	if native != nil {
		e.bindNativeRecords(native)
	}
	for _, binding := range program.BindingOrder {
		object := e.host.Package.Scope().Lookup(binding.HostName)
		if _, variable := object.(*types.Var); binding.Kind == "value" && variable {
			return nil, &semantic.Diagnostic{Position: binding.Position, Message: "host value binding requires a function or constant"}
		}
		_, isConstant := object.(*types.Const)
		if binding.Kind == "const" || binding.Kind == "value" && isConstant {
			if err := e.bindConstant(binding); err != nil {
				return nil, err
			}
		} else if _, _, err := e.host.Hook(binding.HostName); err != nil {
			return nil, &semantic.Diagnostic{Position: binding.Position, Message: err.Error()}
		}
	}
	rules, err := program.Expand(func(rule *semantic.Rule) error {
		checker := resolver{host: e.host, bindings: e.bindings, constructors: e.constructors,
			recordNames: e.recordNames, constants: e.constants, captures: map[string]capture{}, rule: rule.Name}
		return checker.checkPattern(rule.Match, sig.Params().At(0).Type())
	})
	if err != nil {
		return nil, err
	}
	graph, err := e.decisions(rules, sig.Params().At(0).Type())
	if err != nil {
		return nil, err
	}
	if native != nil && native.declaration.Mode == "total" {
		e.captures = map[string]capture{}
		e.rule = "signature fallback"
		native.fallback, err = e.resolveResult(native.declaration.Fallback, native.output, false)
		if err != nil {
			return nil, err
		}
	}
	return e.publish(graph, native), nil
}
func (e *resolver) fail(x *semantic.Expr, message string) error {
	return &semantic.Diagnostic{Position: x.Position, Rule: e.rule, Message: message}
}
func (e *resolver) binding(x *semantic.Expr, kind string) (string, *types.Signature, error) {
	b := e.bindings[x.Name]
	if b == nil || !bindingKindMatches(b.Kind, kind) {
		return "", nil, e.fail(x, "expected "+kind+" binding for "+x.Name)
	}
	name, sig, err := e.host.Hook(b.HostName)
	if err != nil {
		return "", nil, e.fail(x, err.Error())
	}
	return name, sig, nil
}

func bindingKindMatches(declared, required string) bool {
	if declared == required {
		return true
	}
	return declared == "value" && (required == "build" || required == "guard")
}

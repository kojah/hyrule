// Package recordresolve checks schema-maintenance contracts and produces typed helper plans.
// Field policies are explicit; storage identity and host evidence are never
// inferred from a field's spelling. Generated functions have no runtime library.
package recordresolve

import (
	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/compiler"
	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

type field struct {
	declaration semantic.DerivedField
	member      *types.Var
}

type operation struct {
	host        *hostapi.Host
	program     *compiler.Program
	declaration *semantic.DerivedOperation
	typ         types.Type
	sig         *types.Signature
	fields      []field
	plan        goplan.Derivation
}

// Resolve validates derivation signatures and field policies into typed plans.
// The shared compiler validates names and field coverage before this backend.
func Resolve(host *hostapi.Host, program *compiler.Program) ([]goplan.Derivation, error) {
	if err := program.ValidateDerivations(host.Function); err != nil {
		return nil, err
	}
	var result []goplan.Derivation
	for _, derive := range program.Derivations {
		plans, err := resolveDerivation(host, program, derive)
		if err != nil {
			return nil, err
		}
		result = append(result, plans...)
	}
	return result, nil
}

func (op *operation) fail(message string) error {
	return &semantic.Diagnostic{Position: op.declaration.Position, Message: op.declaration.Kind + " " + op.declaration.Function + ": " + message}
}

func (op *operation) resolve() error {
	op.plan = goplan.Derivation{Kind: goplan.DerivationKind(op.declaration.Kind), Function: op.declaration.Function, Signature: op.sig, Type: op.typ}
	if op.declaration.Kind != "load" && (op.sig.Params().Len() < 1 || !types.Identical(op.sig.Params().At(0).Type(), op.typ)) {
		return op.fail("first parameter must be the record type")
	}
	switch op.plan.Kind {
	case goplan.Freeze:
		return op.freeze()
	case goplan.Key:
		return op.key()
	case goplan.Equal:
		return op.equal()
	case goplan.Visit, goplan.Transform:
		return op.children()
	case goplan.Display:
		return op.display()
	case goplan.Store, goplan.Load:
		return op.storage()
	default:
		return op.fail("unknown derivation operation")
	}
}

func (op *operation) signature(params, results int, result types.Type) error {
	if op.sig.Params().Len() != params || op.sig.Results().Len() != results || result != nil && !types.Identical(op.sig.Results().At(0).Type(), result) {
		return op.fail("invalid generated function signature")
	}
	return nil
}

func (op *operation) hook(name string, inputs []types.Type, results int) (string, *types.Signature, error) {
	function, sig, err := op.host.Hook(name)
	if err != nil {
		return "", nil, op.fail(err.Error())
	}
	// Generated maintenance functions have no implicit matching host. Explicit
	// package functions and method expressions retain all of their parameters.
	if strings.HasPrefix(function, "harg1.") || sig.Params().Len() != len(inputs) || sig.Results().Len() != results || sig.Variadic() {
		return "", nil, op.fail("leaf hook signature mismatch: " + name)
	}
	for i, input := range inputs {
		if !types.Identical(sig.Params().At(i).Type(), input) {
			return "", nil, op.fail("leaf hook input mismatch: " + name)
		}
	}
	return function, sig, nil
}

// Immutable scalar aggregates can be copied by value. Pointers/interfaces and
// collection storage need an explicit borrow, clone or domain hook policy.
func immutable(typ types.Type) bool {
	switch typ := typ.Underlying().(type) {
	case *types.Basic:
		return typ.Kind() != types.UnsafePointer && typ.Kind() != types.Invalid
	case *types.Array:
		return immutable(typ.Elem())
	case *types.Struct:
		for i := 0; i < typ.NumFields(); i++ {
			if !immutable(typ.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return false
}

func comparable(typ types.Type) bool {
	switch typ := typ.Underlying().(type) {
	case *types.Interface:
		return false
	case *types.Array:
		return comparable(typ.Elem())
	case *types.Struct:
		for i := 0; i < typ.NumFields(); i++ {
			if !comparable(typ.Field(i).Type()) {
				return false
			}
		}
	}
	return types.Comparable(typ)
}

func resolveDerivation(host *hostapi.Host, program *compiler.Program, derive *semantic.Derivation) ([]goplan.Derivation, error) {
	var result []goplan.Derivation
	record := program.RecordNames[derive.Record]
	typ, err := host.Type(record.HostType)
	if err != nil {
		return nil, err
	}
	structure, ok := typ.Underlying().(*types.Struct)
	if !ok {
		return nil, &semantic.Diagnostic{Position: derive.Position, Message: "derive requires struct storage"}
	}
	members := map[string]*types.Var{}
	for _, declared := range record.Fields {
		for i := 0; i < structure.NumFields(); i++ {
			if structure.Field(i).Name() == declared.HostName {
				members[declared.Name] = structure.Field(i)
			}
		}
	}
	for _, declaration := range derive.Operations {
		resolved, err := host.Type(declaration.Signature)
		if err != nil {
			return nil, err
		}
		sig, ok := resolved.Underlying().(*types.Signature)
		if !ok || sig.Variadic() || sig.TypeParams() != nil && sig.TypeParams().Len() != 0 {
			return nil, &semantic.Diagnostic{Position: declaration.Position, Message: "derivation requires non-generic function signature"}
		}
		op := operation{host: host, program: program, declaration: declaration, typ: typ, sig: sig}
		for _, policy := range declaration.Fields {
			member := members[policy.Name]
			if member == nil || !member.Exported() && member.Pkg() != host.Package {
				return nil, op.fail("missing or inaccessible field " + policy.Name)
			}
			op.fields = append(op.fields, field{policy, member})
		}
		if err := op.resolve(); err != nil {
			return nil, err
		}
		result = append(result, op.plan)
	}
	return result, nil
}

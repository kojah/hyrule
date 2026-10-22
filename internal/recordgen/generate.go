// Package recordgen emits representation maintenance from complete schemas.
// Field policies are explicit; storage identity and host evidence are never
// inferred from a field's spelling. Generated functions have no runtime library.
package recordgen

import (
	"fmt"
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
	out         strings.Builder
}

// Generate resolves derivation signatures and appends ordinary typed functions.
// The shared compiler validates names and field coverage before this backend.
func Generate(host *hostapi.Host, program *compiler.Program) (string, error) {
	if err := program.ValidateDerivations(host.Function); err != nil {
		return "", err
	}
	var result strings.Builder
	for _, derive := range program.Derivations {
		record := program.RecordNames[derive.Record]
		typ, err := host.Type(record.HostType)
		if err != nil {
			return "", err
		}
		structure, ok := typ.Underlying().(*types.Struct)
		if !ok {
			return "", &semantic.Diagnostic{Position: derive.Position, Message: "derive requires struct storage"}
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
				return "", err
			}
			sig, ok := resolved.Underlying().(*types.Signature)
			if !ok || sig.Variadic() || sig.TypeParams() != nil && sig.TypeParams().Len() != 0 {
				return "", &semantic.Diagnostic{Position: declaration.Position, Message: "derivation requires non-generic function signature"}
			}
			op := operation{host: host, program: program, declaration: declaration, typ: typ, sig: sig}
			for _, policy := range declaration.Fields {
				member := members[policy.Name]
				if member == nil || !member.Exported() && member.Pkg() != host.Package {
					return "", op.fail("missing or inaccessible field " + policy.Name)
				}
				op.fields = append(op.fields, field{policy, member})
			}
			if err := op.emit(); err != nil {
				return "", err
			}
			result.WriteString(op.out.String())
		}
	}
	return result.String(), nil
}

func (op *operation) fail(message string) error {
	return &semantic.Diagnostic{Position: op.declaration.Position, Message: op.declaration.Kind + " " + op.declaration.Function + ": " + message}
}

func (op *operation) emit() error {
	var parameters []string
	for i := 0; i < op.sig.Params().Len(); i++ {
		parameters = append(parameters, fmt.Sprintf("h%d %s", i, op.host.TypeName(op.sig.Params().At(i).Type())))
	}
	var results []string
	for i := 0; i < op.sig.Results().Len(); i++ {
		results = append(results, op.host.TypeName(op.sig.Results().At(i).Type()))
	}
	fmt.Fprintf(&op.out, "// %s is schema-derived %s; host leaf hooks retain their declared contracts.\nfunc %s(%s) (%s) {\n", op.declaration.Function, op.declaration.Kind, op.declaration.Function, strings.Join(parameters, ","), strings.Join(results, ","))
	if op.declaration.Kind != "load" && (op.sig.Params().Len() < 1 || !types.Identical(op.sig.Params().At(0).Type(), op.typ)) {
		return op.fail("first parameter must be the record type")
	}
	var err error
	switch op.declaration.Kind {
	case "freeze":
		err = op.freeze()
	case "key":
		err = op.key()
	case "equal":
		err = op.equal()
	case "visit", "transform":
		err = op.children()
	case "display":
		err = op.display()
	case "store", "load":
		err = op.storage()
	}
	op.out.WriteString("}\n")
	return err
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

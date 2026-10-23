package hyrule

import (
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"github.com/kojah/hyrule/internal/semantic"
	"golang.org/x/tools/go/packages"
)

// inferredType spells the Go function type declared by a rule signature, or
// "" when the rules leave the API to a handwritten signature type. Partial and
// implicit APIs append the success Boolean; total APIs return outputs alone.
func inferredType(api *semantic.API) string {
	if api == nil || len(api.Inputs) == 0 {
		return ""
	}
	results := append([]semantic.Param(nil), api.Outputs...)
	if api.Mode != "total" {
		results = append(results, semantic.Param{Type: "bool"})
	}
	return "func(" + goParameters(api.Inputs) + ") (" + goParameters(results) + ")"
}

// goParameters names every parameter when any is named, using _ otherwise.
func goParameters(params []semantic.Param) string {
	named := false
	for _, param := range params {
		named = named || param.Name != ""
	}
	parts := make([]string, len(params))
	for i, param := range params {
		parts[i] = param.Type
		if named {
			name := param.Name
			if name == "" {
				name = "_"
			}
			parts[i] = name + " " + param.Type
		}
	}
	return strings.Join(parts, ", ")
}

// targetSignature selects the generated API. A handwritten binding-file
// signature must be identical to the rule-declared types when both exist;
// otherwise the rule types define it and a named signature is generated.
// It returns the signature and the type name the generator must declare.
func (c *Config) targetSignature(pkg *packages.Package, t Target, api *semantic.API) (*types.Signature, string, error) {
	var declared *types.Signature
	if t.Signature != "" {
		object := pkg.Types.Scope().Lookup(t.Signature)
		if object != nil && filepath.Clean(pkg.Fset.Position(object.Pos()).Filename) == filepath.Join(c.root, t.Bindings) {
			signature, ok := object.Type().Underlying().(*types.Signature)
			if !ok {
				return nil, "", fmt.Errorf("%s is not a function signature", t.Signature)
			}
			declared = signature
		}
	}
	if inferredType(api) == "" {
		if declared == nil && t.Signature == "" {
			return nil, "", fmt.Errorf("signature requires declared rule types or a Go signature type")
		}
		if declared == nil {
			return nil, "", fmt.Errorf("missing signature %s", t.Signature)
		}
		return declared, "", nil
	}
	stub, ok := pkg.Types.Scope().Lookup(t.Function).(*types.Func)
	if !ok {
		return nil, "", fmt.Errorf("missing inferred function %s", t.Function)
	}
	inferred := namedSignature(pkg.Types, stub.Type().(*types.Signature), api)
	if declared != nil {
		if !types.Identical(declared, inferred) {
			return nil, "", fmt.Errorf("signature %s is %s, but its rules declare %s", t.Signature,
				types.TypeString(declared, types.RelativeTo(pkg.Types)), types.TypeString(inferred, types.RelativeTo(pkg.Types)))
		}
		return declared, "", nil
	}
	return inferred, t.Signature, nil
}

// namedSignature retains the rule-declared parameter and result names, which
// name native inputs and outputs fields; unnamed slots stay unnamed.
func namedSignature(pkg *types.Package, stub *types.Signature, api *semantic.API) *types.Signature {
	rename := func(tuple *types.Tuple, params []semantic.Param) *types.Tuple {
		vars := make([]*types.Var, tuple.Len())
		for i := range vars {
			name := ""
			if i < len(params) {
				name = params[i].Name
			}
			vars[i] = types.NewVar(token.NoPos, pkg, name, tuple.At(i).Type())
		}
		return types.NewTuple(vars...)
	}
	return types.NewSignatureType(nil, nil, nil, rename(stub.Params(), api.Inputs), rename(stub.Results(), api.Outputs), false)
}

package recordgen

import (
	"fmt"
	"go/types"
	"strings"
)

func (op *operation) freeze() error {
	if err := op.signature(1, 1, op.typ); err != nil {
		return err
	}
	op.out.WriteString("result := h0\n")
	for _, field := range op.fields {
		selector := "h0." + field.member.Name()
		target := "result." + field.member.Name()
		switch policy := field.declaration.Policy; policy {
		case "keep":
			if !immutable(field.member.Type()) {
				return op.fail("field " + field.declaration.Name + " requires borrow, clone or a typed freeze hook")
			}
		case "borrow": // The schema explicitly owns the immutable/lifetime promise.
		case "clone", "clone_borrow":
			slice, ok := field.member.Type().Underlying().(*types.Slice)
			if !ok || policy == "clone" && !immutable(slice.Elem()) {
				return op.fail("clone requires a slice of immutable scalar aggregates; use a deep freeze hook")
			}
			fmt.Fprintf(&op.out, "if %s != nil { %s = make(%s,len(%s)); copy(%s,%s) }\n", selector, target, op.host.TypeName(field.member.Type()), selector, target, selector)
		default:
			function, sig, err := op.hook(policy, []types.Type{field.member.Type()}, 1)
			if err != nil {
				return err
			}
			if !types.Identical(sig.Results().At(0).Type(), field.member.Type()) {
				return op.fail("freeze hook must return the field type")
			}
			fmt.Fprintf(&op.out, "%s = %s(%s)\n", target, function, selector)
		}
	}
	op.out.WriteString("return result\n")
	return nil
}

func (op *operation) projection(field field, input string) (string, types.Type, error) {
	value := input + "." + field.member.Name()
	typ := field.member.Type()
	if strings.HasPrefix(field.declaration.Policy, "$") {
		if op.declaration.Kind != "key" {
			return "", nil, op.fail("parameter projections are only valid for key derivation")
		}
		name := strings.TrimPrefix(field.declaration.Policy, "$")
		found := false
		for i := 1; i < op.sig.Params().Len(); i++ {
			parameter := op.sig.Params().At(i)
			if parameter.Name() == name {
				value, typ = fmt.Sprintf("h%d", i), parameter.Type()
				found = true
				break
			}
		}
		if !found {
			return "", nil, op.fail("unknown key projection parameter $" + name)
		}
	} else if field.declaration.Policy != "keep" {
		function, sig, err := op.hook(field.declaration.Policy, []types.Type{typ}, 1)
		if err != nil {
			return "", nil, err
		}
		value, typ = function+"("+value+")", sig.Results().At(0).Type()
	}
	if !comparable(typ) {
		return "", nil, op.fail("key/equality field requires a safe comparable projection: " + field.declaration.Name)
	}
	return value, typ, nil
}

func (op *operation) key() error {
	if op.sig.Params().Len() < 1 || op.sig.Results().Len() != 1 {
		return op.fail("key requires a record and optional named projection parameters with one result")
	}
	for i := 1; i < op.sig.Params().Len(); i++ {
		name := op.sig.Params().At(i).Name()
		used := false
		for _, field := range op.fields {
			used = used || field.declaration.Policy == "$"+name
		}
		if name == "" || name == "_" || !used {
			return op.fail("key projection parameters must be named and used")
		}
	}
	resultType := op.sig.Results().At(0).Type()
	if !comparable(resultType) {
		return op.fail("key result must be safely comparable")
	}
	values, err := op.keyFields(resultType, false)
	if err != nil {
		return err
	}

	fmt.Fprintf(&op.out, "return %s{%s}\n", op.host.TypeName(resultType), strings.Join(values, ","))
	return nil
}

func (op *operation) equal() error {
	if err := op.signature(2, 1, types.Typ[types.Bool]); err != nil {
		return err
	}
	if !types.Identical(op.sig.Params().At(1).Type(), op.typ) {
		return op.fail("second equality parameter must be the record type")
	}
	for i, field := range op.fields {
		if field.declaration.Policy == "omit" {
			continue
		}
		left, _, err := op.projection(field, "h0")
		if err != nil {
			return err
		}
		right, _, err := op.projection(field, "h1")
		if err != nil {
			return err
		}
		// Evaluate each projection once, in declaration order. Hooks are pure.
		fmt.Fprintf(&op.out, "hl%d,hr%d := %s,%s\nif hl%d != hr%d { return false }\n", i, i, left, right, i, i)
	}
	op.out.WriteString("return true\n")
	return nil
}

func (op *operation) keyFields(resultType types.Type, exact bool) ([]string, error) {
	var resultFields map[string]*types.Var
	for _, record := range op.program.Records {
		if record.View {
			continue
		}
		candidate, err := op.host.Type(record.GoType)
		if err != nil {
			return nil, err
		}
		if !types.Identical(candidate, resultType) {
			continue
		}
		structure := candidate.Underlying().(*types.Struct)
		resultFields = map[string]*types.Var{}
		for _, declared := range record.Fields {
			for i := 0; i < structure.NumFields(); i++ {
				if structure.Field(i).Name() == declared.GoName {
					resultFields[declared.Name] = structure.Field(i)
				}
			}
		}
	}
	if resultFields == nil {
		return nil, op.fail("key result requires a complete record schema")
	}
	var values []string
	for _, field := range op.fields {
		if field.declaration.Policy == "omit" {
			if exact {
				return nil, op.fail("exact storage cannot omit field " + field.declaration.Name)
			}
			continue
		}
		value, typ, err := op.projection(field, "h0")
		if err != nil {
			return nil, err
		}
		result := resultFields[field.declaration.Name]
		if result == nil || !types.Identical(result.Type(), typ) {
			return nil, op.fail("key result field mismatch: " + field.declaration.Name)
		}
		values = append(values, result.Name()+":"+value)
		delete(resultFields, field.declaration.Name)
	}
	if len(resultFields) != 0 {
		return nil, op.fail("key result has fields without source policies")
	}
	return values, nil
}

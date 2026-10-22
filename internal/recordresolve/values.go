package recordresolve

import (
	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
	"go/types"
	"strings"
)

func (op *operation) freeze() error {
	if err := op.signature(1, 1, op.typ); err != nil {
		return err
	}
	for _, field := range op.fields {
		planned := goplan.DerivedField{Name: field.declaration.Name, Member: field.member}
		switch policy := field.declaration.Policy; policy {
		case "keep":
			if !immutable(field.member.Type()) {
				return op.fail("field " + field.declaration.Name + " requires borrow, clone or a typed freeze hook")
			}
		case "borrow":
		case "clone", "clone_borrow":
			slice, ok := field.member.Type().Underlying().(*types.Slice)
			if !ok || policy == "clone" && !immutable(slice.Elem()) {
				return op.fail("clone requires a slice of immutable scalar aggregates; use a deep freeze hook")
			}
			planned.Policy = goplan.Clone
		default:
			function, sig, err := op.hook(policy, []types.Type{field.member.Type()}, 1)
			if err != nil {
				return err
			}
			if !types.Identical(sig.Results().At(0).Type(), field.member.Type()) {
				return op.fail("freeze hook must return the field type")
			}
			planned.Policy, planned.Hook = goplan.CallHook, function
		}
		op.plan.Fields = append(op.plan.Fields, planned)
	}
	return nil
}

func (op *operation) projection(field field) (goplan.DerivedField, types.Type, error) {
	planned := goplan.DerivedField{Name: field.declaration.Name, Member: field.member}
	typ := field.member.Type()
	if strings.HasPrefix(field.declaration.Policy, "$") {
		if op.plan.Kind != goplan.Key {
			return planned, nil, op.fail("parameter projections are only valid for key derivation")
		}
		name := strings.TrimPrefix(field.declaration.Policy, "$")
		for i := 1; i < op.sig.Params().Len(); i++ {
			parameter := op.sig.Params().At(i)
			if parameter.Name() == name {
				planned.Policy = goplan.ProjectParameter
				planned.Parameter = i
				typ = parameter.Type()
				break
			}
		}
		if planned.Policy != goplan.ProjectParameter {
			return planned, nil, op.fail("unknown key projection parameter $" + name)
		}
	} else if field.declaration.Policy != "keep" {
		function, sig, err := op.hook(field.declaration.Policy, []types.Type{typ}, 1)
		if err != nil {
			return planned, nil, err
		}
		planned.Policy, planned.Hook, typ = goplan.CallHook, function, sig.Results().At(0).Type()
	}
	if !comparable(typ) {
		return planned, nil, op.fail("key/equality field requires a safe comparable projection: " + field.declaration.Name)
	}
	return planned, typ, nil
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
	typ := op.sig.Results().At(0).Type()
	if !comparable(typ) {
		return op.fail("key result must be safely comparable")
	}
	op.plan.KeyType = typ
	return op.keyFields(typ, false)
}

func (op *operation) equal() error {
	if err := op.signature(2, 1, types.Typ[types.Bool]); err != nil {
		return err
	}
	if !types.Identical(op.sig.Params().At(1).Type(), op.typ) {
		return op.fail("second equality parameter must be the record type")
	}
	for _, field := range op.fields {
		if field.declaration.Policy == "omit" {
			continue
		}
		planned, _, err := op.projection(field)
		if err != nil {
			return err
		}
		op.plan.Fields = append(op.plan.Fields, planned)
	}
	return nil
}

func (op *operation) keyFields(resultType types.Type, exact bool) error {
	var resultFields map[string]*types.Var
	for _, record := range op.program.Records {
		if record.View {
			continue
		}
		candidate, err := op.host.Type(record.HostType)
		if err != nil {
			return err
		}
		if !types.Identical(candidate, resultType) {
			continue
		}
		structure := candidate.Underlying().(*types.Struct)
		resultFields = map[string]*types.Var{}
		for _, declared := range record.Fields {
			for i := 0; i < structure.NumFields(); i++ {
				if structure.Field(i).Name() == declared.HostName {
					resultFields[declared.Name] = structure.Field(i)
				}
			}
		}
	}
	if resultFields == nil {
		return op.fail("key result requires a complete record schema")
	}
	for _, field := range op.fields {
		if field.declaration.Policy == "omit" {
			if exact {
				return op.fail("exact storage cannot omit field " + field.declaration.Name)
			}
			continue
		}
		planned, typ, err := op.projection(field)
		if err != nil {
			return err
		}
		member := resultFields[field.declaration.Name]
		if member == nil || !types.Identical(member.Type(), typ) {
			return op.fail("key result field mismatch: " + field.declaration.Name)
		}
		planned.Destination = member
		op.plan.Fields = append(op.plan.Fields, planned)
		delete(resultFields, field.declaration.Name)
	}
	if len(resultFields) != 0 {
		return op.fail("key result has fields without source policies")
	}
	return nil
}

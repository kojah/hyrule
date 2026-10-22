package recordresolve

import (
	"go/types"
)

// Storage is intentionally separate from pure matching. A caller owns the table
// and supplies its entry budget; derivation does not discover identities or work.
func (op *operation) storage() error {
	loading := op.declaration.Kind == "load"
	params := 3
	if loading {
		params = 2
	}
	if err := op.signature(params, 2, nil); err != nil {
		return err
	}
	if !types.Identical(op.sig.Results().At(1).Type(), types.Typ[types.Bool]) {
		return op.fail("storage must return a success boolean")
	}
	dictionary, index, err := op.tableStorage()
	if err != nil {
		return err
	}
	id := dictionary.Elem()
	if loading {
		if !types.Identical(op.sig.Params().At(0).Type(), id) || !types.Identical(op.sig.Results().At(0).Type(), op.typ) {
			return op.fail("load requires (ID,*Table)(Record,bool)")
		}

		return nil
	}
	if !types.Identical(op.sig.Results().At(0).Type(), id) || !types.Identical(op.sig.Params().At(2).Type(), types.Typ[types.Int]) {
		return op.fail("store requires (Record,*Table,int)(ID,bool)")
	}
	var freeze string
	for _, derive := range op.program.Derivations {
		record := op.program.RecordNames[derive.Record]
		typ, err := op.host.Type(record.HostType)
		if err != nil {
			return err
		}
		if !types.Identical(typ, op.typ) {
			continue
		}
		for _, candidate := range derive.Operations {
			if candidate.Kind == "freeze" {
				if freeze != "" {
					return op.fail("store requires one unambiguous freeze derivation")
				}
				freeze = candidate.Function
			}
		}
	}
	if freeze == "" {
		return op.fail("store requires a freeze derivation for owned publication")
	}
	if err := op.keyFields(dictionary.Key(), true); err != nil {
		return err
	}
	op.plan.IDType = id
	op.plan.KeyType = dictionary.Key()
	op.plan.IndexType = index.Type()
	op.plan.Freeze = freeze
	return nil
}

func (op *operation) tableStorage() (*types.Map, *types.Var, error) {
	pointer, ok := op.sig.Params().At(1).Type().Underlying().(*types.Pointer)
	if !ok {
		return nil, nil, op.fail("second parameter must point to caller-owned table storage")
	}
	structure, ok := pointer.Elem().Underlying().(*types.Struct)
	if !ok || structure.NumFields() != 2 {
		return nil, nil, op.fail("table requires exactly Index and Values fields")
	}
	var index, values *types.Var
	for i := 0; i < structure.NumFields(); i++ {
		member := structure.Field(i)
		switch member.Name() {
		case "Index":
			index = member
		case "Values":
			values = member
		}
	}
	if index == nil || values == nil {
		return nil, nil, op.fail("table requires Index and Values fields")
	}
	dictionary, ok := index.Type().Underlying().(*types.Map)
	if !ok || !comparable(dictionary.Key()) {
		return nil, nil, op.fail("Index must map safe comparable keys to IDs")
	}
	list, ok := values.Type().Underlying().(*types.Slice)
	if !ok || !types.Identical(list.Elem(), op.typ) {
		return nil, nil, op.fail("Values must store the record type")
	}
	id := dictionary.Elem()
	basic, ok := id.Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsUnsigned == 0 || basic.Kind() == types.Uintptr {
		return nil, nil, op.fail("storage ID must be an unsigned integer")
	}
	return dictionary, index, nil
}

package recordgen

import (
	"fmt"
	"go/types"
	"strings"
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
	pointer, ok := op.sig.Params().At(1).Type().Underlying().(*types.Pointer)
	if !ok {
		return op.fail("second parameter must point to caller-owned table storage")
	}
	structure, ok := pointer.Elem().Underlying().(*types.Struct)
	if !ok || structure.NumFields() != 2 {
		return op.fail("table requires exactly Index and Values fields")
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
		return op.fail("table requires Index and Values fields")
	}
	dictionary, ok := index.Type().Underlying().(*types.Map)
	if !ok || !comparable(dictionary.Key()) {
		return op.fail("Index must map safe comparable keys to IDs")
	}
	list, ok := values.Type().Underlying().(*types.Slice)
	if !ok || !types.Identical(list.Elem(), op.typ) {
		return op.fail("Values must store the record type")
	}
	id := dictionary.Elem()
	basic, ok := id.Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsUnsigned == 0 || basic.Kind() == types.Uintptr {
		return op.fail("storage ID must be an unsigned integer")
	}
	if loading {
		if !types.Identical(op.sig.Params().At(0).Type(), id) || !types.Identical(op.sig.Results().At(0).Type(), op.typ) {
			return op.fail("load requires (ID,*Table)(Record,bool)")
		}
		fmt.Fprintf(&op.out, "if h1 == nil || h0 == 0 || uint64(h0)>uint64(len(h1.Values)) { var zero %s; return zero,false }; return h1.Values[int(h0)-1],true\n", op.host.TypeName(op.typ))
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
	fields, err := op.keyFields(dictionary.Key(), true)
	if err != nil {
		return err
	}
	fmt.Fprintf(&op.out, "if h1 == nil { return 0,false }; key := %s{%s}\nif id,ok := h1.Index[key]; ok { return id,true }\n", op.host.TypeName(dictionary.Key()), strings.Join(fields, ","))
	// The entry count and ID conversion are checked before allocating or mutating.
	fmt.Fprintf(&op.out, "if h2 <= 0 || len(h1.Values)>=h2 { return 0,false }\nnext := %s(uint64(len(h1.Values))+1)\nif next == 0 || uint64(next)!=uint64(len(h1.Values))+1 { return 0,false }\n", op.host.TypeName(id))
	fmt.Fprintf(&op.out, "frozen := %s(h0)\nif h1.Index == nil { h1.Index=make(%s) }\nh1.Values=append(h1.Values,frozen)\nh1.Index[key]=next\nreturn next,true\n", freeze, op.host.TypeName(index.Type()))
	return nil
}

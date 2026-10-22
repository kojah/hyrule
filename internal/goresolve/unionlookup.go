package goresolve

import "go/types"

func (e *resolver) unionFor(typ types.Type) *boundUnion {
	for _, union := range e.unions {
		if types.Identical(typ, union.typ) {
			return union
		}
	}
	return nil
}

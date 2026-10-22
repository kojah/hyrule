package recordgen

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
)

func (op *operation) storage() {
	if op.plan.Kind == goplan.Load {
		fmt.Fprintf(&op.out, "if h1 == nil || h0 == 0 || uint64(h0)>uint64(len(h1.Values)) { var zero %s; return zero,false }; ", op.host.TypeName(op.plan.Type))
		op.out.WriteString("return h1.Values[int(h0)-1],true\n")
		return
	}
	fmt.Fprintf(&op.out, "if h1 == nil { return 0,false }; key := %s{%s}\n", op.host.TypeName(op.plan.KeyType), op.keyFields())
	op.out.WriteString("if id,ok := h1.Index[key]; ok { return id,true }\nif h2 <= 0 || len(h1.Values)>=h2 { return 0,false }\n")
	fmt.Fprintf(&op.out, "next := %s(uint64(len(h1.Values))+1)\n", op.host.TypeName(op.plan.IDType))
	op.out.WriteString("if next == 0 || uint64(next)!=uint64(len(h1.Values))+1 { return 0,false }\n")
	fmt.Fprintf(&op.out, "frozen := %s(h0)\nif h1.Index == nil { h1.Index=make(%s) }\n", op.plan.Freeze, op.host.TypeName(op.plan.IndexType))
	op.out.WriteString("h1.Values=append(h1.Values,frozen)\nh1.Index[key]=next\nreturn next,true\n")
}

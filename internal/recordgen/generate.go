// Package recordgen emits schema-maintenance functions from resolved plans.
// It has no dependency on source declarations or semantic validation.
package recordgen

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"
	"strings"
)

type operation struct {
	host *hostapi.Host
	plan goplan.Derivation
	out  strings.Builder
}

// Generate emits all resolved helper plans in declaration order.
func Generate(host *hostapi.Host, plans []goplan.Derivation) string {
	var result strings.Builder
	for _, plan := range plans {
		op := operation{host: host, plan: plan}
		op.emit()
		result.WriteString(op.out.String())
	}
	return result.String()
}

func (op *operation) emit() {
	var parameters, results []string
	for i := 0; i < op.plan.Signature.Params().Len(); i++ {
		parameters = append(parameters, fmt.Sprintf("h%d %s", i, op.host.TypeName(op.plan.Signature.Params().At(i).Type())))
	}
	for i := 0; i < op.plan.Signature.Results().Len(); i++ {
		results = append(results, op.host.TypeName(op.plan.Signature.Results().At(i).Type()))
	}
	fmt.Fprintf(&op.out, "// %s is schema-derived %s; host leaf hooks retain their declared contracts.\n", op.plan.Function, op.plan.Kind)
	fmt.Fprintf(&op.out, "func %s(%s) (%s) {\n", op.plan.Function, strings.Join(parameters, ","), strings.Join(results, ","))
	switch op.plan.Kind {
	case goplan.Freeze:
		op.freeze()
	case goplan.Key:
		fmt.Fprintf(&op.out, "return %s{%s}\n", op.host.TypeName(op.plan.KeyType), op.keyFields())
	case goplan.Equal:
		op.equal()
	case goplan.Visit, goplan.Transform:
		op.children()
	case goplan.Display:
		op.display()
	case goplan.Store, goplan.Load:
		op.storage()
	default:
		panic("unhandled resolved derivation")
	}
	op.out.WriteString("}\n")
}

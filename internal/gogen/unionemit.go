package gogen

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
)

func (e *emitter) emitDispatch(node *matchir.Node) error {
	union := e.unions[node.Union.ID]
	fmt.Fprintf(&e.out, "switch %s.%s {\n", valueName(node.Input), union.Tag.Name())
	previous := e.established
	for _, alternative := range node.Cases {
		variant := union.Variants[alternative.Variant]
		fmt.Fprintf(&e.out, "case %s:\n", variant.Tag.Name())
		e.established = variant
		if validation := e.activeValidation(variant, valueName(node.Input)); validation != "" {
			fmt.Fprintf(&e.out, "if !(%s) { var zero %s; return zero,false }\n", validation, e.host.TypeName(e.host.Signature.Results().At(0).Type()))
		}
		if err := e.emitDecisions(alternative.Next, nil); err != nil {
			return err
		}
	}
	e.established = previous
	e.out.WriteString("}\n")
	return nil
}

func (e *emitter) emitVariant(node *matchir.Node) error {
	union := e.unions[node.Union.ID]
	variant := union.Variants[node.Variant]
	known := node.Input == 0 && e.established == variant
	e.out.WriteString("{\n")
	if !known {
		fmt.Fprintf(&e.out, "if %s.%s == %s {\n", valueName(node.Input), union.Tag.Name(), variant.Tag.Name())
	}
	for index, field := range variant.Fields {
		fmt.Fprintf(&e.out, "%s := %s.%s\n_ = %s\n", valueName(node.Outputs[index]), valueName(node.Input), field.Name(), valueName(node.Outputs[index]))
	}
	if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
		return err
	}
	if !known {
		e.out.WriteString("}\n")
	}
	e.out.WriteString("}\n")
	return nil
}

func (e *emitter) variantReturns(node *matchir.Node) bool {
	if node.Input != 0 || e.established == nil || e.established.Union.Schema != node.Union || e.established.ID != node.Variant {
		return false
	}
	for next := node.Next; next != nil && next != node.Fallback && next.Kind != matchir.Fail; next = next.Fallback {
		if next.Kind == matchir.Build {
			return true
		}
	}
	return false
}

package gogen

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

func (e *emitter) decisions(rules []*syntax.Rule, input types.Type) (*matchir.Node, error) {
	union := e.unionFor(input)
	for _, declared := range e.unions {
		if declared.declaration.Exhaustive && declared != union {
			return nil, &syntax.Diagnostic{Position: declared.declaration.Position, Message: "exhaustive union must be the target input type"}
		}
	}
	if union != nil {
		if err := e.checkCoverage(union, rules); err != nil {
			return nil, err
		}
		dispatch := &matchir.Node{Kind: matchir.Dispatch, Union: union.schema, Input: 0, Fallback: &matchir.Node{Kind: matchir.Fail}}
		for _, variant := range union.variants {
			var selected []*syntax.Rule
			for _, rule := range rules {
				root := rootPattern(rule.Match)
				constructor := e.constructors[root.Name]
				if root.Kind == syntax.Call && constructor != nil && constructor.union == union && constructor != variant {
					continue
				}
				selected = append(selected, rule)
			}
			next, err := e.orderedDecisions(selected, input)
			if err != nil {
				return nil, err
			}
			dispatch.Cases = append(dispatch.Cases, matchir.VariantCase{Variant: variant.id, Next: next})
		}
		return dispatch, nil
	}
	return e.orderedDecisions(rules, input)
}

func rootPattern(expression *syntax.Expr) *syntax.Expr {
	for expression.Kind == syntax.Alternation && len(expression.Args) == 1 {
		expression = expression.Args[0]
	}
	return expression
}

func (e *emitter) checkCoverage(union *boundUnion, rules []*syntax.Rule) error {
	if !union.declaration.Exhaustive {
		return nil
	}
	covered := map[string]struct{}{}
	for _, rule := range rules {
		if len(rule.Guards) != 0 {
			continue
		}
		root := rootPattern(rule.Match)
		if root.Kind == syntax.Wildcard || root.Kind == syntax.Variable {
			return nil
		}
		variant := e.constructors[root.Name]
		if root.Kind != syntax.Call || variant == nil || variant.union != union {
			continue
		}
		seen := map[string]struct{}{}
		if rule.Match.Bind != "" {
			seen[rule.Match.Bind] = struct{}{}
		}
		unrestricted := true
		for _, field := range root.Args {
			if field.Kind == syntax.Wildcard {
				continue
			}
			if field.Kind != syntax.Variable {
				unrestricted = false
				break
			}
			if _, repeated := seen[field.Name]; repeated {
				unrestricted = false
				break
			}
			seen[field.Name] = struct{}{}
		}
		if unrestricted {
			covered[variant.declaration.Name] = struct{}{}
		}
	}
	var missing []string
	for _, variant := range union.variants {
		if _, known := covered[variant.declaration.Name]; !known {
			missing = append(missing, variant.declaration.Name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	message := "non-exhaustive union " + union.declaration.Name + ": missing unconditional variants " + strings.Join(missing, ", ")
	return &syntax.Diagnostic{Position: union.declaration.Position, Message: message}
}

func (e *emitter) emitDispatch(node *matchir.Node) error {
	union := e.unions[node.Union.ID]
	fmt.Fprintf(&e.out, "switch %s.%s {\n", valueName(node.Input), union.tag.Name())
	previous := e.established
	for _, alternative := range node.Cases {
		variant := union.variants[alternative.Variant]
		fmt.Fprintf(&e.out, "case %s:\n", variant.tag.Name())
		e.established = variant
		if validation := e.activeValidation(variant, valueName(node.Input)); validation != "" {
			fmt.Fprintf(&e.out, "if !(%s) { var zero %s; return zero,false }\n", validation, e.host.typeName(e.host.Signature.Results().At(0).Type()))
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
	variant := union.variants[node.Variant]
	known := node.Input == 0 && e.established == variant
	e.out.WriteString("{\n")
	if !known {
		fmt.Fprintf(&e.out, "if %s.%s == %s {\n", valueName(node.Input), union.tag.Name(), variant.tag.Name())
	}
	for index, field := range variant.fields {
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
	if node.Input != 0 || e.established == nil || e.established.union.schema != node.Union || e.established.id != node.Variant {
		return false
	}
	for next := node.Next; next != nil && next != node.Fallback && next.Kind != matchir.Fail; next = next.Fallback {
		if next.Kind == matchir.Build {
			return true
		}
	}
	return false
}

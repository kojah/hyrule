package gogen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"

	"github.com/kojah/hyrule/internal/goplan"
)

// emitSignature keeps native parameters and results at the public entry point.
// The internal input product remains an emission detail and is scalarized when
// the rules only project its fields.
func (e *emitter) emitSignature(native *goplan.Native) {
	var params, results, fields []string
	for i := range native.Original.Params().Len() {
		name := fmt.Sprintf("hparam%d", i)
		params = append(params, name+" "+e.host.TypeName(native.Original.Params().At(i).Type()))
		fields = append(fields, fmt.Sprintf("P%d: %s", i, name))
	}
	for i := range native.Original.Results().Len() {
		results = append(results, e.host.TypeName(native.Original.Results().At(i).Type()))
	}
	comment := "applies rules in source order; false means no rule matched"
	if native.Total {
		comment = "applies rules in source order and uses the declared fallback when none match"
	}
	fmt.Fprintf(&e.out, "// %s %s.\nfunc %s(%s) (%s) {\n", native.Function, comment, native.Function, strings.Join(params, ", "), strings.Join(results, ", "))
	fmt.Fprintf(&e.out, "harg0 := %s{%s}\n_ = harg0\n", e.host.TypeName(native.Input), strings.Join(fields, ", "))
}

func (e *emitter) returnValue(value string, matched bool) {
	if e.native == nil {
		fmt.Fprintf(&e.out, "return %s, %t\n", value, matched)
		return
	}

	var results []string
	expression, err := parser.ParseExpr(value)
	if literal, ok := expression.(*ast.CompositeLit); err == nil && ok && len(literal.Elts) == e.native.Output.NumFields() {
		for index, element := range literal.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				results = nil
				break
			}
			key, ok := pair.Key.(*ast.Ident)
			if !ok || key.Name != fmt.Sprintf("V%d", index) {
				results = nil
				break
			}
			var text bytes.Buffer
			if format.Node(&text, token.NewFileSet(), pair.Value) != nil {
				results = nil
				break
			}
			results = append(results, text.String())
		}
	}
	if len(results) == 0 {
		name := value
		if _, simple := expression.(*ast.Ident); !simple {
			e.serial++
			name = fmt.Sprintf("hv%d", e.serial)
			fmt.Fprintf(&e.out, "%s := %s\n", name, value)
		}
		for i := range e.native.Output.NumFields() {
			results = append(results, fmt.Sprintf("%s.V%d", name, i))
		}
	}
	if !e.native.Total {
		results = append(results, fmt.Sprint(matched))
	}
	fmt.Fprintf(&e.out, "return %s\n", strings.Join(results, ", "))
}

func (e *emitter) failure() string {
	text, _ := e.capture(func() error {
		if e.native != nil && e.native.Total && !e.inFallback {
			e.inFallback = true
			e.returnValue(e.emitResult(e.native.Fallback), true)
			e.inFallback = false
		} else {
			if e.native == nil {
				fmt.Fprintf(&e.out, "var zero %s\nreturn zero, false\n", e.host.TypeName(sigOutput(e)))
			} else {
				var names []string
				for i := range e.native.Output.NumFields() {
					e.serial++
					name := fmt.Sprintf("hv%d", e.serial)
					fmt.Fprintf(&e.out, "var %s %s\n", name, e.host.TypeName(e.native.Output.Field(i).Type()))
					names = append(names, name)
				}
				if !e.native.Total {
					names = append(names, "false")
				}
				fmt.Fprintf(&e.out, "return %s\n", strings.Join(names, ", "))
			}
		}
		return nil
	})
	return text
}

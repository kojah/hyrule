package gogen

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
	"strings"
)

func (e *emitter) emitNativeAPI(native *goplan.Native) {
	var params, fields, outputs []string
	for i := 0; i < native.Original.Params().Len(); i++ {
		name := fmt.Sprintf("hparam%d", i)
		params = append(params, name+" "+e.host.TypeName(native.Original.Params().At(i).Type()))
		fields = append(fields, native.Input.Field(i).Name()+":"+name)
	}
	for i := 0; i < native.Original.Results().Len(); i++ {
		outputs = append(outputs, e.host.TypeName(native.Original.Results().At(i).Type()))
	}
	fmt.Fprintf(&e.out, "func %s(%s) (%s) {\n", native.Function, strings.Join(params, ","), strings.Join(outputs, ","))
	fmt.Fprintf(&e.out, "hvalue, hok := %s(%s{%s})\n", e.host.Function, e.host.TypeName(native.Input), strings.Join(fields, ","))
	if native.Total {
		fmt.Fprintln(&e.out, "if !hok {")
		fallback := e.emitResult(native.Fallback)
		fmt.Fprintf(&e.out, "hvalue = %s\n}\n", fallback)
	}
	var returns []string
	for i := 0; i < native.Output.NumFields(); i++ {
		returns = append(returns, fmt.Sprintf("hvalue.V%d", i))
	}
	if !native.Total {
		returns = append(returns, "hok")
	}
	fmt.Fprintf(&e.out, "return %s\n}\n", strings.Join(returns, ","))
}

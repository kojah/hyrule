package valuegen

import (
	"fmt"
	"github.com/kojah/hyrule/internal/matchir"
	"github.com/kojah/hyrule/internal/valueir"
	"strconv"
	"strings"
)

// Result emits a committed continuation. answered is the nearest ordered
// choice's flag, or empty for an unconditional rule result.
func (e *Emitter) Result(result *valueir.Result, answered string) string {
	switch result.Kind {
	case valueir.Answer:
		value := e.Value(result.Value)
		if answered != "" {
			fmt.Fprintf(e.Out, "%s = true\n", answered)
		}
		return value
	case valueir.Query:
		return e.query(result, answered)
	case valueir.Decline:
		name := e.local()
		fmt.Fprintf(e.Out, "var %s %s\n", name, e.Bindings.ValueType(result.Type))
		return name
	case valueir.Branch:
		return e.branch(result, answered)
	case valueir.Choice:
		value := e.choice(result)
		if answered != "" {
			fmt.Fprintf(e.Out, "%s = true\n", answered)
		}
		return value
	default:
		panic("unhandled resolved continuation")
	}
}

func (e *Emitter) local() string { *e.Serial++; return fmt.Sprintf("hv%d", *e.Serial) }
func captureName(id matchir.ValueID) string {
	if id == 0 {
		return "harg0"
	}
	return fmt.Sprintf("hv%d", id)
}

func (e *Emitter) query(result *valueir.Result, answered string) string {
	q := result.Query
	if q.Pure != nil {
		value := e.Value(q.Pure)
		fmt.Fprintf(e.Out, "%s := %s\n", captureName(q.Capture), value)
		return e.decide(result, answered)
	}
	args := make([]string, len(q.Arguments))
	for i, arg := range q.Arguments {
		args[i] = e.temporary(arg.Type, e.Value(arg.Value), false)
	}
	name := captureName(q.Capture)
	call := e.Bindings.ValueHook(q.Hook) + "(" + strings.Join(args, ",") + ")"
	if q.TupleSize <= 1 {
		fmt.Fprintf(e.Out, "%s := %s\n", name, call)
	} else {
		vars, fields := make([]string, q.TupleSize), make([]string, q.TupleSize)
		for i := range vars {
			vars[i] = fmt.Sprintf("%spart%d", name, i)
			fields[i] = fmt.Sprintf("V%d:%s", i, vars[i])
		}
		fmt.Fprintf(e.Out, "%s := %s\n%s := %s{%s}\n", strings.Join(vars, ","), call, name, e.Bindings.ValueType(q.Type), strings.Join(fields, ","))
	}
	return e.decide(result, answered)
}

// decide validates and dispatches an established query or pure decision value.
func (e *Emitter) decide(result *valueir.Result, answered string) string {
	q := result.Query
	name := captureName(q.Capture)
	fmt.Fprintf(e.Out, "_ = %s\n", name)
	if q.Union != nil {
		if check := e.Bindings.ValueValidation(q.Type, name); check != "" {
			fmt.Fprintf(e.Out, "if !%s { var zero %s; return zero,false }\n", check, e.Bindings.ValueFailureType())
		}
	}
	if q.Next != nil {
		return e.Result(q.Next, answered)
	}
	output := e.local()
	selector := name
	if q.Union != nil {
		tag, _, _ := e.Bindings.ValueUnionFields(q.Union.ID, 0)
		selector += "." + tag
	}
	fmt.Fprintf(e.Out, "var %s %s\nswitch %s {\n", output, e.Bindings.ValueType(result.Type), selector)
	for _, arm := range q.Cases {
		tag := strconv.FormatBool(arm.Boolean)
		var fields []string
		if q.Union != nil {
			_, tag, fields = e.Bindings.ValueUnionFields(q.Union.ID, arm.Variant)
		}
		fmt.Fprintf(e.Out, "case %s:\n", tag)
		for _, capture := range arm.Captures {
			local := captureName(capture.Value)
			fmt.Fprintf(e.Out, "%s := %s.%s\n_ = %s\n", local, name, fields[capture.Field], local)
		}
		value := e.Result(arm.Result, answered)
		fmt.Fprintf(e.Out, "%s = %s\n", output, value)
	}
	fmt.Fprintln(e.Out, "}")
	return output
}

func (e *Emitter) choice(result *valueir.Result) string {
	output := e.local()
	answered := output + "answered"
	fmt.Fprintf(e.Out, "var %s %s\n%s := false\n", output, e.Bindings.ValueType(result.Type), answered)
	for _, alternative := range result.Alternatives {
		fmt.Fprintf(e.Out, "if !%s {\n", answered)
		value := e.Result(alternative, answered)
		fmt.Fprintf(e.Out, "if %s { %s = %s }\n}\n", answered, output, value)
	}
	return output
}

package valuegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kojah/hyrule/internal/matchir"
	"github.com/kojah/hyrule/internal/valueir"
)

// Result emits a committed continuation. Nested decisions write one destination
// instead of constructing and copying a result at every branch boundary.
func (e *Emitter) Result(result *valueir.Result, answered string) string {
	if result.Kind == valueir.Answer {
		value := e.Value(result.Value)
		if answered != "" {
			fmt.Fprintf(e.Out, "%s = true\n", answered)
		}
		return value
	}
	output := e.local()
	fmt.Fprintf(e.Out, "var %s %s\n", output, e.Bindings.ValueType(result.Type))
	e.assignResult(result, output, answered)
	return output
}

func (e *Emitter) local() string { *e.Serial++; return fmt.Sprintf("hv%d", *e.Serial) }
func captureName(id matchir.ValueID) string {
	if id == 0 {
		return "harg0"
	}
	return fmt.Sprintf("hv%d", id)
}

// assignResult leaves both destination and flag unchanged on decline. Queries
// already executed are retained; only an answer commits the result destination.
func (e *Emitter) assignResult(result *valueir.Result, output, answered string) {
	switch result.Kind {
	case valueir.Answer:
		value := e.Value(result.Value)
		fmt.Fprintf(e.Out, "%s = %s\n", output, value)
		if answered != "" {
			fmt.Fprintf(e.Out, "%s = true\n", answered)
		}
	case valueir.Decline:
	case valueir.Query:
		e.query(result, output, answered)
	case valueir.Branch:
		condition := e.Value(result.Value)
		fmt.Fprintf(e.Out, "if %s {\n", condition)
		e.assignResult(result.Alternatives[0], output, answered)
		fmt.Fprintln(e.Out, "} else {")
		e.assignResult(result.Alternatives[1], output, answered)
		fmt.Fprintln(e.Out, "}")
	case valueir.Choice:
		e.choice(result, output)
		if answered != "" {
			fmt.Fprintf(e.Out, "%s = true\n", answered)
		}
	default:
		panic("unhandled resolved continuation")
	}
}

func (e *Emitter) query(result *valueir.Result, output, answered string) {
	query := result.Query
	name := captureName(query.Capture)
	if query.Pure != nil {
		value := e.Value(query.Pure)
		if query.Next != nil && query.Union == nil && !e.Bindings.ValueUsed(query.Capture) {
			fmt.Fprintf(e.Out, "_ = %s\n", value)
			e.assignResult(query.Next, output, answered)
			return
		}
		fmt.Fprintf(e.Out, "%s := %s\n", name, value)
	} else {
		args := make([]string, len(query.Arguments))
		for index, argument := range query.Arguments {
			args[index] = e.Value(argument.Value)
			if !stableValue(argument.Value) {
				args[index] = e.temporary(argument.Type, args[index], false, argument.Type == argument.Value.Type)
			}
		}
		call := e.Bindings.ValueHook(query.Hook) + "(" + strings.Join(args, ",") + ")"
		if query.Next != nil && query.Union == nil && !e.Bindings.ValueUsed(query.Capture) {
			fmt.Fprintln(e.Out, call)
			e.assignResult(query.Next, output, answered)
			return
		}
		if query.TupleSize <= 1 {
			fmt.Fprintf(e.Out, "%s := %s\n", name, call)
		} else {
			locals, fields := make([]string, query.TupleSize), make([]string, query.TupleSize)
			for index := range locals {
				locals[index] = fmt.Sprintf("%spart%d", name, index)
				fields[index] = fmt.Sprintf("V%d:%s", index, locals[index])
			}
			fmt.Fprintf(e.Out, "%s := %s\n%s := %s{%s}\n", strings.Join(locals, ","), call, name, e.Bindings.ValueType(query.Type), strings.Join(fields, ","))
		}
	}
	fmt.Fprintf(e.Out, "_ = %s\n", name)
	if query.Union != nil {
		if check := e.Bindings.ValueValidation(query.Type, name); check != "" {
			fmt.Fprintf(e.Out, "if !%s { %s }\n", check, e.Bindings.ValueFailure())
		}
	}
	if query.Next != nil {
		e.assignResult(query.Next, output, answered)
		return
	}
	e.outcomes(query, name, output, answered)
}

func (e *Emitter) outcomes(query *valueir.QueryPlan, name, output, answered string) {
	selector := name
	if query.Union != nil {
		tag, _, _ := e.Bindings.ValueUnionFields(query.Union.ID, 0)
		selector += "." + tag
	}
	fmt.Fprintf(e.Out, "switch %s {\n", selector)
	for _, arm := range query.Cases {
		tag := strconv.FormatBool(arm.Boolean)
		var fields []string
		if query.Union != nil {
			_, tag, fields = e.Bindings.ValueUnionFields(query.Union.ID, arm.Variant)
		}
		fmt.Fprintf(e.Out, "case %s:\n", tag)
		for _, capture := range arm.Captures {
			if !e.Bindings.ValueUsed(capture.Value) {
				continue
			}
			local := captureName(capture.Value)
			fmt.Fprintf(e.Out, "%s := %s.%s\n_ = %s\n", local, name, fields[capture.Field], local)
		}
		e.assignResult(arm.Result, output, answered)
	}
	fmt.Fprintln(e.Out, "}")
}

func (e *Emitter) choice(result *valueir.Result, output string) {
	answered := e.local() + "answered"
	fmt.Fprintf(e.Out, "%s := false\n", answered)
	for index, alternative := range result.Alternatives {
		if index > 0 {
			fmt.Fprintf(e.Out, "if !%s {\n", answered)
		}
		flag := answered
		if index == len(result.Alternatives)-1 {
			flag = ""
		} // The final pure fallback always answers.
		e.assignResult(alternative, output, flag)
		if index > 0 {
			fmt.Fprintln(e.Out, "}")
		}
	}
}

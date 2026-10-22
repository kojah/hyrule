package syntax

import (
	"fmt"
	"strings"
)

// Format parses and prints equation syntax without loading host packages.
func Format(name string, input []byte) ([]byte, error) {
	file, err := Parse(name, strings.NewReader(string(input)))
	if err != nil {
		return nil, err
	}
	return Print(file), nil
}

// Print renders the surface AST. It is also the migration writer; callers must
// resolve legacy shorthand and selective outcomes before passing legacy nodes.
func Print(file *File) []byte {
	var out strings.Builder
	for i, item := range file.Items {
		continuedHost := i > 0 && item.Binding != nil && file.Items[i-1].Binding != nil
		if i > 0 && !continuedHost {
			out.WriteByte('\n')
		}
		switch {
		case item.Binding != nil:
			printBinding(&out, item.Binding, continuedHost)
		case item.Record != nil:
			printRecord(&out, item.Record)
		case item.Union != nil:
			printUnion(&out, item.Union)
		case item.API != nil:
			printAPI(&out, item.API)
		case item.Pattern != nil:
			printPattern(&out, item.Pattern)
		case item.Derivation != nil:
			printDerivation(&out, item.Derivation)
		case item.Rule != nil:
			printRule(&out, item.Rule)
		}

	}
	printComments(&out, file.Trailing)
	return []byte(out.String())
}
func fieldText(field UnionField) string {
	if field.Implicit {
		return identifierText(field.Name)
	}
	return identifierText(field.Name) + ": " + field.GoName
}

func printComments(out *strings.Builder, comments []string) {
	for _, comment := range comments {
		out.WriteString(comment + "\n")
	}
}
func printBinding(out *strings.Builder, item *Binding, continuedHost bool) {

	b := item
	printComments(out, b.Comments)
	mark := ""
	switch b.Kind {
	case "match":
		mark = "?"
	case "query":
		mark = "!"
	}
	if !continuedHost {
		out.WriteString("host\n")
	}
	fmt.Fprintf(out, "  %s%s", identifierText(b.Name), mark)
	for _, arg := range b.Parameters {
		out.WriteString(" " + identifierText(arg))
	}
	if b.Body != nil {
		out.WriteString(" = " + render(b.Body, 2))
	} else if !b.Implicit {
		out.WriteString(" = " + b.GoName)
	}
	out.WriteByte('\n')

}

func printRecord(out *strings.Builder, item *Record) {

	r := item
	printComments(out, r.Comments)
	kind := "type"
	if r.View {
		kind = "view"
	}
	fmt.Fprintf(out, "%s %s = %s\n", kind, identifierText(r.Name), r.GoType)
	for _, f := range r.Fields {
		out.WriteString("  " + fieldText(f) + "\n")
	}

}

func printUnion(out *strings.Builder, item *Union) {

	u := item
	printComments(out, u.Comments)
	if u.Exhaustive {
		out.WriteString("exhaustive ")
	}
	host := u.GoType
	if len(u.Types) > 0 {
		host = "(" + strings.Join(u.Types, ", ") + ")"
	}
	fmt.Fprintf(out, "type %s = %s\n  tag %s\n", identifierText(u.Name), host, u.Tag)
	for _, v := range u.Variants {
		out.WriteString("  " + identifierText(v.Name))
		if len(v.Fields) > 0 {
			fields := make([]string, len(v.Fields))
			for i, f := range v.Fields {
				fields[i] = fieldText(f)
			}
			out.WriteString(" (" + strings.Join(fields, ", ") + ")")
		}
		if !v.Implicit {
			out.WriteString(" = " + v.GoTag)
		}
		out.WriteByte('\n')
	}

}

func printAPI(out *strings.Builder, item *API) {

	api := item
	printComments(out, api.Comments)
	out.WriteString("signature " + api.Mode)
	if api.Fallback != nil {
		out.WriteString(" = " + render(api.Fallback, 0))
	}
	out.WriteByte('\n')

}

func printPattern(out *strings.Builder, item *Pattern) {

	pat := item
	printComments(out, pat.Comments)
	out.WriteString("pattern " + identifierText(pat.Name))
	for _, arg := range pat.Parameters {
		out.WriteString(" " + identifierText(arg))
	}
	out.WriteString(" = " + render(pat.Body, 0) + "\n")

}

func printDerivation(out *strings.Builder, item *Derivation) {

	d := item
	printComments(out, d.Comments)
	out.WriteString("derive " + identifierText(d.Record) + "\n")
	for _, op := range d.Operations {
		fmt.Fprintf(out, "  %s %s: %s\n", op.Kind, op.Function, op.Signature)
		for _, field := range op.Fields {
			fmt.Fprintf(out, "    %s: %s\n", identifierText(field.Name), strings.ReplaceAll(field.Policy, "$", "@"))
		}
	}

}

func printRule(out *strings.Builder, item *Rule) {

	r := item
	printComments(out, r.Comments)
	family := r.Family
	if family == "" {
		family = "rewrite"
	}
	if r.Name != "" {
		out.WriteString("@" + identifierText(r.Name) + "\n")
	}
	out.WriteString(family + " " + operand(r.Match, 0))
	for i, c := range r.Clauses {
		sep := "\n  | "
		if i > 0 {
			sep = "\n  , "
		}
		out.WriteString(sep)
		if c.Name != "" {
			out.WriteString("let " + identifierText(c.Name) + " = ")
		}
		out.WriteString(render(c.Value, 2))
	}
	if len(r.Clauses) > 0 {
		out.WriteString("\n  =")
	} else {
		out.WriteString(" =")
	}
	result := render(r.Build, 2)
	if len(r.Clauses) == 0 && !strings.Contains(result, "\n") && len(family)+len(operand(r.Match, 0))+len(result) < 100 {
		out.WriteString(" " + result + "\n")
	} else {
		out.WriteString("\n  " + result + "\n")
	}
}

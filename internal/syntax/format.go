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
		continuedRule := item.Rule != nil && item.Rule.Continued
		if i > 0 && !continuedHost && !continuedRule {
			out.WriteByte('\n')
		}
		switch {
		case item.Function != nil:
			printFunction(&out, item.Function)
		case item.Binding != nil:
			printBinding(&out, item.Binding, continuedHost)
		case item.Record != nil:
			printRecord(&out, item.Record)
		case item.Union != nil:
			printUnion(&out, item.Union)
		case item.Pattern != nil:
			printPattern(&out, item.Pattern)
		case item.Rule != nil:
			last := i+1 == len(file.Items) || file.Items[i+1].Rule == nil || !file.Items[i+1].Rule.Continued
			printRule(&out, item.Rule, last)
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
	if r.Implicit {
		fmt.Fprintf(out, "%s %s\n", kind, identifierText(r.Name))
	} else {
		fmt.Fprintf(out, "%s %s = %s\n", kind, identifierText(r.Name), r.GoType)
	}
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
	if u.Implicit {
		fmt.Fprintf(out, "type %s\n  tag %s\n", identifierText(u.Name), u.Tag)
	} else {
		fmt.Fprintf(out, "type %s = %s\n  tag %s\n", identifierText(u.Name), host, u.Tag)
	}
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

func printPattern(out *strings.Builder, item *Pattern) {

	pat := item
	printComments(out, pat.Comments)
	out.WriteString("pattern " + identifierText(pat.Name))
	for _, arg := range pat.Parameters {
		out.WriteString(" " + identifierText(arg))
	}
	out.WriteString(" = " + render(pat.Body, 0) + "\n")

}

// printRule writes one equation, or one later guarded alternative of it,
// which continues on the line after the previous alternative's result. The
// shared where block follows the equation's last alternative.
func printRule(out *strings.Builder, r *Rule, last bool) {
	family := r.Family
	if family == "" {
		family = "rewrite"
	}
	header := family + " " + operand(r.Match, 0)
	if !r.Continued {
		printComments(out, r.Comments)
		if r.Name != "" {
			out.WriteString("@" + identifierText(r.Name) + "\n")
		}
	}
	result := render(r.Build, 2)
	switch {
	case len(r.Clauses) > 0 && r.Continued:
		out.WriteString(clausesText(r.Clauses) + "\n  =\n  " + result + "\n")
	case len(r.Clauses) > 0:
		out.WriteString(header + "\n" + clausesText(r.Clauses) + "\n  =\n  " + result + "\n")
	case !strings.Contains(result, "\n") && len(family)+len(operand(r.Match, 0))+len(result) < 100:
		out.WriteString(header + " = " + result + "\n")
	default:
		out.WriteString(header + " =\n  " + result + "\n")
	}
	if len(r.Where) > 0 && last {
		out.WriteString("  where\n")
		for _, binding := range r.Where {
			out.WriteString("    " + identifierText(binding.Name) + " = " + render(binding.Value, 4) + "\n")
		}
	}
}

func clausesText(clauses []Clause) string {
	var out strings.Builder
	for i, c := range clauses {
		if i == 0 {
			out.WriteString("  | ")
		} else {
			out.WriteString("\n  , ")
		}
		if c.Name != "" {
			out.WriteString("let " + identifierText(c.Name) + " = ")
		}
		if c.Pattern != nil {
			out.WriteString(render(c.Pattern, 2) + " <- ")
		}
		out.WriteString(render(c.Value, 2))
	}
	return out.String()
}

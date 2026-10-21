package syntax

import "strings"

// renderLayout keeps short patterns compact and exposes long matcher fields and
// OR arms on separate lines. Indent is the expression's first-column offset.
func renderLayout(expression *Expr, indent int) string {
	if expression.Kind == Effects || expression.Kind == Query {
		return renderResult(expression, indent)
	}
	inline := render(expression)
	if indent+len(inline) <= 100 || len(expression.Args) == 0 {
		return inline
	}
	if expression.Kind == Comparison {
		return inline
	}
	if expression.Kind == List {
		var out strings.Builder
		out.WriteString("[")
		for _, item := range expression.Args {
			out.WriteString("\n" + strings.Repeat(" ", indent+2) + renderLayout(item, indent+2) + ",")
		}
		out.WriteString("\n" + strings.Repeat(" ", indent) + "]")
		return out.String()
	}
	prefix := ""
	if expression.Bind != "" {
		prefix = "$" + expression.Bind + ":"
	}
	var out strings.Builder
	opening := "("
	if expression.Unparenthesized {
		opening = ""
	}
	out.WriteString(prefix + opening + expression.Name)
	for _, child := range expression.Args {
		field := ""
		if child.Field != "" {
			field = child.Field + ":"
		}
		out.WriteString("\n" + strings.Repeat(" ", indent+2) + field)
		out.WriteString(renderLayout(child, indent+2+len(field)))
	}
	if !expression.Unparenthesized {
		out.WriteString("\n" + strings.Repeat(" ", indent) + ")")
	}
	return out.String()
}

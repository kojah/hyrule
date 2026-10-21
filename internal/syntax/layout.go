package syntax

import "strings"

// renderLayout keeps short patterns compact and exposes long matcher fields and
// OR arms on separate lines. Indent is the expression's first-column offset.
func renderLayout(expression *Expr, indent int) string {
	inline := render(expression)
	if indent+len(inline) <= 100 || len(expression.Args) == 0 {
		return inline
	}
	prefix := ""
	if expression.Bind != "" {
		prefix = "$" + expression.Bind + ":"
	}
	var out strings.Builder
	out.WriteString(prefix + "(" + expression.Name)
	for _, child := range expression.Args {
		field := ""
		if child.Field != "" {
			field = child.Field + ":"
		}
		out.WriteString("\n" + strings.Repeat(" ", indent+2) + field)
		out.WriteString(renderLayout(child, indent+2+len(field)))
	}
	out.WriteString("\n" + strings.Repeat(" ", indent) + ")")
	return out.String()
}

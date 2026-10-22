package syntax

import "strings"

func operand(x *Expr, indent int) string {
	value := render(x, indent)
	if x.Kind == Call && len(x.Args) > 0 && x.Bind == "" || x.Kind == Comparison || x.Kind == Update ||
		x.Kind == OrderedAlternatives || x.Kind == Query || x.Kind == Do || x.Kind == Pure ||
		x.Kind == Literal && strings.HasPrefix(x.Name, "-") {
		return "(" + value + ")"
	}
	return value
}
func render(x *Expr, indent int) string {
	if x == nil {
		return ""
	}
	switch x.Kind {
	case Variable:
		if x.Quoted {
			base, rest, has := strings.Cut(x.Name, ".")
			value := "`" + base + "`"
			if has {
				value += "." + rest
			}
			return value
		}
		return identifierText(x.Name)
	case Symbol:
		if x.Bare || x.Unparenthesized {
			return identifierText(x.Name)
		}
		return "^" + x.Name
	case Literal:
		return x.Name
	case Wildcard:
		return "_"
	case Decline:
		return "decline"
	case Pure:
		return render(x.Args[0], indent)
	case Do:
		return renderDo(x, indent)
	case Query, Selection:
		return renderCase(x, indent)
	case OrderedAlternatives:
		parts := make([]string, len(x.Args))
		for i, a := range x.Args {
			parts[i] = render(a, indent)
		}
		return strings.Join(parts, "\n"+strings.Repeat(" ", indent)+"<|> ")
	case Comparison:
		return operand(x.Args[0], indent) + " " + x.Name + " " + operand(x.Args[1], indent)
	case Alternation:
		parts := make([]string, len(x.Args))
		for i, a := range x.Args {
			parts[i] = render(a, indent)
		}
		return bindPrefix(x) + "(" + strings.Join(parts, " | ") + ")"
	case List, Effects:
		parts := make([]string, len(x.Args))
		for i, a := range x.Args {
			parts[i] = render(a, indent)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case Update:
		return render(x.Args[0], indent) + " with " + namedText(x.Args[1:], indent)
	default:
		return renderCall(x, indent)
	}
}
func bindPrefix(x *Expr) string {
	if x.Bind != "" {
		return identifierText(x.Bind) + "@"
	}
	return ""
}
func infix(name string) string {
	switch name {
	case "add":
		return "+"
	case "sub":
		return "-"
	case "mul":
		return "*"
	case "div":
		return "/"
	case "rem":
		return "%"
	}
	return ""
}
func namedText(args []*Expr, indent int) string {
	fields := make([]string, len(args))
	for i, a := range args {
		fields[i] = identifierText(a.Field) + ": " + render(a, indent)
	}
	return "(" + strings.Join(fields, ", ") + ")"
}
func renderCase(x *Expr, indent int) string {
	pad := strings.Repeat(" ", indent)
	prefix := ""
	scrutinee := render(x.Args[0], indent)
	if x.Name != "" {
		prefix = "do\n" + pad + "  " + identifierText(x.Name) + " <- " + scrutinee + "\n" + pad + "  "
		scrutinee = identifierText(x.Name)
		indent += 2
		pad += "  "
	}
	value := prefix + "case " + scrutinee + " of"
	for _, arm := range x.Arms {
		value += "\n" + pad + "  " + render(arm.Pattern, indent+2) + " -> " + render(arm.Result, indent+2)
	}
	return value
}
func renderDo(x *Expr, indent int) string {
	pad := strings.Repeat(" ", indent)
	value := "do"
	step := x.Args[0]
	for step.Kind == Query && step.Sequential {
		value += "\n" + pad + "  " + render(step.Arms[0].Pattern, indent+2) + " <- " + render(step.Args[0], indent+2)
		if len(step.Arms) > 1 {
			value += "\n" + pad + "    else"
			for _, arm := range step.Arms[1:] {
				value += "\n" + pad + "      " + render(arm.Pattern, indent+6) + " -> " + render(arm.Result, indent+6)
			}
		}
		step = step.Arms[0].Result
	}
	return value + "\n" + pad + "  " + render(step, indent+2)
}

func renderCall(x *Expr, indent int) string {
	if op := infix(x.Name); op != "" && len(x.Args) == 2 {
		return operand(x.Args[0], indent) + " " + op + " " + operand(x.Args[1], indent)
	}
	value := identifierText(x.Name)
	if len(x.Args) == 0 {
		value += "()"
	} else if x.Args[0].Field != "" {
		value += " " + namedText(x.Args, indent)
	} else {
		for _, arg := range x.Args {
			value += " " + operand(arg, indent)
		}
	}
	if x.Bind != "" {
		value = bindPrefix(x) + "(" + value + ")"
	}
	return value
}

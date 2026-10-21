package compiler

import "github.com/kojah/lockdoc/tools/hyrule/internal/matchir"

// Arithmetic resolves language operations independently of host callbacks.
func Arithmetic(name string) (matchir.Arithmetic, bool) {
	switch name {
	case "add":
		return matchir.Add, true
	case "sub":
		return matchir.Subtract, true
	case "mul":
		return matchir.Multiply, true
	case "div":
		return matchir.Divide, true
	case "rem":
		return matchir.Remainder, true
	default:
		return 0, false
	}
}

func reservedOperation(name string) bool {
	_, arithmetic := Arithmetic(name)
	return arithmetic || name == "update" || name == "or"
}

package gogen

import "github.com/kojah/lockdoc/tools/hyrule/internal/matchir"

func comparisonSpelling(operator matchir.Comparison) string {
	return [...]string{"==", "!=", "<", "<=", ">", ">="}[operator]
}

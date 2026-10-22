package matchir

// Comparison is a typed scalar relation. Ordered comparisons require an
// ordered scalar type; equality requires safe, compatible comparable operands.
type Comparison uint8

const (
	Equal Comparison = iota
	NotEqual
	Less
	LessEqual
	Greater
	GreaterEqual
)

// Compare appends one pure typed relation without changing operand order.
func (cursor *Cursor) Compare(operator Comparison, left, right Operand) *Node {
	return cursor.predicate(&Node{Kind: Compare, Comparison: operator, Left: left, Right: right, Origin: cursor.origin})
}

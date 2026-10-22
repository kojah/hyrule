package valueir

import "github.com/kojah/lockdoc/tools/hyrule/internal/matchir"

// ResultKind distinguishes returned values, committed queries and ordered choice.
type ResultKind uint8

const (
	Answer ResultKind = iota
	Query
	Choice
	Decline
)

// Result is a typed continuation. Choice tries branches in order until one
// answers. Decline targets the nearest choice; it never retries a matching rule
// or undoes a query. A Query executes exactly once each time it is reached.
type Result struct {
	Kind         ResultKind
	Type         matchir.TypeID
	Value        *Value
	Query        *QueryPlan
	Alternatives []*Result
}

// QueryPlan records an effectful call and all established outcome branches.
// A nil Union means a Boolean decision, unless Next binds the complete result.
// TupleSize greater than one packs a host tuple into its declared product type.
type QueryPlan struct {
	Hook      matchir.HookID
	Type      matchir.TypeID
	Arguments []Argument
	Capture   matchir.ValueID
	TupleSize int
	Union     *matchir.UnionSchema
	Next      *Result
	Cases     []Outcome
}

// Outcome carries resolved variant fields into a continuation. Boolean is used
// only for Boolean queries; Variant is used only for a declared union.
type Outcome struct {
	Variant  matchir.VariantID
	Boolean  bool
	Captures []FieldCapture
	Result   *Result
}

// FieldCapture introduces a typed local for an established active payload field.
type FieldCapture struct {
	Field matchir.FieldID
	Value matchir.ValueID
}

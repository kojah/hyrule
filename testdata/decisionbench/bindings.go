package decisionbench

// A small slice-state dispatch workload isolates generated matching overhead.
// Counters are test instrumentation, not part of the hook purity contract.
type Header struct{ Length, Capacity int }
type Request struct {
	Action int
	Header Header
}
type Plan struct {
	Kind   int
	Header Header
}
type Signature func(Request) (Plan, bool)

var MatcherCalls uint64

func Append(input Request) (header Header, matched bool) {
	MatcherCalls++
	return input.Header, input.Action == 1
}
func Decode(input Header) (length, capacity int, matched bool) {
	MatcherCalls++
	return input.Length, input.Capacity, true
}
func Fits(length, capacity int) bool { return length < capacity }
func Empty(header Header) Plan       { return Plan{Kind: 0, Header: header} }
func Reuse(header Header) Plan       { return Plan{Kind: 1, Header: header} }
func Grow(header Header) Plan        { return Plan{Kind: 2, Header: header} }

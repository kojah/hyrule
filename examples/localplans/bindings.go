// Package localplans demonstrates an immutable local append decision. Backing
// and Source are illustrative identifiers; the host retains memory and execution
// ownership. A refused plan establishes no growth or allocation behavior.
package localplans

// Outcome identifies the effect selected from the established input facts.
type Outcome uint8

const (
	Unplanned Outcome = iota
	Reuse
	Refused
)

// Header is an established exact header in this example's abstract snapshot.
type Header struct {
	Backing          string
	Length, Capacity int
}

// Copy describes one append tail transfer without applying it.
type Copy struct{ Offset, Count int }

// Request also stores the resulting plan. Explicit updates preserve Source,
// header backing and capacity; all inputs are borrowed immutable values.
type Request struct {
	Source  string
	Header  Header
	Count   int
	Outcome Outcome
	Copies  []Copy
}

// Signature is a complete local planning invocation over immutable evidence.
type Signature func(Request) (Request, bool)

// Package unions demonstrates a checked tagged schema over existing Go storage.
package unions

// Kind is deliberately closed only by the Hyrule schema, not by Go's enum type.
type Kind uint8

const (
	NumberKind Kind = iota
	EmptyKind
)

// Value is existing host storage. Only Number is active for NumberKind; Hyrule
// rejects undeclared tags and its constructors initialize inactive fields to zero.
type Value struct {
	Kind   Kind
	Number int
}

type RewriteSignature func(Value) (Value, bool)

func Positive(value int) bool { return value > 0 }
func Increment(value int) int { return value + 1 }

// Package usefulness decides whether ordered pattern rows can match an input
// no earlier row matches, after Maranget, "Warnings for pattern matching"
// (Journal of Functional Programming, 2007). It reports unreachable rows and
// missing inputs with witnesses over an abstract pattern space; target
// adapters translate typed patterns into that space and never emit code.
//
// The space is deliberately partial. Heads of a closed domain (union tags in a
// validated position, Booleans, nil and non-nil pointers) enumerate every
// value. Open domains (typed constants, literals, implementer sets) have
// values outside their heads. Opaque tests (host matchers, accessor reads,
// repeated-capture equality) match an unknown subset: they never cover other
// rows and never prove coverage.
package usefulness

import "strings"

// Kind distinguishes wildcards, constructors and opaque tests.
type Kind uint8

const (
	// Wild matches every value: a wildcard or a first capture.
	Wild Kind = iota
	// Constructor matches one head of a domain and its argument patterns.
	Constructor
	// Opaque matches an unknown subset of values. A shared Head key marks
	// the same deterministic test, whose arguments are then compared.
	Opaque
	// Other is a witness for a value outside every head in its column.
	Other
)

// Residual says whether a domain has values outside its enumerated heads.
type Residual uint8

const (
	// Closed domains have no values outside Heads.
	Closed Residual = iota
	// Possible residual values may exist, such as other dynamic types.
	Possible
	// Definite residual values exist, such as integers between constants.
	Definite
)

// Domain is the value space of one column. Heads may be incomplete for open
// domains; they then only name witnesses.
type Domain struct {
	Heads    []*Head
	Residual Residual
	// List domains are sequences; their heads are lengths, not enumerated.
	List bool
}

// Head identifies one constructor. Equal keys within a column denote the same
// test with the same arity.
type Head struct {
	Key    string
	Label  string
	Arity  int
	Fields []string // Argument labels; empty labels render positionally.
	// Empty renders the head when no argument is shown in a witness.
	Empty  string
	Domain *Domain // Nil for opaque tests.
	// Length and Rest describe a list head: exactly Length elements, or at
	// least Length elements when Rest is set.
	Length int
	Rest   bool
}

// Pattern is one abstract pattern or witness.
type Pattern struct {
	Kind Kind
	Head *Head
	Args []*Pattern
	// Within over-approximates an opaque pattern when it is the queried row.
	Within *Pattern
}

// Any is the shared wildcard.
var Any = &Pattern{Kind: Wild}

// Render spells a witness in rule syntax. Unconstrained named arguments are
// omitted; values outside a column's heads render as _.
func Render(p *Pattern) string {
	if p.Kind != Constructor {
		return "_"
	}
	head := p.Head
	if head.Domain != nil && head.Domain.List {
		parts := make([]string, len(p.Args))
		for index, argument := range p.Args {
			parts[index] = Render(argument)
		}
		if head.Rest {
			parts = append(parts, ".._")
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	var parts []string
	named := false
	for index, argument := range p.Args {
		label := ""
		if index < len(head.Fields) {
			label = head.Fields[index]
		}
		if label != "" {
			named = true
			// Residual values render as _; unconstrained ones are omitted.
			text := Render(argument)
			if argument.Kind == Wild || argument.Kind == Constructor && text == "_" {
				continue
			}
			parts = append(parts, label+": "+text)
		} else if text := Render(argument); strings.Contains(text, " ") {
			parts = append(parts, "("+text+")")
		} else {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return head.Empty
	}
	if named {
		return head.Label + " { " + strings.Join(parts, ", ") + " }"
	}
	return head.Label + " " + strings.Join(parts, " ")
}

func wilds(count int) []*Pattern {
	result := make([]*Pattern, count)
	for index := range result {
		result[index] = Any
	}
	return result
}

func prepend(first []*Pattern, rest []*Pattern) []*Pattern {
	return append(append(make([]*Pattern, 0, len(first)+len(rest)), first...), rest...)
}

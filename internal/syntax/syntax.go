// Package syntax parses Hyrule's declarative patterns. It has no host-language
// or graph dependencies; matching and construction are distinct typed phases.
package syntax

import "fmt"

// Position identifies original rule text; paths are supplied by the caller.
type Position struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Diagnostic is a source-located compilation error.
type Diagnostic struct {
	Position
	Rule    string `json:"rule,omitempty"`
	Message string `json:"message"`
}

func (diagnostic *Diagnostic) Error() string {
	message := diagnostic.Message
	if diagnostic.Rule != "" {
		message = "rule " + diagnostic.Rule + ": " + message
	}
	return fmt.Sprintf("%s:%d:%d: %s", diagnostic.File, diagnostic.Line, diagnostic.Column, message)
}

// Kind distinguishes constructors, captures, ignored inputs and literals.
type Kind uint8

const (
	Call Kind = iota
	Variable
	Wildcard
	Literal
	Alternation
	Effects
	Query
	Symbol
	Comparison
	List
	Update
)

// Expr is a pattern or construction expression. Bind names an entire matched
// operation; a variable without children binds or reuses an operand.
type Expr struct {
	Position
	Kind Kind
	Name string
	Bind string
	// Field selects a named matcher result when this expression is a child.
	Field string
	Args  []*Expr
	Arms  []DecisionArm
	// Unparenthesized marks an outer application or unresolved outer name.
	Unparenthesized bool
}

// Binding declares a named matcher, builder, pure guard or ordered host query.
type Binding struct {
	Position
	Kind      string
	Name      string
	GoName    string
	Implicit  bool // Preserve alias-free spelling when formatting.
	Continued bool // This name continues the preceding comma-separated declaration.
	Comments  []string
}

// Rule is one match, ordered immutable bindings/guards and replacement.
type Rule struct {
	Position
	Name     string
	Match    *Expr
	Clauses  []Clause
	Build    *Expr
	Comments []string
}

// Clause preserves evaluation order. A nonempty Name declares an immutable
// local; an empty Name requires Value to establish a true Boolean guard.
type Clause struct {
	Name  string
	Value *Expr
}

// Pattern declares a reusable match with explicit, hygienic capture parameters.
type Pattern struct {
	Position
	Name       string
	Parameters []string
	Body       *Expr
	Comments   []string
}

// Item preserves declaration order for formatting.
type Item struct {
	Binding *Binding
	Pattern *Pattern
	Union   *Union
	Record  *Record
	Rule    *Rule
}

// File retains declarations and comments. Files combine in lexical filename
// order; rules within each file retain their source order.
type File struct {
	Items    []Item
	Trailing []string
}

// Union declares a closed variant domain bound to existing host storage.
// Exhaustive requests proof that unconditional rules cover every input variant.
type Union struct {
	Position
	Name, GoType, Tag string
	Exhaustive        bool
	Variants          []*Variant
	Comments          []string
}

// Variant binds one constructor and its active fields to a distinct host tag.
type Variant struct {
	Position
	Name, GoTag string
	Implicit    bool // Preserve an omitted same-name tag alias when formatting.
	OmitFields  bool // Preserve an omitted empty field block when formatting.
	Fields      []UnionField
}

// UnionField maps a language field name to one direct host storage field.
type UnionField struct {
	Position
	Name, GoName string
	Implicit     bool // Preserve an omitted same-name storage mapping when formatting.
}

// DecisionArm handles one complete query outcome without refinement or fallthrough.
type DecisionArm struct {
	Pattern *Expr
	Result  *Expr
}

// Record describes complete direct-field storage without a discriminator.
// Construction supplies every declared field; patterns may omit named fields.
type Record struct {
	Position
	Name, GoType string
	Fields       []UnionField
	Comments     []string
}

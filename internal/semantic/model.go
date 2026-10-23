// Package semantic owns the language model consumed by checking and planning.
// It carries source locations but no formatting, parser state or host types.
// Frontends construct independent values; subsequent passes never mutate parser nodes.
package semantic

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
	OrderedAlternatives
	Selection
	Pure
	Do
	Decline
	// Logic is a short-circuit Boolean operator: "&&", "||" or "not".
	Logic
	// Conditional is if/then/else: condition, then branch, else branch.
	Conditional
	// InlineGo carries an opaque Go expression or result block in Name.
	InlineGo
	// InputParameter projects a native input slot introduced by a fn header.
	InputParameter
)

// Expr is a pattern or construction expression. Bind names an entire matched
// operation; a variable without children binds or reuses an operand.
type Expr struct {
	// ParameterIndex is the zero-based slot for InputParameter.
	ParameterIndex int
	Position
	Kind Kind
	Name string
	Bind string
	// Field selects a named matcher result when this expression is a child.
	Field string
	Args  []*Expr
	Arms  []DecisionArm
	// BindResult consumes a complete query result rather than matching a variant.
	BindResult bool
	// Rest marks a list pattern whose final element matches the remaining suffix.
	Rest bool
	// Quoted marks a backtick-escaped capture that never names a Go constant.
	Quoted bool
}

// Binding declares a named matcher, builder, pure guard or ordered host query.
type Binding struct {
	Position
	Kind       string
	Name       string
	HostName   string
	Parameters []string
	Body       *Expr
}

// Rule is one match, ordered immutable bindings/guards and replacement.
type Rule struct {
	Position
	Name    string
	Family  string
	Match   *Expr
	Clauses []Clause
	Build   *Expr
	// Choices are the source positions of the OR alternatives, spelled in the
	// rule itself, that this OR-free expansion takes.
	Choices []Position
}

// Clause preserves evaluation order. A nonempty Name declares an immutable
// local; a Pattern matches the pure Value (a pattern guard); otherwise Value
// must establish a true Boolean guard.
type Clause struct {
	Name    string
	Pattern *Expr
	Value   *Expr
}

// Pattern declares a reusable match with explicit, hygienic capture parameters.
type Pattern struct {
	Position
	Name       string
	Parameters []string
	Body       *Expr
}

// Item preserves declaration order for deterministic resolution.
type Item struct {
	Binding *Binding
	Pattern *Pattern
	Union   *Union
	Record  *Record
	Rule    *Rule
	API     *API
}

// File holds semantic declarations. Files combine in lexical filename
// order; rules within each file retain their source order.
type File struct {
	Items []Item
}

// Union declares a closed variant domain bound to existing host storage.
// Exhaustive requests proof that unconditional rules cover every input variant.
type Union struct {
	Position
	Name, HostType, Tag string
	Types               []string // Native tuple result types; storage fields are zero-based indices.
	Exhaustive          bool
	Variants            []*Variant
}

// Variant binds one constructor and its active fields to a distinct host tag.
type Variant struct {
	Position
	Name, HostTag string
	Fields        []UnionField
}

// UnionField maps a language field name to one direct host storage field.
type UnionField struct {
	Position
	Name, HostName string
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
	Name, HostType string
	View           bool // Read-only projection; permits partial and imported pointer storage.
	Fields         []UnionField
}

// API opts into a native parameter/result list. Total APIs declare a failure value.
// Declared Inputs and Outputs let the generator infer the Go signature; an
// empty Mode with declared types keeps the implicit func(Input[, Host]) API.
type API struct {
	// Function marks an API declared by a function header.
	Function bool
	Position
	Mode            string
	Inputs, Outputs []Param
	Fallback        *Expr
}

// Native reports whether the API uses native parameter/result records.
func (api *API) Native() bool { return api != nil && api.Mode != "" }

// Param is one declared Go parameter or result type; Name may be empty.
type Param struct {
	Name, Type string
}

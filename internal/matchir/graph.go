// Package matchir represents ordered matching independently of source syntax,
// host types and output languages. Frontends intern hook, expression, literal
// and type identities; this package owns value identities and control flow.
package matchir

// ValueID identifies an established input or extraction, scoped to one Graph.
// The original input is always zero; subsequent values follow insertion order.
type ValueID uint32

// HookID identifies one pure extraction operation in a frontend-owned table.
type HookID uint32

// ExprID identifies a frontend-owned guard or construction expression.
type ExprID uint32

// LiteralID identifies a frontend-owned literal, without target-language text.
type LiteralID uint32

// TypeID identifies a frontend-owned host type. The IR never interprets it.
type TypeID uint32

// Kind identifies a decision or terminal continuation.
type Kind uint8

const (
	Fail Kind = iota
	Match
	Test
	Guard
	Build
	Variant
	Dispatch
	Record
	Compare
	Evaluate
)

// OperandKind distinguishes established values from frontend-owned literals.
type OperandKind uint8

const (
	ValueOperand OperandKind = iota
	LiteralOperand
	ConstantOperand
)

// Operand is one side of an equality test. Only its selected identity applies.
type Operand struct {
	Kind     OperandKind
	Value    ValueID
	Literal  LiteralID
	Constant ConstantID
}

// Value refers to an established input or extraction.
func Value(identity ValueID) Operand { return Operand{Kind: ValueOperand, Value: identity} }

// Literal refers to an interned literal.
func Literal(identity LiteralID) Operand { return Operand{Kind: LiteralOperand, Literal: identity} }

// Constant refers to a typed immutable frontend binding.
func Constant(identity ConstantID) Operand { return Operand{Kind: ConstantOperand, Constant: identity} }

// Origin identifies the first source occurrence of a shared decision. This is
// diagnostic provenance, never a runtime expression or a sharing identity.
type Origin struct {
	Rule         string
	File         string
	Line, Column int
}

// Node contains a pure decision or one unshared construction. Match produces
// Outputs on success; its hook's Boolean success is implicit. Test and Guard
// branch on their predicates. Next is success, Fallback is failure. Build is
// terminal at runtime, but its Fallback retains the unreachable ordered suffix
// for static validation; execution must not follow it. Fail is terminal. IDs
// resolve only through frontend-owned tables.
// Nodes and their slices are immutable after Graph.Root seals the graph.
type Node struct {
	Kind           Kind
	Hook           HookID
	Input          ValueID
	Outputs        []ValueID
	Left, Right    Operand
	Expression     ExprID
	Origin         Origin
	Next, Fallback *Node
	Union          *UnionSchema
	Variant        VariantID
	Cases          []VariantCase
	Construction   *Construction
	Record         *RecordSchema
	Comparison     Comparison
	Evaluation     *Evaluation
}

type branch struct{ attempts []*attempt }
type attempt struct {
	node     *Node
	children branch
}

// Graph allocates typed values and folds adjacent identical pure prefixes.
// It must be built by one owner; it is read-only after Root is requested.
type Graph struct {
	root    branch
	types   []TypeID
	failure *Node
	entry   *Node
	sealed  bool
}

// New creates a graph whose original input has the supplied opaque type.
func New(inputType TypeID) *Graph {
	return &Graph{types: []TypeID{inputType}, failure: &Node{Kind: Fail}}
}

// Input is the original matched value.
func (graph *Graph) Input() ValueID { return 0 }

// Type returns the frontend-owned type of an established value.
func (graph *Graph) Type(value ValueID) TypeID { return graph.types[value] }

// Root seals the graph and exposes explicit, shared failure continuations.
// Calling Root again returns the same immutable entry.
func (graph *Graph) Root() *Node {
	if !graph.sealed {
		graph.entry = link(&graph.root, graph.failure)
		graph.sealed = true
	}
	return graph.entry
}

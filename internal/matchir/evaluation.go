package matchir

// EvaluationKind distinguishes typed pure value expressions. Foreign values
// retain a resolved value-plan ID; no target syntax enters this representation.
type EvaluationKind uint8

const (
	OperandEvaluation EvaluationKind = iota
	ArithmeticEvaluation
	ForeignEvaluation
)

// Arithmetic identifies ordinary host integer operations. Integer overflow
// wraps; division and remainder retain the host language's zero-divisor behavior.
type Arithmetic uint8

const (
	Add Arithmetic = iota
	Subtract
	Multiply
	Divide
	Remainder
)

// Evaluation is an immutable typed expression. Arithmetic evaluates operands
// left to right and does not introduce overflow checks or matching failure.
// Foreign evaluations may allocate fresh results and are never shared.
type Evaluation struct {
	Kind       EvaluationKind
	Type       TypeID
	Operand    Operand
	Arithmetic Arithmetic
	Arguments  []*Evaluation
	Expression ExprID
}

// Evaluate appends value computation before the ordered continuation. The
// output may be captured as an immutable local; arithmetic is unchecked.
func (cursor *Cursor) Evaluate(expression *Evaluation) ValueID {
	cursor.check()
	if previous := cursor.previous(); previous != nil && previous.node.Kind == Evaluate && equalEvaluation(previous.node.Evaluation, expression) {
		cursor.branch = &previous.children
		return previous.node.Outputs[0]
	}
	output := ValueID(len(cursor.graph.types))
	cursor.graph.types = append(cursor.graph.types, expression.Type)
	node := &Node{Kind: Evaluate, Evaluation: expression, Outputs: []ValueID{output}, Origin: cursor.origin}
	cursor.append(node)
	return output
}

func equalEvaluation(left, right *Evaluation) bool {
	if left.Kind == ForeignEvaluation || right.Kind == ForeignEvaluation || left.Kind != right.Kind || left.Type != right.Type ||
		left.Operand != right.Operand || left.Arithmetic != right.Arithmetic || len(left.Arguments) != len(right.Arguments) {
		return false
	}
	for index, argument := range left.Arguments {
		if !equalEvaluation(argument, right.Arguments[index]) {
			return false
		}
	}
	return true
}

package matchir

import "slices"

// Cursor appends one ordered rule's decisions. Begin a separate cursor for each
// source alternative. A cursor is finished by Build; it must not be copied or
// reused afterward. Mutating a sealed Graph is a compiler programming error.
type Cursor struct {
	graph    *Graph
	branch   *branch
	origin   Origin
	finished bool
}

// Begin starts the next source alternative at the original input. Folding only
// considers the last adjacent sibling, never an older matching alternative.
func (graph *Graph) Begin(origin Origin) *Cursor {
	if graph.sealed {
		panic("matchir: cannot append to a sealed graph")
	}
	return &Cursor{graph: graph, branch: &graph.root, origin: origin}
}

// Match appends one pure extraction and returns its canonical typed children.
// outputTypes is borrowed only during this call. Returned IDs are a caller-owned
// copy, so mutating the result cannot corrupt the graph or another cursor.
func (cursor *Cursor) Match(hook HookID, input ValueID, outputTypes []TypeID) []ValueID {
	cursor.check()
	if previous := cursor.previous(); previous != nil && previous.node.Kind == Match && previous.node.Hook == hook && previous.node.Input == input &&
		cursor.sameTypes(previous.node.Outputs, outputTypes) {
		cursor.branch = &previous.children
		return slices.Clone(previous.node.Outputs)
	}
	node := &Node{Kind: Match, Hook: hook, Input: input, Origin: cursor.origin}
	for _, typ := range outputTypes {
		node.Outputs = append(node.Outputs, ValueID(len(cursor.graph.types)))
		cursor.graph.types = append(cursor.graph.types, typ)
	}
	cursor.append(node)
	return slices.Clone(node.Outputs)
}

// Test appends an equality predicate without swapping its operands.
func (cursor *Cursor) Test(left, right Operand) *Node {
	return cursor.predicate(&Node{Kind: Test, Left: left, Right: right, Origin: cursor.origin})
}

// Guard appends a pure predicate expression. Preconditions and guard order are
// retained: identical nonadjacent guards are not searched for or moved earlier.
func (cursor *Cursor) Guard(expression ExprID) *Node {
	return cursor.predicate(&Node{Kind: Guard, Expression: expression, Origin: cursor.origin})
}

// Build appends one terminal construction. Builders are never shared, even when
// their expression identity matches another source alternative.
func (cursor *Cursor) Build(expression ExprID) *Node {
	cursor.check()
	node := &Node{Kind: Build, Expression: expression, Origin: cursor.origin}
	cursor.append(node)
	cursor.finished = true
	return node
}

func (cursor *Cursor) check() {
	if cursor.finished || cursor.graph.sealed {
		panic("matchir: cursor is finished or graph is sealed")
	}
}

func (cursor *Cursor) previous() *attempt {
	count := len(cursor.branch.attempts)
	if count == 0 {
		return nil
	}
	return cursor.branch.attempts[count-1]
}

func (cursor *Cursor) sameTypes(values []ValueID, types []TypeID) bool {
	if len(values) != len(types) {
		return false
	}
	for index, value := range values {
		if cursor.graph.Type(value) != types[index] {
			return false
		}
	}
	return true
}

func (cursor *Cursor) predicate(node *Node) *Node {
	cursor.check()
	if previous := cursor.previous(); previous != nil && previous.node.Kind == node.Kind {
		old := previous.node
		if old.Left == node.Left && old.Right == node.Right && old.Expression == node.Expression && old.Comparison == node.Comparison {
			cursor.branch = &previous.children
			return old
		}
	}
	cursor.append(node)
	return node
}

func (cursor *Cursor) append(node *Node) {
	next := &attempt{node: node}
	cursor.branch.attempts = append(cursor.branch.attempts, next)
	cursor.branch = &next.children
}

// link gives each exhausted shared prefix its parent's fallback. A successful
// shared extraction is not retried when one child fails: remaining ordered
// children reuse its established values, then rejoin that same fallback.
func link(choices *branch, fallback *Node) *Node {
	for index := len(choices.attempts) - 1; index >= 0; index-- {
		attempt := choices.attempts[index]
		node := attempt.node
		node.Fallback = fallback
		if node.Kind != Build {
			node.Next = link(&attempt.children, fallback)
		}
		attempt.children = branch{}
		fallback = node
	}
	return fallback
}

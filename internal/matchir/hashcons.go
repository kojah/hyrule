package matchir

import (
	"encoding/binary"
	"fmt"
	"slices"
)

// HashCons returns a new DAG with structurally identical pure decisions shared.
// Children are canonicalized before their parents. Input nodes are never mutated.
// All IDs and schema pointers must belong to one identity namespace: independently
// built Graph roots cannot be combined without first remapping their value IDs.
// Build and Evaluate nodes remain distinct, preserving terminal provenance and
// allocation identity. This is code-structure sharing, not result memoization.
// The first visited pure decision supplies diagnostic Origin, as in prefix sharing.
// Cyclic inputs are rejected; traversal is iterative and visits each input once.
func HashCons(root *Node) (*Node, error) {
	if root == nil {
		return nil, nil
	}
	pass := consing{done: map[*Node]*Node{}, active: map[*Node]bool{}, table: map[decisionKey]*Node{}, identities: map[*Node]uint64{}}
	stack := []consFrame{{node: root}}
	pass.active[root] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.child < 2+len(top.node.Cases) {
			child := decisionChild(top.node, top.child)
			top.child++
			if child == nil || pass.done[child] != nil {
				continue
			}
			if pass.active[child] {
				return nil, fmt.Errorf("matching IR contains a cycle at rule %q", child.Origin.Rule)
			}
			pass.active[child] = true
			stack = append(stack, consFrame{node: child})
			continue
		}
		node := top.node
		pass.done[node] = pass.intern(node)
		delete(pass.active, node)
		stack = stack[:len(stack)-1]
	}
	return pass.done[root], nil
}

type consFrame struct {
	node  *Node
	child int
}
type consing struct {
	done       map[*Node]*Node
	active     map[*Node]bool
	table      map[decisionKey]*Node
	identities map[*Node]uint64
}

// Every semantic node field participates except Origin and Evaluation. Origin
// is diagnostic; evaluations are deliberately ineligible for interning.
type decisionKey struct {
	kind           Kind
	hook           HookID
	input          ValueID
	outputs        string
	left, right    Operand
	expression     ExprID
	next, fallback *Node
	union          *UnionSchema
	variant        VariantID
	cases          string
	record         *RecordSchema
	comparison     Comparison
}

func decisionChild(node *Node, index int) *Node {
	switch index {
	case 0:
		return node.Next
	case 1:
		return node.Fallback
	default:
		return node.Cases[index-2].Next
	}
}

func (p *consing) intern(original *Node) *Node {
	shareable := shareableDecision(original)
	var key decisionKey
	if shareable {
		key = p.key(original)
		if existing := p.table[key]; existing != nil {
			return existing
		}
	}
	node := *original
	node.Outputs = slices.Clone(original.Outputs)
	node.Next = p.done[original.Next]
	node.Fallback = p.done[original.Fallback]
	node.Cases = slices.Clone(original.Cases)
	for i := range node.Cases {
		node.Cases[i].Next = p.done[original.Cases[i].Next]
	}
	if shareable {
		p.table[key] = &node
	}
	p.identities[&node] = uint64(len(p.identities)) + 1
	return &node
}

func shareableDecision(node *Node) bool {
	// A future node kind must opt in after its effect and identity contract is
	// established. Even a stray evaluation payload prevents accidental sharing.
	if node.Evaluation != nil {
		return false
	}
	switch node.Kind {
	case Fail, Match, Test, Guard, Variant, Dispatch, Record, Compare:
		return true
	default:
		return false
	}
}

func (p *consing) key(node *Node) decisionKey {
	var outputs, cases []byte
	for _, value := range node.Outputs {
		outputs = binary.AppendUvarint(outputs, uint64(value))
	}
	for _, arm := range node.Cases {
		cases = binary.AppendUvarint(cases, uint64(arm.Variant))
		cases = binary.AppendUvarint(cases, p.identities[p.done[arm.Next]])
	}
	return decisionKey{
		kind: node.Kind, hook: node.Hook, input: node.Input, outputs: string(outputs),
		left: node.Left, right: node.Right, expression: node.Expression,
		next: p.done[node.Next], fallback: p.done[node.Fallback], union: node.Union, variant: node.Variant,
		cases: string(cases), record: node.Record, comparison: node.Comparison,
	}
}

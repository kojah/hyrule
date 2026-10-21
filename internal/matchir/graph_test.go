package matchir_test

import (
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
)

const integerType matchir.TypeID = 1
const extract matchir.HookID = 1
const largerThanTen matchir.ExprID = 1
const even matchir.ExprID = 2
const scaled matchir.ExprID = 3
const fallback matchir.ExprID = 4
const three matchir.LiteralID = 1

// This backend interprets opaque integer/Boolean operations directly. It has no
// Go source emitter, syntax tree, host type objects or generated-code dependency.
func TestOrderedGraphExecutesSharedExtractionAndFallback(t *testing.T) {
	graph := matchir.New(integerType)
	first := graph.Begin(matchir.Origin{Rule: "large"})
	value := first.Match(extract, graph.Input(), []matchir.TypeID{integerType})[0]
	first.Guard(largerThanTen)
	first.Build(scaled)
	second := graph.Begin(matchir.Origin{Rule: "even"})
	if next := second.Match(extract, graph.Input(), []matchir.TypeID{integerType})[0]; next != value {
		t.Fatal("adjacent shared extraction returned a different value identity")
	}
	second.Guard(even)
	second.Build(scaled)
	third := graph.Begin(matchir.Origin{Rule: "three"})
	third.Match(extract, graph.Input(), []matchir.TypeID{integerType})
	third.Test(matchir.Value(value), matchir.Literal(three))
	third.Build(scaled)
	graph.Begin(matchir.Origin{Rule: "fallback"}).Build(fallback)
	root := graph.Root()
	if graph.Type(value) != integerType {
		t.Fatal("extraction lost its opaque output type")
	}

	for _, test := range []struct {
		input, want int
		guards      []matchir.ExprID
	}{
		{0, -1, []matchir.ExprID{largerThanTen, even}},
		{1, 20, []matchir.ExprID{largerThanTen, even}},
		{2, 30, []matchir.ExprID{largerThanTen, even}},
		{11, 120, []matchir.ExprID{largerThanTen}},
	} {
		execution := integerExecution{test: t, values: map[matchir.ValueID]int{graph.Input(): test.input}, extracted: value}
		node := root
		for work := 0; node != nil && work < 32; work++ {
			node = execution.step(node)
		}
		got, returned, extracts, builds, guards := execution.got, execution.returned, execution.extracts, execution.builds, execution.guards
		if !returned || got != test.want || extracts != 1 || builds != 1 {
			t.Fatalf("input %d => %d returned=%v extracts=%d builds=%d", test.input, got, returned, extracts, builds)
		}
		if len(guards) != len(test.guards) {
			t.Fatalf("input %d guard order %v", test.input, guards)
		}
		for index, guard := range guards {
			if guard != test.guards[index] {
				t.Fatalf("input %d guard order %v", test.input, guards)
			}
		}
	}

	// Backend traversal includes the validation-only suffix: equal construction
	// expressions stay separate terminals rather than sharing an effectful build.
	seen := map[*matchir.Node]struct{}{}
	var visit func(*matchir.Node)
	terminals := 0
	visit = func(node *matchir.Node) {
		if node == nil {
			return
		}
		if _, known := seen[node]; known {
			return
		}
		seen[node] = struct{}{}
		if node.Kind == matchir.Build && node.Expression == scaled {
			terminals++
		}
		visit(node.Next)
		visit(node.Fallback)
	}
	visit(root)
	if terminals != 3 {
		t.Fatalf("construction terminals were merged: got %d", terminals)
	}
}

// integerExecution is a tiny independent backend for the public Node contract.
type integerExecution struct {
	test                  *testing.T
	values                map[matchir.ValueID]int
	extracted             matchir.ValueID
	extracts, builds, got int
	returned              bool
	guards                []matchir.ExprID
}

func (execution *integerExecution) operand(input matchir.Operand) int {
	if input.Kind == matchir.LiteralOperand {
		if input.Literal != three {
			execution.test.Fatalf("unknown literal %d", input.Literal)
		}
		return 3
	}
	result, known := execution.values[input.Value]
	if !known {
		execution.test.Fatalf("value %d used before extraction", input.Value)
	}
	return result
}

func (execution *integerExecution) step(node *matchir.Node) *matchir.Node {
	switch node.Kind {
	case matchir.Match:
		if node.Hook != extract || len(node.Outputs) != 1 {
			execution.test.Fatal("unexpected extraction contract")
		}
		execution.extracts++
		execution.values[node.Outputs[0]] = execution.values[node.Input] + 1
		return node.Next
	case matchir.Test:
		if execution.operand(node.Left) == execution.operand(node.Right) {
			return node.Next
		}
		return node.Fallback
	case matchir.Guard:
		execution.guards = append(execution.guards, node.Expression)
		accepted := execution.values[execution.extracted] > 10
		if node.Expression == even {
			accepted = execution.values[execution.extracted]%2 == 0
		}
		if accepted {
			return node.Next
		}
		return node.Fallback
	case matchir.Build:
		execution.builds++
		execution.got = -1
		if node.Expression == scaled {
			execution.got = execution.values[execution.extracted] * 10
		}
		execution.returned = true
		// Build.Fallback is retained for static validation only.
		return nil
	case matchir.Fail:
		return nil
	default:
		execution.test.Fatal("unknown graph node kind")
		return nil
	}
}

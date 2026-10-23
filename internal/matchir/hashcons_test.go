package matchir_test

import (
	"testing"

	"github.com/kojah/hyrule/internal/matchir"
)

// The portable IR is a public compiler boundary. Pointer sharing is observable
// to any backend traversing it; this test does not inspect optimizer internals.
func TestHashConsSharesSuffixesWithoutChangingPaths(t *testing.T) {
	fail := &matchir.Node{Kind: matchir.Fail}
	answer := &matchir.Node{Kind: matchir.Build, Expression: 7, Fallback: fail, Origin: matchir.Origin{Rule: "answer"}}
	branch := func() *matchir.Node {
		test := &matchir.Node{Kind: matchir.Test, Left: matchir.Value(0), Right: matchir.Literal(1), Next: answer, Fallback: fail}
		return &matchir.Node{Kind: matchir.Guard, Expression: 2, Next: test, Fallback: fail}
	}
	left, right := branch(), branch()
	root := &matchir.Node{Kind: matchir.Test, Left: matchir.Value(0), Right: matchir.Literal(0), Next: left, Fallback: right}
	optimized, err := matchir.HashCons(root)
	if err != nil {
		t.Fatal(err)
	}
	if optimized.Next != optimized.Fallback {
		t.Fatal("identical multi-node suffixes were not shared")
	}
	if root.Next != left || root.Fallback != right || left == right {
		t.Fatal("optimizer changed the input graph")
	}
	for _, input := range []int{0, 1, 2} {
		before, events := executeConsGraph(root, input)
		after, optimizedEvents := executeConsGraph(optimized, input)
		if before != after || events != optimizedEvents {
			t.Fatalf("input %d: before %d/%s after %d/%s", input, before, events, after, optimizedEvents)
		}
	}
	twice, err := matchir.HashCons(optimized)
	if err != nil || twice.Next != twice.Fallback {
		t.Fatalf("second pass lost canonical sharing: %v", err)
	}
}

func executeConsGraph(node *matchir.Node, input int) (matchir.ExprID, string) {
	events := ""
	for node != nil {
		switch node.Kind {
		case matchir.Fail:
			return 0, events
		case matchir.Build:
			return node.Expression, events + "build;"
		case matchir.Guard:
			events += "guard;"
			node = node.Next
		case matchir.Test:
			events += "test;"
			if input == int(node.Right.Literal) {
				node = node.Next
			} else {
				node = node.Fallback
			}
		default:
			panic("unexpected test graph node")
		}
	}
	return 0, events
}

func TestHashConsRetainsIdentityAndPriorityBoundaries(t *testing.T) {
	fail := &matchir.Node{Kind: matchir.Fail}
	answer := &matchir.Node{Kind: matchir.Build, Expression: 1, Origin: matchir.Origin{Rule: "first"}}
	for _, test := range []struct {
		name   string
		change func(*matchir.Node)
	}{
		{"input", func(n *matchir.Node) { n.Input = 2 }},
		{"hook", func(n *matchir.Node) { n.Hook = 2 }},
		{"capture", func(n *matchir.Node) { n.Outputs = []matchir.ValueID{2} }},
		{"fallback", func(n *matchir.Node) { n.Fallback = answer }},
		{"success", func(n *matchir.Node) { n.Next = fail }},
		{"schema", func(n *matchir.Node) { n.Record = &matchir.RecordSchema{ID: 1, Type: 2} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			left := &matchir.Node{Kind: matchir.Match, Hook: 1, Outputs: []matchir.ValueID{1}, Next: answer, Fallback: fail}
			right := *left
			test.change(&right)
			root := &matchir.Node{Kind: matchir.Test, Next: left, Fallback: &right}
			got, err := matchir.HashCons(root)
			if err != nil || got.Next == got.Fallback {
				t.Fatalf("merged distinct %s: %v", test.name, err)
			}
		})
	}
	for _, kind := range []matchir.Kind{matchir.Build, matchir.Evaluate} {
		left := &matchir.Node{Kind: kind, Expression: 1, Origin: matchir.Origin{Rule: "first"}}
		right := *left
		right.Origin.Rule = "second"
		root := &matchir.Node{Kind: matchir.Test, Next: left, Fallback: &right}
		got, err := matchir.HashCons(root)
		if err != nil || got.Next == got.Fallback || got.Next.Origin.Rule != "first" || got.Fallback.Origin.Rule != "second" {
			t.Fatalf("allocation or terminal provenance was shared for %v: %v", kind, err)
		}
	}
}

func TestHashConsPreservesDispatchCaseOrder(t *testing.T) {
	leaf := &matchir.Node{Kind: matchir.Build}
	schema := &matchir.UnionSchema{ID: 1, Type: 2}
	left := &matchir.Node{Kind: matchir.Dispatch, Union: schema, Cases: []matchir.VariantCase{{Variant: 0, Next: leaf}, {Variant: 1, Next: leaf}}}
	right := &matchir.Node{Kind: matchir.Dispatch, Union: schema, Cases: []matchir.VariantCase{{Variant: 1, Next: leaf}, {Variant: 0, Next: leaf}}}
	root := &matchir.Node{Kind: matchir.Test, Next: left, Fallback: right}
	got, err := matchir.HashCons(root)
	if err != nil || got.Next == got.Fallback {
		t.Fatalf("case order was discarded: %v", err)
	}
	right.Cases = append([]matchir.VariantCase{}, left.Cases...)
	got, err = matchir.HashCons(root)
	if err != nil || got.Next != got.Fallback {
		t.Fatalf("equal dispatches were not shared: %v", err)
	}
}

func TestHashConsRejectsCyclesAndHandlesDeepGraphs(t *testing.T) {
	cycle := &matchir.Node{Kind: matchir.Test}
	cycle.Next = cycle
	if _, err := matchir.HashCons(cycle); err == nil {
		t.Fatal("cyclic graph accepted")
	}
	if cycle.Next != cycle {
		t.Fatal("cycle rejection mutated input")
	}
	var deep *matchir.Node
	for range 20000 {
		deep = &matchir.Node{Kind: matchir.Test, Next: deep}
	}
	if _, err := matchir.HashCons(deep); err != nil {
		t.Fatal(err)
	}
	if empty, err := matchir.HashCons(nil); err != nil || empty != nil {
		t.Fatalf("empty graph: %v %v", empty, err)
	}
}

// This is a synthetic shared-suffix workload, not a claim about production rules.
func BenchmarkHashConsRepeatedSuffix(b *testing.B) {
	leaf := &matchir.Node{Kind: matchir.Build}
	root := &matchir.Node{Kind: matchir.Dispatch}
	for variant := range 64 {
		next := leaf
		for depth := range 8 {
			next = &matchir.Node{Kind: matchir.Test, Left: matchir.Value(0), Right: matchir.Literal(matchir.LiteralID(depth)), Next: next}
		}
		root.Cases = append(root.Cases, matchir.VariantCase{Variant: matchir.VariantID(variant), Next: next})
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := matchir.HashCons(root); err != nil {
			b.Fatal(err)
		}
	}
}

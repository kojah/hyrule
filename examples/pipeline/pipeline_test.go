package pipeline_test

import (
	"github.com/kojah/lockdoc/tools/hyrule/examples/pipeline"
	"testing"
)

func TestLiftAndLower(t *testing.T) {
	source := pipeline.Source{Kind: "lookup", Origin: 42, Map: "m", Key: "k", Tuple: true}
	if _, ok := pipeline.Lift(source, &pipeline.Host{}); ok {
		t.Fatal("guard failure matched")
	}
	op, ok := pipeline.Lift(source, &pipeline.Host{Ready: true})
	if !ok {
		t.Fatal("lookup did not lift")
	}
	plan, ok := pipeline.Lower(op)
	if !ok || len(plan) != 4 {
		t.Fatalf("tuple plan: %v %v", plan, ok)
	}
	for _, effect := range plan {
		if effect.Origin != 42 {
			t.Fatal("lost origin")
		}
	}
	source.Tuple = false
	op, ok = pipeline.Lift(source, &pipeline.Host{Ready: true})
	if !ok {
		t.Fatal("single-result lookup did not lift")
	}
	plan, ok = pipeline.Lower(op)
	if !ok || len(plan) != 3 {
		t.Fatalf("single-result plan: %v %v", plan, ok)
	}
	source.Kind = "unknown"
	if _, ok = pipeline.Lift(source, &pipeline.Host{Ready: true}); ok {
		t.Fatal("unknown operation matched")
	}
}

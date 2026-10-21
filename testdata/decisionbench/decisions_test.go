package decisionbench

import (
	"os"
	"testing"
)

func TestEquivalentChoices(t *testing.T) {
	for action := 0; action < 3; action++ {
		for length := 0; length < 9; length++ {
			for capacity := 0; capacity < 9; capacity++ {
				input := Request{Action: action, Header: Header{length, capacity}}
				old, oldOK := Legacy(input)
				got, ok := Optimized(input)
				if old != got || oldOK != ok {
					t.Fatalf("%+v: before=%+v,%v after=%+v,%v", input, old, oldOK, got, ok)
				}
			}
		}
	}
}

func BenchmarkDecisions(b *testing.B) {
	for _, version := range []struct {
		name, file string
		run        Signature
	}{
		{"legacy", "legacy_test.go", Legacy}, {"graph", "optimized_gen.go", Optimized},
	} {
		b.Run(version.name, func(b *testing.B) {
			source, err := os.ReadFile(version.file)
			if err != nil {
				b.Fatal(err)
			}
			for _, workload := range []struct {
				name  string
				input Request
			}{
				{"miss", Request{Action: 0}}, {"empty", Request{1, Header{0, 8}}},
				{"reuse", Request{1, Header{2, 8}}}, {"growth", Request{1, Header{8, 8}}},
			} {
				b.Run(workload.name, func(b *testing.B) {
					b.ReportAllocs()
					MatcherCalls = 0
					sum := 0
					for b.Loop() {
						plan, ok := version.run(workload.input)
						if ok {
							sum += plan.Kind
						}
					}
					b.ReportMetric(float64(MatcherCalls)/float64(b.N), "match/op")
					b.ReportMetric(float64(len(source)), "source-B")
					if sum < 0 {
						b.Fatal(sum)
					}
				})
			}
		})
	}
}

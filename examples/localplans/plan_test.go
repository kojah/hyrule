package localplans_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule/examples/localplans"
)

func TestAppendDecisionPreservesSnapshotAndOrdersEffects(t *testing.T) {
	for _, test := range []struct {
		name           string
		length, cap, n int
		outcome        localplans.Outcome
	}{
		{"fits", 3, 8, 2, localplans.Reuse},
		{"at capacity", 3, 5, 2, localplans.Reuse},
		{"needs allocation", 3, 4, 2, localplans.Refused},
		{"negative count", 3, 8, -1, localplans.Refused},
		{"overflow", math.MaxInt, math.MaxInt, 1, localplans.Refused},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := localplans.Request{Source: "original append", Header: localplans.Header{Backing: "original backing", Length: test.length, Capacity: test.cap},
				Count: test.n, Copies: []localplans.Copy{{Offset: 17, Count: 19}}}
			before := input
			before.Copies = append([]localplans.Copy(nil), input.Copies...)
			got, matched := localplans.Plan(input)
			if !matched || got.Outcome != test.outcome || got.Source != input.Source || got.Header.Backing != input.Header.Backing || got.Header.Capacity != input.Header.Capacity {
				t.Fatalf("plan=%+v matched=%t", got, matched)
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatalf("input mutated: got %+v want %+v", input, before)
			}
			if test.outcome == localplans.Reuse {
				want := []localplans.Copy{{Offset: test.length, Count: test.n}}
				if got.Header.Length != test.length+test.n || !reflect.DeepEqual(got.Copies, want) {
					t.Fatalf("reuse plan=%+v want effects=%+v", got, want)
				}
			} else if got.Header != input.Header || got.Copies == nil || len(got.Copies) != 0 {
				t.Fatalf("refused plan changed header or retained effects: %+v", got)
			}
		})
	}
}

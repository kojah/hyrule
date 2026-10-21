package transitions_test

import (
	"reflect"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule/examples/transitions"
)

func TestPlanPreservesUnknownAndSkipsUnneededQueries(t *testing.T) {
	for _, tt := range []struct {
		name              string
		event             transitions.Event
		proof             transitions.Proof
		budget, remaining int
		want              []transitions.Effect
	}{
		{"exclusive", transitions.Event{Lock: 7}, transitions.Proof{Kind: transitions.Proven}, 1, 0,
			[]transitions.Effect{{Kind: transitions.Acquire, Lock: 7}}},
		{"shared", transitions.Event{Lock: 7}, transitions.Proof{Kind: transitions.Disproven}, 1, 0,
			[]transitions.Effect{{Kind: transitions.Acquire, Lock: 7}, {Kind: transitions.Order, Lock: 7}}},
		{"cutoff", transitions.Event{Lock: 7}, transitions.Proof{Kind: transitions.Proven}, 0, 0,
			[]transitions.Effect{{Kind: transitions.Acquire, Lock: 7}, {Kind: transitions.Uncertain, Lock: 7, Reason: "budget exhausted"}}},
		{"deferred release", transitions.Event{Lock: 7, Release: true, Deferred: true}, transitions.Proof{}, 1, 1,
			[]transitions.Effect{{Kind: transitions.DeferRelease, Lock: 7}}},
		{"release", transitions.Event{Lock: 7, Release: true}, transitions.Proof{}, 1, 1,
			[]transitions.Effect{{Kind: transitions.ReleaseNow, Lock: 7}}},
		{"deferred acquire", transitions.Event{Lock: 7, Deferred: true}, transitions.Proof{}, 1, 1,
			[]transitions.Effect{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			host := &transitions.Host{Budget: tt.budget, Facts: map[int]transitions.Proof{7: tt.proof}}
			got, ok := transitions.Plan(tt.event, host)
			if !ok || !reflect.DeepEqual(got, tt.want) || host.Budget != tt.remaining {
				t.Fatalf("got %v,%v budget=%d", got, ok, host.Budget)
			}
		})
	}
}

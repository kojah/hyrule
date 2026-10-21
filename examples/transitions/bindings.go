// Package transitions demonstrates planning lock effects from explicit proof
// outcomes. It is an example host, not a concurrency analysis or a lock executor.
package transitions

// Event describes one acquisition or release registration.
type Event struct {
	Lock              int
	Release, Deferred bool
}

// PlanSignature is the generated planner's public contract.
type PlanSignature func(Event, *Host) ([]Effect, bool)

// ProofKind distinguishes an established answer from an unavailable answer.
type ProofKind uint8

const (
	// Proven establishes exclusivity.
	Proven ProofKind = iota
	// Disproven establishes lack of exclusivity.
	Disproven
	// Unknown means the host could not establish either answer.
	Unknown
)

// Proof is the closed outcome of a host query.
type Proof struct {
	Kind   ProofKind
	Reason string
}

// EffectKind identifies an update for the host to apply after planning.
type EffectKind uint8

const (
	// Acquire records that the lock was acquired.
	Acquire EffectKind = iota
	// ReleaseNow records an immediate release.
	ReleaseNow
	// DeferRelease records a future release.
	DeferRelease
	// Order records an ordering relation justified by host evidence.
	Order
	// Uncertain records that ordering evidence is incomplete.
	Uncertain
)

// Effect carries one planned update, in application order.
type Effect struct {
	Kind   EffectKind
	Lock   int
	Reason string
}

// Host owns the counted allowance and supplied facts. An absent fact is unknown.
type Host struct {
	Budget int
	Facts  map[int]Proof
}

// MatchEvent is pure and exposes the instruction's structural fields.
func MatchEvent(event Event) (lock int, release, deferred, matched bool) {
	return event.Lock, event.Release, event.Deferred, true
}

// Exclusive consumes one query step. Its mutation is legal only as a query hook.
func (host *Host) Exclusive(lock int) Proof {
	if host.Budget <= 0 {
		return Proof{Kind: Unknown, Reason: "budget exhausted"}
	}
	host.Budget--
	if proof, ok := host.Facts[lock]; ok {
		return proof
	}
	return Proof{Kind: Unknown, Reason: "fact unavailable"}
}

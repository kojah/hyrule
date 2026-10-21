// Package pipeline demonstrates lifting source nodes to semantic operations and
// lowering operations to complete effect plans. Origin IDs survive both passes.
package pipeline

// Source is a small source IR; actual hosts can use their own SSA types.
type Source struct {
	Kind     string
	Origin   int
	Map, Key string
	Tuple    bool
}

// Operation is a typed semantic map read, independent of source syntax.
type Operation struct {
	Origin   int
	Map, Key string
	Tuple    bool
}

// Effect is one output of lowering.
type Effect struct {
	Kind    string
	Origin  int
	Operand string
}

// Plan bundles every effect before the caller applies any of them.
type Plan []Effect

// Host supplies facts without global state.
type Host struct{ Ready bool }

// LiftSignature defines the generated source-to-semantics API.
type LiftSignature func(Source, *Host) (Operation, bool)

// LowerSignature defines the generated semantics-to-plan API.
type LowerSignature func(Operation) (Plan, bool)

// MatchLookup exposes source structure without changing it.
func MatchLookup(s Source) (int, string, string, bool, bool) {
	return s.Origin, s.Map, s.Key, s.Tuple, s.Kind == "lookup"
}

// IsReady is a guard supplied by the current analysis context.
func (h *Host) IsReady() bool { return h != nil && h.Ready }

// BuildRead preserves source identity and tuple shape.
func BuildRead(origin int, m, key string, tuple bool) Operation {
	return Operation{origin, m, key, tuple}
}

// MatchRead exposes the semantic operation to the lowerer.
func MatchRead(op Operation) (int, string, string, bool, bool) {
	return op.Origin, op.Map, op.Key, op.Tuple, true
}

// BuildPlan produces a complete value/presence effect bundle.
func BuildPlan(origin int, m, key string, tuple bool) Plan {
	p := Plan{{"read-map", origin, m}, {"read-key", origin, key}, {"result-value", origin, ""}}
	if tuple {
		p = append(p, Effect{"result-present", origin, ""})
	}
	return p
}

package hyrule_test

import "testing"

const usefulnessHost = `package fixture
type Kind uint8
const (
 KindA Kind = iota
 KindB
 KindC
)
type Capacity struct { Kind Kind; Bound int }
type Node interface{ node() }
type Leaf struct{ Value int }
func (*Leaf) node() {}
type Pair struct{ Left, Right Node }
func (Pair) node() {}
type Seq struct{ values []int }
func (s Seq) Len() int { return len(s.values) }
func (s Seq) At(i int) int { return s.values[i] }
func (s Seq) Sub(low, high int) Seq { return Seq{s.values[low:high]} }
type Box struct{ n int }
//hyrule:view
func (b Box) Size() int { return b.n }
type Input struct {
 Flag bool
 Kind Kind
 Count int
 Name string
 Capacity Capacity
 Next *Input
 Node Node
 Items []int
 Seq Seq
 Box Box
}
func Split(n int) (int, int, bool) { return n, n, true }
func Positive(n int) bool { return n > 0 }
`

const usefulnessPrelude = `fn rewrite(arg0: Input) -> int

host
  Split? = Split
  Positive = Positive

`

func expectUsefulness(t *testing.T, rules, want string) {
	t.Helper()
	root := fixture(t)
	write(t, root, "bindings.go", usefulnessHost)
	signatureConfig(t, root, "")
	write(t, root, "input.rules", usefulnessPrelude+rules)
	expectUnionCompile(t, root, want)
}

// A rule is unreachable when earlier unguarded rules match every input it
// matches. Closed domains (Booleans, nil and non-nil pointers, list lengths
// and products) can be covered; equal deterministic tests subsume each other.
func TestUnreachableRules(t *testing.T) {
	for _, test := range []struct{ name, rules, want string }{
		{"after wildcard", `@First
rewrite _ = 0

@Late
rewrite (Input (Flag: true)) = 1
`, "rule Late: unreachable rule: rule First matches every input it matches"},
		{"Booleans", `@True
rewrite (Input (Flag: true)) = 0

@False
rewrite (Input (Flag: false)) = 1

@Late
rewrite (Input (Count: 1)) = 2
`, "rule Late: unreachable rule: earlier rules match every input"},
		{"nil and non-nil", `@Nil
rewrite (Input (Next: nil)) = 0

@Some
rewrite (Input (Next: Input ())) = 1

@Late
rewrite (Input (Count: 2)) = 2
`, "rule Late: unreachable"},
		{"list lengths", `@Empty
rewrite (Input (Items: [])) = 0

@One
rewrite (Input (Items: [_])) = 1

@More
rewrite (Input (Items: [_, _, .._])) = 2

@Late
rewrite (Input (Items: [3, ..rest])) = 3
`, "rule Late: unreachable"},
		{"list rest covers longer lists", `@Rest
rewrite (Input (Items: [_, ..rest])) = 0

@Late
rewrite (Input (Items: [1, 2, 3])) = 1
`, "rule Late: unreachable rule: rule Rest matches"},
		{"or covers", `@Either
rewrite (Input (Flag: (true | false))) = 0

@Late
rewrite _ = 1
`, "rule Late: unreachable"},
		{"repeated literal", `@One
rewrite (Input (Count: 1)) = 0

@Again
rewrite (Input (Count: 1, Flag: true)) = 1
`, "rule Again: unreachable"},
		{"repeated typed constant", `@A
rewrite (Input (Kind: KindA)) = 0

@Again
rewrite (Input (Kind: KindA)) = 1
`, "rule Again: unreachable"},
		{"tagged variant", `@B
rewrite (Input (Capacity: KindB (Bound: bound))) = bound

@Zero
rewrite (Input (Capacity: KindB (Bound: 0))) = 1
`, "rule Zero: unreachable"},
		{"implementer", `@Leaf
rewrite (Input (Node: Leaf (Value: value))) = value

@Three
rewrite (Input (Node: Leaf (Value: 3))) = 3
`, "rule Three: unreachable"},
		{"product view", `@Any
rewrite (Input (Capacity: Capacity ())) = 0

@Late
rewrite (Input (Count: 4)) = 1
`, "rule Late: unreachable"},
		{"same host matcher", `@Split
rewrite (Input (Count: Split left right)) = left + right

@Again
rewrite (Input (Count: Split _ 0)) = 0
`, "rule Again: unreachable"},
		{"guarded branch after otherwise", `@Count
rewrite (Input (Count: count))
  | Positive count = 1
  | otherwise = 2
  | Positive (count + 1) = 3
`, "rule Count_3: unreachable"},
		{"nested", `@Deep
rewrite (Input (Next: Input (Next: Input (Flag: flag)))) = 0

@Late
rewrite (Input (Next: Input (Next: Input (Flag: true, Count: 2)))) = 1
`, "rule Late: unreachable"},
		{"dead alternative", `@True
rewrite (Input (Flag: true)) = 0

@Either
rewrite (Input (Flag: (true | false))) = 1
`, "input.rules:11:24: rule Either: unreachable alternative"},
	} {
		t.Run(test.name, func(t *testing.T) { expectUsefulness(t, test.rules, test.want) })
	}
}

// Anything the analysis cannot see through never subsumes a later rule:
// guards, host matchers, repeated captures, accessors, literals of infinite
// domains, typed constants (Go admits other values), interface inputs (other
// dynamic types and typed nil), lists without a rest, and host sequences,
// whose Len may be negative.
func TestReachableRulesAreNotReported(t *testing.T) {
	for _, test := range []struct{ name, rules string }{
		{"guard", `@Positive
rewrite (Input (Count: count))
  | Positive count
  = 0

@Again
rewrite (Input (Count: count))
  | Positive count
  = 1

@Rest
rewrite (Input (Count: count)) = 2
`},
		{"host matcher", `@Split
rewrite (Input (Count: Split _ _)) = 0

@Rest
rewrite _ = 1
`},
		{"repeated capture", `@Same
rewrite (Input (Count: count, Capacity: Capacity (Bound: count))) = 0

@Again
rewrite (Input (Count: count, Capacity: Capacity (Bound: count))) = 1
`},
		{"accessor", `@Size
rewrite (Input (Box: Box (Size(): 1))) = 0

@Again
rewrite (Input (Box: Box (Size(): 1))) = 1
`},
		{"literals", `@Zero
rewrite (Input (Count: 0, Name: "")) = 0

@One
rewrite (Input (Count: 1)) = 1

@Rest
rewrite _ = 2
`},
		{"typed constants", `@A
rewrite (Input (Kind: KindA)) = 0

@B
rewrite (Input (Kind: KindB)) = 1

@C
rewrite (Input (Kind: KindC)) = 2

@Rest
rewrite _ = 3
`},
		{"implementers", `@Nil
rewrite (Input (Node: nil)) = 0

@Leaf
rewrite (Input (Node: Leaf ())) = 1

@Pair
rewrite (Input (Node: Pair ())) = 2

@Rest
rewrite _ = 3
`},
		{"list lengths", `@Empty
rewrite (Input (Items: [])) = 0

@One
rewrite (Input (Items: [_])) = 1

@More
rewrite (Input (Items: [_, .._])) = 2
`},
		{"host sequence lengths", `@Empty
rewrite (Input (Seq: [])) = 0

@More
rewrite (Input (Seq: [_, ..rest])) = 1

@Negative
rewrite _ = 2
`},
		{"different fields", `@Flag
rewrite (Input (Flag: true)) = 0

@Count
rewrite (Input (Count: 1)) = 1
`},
	} {
		t.Run(test.name, func(t *testing.T) { expectUsefulness(t, test.rules, "") })
	}
}

const coverageHost = `package fixture
type Tag uint8
const (
 ChannelTag Tag = iota
 ClosedTag
)
type CapacityKind uint8
const (
 NilCapacity CapacityKind = iota
 FiniteCapacity
 UnboundedCapacity
)
type ChannelCapacity struct { Kind CapacityKind; Bound int }
type Link struct { ID int }
type Event struct { Kind Tag; Capacity ChannelCapacity; Ready bool; Link *Link; Items []int }
func Positive(n int) bool { return n > 0 }
`

const coverageSchema = `fn rewrite(arg0: Event) -> int

host
  Positive = Positive

exhaustive type Event = Event
  tag Kind
  Channel (capacity: Capacity, ready: Ready, link: Link, items: Items) = ChannelTag
  Closed = ClosedTag

@Closed
rewrite Closed = 9

`

// An exhaustive input union must be covered by unguarded rules. Missing
// inputs are reported with a witness; inputs covered only under guards or
// host tests are reported as unproven rather than missing.
func TestNonExhaustiveWitnesses(t *testing.T) {
	for _, test := range []struct{ name, rules, want string }{
		{"typed constant", `@Nil
rewrite (Channel (capacity: NilCapacity ())) = 0

@Finite
rewrite (Channel (capacity: FiniteCapacity ())) = 1
`, "non-exhaustive union Event: missing Channel (capacity: ChannelCapacity (Kind: UnboundedCapacity))"},
		{"typed constants are open", `@Nil
rewrite (Channel (capacity: NilCapacity ())) = 0

@Finite
rewrite (Channel (capacity: FiniteCapacity ())) = 1

@Unbounded
rewrite (Channel (capacity: UnboundedCapacity ())) = 2
`, "missing Channel (capacity: ChannelCapacity (Kind: _))"},
		{"Booleans", `@Ready
rewrite (Channel (ready: true)) = 0

@Waiting
rewrite (Channel (ready: false)) = 1
`, ""},
		{"missing Boolean", `@Ready
rewrite (Channel (ready: true, link: nil)) = 0

@Waiting
rewrite (Channel (ready: false)) = 1
`, "missing Channel (ready: true, link: Link())"},
		{"pointers", `@None
rewrite (Channel (link: nil)) = 0

@Some
rewrite (Channel (link: Link ())) = 1
`, ""},
		{"lists", `@Empty
rewrite (Channel (items: [])) = 0

@One
rewrite (Channel (items: [_])) = 1
`, "missing Channel (items: [_, _, .._])"},
		{"list rest", `@Empty
rewrite (Channel (items: [])) = 0

@More
rewrite (Channel (items: [_, ..rest])) = 1
`, ""},
		{"literal", `@Zero
rewrite (Channel (capacity: ChannelCapacity (Bound: 0))) = 0
`, "missing Channel (capacity: ChannelCapacity (Bound: _))"},
		{"guard", `@Positive
rewrite (Channel (capacity: capacity))
  | Positive capacity.Bound
  = 0
`, "non-exhaustive union Event: unproven Channel (guards, host matchers"},
		{"repeated capture", `@Same
rewrite (Channel (capacity: ChannelCapacity (Bound: bound, Kind: _), items: [bound])) = 0

@Empty
rewrite (Channel (items: [])) = 1

@More
rewrite (Channel (items: [_, _, .._])) = 2
`, "unproven Channel (items: [_])"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", coverageHost)
			signatureConfig(t, root, "")
			write(t, root, "input.rules", coverageSchema+test.rules)
			expectUnionCompile(t, root, test.want)
		})
	}
}

// Root dispatch validates the input union and its active union fields, so
// their declared variants are the whole domain. A union reached through a
// view is unvalidated: an undeclared tag still reaches a later fallback.
func TestUnionReachabilityAndCoverage(t *testing.T) {
	for _, test := range []struct{ name, rules, want string }{
		{"nested union", `@Number
rewrite Number = Empty()

@Empty
rewrite Empty = Empty()

@Wrapped
rewrite (Wrapped (atom: AtomNumber)) = Empty()
`, "non-exhaustive union Value: missing Wrapped (atom: AtomEmpty)"},
		{"nested or covers", `@Number
rewrite Number = Empty()

@Empty
rewrite Empty = Empty()

@Wrapped
rewrite (Wrapped (atom: (AtomNumber | AtomEmpty))) = Empty()
`, ""},
		{"fallback after every variant", `@Number
rewrite Number = Empty()

@Rest
rewrite (Empty | Wrapped) = Empty()

@Late
rewrite _ = Empty()
`, "rule Late: unreachable rule: earlier rules match every input"},
		{"unvalidated union stays open", `@Number
rewrite (Value (Atom: AtomNumber)) = Empty()

@Empty
rewrite (Value (Atom: AtomEmpty)) = Empty()

@Rest
rewrite _ = Empty()
`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", unionHost)
			write(t, root, "input.rules", unionSchema+test.rules)
			expectUnionCompile(t, root, test.want)
		})
	}
}

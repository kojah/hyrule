# Tagged union schemas

Schemas make constructor membership, active fields and exclusivity available to
Hyrule. Go provides the initial representation; generated code has no schema
runtime dependency.

```go
type Kind uint8
const (
    NumberKind Kind = iota
    EmptyKind
)
type Value struct {
    Kind Kind
    Number int
}
```

```text
union Value = Value tag Kind {
  Number = NumberKind { value: Number; };
  Empty = EmptyKind {};
};
```

`Value` on the left names the schema. The type, tag, constants and storage field
names on the right resolve in the configured Go package. Field types come from
that storage. Tags must be constants with exactly the tag field's Go type;
duplicate tag values are rejected even when their constant names differ. Schema,
constructor and field duplicates, inaccessible fields and constructor collisions
with declared match/build/guard aliases or reusable patterns are rejected.

A value has exactly one active variant. Other Go fields are inactive and carry
no schema meaning. Constructors set the tag, populate every declared active
field, and leave remaining storage fields zero. Copying an existing whole value
can retain inactive storage. Include provenance and other metadata as explicit
active fields wherever they must survive construction.

## Matching and construction

```text
pattern NumberValue($number) = (Number value:$number);
rule Positive:
  (NumberValue $number)
  && (Positive $number)
  => (Number value:(Increment $number));
rule OtherNumber:
  (Number value:$number)
  => (Number value:$number);
rule Empty:
  (Empty)
  => (Empty);
```

Constructor patterns use the existing positional/named child contracts. Named
children may be omitted, and `(Number)` tests the variant without inspecting any
field. Nested constructor patterns destructure fields whose Go type has another
declared schema. OR alternatives and reusable patterns retain their existing
capture-type and priority contracts. Builders accept positional fields or every
named field exactly once. Nested builders evaluate once, in written order.

A guarded constructor is still eligible for its later unguarded fallback. Root
variant dispatch retains all rules applicable to that variant in original order,
including opaque matchers and catchalls. It does not move partial guards before
their preconditions. Nested variant tests use the same ordered matching graph;
adjacent identical tests may share extraction.

## Representation validation

Go enums do not restrict their possible values. A declared root input schema
therefore rejects undeclared tags before a catchall. Direct active fields with
another declared union type are validated recursively. A union-typed final
result is likewise validated before success is published, including results
returned by ordinary Go builder hooks. An invalid representation yields the
zero result and `false`; it does not become another declared variant or select a
rule for one. Consumers must distinguish this from a successful normal result.

The root tag is inspected once by the matching dispatch. Separate result and
nested-value validators can inspect their own tags. These checks do not prove
application-specific relationships among payload fields. Match/guard hooks must
remain pure, and storage must stay stable throughout matching.

Only local Go struct types, direct fields, and local typed constants are
supported initially. Pointer/interface representations, generated Go type
declarations, qualified imported bindings, pointer traversal and collection
validation are not implemented. Pointer, interface and collection payload fields
are opaque host values: their deeper representation invariants remain an
explicit host responsibility. No solver, memory, lifetime or concurrency
semantics follow from declaring a union.

## Exhaustiveness

Use `exhaustive union` instead of `union` to require input coverage. This is an
explicit requirement because partial recognizers are useful. It applies to the
target's input schema, not to every nested payload schema.

Only an unguarded constructor with unrestricted fields proves that variant
covered. Wildcards and distinct captures are unrestricted; literals, nested
patterns, repeated captures and guards are conservative gaps. An unguarded
whole-input wildcard/capture covers every declared variant. OR and reusable
patterns are expanded before this check, so their proven coverage composes.
Unknown tags are outside the closed domain and never count as a missing variant.

This check does not prove reachability, payload constraints or arbitrary host
guard tautologies. Unsupported proofs produce a missing-variant diagnostic
rather than a false claim of exhaustiveness.

## Portable graph

The matching IR records union, variant, field and type IDs; active field schemas;
exclusive variant-test nodes; root dispatch cases; and constructor field
expression bindings in written order. It contains no Go selectors, tag constant
spellings or `go/types` objects. Those belong to the Go adapter. Hook/expression
IDs still reference frontend-owned tables; Go remains the only implemented
backend.

The example has its own configuration. From `tools/hyrule`, compile/generate it
with `go run ./cmd/hyrule generate --config examples/unions/hyrule.yaml` (under
`agent-test` on Xenia). This does not alter the repository's root target list.

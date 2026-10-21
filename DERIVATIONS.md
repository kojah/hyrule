# Schema-derived representation operations

A complete `record` describes storage once. A `derive` declaration requests
ordinary Go functions whose signatures are named function types in the bindings
file. Derivations can accompany rules, or live in a `schema: true` target with
no matching signature/function. The latter accepts records and derivations only;
it does not emit a dummy rewrite dispatcher. See
[the complete example](examples/derived/snapshot.rules) and
[its host signatures](examples/derived/bindings.go).

```text
record Snapshot = Snapshot {
  backing: Backing;
  cells: Cells;
  guard: Guard;
  label: Label;
};
derive Snapshot {
  freeze FreezeSnapshot = FreezeSignature {
    backing: keep;
    cells: clone;
    guard: keep;
    label: keep;
  };
};
```

Here `FreezeSignature` is `func(Snapshot) Snapshot`. Every operation must account
for every schema field exactly once, using its logical name. A bare `field;`
means `field: keep;`. Unknown, duplicate and omitted policies are errors, including
in unused derivations. Adding a storage field first requires updating the complete
record and then every operation's policy. Read-only `view` schemas cannot derive
representation operations. Function names must be unique within the target.

## Operations

| Operation | Host signature | Field policies |
| --- | --- | --- |
| `freeze` | `func(Record) Record` | `keep`, `borrow`, `clone`, `clone_borrow`, or a typed freeze hook |
| `key` | `func(Record) Key` | `keep`, `omit`, or a typed projection hook |
| `equal` | `func(Record, Record) bool` | Same policies as `key` |
| `visit` | `func(Record, func(Child))` | `keep`, `apply`, `each` |
| `visit` | `func(Record, func(Child) bool) bool` | Same; false stops immediately, true means all visited |
| `transform` | `func(Record, func(Child) Child) Record` | `keep`, `apply`, `each` |
| `display` | `func(Record, int) string` | `keep`, `omit`, or a budget-aware display hook |
| `store` | `func(Record, *Table, int) (ID, bool)` | Complete exact-key policies; omission is forbidden |
| `load` | `func(ID, *Table) (Record, bool)` | Empty block; layout comes from the table type |

Construction, destructuring and explicit record updates already use the same
record definitions. A derivation does not create a new host data type.

`freeze` preserves scalar aggregates by value. Pointer/interface/collection
fields cannot silently use `keep`: explicitly borrow immutable, sufficiently
long-lived storage or supply `func(Field) Field`. `clone` copies a slice of scalar
aggregates; nested references require a deep hook or explicit `clone_borrow`.
The latter owns the list while borrowing its elements under the host's immutable
sharing contract. Neither cloning policy copies reachable pointees. Nil and
non-nil empty lists remain distinct.

`key` requires a complete record schema for the result. Its logical fields match
the included source fields. A projection hook has type `func(Field) KeyField`;
its result must exactly match the corresponding key field type. Key and equality
projections must be safely comparable: interface-containing values are rejected,
including comparable structs whose dynamic interface contents could panic.
`equal` checks included fields in policy order, evaluating each pure projection
once per side. Excluded diagnostic metadata can differ without splitting meaning.
Choosing which fields define meaning remains the author's responsibility.

`visit` and `transform` use one explicit child type. `apply` passes a whole field;
`each` passes slice elements in order. `keep` does not traverse a field.
Transformations allocate fresh selected slice storage and preserve other fields;
callbacks must honor the nested values' sharing contracts. Nested traversal uses
explicit typed callbacks; there is no inferred graph walk or recursion. Generated
visitors may accumulate observations; their callback contract belongs to the
caller. When used inside pure matching, the whole callback must remain pure.

`display` takes a byte budget; nonpositive budgets return an empty string. Output
never exceeds that budget. Exhausted fields do not invoke display hooks. `keep`
formats scalars through their underlying scalar types, bypassing Stringer methods.
An opaque field needs `func(Field, int) string`; the hook must bound its own work
and respect nested budgets. The generator truncates its returned text as well.
Truncation may end within a UTF-8 sequence. This operation bounds output bytes,
not the cost of arbitrary host hooks.

## Exact snapshot storage

`Table` must contain exactly `Index map[Key]ID` and `Values []Record`. `Key` needs a
complete record schema and safe comparable fields; `ID` is an unsigned integer
other than uintptr. Zero is absent, and IDs belong only to their issuing table.
The caller owns the table exclusively and must not mutate its published values,
index or slices. Loading returns the immutable shared snapshot by value.

`store` requires one unambiguous `freeze` derivation for the source record. Its
field policies construct a complete key, freeze the incoming record on insertion,
and retain every evidence/metadata field. The explicit entry limit bounds new
records. Existing records remain available after the limit is reached. Nil table,
entry exhaustion or ID overflow returns zero/false without inserting. `load`
rejects nil tables, zero IDs and IDs beyond the retained vector.

Exact projections must distinguish every difference relevant to retained data,
including nil/empty collection distinctions. Hash digests alone do not establish
exact equality. Host projections are trusted contracts: Hyrule verifies their
signature and field coverage, not their injectivity. Freeze hooks must preserve
represented values. This facility does not replace a domain's physical resource
identity or infer semantic equivalence from source addresses.

Storage functions mutate their explicit caller-owned table. They are separate
from pure matching and must not be invoked from matchers, guards or builders.
Traversal, analysis joins, fixed points, scheduling and source-time evidence
remain host responsibilities.

## Generation

Signature stubs include every derived function before host loading. A handwritten
consumer may call the generated API before its first output exists. Stubs are
never published. Final generated packages are type-checked before publication;
outputs retain the normal ownership checks and 500-line limit.

The shared syntax/compiler own declaration structure, operation kinds, names and
complete field policies. The Go-only `internal/recordgen` backend resolves Go
storage, hook types and function bodies; no Go types enter the shared compiler.
There is no runtime reflection, descriptor registry or Hyrule runtime dependency.

# An indentation-based Hyrule frontend

Status: design proposal, not implemented. The current `.rules` syntax remains
supported and unchanged. This proposal follows the compiler boundary cleanup in
`3db73297`, `fd742ebd` and `9560ec04`.

The aim is to keep Hyrule's expressive power while making common rules look like
patterns and expressions. Keep `do` for dependent sequencing and `<|>` for lazy
ordered fallback, as requested. Use indentation for blocks, parentheses for
compact applications and brackets for literal lists. No braces or semicolons.

## Recommended shape

```text
Fits:
  request @ Request
    header: header @ Header
      length
      capacity
    count
  when length >= 0
  when count >= 0
  end = length + count
  when end >= length
  when end <= capacity
  =>
    request with
      header: header with
        length: end
      outcome: Reuse
      copies: [Copy(offset: length, count: count)]

Decline:
  request
  =>
    request with
      outcome: Refused
      copies: []
```

This is a translation of [the existing append example](examples/localplans/append.rules).
It preserves the negative-input guards, unchecked arithmetic, overflow admission
guard, capacity guard, backing identity, source evidence and non-nil empty list.
The label `Decline` here is a rule name, not the lowercase `decline` expression.

`request @ Request(...)` captures the whole matched value. In an indented named
field block, `length` is shorthand for `length: length`: bind it in a pattern,
read it in construction. Explicit `field: expression` always remains available.
`with` copies an established record and changes only the written fields.

A compact form uses the same expressions:

```text
ImmediateRelease:
  Event(lock: lock, release: true, deferred: false)
  => [Release(lock: lock)]
```

There is no `rule` keyword, no `$` on ordinary bindings/references, no prefix
arithmetic operation and no `effects` wrapper. The meaningful words `when` and
`with` remain visible instead of being replaced by obscure operators.

## Sequencing and ordered choice

Keep `do`; its final expression supplies the result without `pure`:

```text
Main:
  inputs(input, n, host)
  =>
    do
      Proven(reason: first) <- ask(host, n, 0)
        else
          Disproven -> decline
          Unavailable(reason: why) -> outputs(why, 3)
      Proven(reason: second) <- ask(host, host.Offset.Value, first)
        else
          Disproven -> decline
          Unavailable(reason: why) -> outputs(why, 4)
      outputs(mark(host, second), 1)
    <|> do
      true <- ready(host, "ok")
        else false -> decline
      outputs(mark(host, 20), 2)
    <|> outputs(mark(host, 99), 5)
```

This translates the `Main` rule in [the sequencing fixture](sequencing_test.go).
The tuple schema, host aliases and generated signature are declared separately.
There is no function definition, unrestricted recursion or general-purpose
runtime implied by this example.

The execution contract stays the same:

- Matching and guards finish before entering a result continuation.
- Each reached query executes once. Later steps can consume earlier captures.
- A mismatch requires explicit handling of the remaining valid outcomes. It does
  **not** silently become `decline`. A Boolean step can use the compact else arm.
- A bare whole-result binding, `proof <- ask(...)`, consumes the complete result
  and needs no else; Boolean and tuple results retain their exact host types.
- `decline` tries the next alternative in the nearest enclosing `<|>` chain.
  It never tries the next rule and never rolls back already executed queries.
- The final alternative must be a pure total result expression. It cannot
  contain queries or decline. This retains the current final-fallback guarantee.
- Invalid runtime union tags stop the selected continuation conservatively;
  they do not cause ordinary alternative fallback.
- Nested `do`/choice blocks retain lexical capture scopes and local decline.

`<|>` is left-biased choice, not Boolean OR. The `|` operator described below
belongs only to patterns. Neither changes the rules' source-order priority.

## One outcome-decision form

Use `match` in place of `query ... decide` and `query ... select`:

```text
Acquisition:
  Event(lock: lock, release: false, deferred: false)
  =>
    match Exclusive(lock)
      Proven -> [Acquire(lock: lock)]
      Disproven -> [Acquire(lock: lock), Order(lock: lock)]
      Unknown(reason: reason) ->
        [Acquire(lock: lock), Uncertain(lock: lock, reason: reason)]
```

This translates [the acquisition transition](examples/transitions/transitions.rules).
The call is known to be a query from its declaration. `match` evaluates it once
and requires exhaustive outcomes. The initial frontend need only support the
existing query-decision semantics; arbitrary matching of newly computed pure
expressions would be a separate extension, not necessary for this migration.

Selective queries become an explicit step with a declining else:

```text
=>
  do
    (Proven | Disproven) <- Exclusive(lock)
      else Unknown(reason: reason) -> decline
    [Acquire(lock: lock)]
  <|> []
```

The selected alternatives must expose identical capture names and types. The
query still runs once for the whole OR pattern. Named query results can use
`proof <- Exclusive(lock)` followed immediately by `match proof` with exhaustive
arms, lowering to the existing query node plus whole-outcome capture. This is
not a new arbitrary control-flow or delayed-decision mechanism.

## Host declarations: centralize the distinctions

Recommended declaration syntax:

```text
host
  Event? = MatchEvent
  Exclusive! = Exclusive
  mark = mark
  Reuse = Reuse
  Refused = Refused
```

`?` marks an extractor whose last host result is a match Boolean. `!` marks an
ordered query. These marks classify the declaration, not the callable's name:
call `Exclusive(lock)`, not `Exclusive!(lock)`. Unmarked aliases bind a pure
value function or a typed constant, distinguished by the host symbol. A pure
Boolean function can be used in `when`; a separate guard declaration is unnecessary.
No call is classified as pure merely because its name looks harmless.

This eliminates repeated `query` markers in result bodies while preserving the
important purity/effect distinction. Pure calls may construct values but queries
remain forbidden in patterns, preconditions, immutable pre-match locals and pure
fallbacks. Builders remain on the winning path; their allocating results are
not shared. Context and explicit receiver arguments remain ordinary arguments.

Forwarding declarations retain explicit parameters and lexical evidence:

```text
host
  transferred!(instruction, obligation) =
    transfersResource(evidence, instruction, obligation.target)
```

The forwarding expression still permits only captures, field projections,
constants and literals. It cannot hide nested calls, traversal or effects.
Qualified host functions and explicit receiver methods retain their bindings.
Same-name aliases can omit `= Name`, e.g. `Exclusive!` and `Reuse`.

This declaration syntax is the least settled visual choice in the proposal.
The semantics above are required; the `?`/`!` spelling is a recommendation to
review, not an implemented language contract.

## Data schemas and reusable patterns

Use one `type` declaration for product and tagged-union schemas. A `tag` entry
identifies a union; other entries bind constructors and their active storage.
Mappings name existing host fields/tags, not newly inferred memory semantics.

```text
type Header = Header
  backing: Backing
  length: Length
  capacity: Capacity

type Proof = Proof
  tag Kind
  Proven = Proven
  Disproven = Disproven
  Unknown(reason: Reason) = Unknown

exhaustive type Capacity = Capacity
  tag Kind
  Nil = NilCapacity
  Finite(bound: Bound) = FiniteCapacity
  Unknown = UnknownCapacity
```

`exhaustive` retains the existing unconditional input-variant coverage check.
It is distinct from exhaustive handling of a query's outcomes. Ordinary union
schemas continue to support partial recognizers. Same-name storage/tag mappings
can be omitted. Tuple-result schemas use the same form with a tuple host type
and numeric tag/payload positions, e.g. `type Proof = (Kind, int)` and `tag 0`.

Read-only `view` declarations retain their own explicit spelling and pointer/
interface behavior. Native generated signatures retain `signature partial` or
`signature total = outputs(...)`; the latter replaces `total fallback` with an
ordinary fallback expression, without changing when that fallback is invoked.
Legacy signatures and the YAML target configuration remain supported.

```text
pattern FiniteHeader(backing, length, capacity) =
  Header(backing: backing, length: length, capacity: capacity)

pattern SafeKey = ExactEvidence | SafeUnknownEvidence
```

Patterns remain hygienic, typed and bounded; recursive pattern definitions stay
invalid. Constructor patterns can nest, omit named fields, bind their whole
value with `@`, and combine alternatives with `|`. Repeated capture names retain
the existing checked equality semantics; they do not establish aliasing.

Bare names bind in patterns. `^Limit` explicitly matches a declared constant;
the same escape selects a constant in expressions when a local shadows it.
Declared constructors are recognized by declaration, not capitalization. A
nullary host *function* uses `Name()`; it is not guessed from a bare identifier.
Keywords are contextual at field labels. Backtick-escaped identifiers preserve
otherwise ambiguous host/capture names, so dropping `$` does not lose naming
capabilities.

## Schema-derived helpers

The derivation operation and field-policy vocabulary describes different
semantics, so it stays explicit. Only the block punctuation changes:

```text
derive Snapshot
  freeze FreezeSnapshot: FreezeSignature
    backing: keep
    cells: clone
    guard: keep
    label: keep
  key SnapshotKey: KeySignature
    backing: keep
    cells: encodeCells
    guard: omit
    label: omit
  load LoadSnapshot: LoadSignature
```

This is a selected portion of [the snapshot schema](examples/derived/snapshot.rules).
Equality, visitors (including Boolean short-circuit visitors), transforms,
bounded display and exact store/load use the same layout. Keep/borrow,
clone/clone_borrow, typed hook, omit, apply/each and parameter-projection policies
remain available. Parameter projections use the parameter name with an explicit
`@` prefix in this policy-only context, replacing the old `$parameter` spelling.
All existing ownership, nil/empty, budget, ID and completeness checks remain.

## Layout and expression rules

- Indentation is structural; dedent ends a block. Use spaces, with a canonical
  formatter choosing two spaces. Reject mixed indentation.
- A rule is a label, one input pattern, ordered locals/guards, and `=>` followed
  by one result expression. Pure local bindings use `name = expression` in the
  existing pre-match-clause positions; this proposal does not add arbitrary
  imperative statements or assignment to existing names.
- An indented constructor/update block contains named fields. Parentheses are
  the compact positional/named application form. Mixing named and positional
  arguments in a single application remains an error where it is today.
- Lists use `[...]`, including multiline lists. They remain statically bounded
  effect/value bundles; no comprehensions, dynamic matching or implicit loops.
- Infix `+ - * / %` lowers to the existing unchecked integer operations. Keep
  host overflow and division semantics. Guards admit bounds explicitly.
- Existing comparison operators remain typed. Repeated `when` clauses preserve
  evaluation order. Guard `&&` can desugar to those ordered clauses; this does
  not introduce general Boolean OR or new matching backtracking behavior.
- `do` contains dependent query bindings and a terminal result expression.
  Indented else arms exit that block, as today. Nested result choices are
  grouped by `do` and indentation; parentheses remain available when needed.
- `<|>` has lower precedence than calls/arithmetic and associates in written
  order. At the result expression's indentation, it combines complete blocks.
- Comments and source locations remain available for formatting and diagnostics.
  Lexical file ordering, rule priority and expansion limits remain deterministic.

## Expressiveness checklist

| Current responsibility | Proposed representation |
|---|---|
| Ordered rules / guards / guard failure | Labels, patterns, ordered `when`, `=>` |
| Captures / whole captures / ignored inputs / constants | Bare bindings, `@`, `_`, `^Constant` |
| Named and positional nested matchers/builders | Calls or indented named-field blocks |
| OR and reusable patterns | `|` and `pattern` |
| Typed constants / pure builders / guards | Unmarked `host` aliases; `when` gives Boolean context |
| Matcher failure | `host` extractor declaration with `?` |
| Tagged unions / products / read-only views | `type`, `tag`, `view`, explicit host mappings |
| Coverage checks / invalid runtime tags | Retained contracts; `exhaustive` where requested |
| Locals / comparisons / unchecked arithmetic | `name = expr`, infix expressions, ordered guards |
| Typed lists / effect bundles / record updates | Lists and `with` |
| Committed queries / exhaustive outcome decisions | `!` declaration and `match` |
| Named, Boolean and tuple query results | `<-`, typed outcome schemas, literal/constructor arms |
| Dependent sequencing / early outcome answers | Indented `do` with explicit `else` arms |
| Selective query / OR selections | Pattern bind plus explicit declining else |
| Lazy alternatives / nested decline | `<|>` and `decline`, unchanged lexical semantics |
| Field projection / host forwarding / receiver methods | Ordinary field paths and `host` aliases/forwarders |
| Legacy and native partial/total APIs | Existing YAML/signatures; compact total fallback |
| All schema derivations and policy types | Indented `derive` operations and policy entries |
| Deterministic formatting / bounded expansion / source errors | New layout parser; existing limits and source metadata |

The table is a design-level constructive mapping, not evidence that a new parser
already exists. No current feature is intentionally removed. No unrestricted
recursion, general-purpose mutation or new runtime is proposed.

## Migration evidence required before replacement

Implement the new frontend against the semantic model; keep matching/value/helper
plans and emitters as the execution boundary. Translate the complete configured
rule inventory, not just these examples. Reuse public compiler and generated-code
behavior tests for precedence, purity boundaries, query call order, decline,
invalid tags, named evaluation order, native interfaces and derivation ownership.

Round-trip formatting, diagnostics and limits need their own new-syntax fixtures.
All production/example targets must generate and type-check. Compare translated
behavior against the current syntax before deleting the old parser; successful
parsing or visually pleasing examples alone do not establish equivalence. The
current runtime and host contracts are the reference, not the new spelling.

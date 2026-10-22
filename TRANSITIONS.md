# Queries and transition plans

Matching and guards finish before a rule commits its result. The result may issue
ordered host queries and construct an effect plan; the caller applies that plan.
Queries are not matching predicates and are never hoisted, merged, repeated or
skipped once their occurrence is reached.

## Exhaustive outcome decisions

```text
host
  Exclusive! = ProveExclusive

type Proof = Proof
  tag Kind
  Proven = ProvenKind
  Disproven = DisprovenKind
  Unknown (reason: Reason) = UnknownKind

acquire (AcquireEvent (lock: lock)) =
  case Exclusive lock of
    Proven -> [Acquire lock]
    Disproven -> [Acquire lock, RecordOrder lock]
    Unknown (reason: reason) -> [Acquire lock, Uncertain lock reason]
```

The constructors/builders in this illustration need host declarations. The
[transition example](examples/transitions/transitions.rules) is complete.

A case invokes its query exactly once, then handles the declared tag. Every valid
variant must occur exactly once. Payloads can bind fresh captures or wildcards;
refinements would undermine exhaustiveness and are rejected. Boolean queries
use `true` and `false`; tuple queries use a declared tuple schema. Invalid runtime
tags, including invalid nested declared value schemas, stop conservatively before
any outcome body runs. They never retry a matching rule.

Case scrutinees are query applications, or the whole result immediately bound
by a preceding query. Arbitrary pure-value case analysis is not supported.
Query arguments are positional captures, projections, constants or literals;
nested builders/queries cannot hide in an argument. Fixed-arity, non-generic
host functions and explicit receiver methods are supported.

For a named result:

```text
classify request =
  do
    proof <- Exclusive request.Lock
    case proof of
      Proven -> proof
      Disproven -> proof
      Unknown (reason: reason) -> conservative reason
```

This retains the whole result without a second query. Names cannot shadow an
existing capture. Each arm has its own payload scope; an unselected arm never
executes. All arms are type-checked, including unreachable ones.

## Sequential queries with do

```text
resolve request =
  do
    Proven (reason: first) <- ask request
      else
        Disproven -> missing request
        Unknown (reason: why) -> uncertain why
    true <- ready request first
      else false -> missing request
    answer first
```

Each bind evaluates its query, selects the success pattern and either continues
or returns an explicit else answer. Later steps see earlier captures. Else arms
cannot see payload captures from the successful outcome. The final expression
supplies the block's answer; no `pure` keyword is required.

A whole-result bind, `proof <- ask request`, introduces the exact host result and
needs no else. It works for opaque results as well as declared unions and Booleans.
If a declared schema applies, its representation is still checked even when the
capture is unused. Whole-result binds cannot have else branches.

Boolean pattern binds also accept `true <- ready request` followed by
`else missing request`; this handles the complementary Boolean. Tagged outcomes
require explicit arms. Missing outcomes, duplicates, shadowing and refinements
are errors. Sequencing depth is bounded; `do` is not an unrestricted statement
language.

## Lazy ordered alternatives

```text
resolve request =
  do
    (Proven (reason: why) | Unknown (reason: why)) <- ask request
      else Disproven -> decline
    answer why
  <|> case secondary request of
    true -> fallbackAnswer request
    false -> decline
  <|> unavailable request
```

Alternatives run left to right after the equation commits. The first answer wins,
including false, empty or zero answers. Only explicit `decline` tries the next
strategy. Effects and consumed budgets are not rolled back. Invalid runtime tags
stop conservatively instead of declining. A decline never retries another rule.

The final alternative must be a pure total result expression, without queries,
`do` or decline. Earlier alternatives are `do` blocks or exhaustive query cases.
An OR outcome pattern runs one query and shares a continuation: all alternatives
must establish the same capture names and types. Pattern matching OR, equation
guard bars and result `<|>` are separate operations.

Choices can nest inside outcome arms. Decline targets the nearest enclosing
choice, and captures remain lexical. A nested choice's answer ends its containing
arm. A standalone decline without an enclosing choice is rejected.

## Typed effect lists

`[Acquire lock, RecordOrder lock]` constructs a typed slice. Elements execute once
in written order, including inside named builder arguments and record updates.
The expected slice type comes from the generated result, record field, builder
parameter or typed enclosing list. Invalid declared element representations stop
before later element builders execute. `[]` means non-nil empty, distinct from nil.

List construction does not apply effects. An action union can express complete
plans while the host retains mutation, lifecycle, storage, work bounds and graph
traversal. There is no dynamic collection loop or runtime matching engine.

The value IR records typed query arguments, active outcomes, payload captures,
lexical choices and decline. It contains no source-language syntax. The emitter
uses ordinary calls, switches and local variables. Empty declining outcomes need
no emitted switch body after representation and exhaustiveness checks succeed.

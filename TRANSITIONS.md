# Queries and transition plans

Hyrule separates pure recognition from committed computation. A rule first
matches its input and evaluates its pure guards. Its selected result may then
issue ordered host queries, handle their outcomes, and construct a typed effect
list. The host applies the returned list after the generated function returns.

This extends construction at the terminal leaves of the existing decision graph.
It does not introduce a second matcher or change pure-prefix sharing. A query is
never a matching predicate: the optimizer cannot hoist, merge, repeat, or skip a
reached query occurrence. Existing rules generate the same code.

## Syntax

```text
query Exclusive = ProveExclusive;
union Proof = Proof tag Kind {
  Proven = ProvenKind {};
  Disproven = DisprovenKind {};
  Unknown = UnknownKind { reason: Reason; };
};

rule Acquire:
  (AcquireEvent lock:$lock)
  => query $proof = (Exclusive $lock)
     decide $proof {
       (Proven) => effects [(Acquire $lock)];
       (Disproven) => effects [(Acquire $lock), (RecordOrder $lock)];
       (Unknown reason:$reason)
         => effects [(Acquire $lock), (Uncertain $lock $reason)];
     };
```

`Acquire`, `RecordOrder`, and `Uncertain` can be ordinary builder bindings or
constructors of a declared effect union. This illustration requires those host
bindings; the complete executable example is `examples/transitions`.

## Ordered query contract

A `query` declaration binds a Go function or host method with exactly one result
and fixed, non-generic parameters. Its result must be a declared union. Query
arguments are positional captures or literals, checked against the Go parameter
types. Builders and other queries cannot be hidden inside an argument.

A query is a rule result or an outcome arm's result. It executes exactly once
when that occurrence is reached, after all preceding matching and guards. The
host may spend a counted budget, fill caches, or record query evidence. The
query must preserve the meaning of captured input values; it must not mutate
source IR behind captures or reenter the generated planner. Hyrule cannot
verify these host obligations.

The immediately following `decide` must name the query's fresh capture. Each
arm handles one declared constructor. A branch may issue another query; only
the selected branch executes. This provides explicit sequential, conditional
proof searches without eagerly evaluating unused questions.

Queries commit to the selected rule. There is no failure Boolean and no retry
of another rule or another OR alternative after a query. An unsuccessful proof
is a data outcome, not matching failure. Unknown outcomes can request further
queries explicitly or return uncertainty. Budget charges are not rolled back.
An invalid runtime union tag or invalid active nested union returns zero/false
from the generated function immediately; it never reaches a fallback rule.

Keeping queries in committed continuations is intentional. Allowing effectful
predicates in the matching DAG would entangle speculative matching, shared
prefixes, retries, and budget consumption. A future fallthrough feature needs a
separate execution contract rather than silently weakening pure guard semantics.

## Exhaustive decisions

Each decision must include every variant exactly once, regardless of whether
the union declaration uses `exhaustive`. That modifier still checks coverage of
the target input; query coverage is mandatory independently.

Arms may select named fields or positional fields, using fresh captures or
wildcards. A zero-argument constructor ignores all its fields. Literal tests,
nested patterns, OR patterns, catchalls, guards, repeated captures, and capture
shadowing are rejected in arms: they could leave a variant partially handled or
conceal a newly added outcome. Payload captures are scoped to their own arm.
Nested decisions inherit outer captures but cannot redefine them.

Outcome names have no built-in semantic meaning. The host chooses its union;
Hyrule checks total handling, not the truth or soundness of each policy branch.
Unknown, disproven, unavailable, and cutoff remain distinct only when the host
represents them distinctly. Adding a variant requires updating every decision.

## Effect-list construction

`effects [expression, ...]` constructs the expected Go slice type, including a
named slice. The expected type comes from the target output or a builder or union
constructor parameter. Lists may nest when their element type is itself a slice.
Empty lists are non-nil empty slices; `nil` remains available as a separate value.
Arrays and non-slice contexts are rejected.

Elements execute exactly once in written order. Each is assigned to the slice's
element type, retaining Go checks for assignability, nil, and constant overflow.
Declared union elements are checked for valid tags and active nested unions.
Only the selected outcome constructs effects. Named builder arguments preserve
written evaluation order even when their parameter positions differ.

Effects are ordinary host values. The generator does not execute them, mutate a
lock table, infer rollback, or guarantee atomic application. It does not undo
query side effects if a later query returns an invalid tag. Builders must obey
the existing construction contract: produce complete results and avoid mutation
of input state. No special collection, runtime interpreter, or Hyrule runtime
package is introduced.

## Validation and scope

Public compiler tests cover ordered queries, nested short-circuiting, unknown
payloads, skipped queries, invalid-tag termination, exhaustive diagnostics,
scoping, typed effects, formatting, publication checks, and legacy rules.
The lock example demonstrates acquisition/release registration and uncertainty;
it is not production integration into Lockdoc or GoHawk.

The existing input, token, nesting, OR-expansion, and generated-file limits apply.
Large decision trees should be split at meaningful host boundaries. Queries do
not add traversal, fixed-point iteration, collect-all matching, or transactional
state updates.

## Optional outer parentheses

Query calls and decision arms also accept the boundary-delimited application
syntax described in [optional application parentheses](APPLICATIONS.md).
For example, `query $proof = Exclusive $lock decide $proof { ... }` and
`Proven => Acquire $lock;` retain the same ordering and exhaustive handling as
their parenthesized forms. Nested applications still require parentheses.

## Anonymous query decisions

When the result needs no name, combine the query and its immediate decision:

```text
query Exclusive;

rule Acquire:
  AcquireEvent lock:$lock
  => query Exclusive $lock {
       Proven => effects [(Acquire $lock)];
       Disproven => effects [(Acquire $lock), (RecordOrder $lock)];
       Unknown reason:$why => effects [(Uncertain $lock $why)];
     };
```

This is the anonymous form of `query $proof = Exclusive $lock decide $proof`.
The `query` keyword remains at the execution site: the operation is stateful,
executes exactly once when reached, and never participates in speculative
matching or guard fallthrough. Every declared outcome still needs an arm.
Invalid runtime tags still return zero/false without trying a fallback rule.

The shorthand introduces no source-level result capture or synthetic name.
Payload captures are scoped to their arm, enclosing captures remain available,
and nested anonymous queries do not shadow one another. Use the named form
when an arm needs the complete query result as well as its payload. Both forms
accept optional outer call parentheses, and formatting preserves the spelling.

## Native host results

Queries also accept native Boolean and tuple outputs. See
[native host interfaces](HOST_INTERFACES.md) for their exhaustive decisions,
forwarded field arguments, and explicit failure contracts for total APIs.

## Lazy ordered alternatives

Use `<|>` when each question may supply an answer and other valid outcomes
should continue with the next question:

```text
rule Classify:
  inputs $instruction $classifier
  => query recognized $classifier $instruction
       select Decided label:$label => outputs $label
     <|> query ownerHolds $classifier $instruction
       select true => outputs (NoAction reasonNone)
     <|> pure outputs (Opaque reasonUnknown);
```

A query alternative is an explicit `query` followed by `select Pattern => result`.
A non-final alternative may also be a `do` block (described below).
The pattern selects a complete union variant or Boolean literal. A top-level
OR such as `select (or (Proven reason:$why) (Unavailable reason:$why)) => result`
shares one result between complete variants. A named selection,
`query $proof = ask $x select Proven => $proof`, retains the complete typed
query result in the selected answer; that capture cannot escape the alternative.
This also forwards native tuples without rebuilding their fields. Every OR
alternative must bind the same capture names with identical Go types; payload fields may have different names
or positions. The query still executes exactly once. Duplicate variants, nested
patterns and payload refinements remain invalid. Boolean OR can select both
literals. Payloads allow fresh captures or wildcards, under the same restrictions as exhaustive
decision arms. Other valid outcomes mean no answer from this alternative.
Adding a variant to the declared domain does not require extending a selection;
use an exhaustive query decision when every outcome needs explicit handling.

Alternatives execute lazily in written order, once per reached occurrence. The
first selected outcome supplies the result and skips all later queries and
construction. Query state and spent budgets persist when an outcome is not
selected. This is local continuation within the committed rule, never retry of
another rule or speculative pattern alternative. An invalid tag or invalid
active nested payload stops immediately, even for an unselected outcome.

The final alternative must be `pure result`, which supplies the default answer
without running queries. It may call ordinary builders, with the same purity
contract as any construction. Every selected result and the fallback must have
the expected result type. Captures belong only to their selected result: they
cannot escape into later alternatives or the fallback. Native `outputs` and
typed effect lists work in each answer.

Chains are available as rule results and inside ordinary exhaustive decision
arms. A selection requires a chain with a final `pure` fallback. Ordinary query
decisions retain mandatory exhaustive coverage. The compiler emits direct Go
branches and locals; it adds no optional-result adapters or runtime dependency.

## Sequential queries with `do`

A `do` block writes a dependent query pipeline top-to-bottom. Each binding names
one complete outcome that continues the block; its `else` arms handle every
remaining outcome. The block ends with a `pure` result:

```text
rule Read:
  inputs $request $host
  => do {
       Found node:$node <- query FindNode $host $request
         else {
           Missing => outputs Unavailable;
         };
       Exact value:$value <- query ReadNode $host $node
         else {
           Incomplete => outputs Unavailable;
           Interfered => outputs Interfered;
         };
       pure outputs $value;
     };
```

The hook and variant names above are illustrative; bindings must declare their
actual host signatures and outcome domains. Query arguments remain positional
captures, field projections, constants or literals. Parentheses around calls and
outcome patterns remain optional. Payload captures from a successful binding are
available in subsequent steps and the final result, but not in that binding's
`else` arms. Earlier captures are available in later `else` arms. Shadowing is
rejected. An `else` arm produces the block's result immediately: later steps and
the final construction do not execute.

Each step uses the existing exhaustive query contract. For Boolean queries,
select `true` or `false` and handle the other literal. Tagged outcomes and native
tuple outcomes work identically to ordinary decisions. No variant is implicitly
regarded as success or failure. If the selected variant is the whole domain,
`else` can be omitted. Otherwise missing arms are errors. OR/refined patterns and
whole-result variable binds are not supported in steps; use explicit decisions
when multiple outcomes continue differently. A block may contain only its final
`pure` result. `pure` cannot contain queries, `do`, or `decline`.

### Composing sequencing with alternatives

A block can be a non-final operand of `<|>`. Use `decline` in an outcome arm to
abandon that strategy and continue with the next alternative:

```text
=> do {
     Found node:$node <- query FindNode $host $request
       else { Missing => decline; };
     Exact value:$value <- query ReadNode $host $node
       else {
         Incomplete => decline;
         Interfered => outputs Interfered;
       };
     pure outputs $value;
   }
   <|> query ReadSummary $host $request
         select Exact value:$value => outputs $value
   <|> pure outputs Unavailable
```

Only `decline` requests fallback. An ordinary result from an `else` arm is an
answer and skips the remaining alternatives, just like the final `pure` result.
This lets a procedure distinguish an unavailable strategy from interference or
an exhausted budget that must stop the procedure. Choosing which outcomes may
fall back is an explicit domain policy.

`decline` is legal only within an enclosing ordered alternative, including its
nested query decisions or `do` blocks. It targets the nearest enclosing choice;
an inner `<|>` has its own required final `pure` fallback. Captures cannot escape
their block or alternative. Standalone `decline` has no meaning and is rejected.

Reached queries execute once, in source order. Declining does not undo queries,
refund budgets, restore caches, or retry another rule. Invalid tags and invalid
active nested payloads still terminate conservatively, without trying later
steps, alternatives or rules. The host applies returned effects afterward.

The parser lowers each step into the same exhaustive query continuation used by
nested `query`/`decide`, retaining a wrapper for formatting. There is no new
runtime or loop construct, and query work never moves into the pure matching
DAG. Sequence length consumes the existing expression-nesting budget, preventing
a flat block from bypassing compiler depth limits. Public compiler tests in
`sequencing_test.go` exercise execution, dependent captures, explicit early
results, fallback, invalid outcomes, formatting and publication.

# Hyrule

Hyrule compiles ordered structural rules into ordinary typed Go calls. It lives
under `tools/hyrule` and has no Lockdoc analyzer dependencies. Generated code
has no Hyrule runtime dependency. The pipeline example demonstrates both lifting and lowering. Production rules
live under `internal/lower/memory` and `internal/lower/scalarop`: shared loads/stores, field/index projections
stay in that package; `mapop`, `interfaceop`, `channelop` and `sliceop` own their typed family operations and original-source projections.
The slicetransfer family lowers append/copy operations into complete exact, empty or conservative transfer plans consumed by symbolic summaries and native graph emission. Its clear rules choose empty, exact-span or possible-zero state effects; native emission applies those typed element writes without changing the slice header.
Interface assertion lowering also selects retained-tag, copied-payload, typed-zero, panic-only or unsupported outcomes, shared by native graph emitters and public reads.
The sliceheader family selects correlated nil/make/view/retype and append reuse/growth transitions, preserving explicitly partial capacity and possible bounds panics. Geometry and graph evidence remain host-owned.
The mapstate family selects finite map write, clear, delete and paired lookup state transitions. Generic Go applies immutable entry edits; equality, provenance, graph traversal and evidence admission remain host responsibilities.
The scalarop family recognizes original scalar literals, arithmetic, conversions, loads and input/call/tuple-result/phi boundaries. The bounded integer evaluator and native constant-control evaluator consume these descriptors; arithmetic, actual argument admission, work bounds and memory interpretation remain host policy.
Matchers expose SSA structure and static types. Container state, identity,
temporal interpretation and execution selection remain with their consumers.

```sh
go run ./tools/hyrule/cmd/hyrule check
go run ./tools/hyrule/cmd/hyrule generate
go run ./tools/hyrule/cmd/hyrule generate --check
go run ./tools/hyrule/cmd/hyrule fmt --write tools/hyrule/examples/pipeline/lift.rules
```

`check` validates syntax, captures, hooks and generated Go types without writing.
`generate` performs the same validation and publishes outputs. `generate --check`
compares outputs and ownership metadata without modifying them. Both accept
`--config PATH`, `--target NAME`, and `--format text|json`. JSON reports `ok`,
`error` and the number of compiled `targets`; errors return a nonzero exit status.

`fmt FILE` writes canonical rules to stdout. `fmt --write FILES...` replaces files;
`fmt --check FILES...` rejects noncanonical files without writing. Comments remain,
but comments inside expressions move ahead of their declaration. Formatting
checks syntax; it does not load Go packages.

## Rules

```text
match Extract = MatchExtract;
match Lookup = MatchLookup;
guard Ready = IsReady;
build Payload = BuildPayload;

rule LookupPayload:
  (Extract $lookup:(Lookup _ _ true) 0)
  && (Ready $lookup)
  => (Payload $lookup);
```

Declarations bind DSL operation names to Go functions or methods on the optional
host argument. Rule names are stable diagnostic identities, unique per target.
When the DSL and Go names are identical, omit the alias: `match lockCall;`
means `match lockCall = lockCall;`. The same shorthand works for `build`,
`guard`, `query`, and `const`. Names are exact and case-sensitive; no naming
conversion is inferred. Several exact-name bindings of the same kind can share
a declaration: `match lockCall, unlockCall, readLockCall;`. This also works for
`build`, `guard`, `query`, and `const`. Explicit aliases use separate declarations.
Formatting preserves groups and either spelling; each name receives the same
Go type validation and duplicate-name checks as a separate declaration.
`$name` binds a pattern value and references it on the right; `$name:(Op ...)`
binds the entire input to that subpattern. `_` ignores a pattern value.
Parenthesized calls nest. Literals use Go spelling for numbers, strings, raw
strings, `true`, `false`, and `nil`; negative numeric literals are supported.
A rule may have multiple guard calls, each introduced by `&&`. Their preconditions and short-circuit meaning are preserved: a false guard falls through without constructing that rule's result. Matcher and guard invocation counts are not a language guarantee.
Declarations and rules end with `;`. Line and block comments are accepted.

Matcher children can be selected by the names of the Go matcher's result
parameters (excluding its final success Boolean):

```text
(Write request:$request allocated:true unknownMembership:false resident:true)
```

Names are case-sensitive. Omitted named children act as wildcards. Named and
positional children cannot be mixed in one call; unknown and duplicate names
are errors. Positional patterns keep their existing arity contract. Named
children are matched in written order, independent of the Go return order.

Builder arguments can also use the names of the Go builder's input parameters:

```text
=> (SliceHeader backing:$backing offset:$start length:$len capacity:$cap)
```

Names are exact and case-sensitive. Every parameter must be supplied once;
missing, duplicate, unknown names and mixed named/positional arguments are
errors. Named calls require usable Go parameter names (neither unnamed nor `_`).
Positional calls remain supported. Nested builders execute once in written
order, even when arguments are rearranged into Go parameter order; typed
assignment checks still apply. The optional host receiver is implicit and is
not a builder argument. Guards continue to use positional arguments.

Use `(or PATTERN PATTERN ...)` for ordered alternatives in match patterns:

```text
(Delete request:$request allocated:false
  evidence:(or (ExactEvidence) (SafeUnknownEvidence)))
```

OR-patterns may nest. Every alternative must bind the same new capture names
with identical Go types; captures from the surrounding pattern remain available.
Each expanded alternative tries its match and the rule's ordered guards before
the next alternative. A false guard can therefore fall through to another
alternative of that rule. Builders run only for a successful alternative.
Expansion is bounded to 64 variants per rule; oversized patterns are rejected
before constructing their expansion. OR-patterns are not build expressions.
Reusable match patterns give repeated shapes a name with explicit parameters:

```text
pattern FiniteWrite($request, $resident, $originKnown) =
  (Write request:$request allocated:true unknownMembership:false
    resident:$resident newOriginKnown:$originKnown evidence:(ExactEvidence));
pattern SafeKey() = (or (ExactEvidence) (SafeUnknownEvidence));

rule ReplaceKnown:
  (FiniteWrite $request true true)
  && (WithinBound $request)
  => (ReplaceKnown $request);
```

Every capture in a definition must be a declared parameter; expansion cannot
introduce hidden captures. Arguments are match expressions, including captures,
literals, wildcards and nested patterns. A parameter used as a whole-input capture
(`$value:(...)`) accepts a capture or `_`. Repeated arguments preserve ordinary
capture equality checks. Named matcher fields, nested destructuring and OR
patterns keep their existing contracts after expansion.

Definitions are scoped to a target, may reference later declarations or other
configured rule files, and do not affect rule precedence. Names cannot collide
with hook names. Definitions have exact arity and are match-only; free captures,
unknown references, duplicate or unused parameters and recursive definitions
are rejected, including unused definitions. Expansion is bounded to depth 64 and 10,000 match nodes per rule before OR
expansion; the 64-variant OR bound still applies. Reuse introduces no runtime
registry, graph traversal or fixed-point evaluation.

Files are sorted lexically; rules retain source order within each file. The first
successful rule wins. Unknown operations, failed matchers and false guards fall
through; exhausting the rules returns the zero output and `false`. There is no
implicit traversal or fixed-point iteration.

Repeated captures compare concrete comparable values using Go equality. Types
containing interfaces are rejected because their dynamic values could make `==`
panic. Use distinct captures and a typed equality guard for those cases. Matching
does not infer structural equality, aliasing, pointer-analysis facts or semantics.

## Queries and effect lists

A selected rule can run an explicitly effectful `query` and exhaustively
`decide` its tagged outcome. Queries run exactly once when reached, after pure
matching succeeds; they never fall through to another rule. Outcome branches
can issue further queries without evaluating unselected branches. Pure matchers
and guards retain their existing optimization contract.

`effects [...]` constructs a typed slice in written order, either as the target
result or a builder argument. The host applies the resulting plan afterward.
See [queries and transition plans](TRANSITIONS.md) for syntax, execution and
exhaustiveness rules, and `examples/transitions` for a runnable lock-state plan.

## Tagged unions

A schema declares exclusive constructors over existing Go struct storage:

```text
exhaustive union Value = Value tag Kind {
  Number = NumberKind { value: Number; };
  Empty = EmptyKind {};
};

rule Number: (Number value:$number) => (Number value:$number);
rule Empty: (Empty) => (Empty);
```

Constructors support named-field and nested patterns, OR alternatives and reusable
patterns. They also build tagged values directly; no matcher or builder hook is
required for a declared constructor. A zero-argument constructor pattern tests
its variant and ignores its fields; construction still requires every field.

A variant may omit a same-name Go tag alias: `Lock { resource: Resource; };`
means `Lock = Lock { resource: Resource; };`. Names are exact and case-sensitive.
Each variant remains a separate declaration; explicit aliases can be mixed with
shorthand variants, and formatting preserves both forms.

Record and union fields may likewise omit a same-name storage mapping:
`start;` means `start: start;`. Explicit mappings such as `resource: Resource;`
remain available. Field names are exact and case-sensitive, and each field still
ends with `;`. This shorthand changes schema declarations only, not named
arguments in patterns, constructors, or record updates.

Ordinary `union` declarations allow partial recognizers. `exhaustive union`
requests a conservative coverage check for the target input: guarded or refined
patterns do not discharge a variant, while an unguarded whole-input fallback
covers all declared variants. Missing coverage is a compilation diagnostic.

The initial binding uses a local Go struct, a direct integer/string/Boolean tag
field, distinct same-typed Go constants, and direct active fields. Undeclared
input or output tags return `false`, including before a catchall; this is a
checked closed-domain API, not an assertion that Go prevents invalid enum values.
The root discriminator is read once for variant dispatch. Go still owns storage.
See [union contracts and limitations](UNIONS.md) and the
[union example](examples/unions/rewrite.rules).

## Go host contract

Declare a named function type directly in the configured binding file:

```go
type LiftSignature func(Source, *Host) (Operation, bool)
type LowerSignature func(Operation) (Plan, bool)
```

The first parameter is the input; an optional second parameter provides context.
Input and output types can differ. Results must be `(Output, bool)`. Signature
imports whose package name differs from the import path's last component need an
explicit alias. Dot imports are unsupported in binding files.

Hook contracts:

| Binding | Go signature |
| --- | --- |
| `match Op = MatchOp;` | `func(Input) (Child1, Child2, ..., bool)` |
| `guard Fact = HasFact;` | `func(Capture1, ...) bool` |
| `build Result = BuildResult;` | `func(Argument1, ...) Output` |

Hooks may be package functions or methods of the host. Ambiguous names are errors.
Generic and variadic hook functions are unsupported. Builders return one value;
use a struct or slice for tuples, multiple effects, provenance and failure data.
A guard decides whether a rule applies; a builder must produce its result once
that guard succeeds. Nested replacement calls use `build` bindings.

Matchers and guards **must be pure**: they must not mutate inputs, the host or
externally observable state, and identical arguments against the same host
snapshot must produce identical results. Analysis facts must remain stable for
the whole matching invocation. Hyrule may skip, repeat or share these calls;
call counts and evaluation order are not guaranteed. This is a language contract,
not a property Hyrule can prove about arbitrary Go callbacks. Guard preconditions
still apply: the compiler preserves ordered guard fallthrough and does not hoist
partial guards before their required evidence.

Builders construct complete results and may allocate fresh objects. Their
callbacks must likewise leave inputs, host state and externally observable state
unchanged. Results are never cached or shared; replacement builders stay on the winning
rule's execution path. The caller applies returned effects after matching.
Host code owns memory identity, soundness, unknown-value policy, lifecycle,
control flow, budgets and scheduling.

The shared compiler expands reusable patterns and OR alternatives while retaining
source priority. The matching IR uses opaque compiler-owned hook, type, value,
literal and expression IDs; its optimizer and decision edges contain no Go
syntax or `go/types` objects. The Go adapter discovers signatures, checks typed
captures, maps IDs to Go types/functions, emits branches and formats imports.
Go is the only implemented backend. A future backend can consume the matching
IR with its own hook/type tables; a universal bindings language is not provided.

The compiler folds adjacent identical pure matcher/test prefixes into an ordered
decision DAG. Match failure and exhausted guard alternatives rejoin explicit
fallback edges; builders remain separate leaves. It does not reorder unrelated
rules or infer exclusivity for opaque hooks. Declared unions add exclusive
variant/field knowledge to the portable IR and permit checked variant switches.
Generated functions use typed locals and direct branches, with no runtime graph,
term boxing, reflection or runtime capture maps. Unreachable declared rules
remain type-checked. See [decision-graph measurements](DECISION_GRAPH.md) for
reproduction commands, baseline scope and limitations.

## Configuration and ownership

See the repository's `hyrule.yaml`. Version 1 requires explicit rules paths,
package directory, bindings file, signature, generated function name and output
path. All paths stay inside the configuration directory; no globs or includes.
`targets: []` retires all previously owned outputs. Each output must be a `.go` file directly inside its target package. The Go toolchain
and module dependencies must be available to type-check the host.

Hyrule loads hosts with in-memory signature stubs, then checks the rendered output.
This supports a host that calls its generated function before first generation,
and regeneration after changing a signature. Stubs are never written to disk.
All configured hosts load even when selecting one target; only selected rules and
outputs are compiled. Checks do not run host code or tests.

`.hyrule-manifest.json` records generated paths and hashes. Commit it with rules,
bindings and generated outputs. Renamed/removed target outputs are deleted only
if their content still matches the recorded hash. Edited obsolete files are an
error. Hyrule refuses to overwrite handwritten files or output symlinks. Current
outputs bearing the Hyrule header may be regenerated. Keep one configuration per
directory and do not publish concurrently. Each file is replaced atomically;
multi-file publication is not transactional. Validation precedes all writes.

Rule inputs are bounded to 4 MiB, 100,000 tokens and nesting depth 64. Generated
files are limited to 500 lines; split larger targets into separately called passes.
Rule comments and output bytes contain no timestamps or machine-specific paths.

## Workflow and extraction

`make hyrule-check`, `make hyrule-generate`, and `make hyrule-generated-check`
use the repository's resource-bounded launcher. `make generate` runs Hyrule before
API references and diagrams. `make generated-check` includes its freshness check,
after CI prepares local outputs with `make generate-go`. Generated Go and
ownership manifests are ignored build artifacts. `make build` and canonical
validation targets regenerate them; run `make generate-go` before raw Go commands
in a fresh checkout. Hyrule generation also prepares the standalone example and
benchmark configurations.

The public library surface is `Load`, `Config.Compile`, `Config.Publish`, and
`Format`. The CLI is a small adapter. Syntax and Go emission are private packages.
To extract Hyrule later, move this directory into its own module, change internal
import prefixes, and retain the public contract tests. The only external library
dependencies are YAML configuration decoding and Go package loading.

## Local semantic plans

One invocation computes a result from an immutable snapshot. The host decides
when to invoke it and applies the returned plan. Mutable storage, graph traversal,
worklists, fixed points, I/O and concurrency stay in the host. Pure host functions
remain appropriate for domain operations; unrestricted
loops and recursion are not part of the rule language.

Typed constants bind local Go constants directly, without builder callbacks:

```text
const Reference = Reference;
const ValueCopy = ValueCopy;
const NilZero = NilZero;
record Plan = Plan {
  kind: Kind;
  copy: Copy;
  zero: Zero;
};
```

A construction can then use `(Plan kind:Reference copy:ValueCopy zero:NilZero)`.
Bare names resolve only through declared constant bindings; variables, functions
and host methods cannot be bound as constants. Named enum types retain their Go
assignment and comparison rules. Literal range and assignment checks still run
before generated output is published.

Records bind a local, non-generic Go struct and explicitly name every direct
nonblank storage field. Duplicate fields, omitted storage fields, inaccessible
fields and duplicate record/union storage bindings are errors. Patterns use the
same named and positional field contracts as union constructors; omitted named
fields are wildcards. Construction requires every declared field exactly once.
No pointer dereference or graph traversal is implied by record destructuring.
Nested declared value unions remain checked closed domains, including through
records and whole-input fallbacks.

Comparisons can appear as guards, with `when` or the existing `&&` introducer:

```text
rule Bounded:
  (Input index:$index length:$length)
  when $index >= 0
  && $index < $length
  => (Located offset:$index);
```

Operands are captures, literals or declared constants. Supported relations are
`==`, `!=`, `<`, `<=`, `>` and `>=`. Ordered relations require compatible ordered
scalar types. Equality refuses unsafe interface-containing comparisons. Guards
retain their order and fall through on failure; comparison nodes participate in
pure-prefix sharing. Native Boolean disjunction is not implemented yet.

Immutable local bindings and integer arithmetic can express derived values:

```text
rule Fits:
  $request:(Request length:$length capacity:$capacity count:$count)
  let $end = (add $length $count)
  && $end >= $length
  && $end <= $capacity
  => (update $request length:$end);
```

`add`, `sub`, `mul`, `div` and `rem` take two positional integer operands.
Operations use ordinary Go integer semantics, including overflow wraparound.
No automatic overflow checks or overflow fallthrough branches are generated.
Division and remainder retain Go's zero-divisor behavior; rules must establish
valid divisors when their inputs may be zero. Container bounds and valid-state
conditions remain explicit domain guards, independent of arithmetic semantics.

A `let $name = expression` binding is immutable, inferred from its expression,
and visible only in later clauses and the result of that alternative. Names
cannot shadow captures or earlier locals. Bind domain function results before
using them as arithmetic operands. Local expressions may construct records,
updates and nonempty lists; an empty list or untyped nil needs a typed context.
Local construction occurs where the binding is written, so later guard failure
can discard a freshly allocated local. Builders still must not mutate host or
input state. Unrestricted loops and recursive evaluation are unsupported.

Explicit record updates copy a captured record before changing named fields:

```text
=> (update $request outcome:Located offset:$physical)
```

The base must have a declared record schema. Unmentioned fields are preserved;
duplicate or unknown updates are errors. Updates evaluate in written order and
never mutate the matched input. This is a value copy, so preserved references
continue to point to the same objects. Union updates require an explicit
constructor instead of a record update.

Lists construct ordered effect bundles in a known slice context:

```text
=> [(Observe domain:Contents), (Observe domain:Iterator), (Change domain:Iterator)]
```

The context comes from the generated result, a record field or a builder
parameter. The `effects [...]` spelling also retains this context inside record
construction and record updates. Elements are evaluated once in written order and checked against the
element type; declared value schemas are validated before publication. `[]`
constructs a non-nil empty slice. List matching and dynamic collection
transformations are not implemented. List size is statically bounded by the
rule source; host helpers must retain explicit bounds for dynamic collections.

The neutral matching IR retains typed constant identities, record schemas and
extractions, comparison operators, ordered list construction and explicit
preserved-base updates. Go names, selectors, concrete types and syntax remain in
the Go adapter. See [the local append-plan example](examples/localplans/append.rules)
for nested updates that preserve backing and source provenance while producing a
copy-effect list. Its sum uses ordinary integer arithmetic; explicit guards admit
only the geometry that the example can establish.

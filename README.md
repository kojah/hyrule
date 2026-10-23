# Hyrule

Hyrule compiles ordered structural rules into typed Go functions with no Hyrule
runtime dependency. The host supplies types and domain operations; Hyrule owns
local matching, decisions and construction. Graph traversal, mutable storage,
worklists, fixed points, budgets and applying effect plans remain host concerns.

The frontend is a handwritten lexer and parser. The checked-in
[EBNF grammar](grammar.ebnf) and [layout specification](LAYOUT.md) define its
surface syntax. It uses equations, whitespace application and layout blocks;
Hyrule blocks have no braces or semicolons (embedded Go does). This is Haskell-inspired syntax over strict
local decisions, without currying, lazy values, unrestricted recursion or loops in Hyrule itself.
The explicit `go!` escape accepts ordinary Go expressions and statement blocks.

## Function declarations

Types belong in the function header; arm captures and locals are inferred:

```text
fn rewrite(value: Request) -> Plan
  match value
    @Positive
    Request { count }
      when count > 0 ->
      go! { Plan{Count: count} }
    @rewrite_L6_F7b374983
    _ ->
      go! { Plan{} }
```

A body may instead be a direct expression, including `do`, `case` and
strategy choice. `match` at the function boundary accepts structural patterns;
`case` handles exhaustive query outcomes inside results.

Plain `fn` retains the implicit `func(Input[, Host]) (Output, bool)` API.
`partial fn` supports native parameter/result lists with a final success Boolean.
`total fn` returns its declared results without a success flag and requires a
fallback before the body:

```text
total fn classify(value: Request) -> Plan
  fallback go! { Plan{} }
  match value
    @classify_L4_F3bc2397b
    Request { count }
      when count > 0 ->
      go! { Plan{Count: count} }
```

For a single native output, result expressions can return that value directly;
`outputs` remains available, and assembles multiple native results.
`match inputs` matches the complete native parameter bundle using `inputs`
patterns. Other match subjects must name a function parameter.

A Go binding may supply the types: write `fn rewrite(value)`, or omit the
parameter list entirely and use `match inputs`. The latter retains the Go
binding's parameter/result names and exposes captures through the arm patterns.
Use `partial fn` or `total fn` for multiple explicit parameters.
A header without a body supplies API types for equation fragments in other
files of the same target. Fragments keep the ordered equation form below.
Standalone `signature` and `implementers` declarations are no longer accepted.

## Equation fragments and host bindings

```text
host
  Extract? = MatchExtract
  Lookup? = MatchLookup
  ready = IsReady
  payload = BuildPayload

@LookupPayload
rewrite (Extract ((Lookup _ _ true) as lookup) 0)
  | ready lookup
  =
  payload lookup
```

`?` declares a matcher returning children followed by a success Boolean. `!`
declares an ordered host query. Unmarked aliases bind pure functions or typed
constants, distinguished by the host symbol. Same-name aliases can omit `= Name`.
Names are exact and case-sensitive; a Boolean value builder is not a matcher.

Each target has one equation family, inferred across its configured files. It is
not a callable DSL function. Files sort lexically; equations and guarded branches
retain written order. The first successful match and guard sequence commits the
result. Failed matching or guards continue; an exhausted partial API returns
zero/false. Unknown names are compilation errors, never a failed match.

`@Label` is optional diagnostic metadata, unique per target. Unlabelled equations
receive deterministic source identities. Multiple guarded right-hand sides are
ordered alternatives with separate local scopes:

```text
@append_L1_Fbb6aced4
append (Append header count)
  | fits header count
  =
  reuseBacking header count
append (Append header count) = allocateBacking header count
```

Bare pattern names bind captures unless they name declared constructors or
patterns. `Pattern as whole` captures an entire matched value; `_` ignores it.
`^Limit` explicitly references a declared constant. Backticks escape a capture
whose spelling conflicts with a constructor or keyword. Field paths such as
`request.Header.Length` read established captures; they do not establish non-nil
pointers. Nullary host calls use `Name()`.

Repeated captures use checked Go equality on compatible comparable types.
Interface-containing values are rejected because dynamic contents could panic.
Use distinct captures and a typed equality guard when that contract is unsuitable.
Capture equality does not establish aliasing, structural equality or solver facts.

### Guards, conditionals and where

```text
@classify_L1_Fc36e76e3
classify (Entry { node })
  | Found { size } <- lookup node
  , size > 0
  , not (excluded node)
  =
  if size > limit then outputs large else outputs small
  where
    limit = base * 2
    large = Big { size }
```

`&&`, `||` and `not` take bool operands and short-circuit as in Go and Haskell;
`||` binds looser than `&&`, which binds looser than comparisons. A top-level
`&&` in a guard is a sequence of ordinary guards. A pattern guard, `Pattern <-
expression`, evaluates a pure expression once and matches it like an equation
pattern; a mismatch tries the next alternative, and its captures are in scope for
later qualifiers and the result. `if condition then a else b` evaluates the pure
condition and exactly one branch. It may appear as a value or as a result, where
branches may run `do`/`case` queries or `decline` inside `<|>`.

`where` follows an equation's last guarded alternative and is shared by all of
them. Each binding is substituted where it is used and evaluated only there, so
a binding guarded by a condition never runs before it. Bindings see the
equation's pattern captures and other where bindings, not guard locals or `do`
results. They cannot recurse, be projected with `.`, or reuse a capture or
declaration name.

## Applications and patterns

Calls are saturated whitespace applications: `sum left right`. Parenthesize
nested calls: `sum (increment left) right`. Host arity never determines grouping.
Negative call arguments need grouping: `outputs 0 (-1)`; `a - 1` is subtraction.
See [applications](APPLICATIONS.md) for precedence and name resolution.

Named fields use braces: `Header { length, capacity }` is shorthand for
`Header { length: length, capacity: capacity }`. Punning works in patterns,
construction and `with` updates; fields are selected by name, never position.
Mix puns with explicit fields, for example `Header { length, capacity: limit }`.
Use `Header { length } as header` to capture the whole matched header as well
as its length. For positional patterns, group the whole pattern before `as`:
`(Pair left right) as pair`. Aliases are pattern-only and bind the immediately
preceding pattern atom. Named records and updates can span lines and have a
trailing comma. `Header {}` is an empty named record pattern/construction.
The old `name@pattern`, `Type (field: value)` and `with (...)` spellings are
rejected. Positional calls and grouping still use their existing syntax.

Matcher labels
name Go result parameters excluding the match Boolean. Omitted named children
are wildcards. Builder labels name input parameters; every parameter must be
supplied exactly once. Missing, duplicate, unknown and mixed positional/named
arguments are errors. Labels are case-sensitive; named host calls require usable
Go parameter names. Written evaluation order is retained even when the Go
parameter order differs. Guards use positional arguments.

Pattern OR is parenthesized: `(Exact value | Conservative value)`. Each alternative
must establish identical capture names and types. Its guards run before trying
the next alternative. OR is match-only and expands to at most 64 alternatives.
It is separate from result choice `<|>`.

```text
pattern FiniteWrite request resident = Write { request, allocated: true, resident, evidence: ExactEvidence }
```

Reusable patterns have explicit capture parameters and exact arity. All captures
must be parameters; unused parameters, unknown references, hidden captures and
recursive definitions are errors, including unused definitions. Definitions can
refer forward and across configured files. A whole-capture parameter accepts a
capture or `_`. Expansion is bounded to 64 levels and 10,000 nodes before the OR
limit is applied. Reuse introduces no runtime registry or traversal.

## Inline Go

Use `go! { expression }` for a Go expression, or `go! { statements; return value }`
for a result block. Existing Hyrule expression syntax remains available.

```text
fn rewrite(input: Request) -> Plan

@rewrite_L3_F1fe98495
rewrite (Request { Text: text, A: a, B: b })
  | go! { a >= 0 && b >= 0 }
  , let total = go! { a + b }
  =
  go! {
      result := Plan{Text: strings.ToUpper(text), Total: total}
      if result.Text == "" {
          result.Text = "EMPTY"
      }
      return result
    }
```

Go code sees lexical pattern captures, earlier guard locals and query-result
captures, plus Go package declarations and imports available through the binding
file's import names. Hyrule aliases and lazy `where` names are not Go variables;
bind a value with `let` before using it inside Go. A `where` expression containing
Go retains the equation's capture scope. Go-local declarations follow Go's normal
shadowing rules and do not escape the block. Generated dispatcher locals are not
visible.

Expressions infer a type when needed. Statement blocks require a contextual
result type, such as a rule result, a builder argument or a Boolean guard.
When inference is needed, an explicitly typed Go function expression works:
`go! { func() int { return value + 1 }() }`.
Each escape produces exactly one value; a block can unpack a multi-result Go
call internally. Its `return` returns from the embedded block, not from the
rule dispatcher. Use `outputs` to assemble multiple native API results.

The compiler type-checks embedded Go before emission, binds captures through
typed function parameters, and resolves import identifiers without rewriting
strings or field names. Invalid Go is reported at the `go!` source location
with the inner Go diagnostic. Bodies are preserved verbatim by the rule formatter,
including comments and raw strings. See the runnable configuration at
[examples/inlinego](examples/inlinego/rewrite.rules).

The bang marks embedded Go, not permission to run ordered effects. Inline Go
obeys the same purity contract as host builders and guards: leave existing inputs
and host state unchanged; constructing fresh output values is allowed. Guard work
may be shared or skipped, and result work runs only on its reached branch.
Hyrule does not prove purity of Go code. Keep budget spending, evidence recording
and other ordered effects in `!` query hooks. `go!` is not allowed in patterns.

## Records, unions and local values

Schemas describe existing host storage; they do not generate data types:

```text
type Header = Header
  backing: Backing
  length: Length
  capacity: Capacity

exhaustive type Capacity = Capacity
  tag Kind
  Nil = NilCapacity
  Finite (bound: Bound) = FiniteCapacity
  Unknown = UnknownCapacity
```

A product names every direct nonblank field of a local non-generic Go struct.
Matching can omit named fields; construction supplies all fields and emits a Go
composite literal, so a one-line host builder is unnecessary.

```text
type Copy

@copy_L3_Faad7ae20
copy (Builtin source "copy" [destination, input]) = Copy source destination input
```

A `type` without a field block derives its schema from the Go struct: every
direct nonblank field in declaration order, labelled by lowercasing the leading
capital run (`Source` is `source`, `ID` is `id`, `URLPath` is `urlPath`).
Positional construction follows Go field order and supplies every field. Named
construction uses the labels and, as in a Go composite literal, may omit fields,
which keep their zero values; `Copy()` is the zero value. Explicit schemas
still require every field. Colliding labels and inaccessible fields are errors; declare the fields
explicitly then. `type Name` omits `= Name` when the Go type has the same name.
Keep a host builder when construction does real work, such as normalization. A `tag` line
instead declares a closed variant domain. Constructors expose only active fields
and zero inactive storage on construction. Whole-value copies retain existing
storage. Include provenance explicitly where constructors must preserve it.

`exhaustive type` requires unguarded rules to cover every input variant; see
[unreachable rules and coverage](#unreachable-rules-and-coverage). Query
decisions independently require all outcomes. Undeclared runtime tags fail
conservatively, even before catchalls.
See [union contracts](UNIONS.md) and [native views/tuple schemas](HOST_INTERFACES.md).
Patterns may also name Go types directly, `ChannelSnapshot { Fill: fill }`, as
derived read-only views over exact Go field names, and typed Go constants by
name, `SendChannel` or the tagged variant `FiniteCapacity { Bound: bound }`.
Opted-in accessor methods project as `Name(): pattern` labels.

Guard qualifiers are strict and ordered. Commas or `&&` separate requirements;
`let` binds an immutable value at its exact position:

```text
@Fits
append (Request { length, capacity, count } as request)
  | count >= 0
  , let end = length + count
  , end >= length
  , end <= capacity
  =
  request with { length: end }
```

Locals cannot shadow captures or earlier locals, and are visible only to later
qualifiers and the result of that alternative. Failed earlier guards prevent
local evaluation. Arithmetic uses typed unchecked integer `+ - * / %`; overflow
and division follow Go behavior. Establish nonzero divisors and container bounds
with explicit guards where required. Comparison operators are `== != < <= > >=`;
types must be compatible and equality must be safely comparable. Boolean
`&&`, `||` and `not` are described under "Guards, conditionals and where".

`request with { field: value }` copies a captured product and preserves unmentioned
fields, including reference identity. It never mutates input. Duplicate/unknown
fields and non-record bases are errors. Union updates use constructors instead.

Lists such as `[Observe resource, Change resource]` construct typed slices in
written order. Context comes from the result, record field or builder parameter;
`[]` is non-nil empty. Declared value representations are checked before success.
Dynamic collection transforms are not supported. Nonempty lists and records can
be local values; empty lists and nil need a known type context.

### List patterns

```text
pair (Arguments [left, right]) = Pair left right
first (Arguments [head, ..rest]) = Split head rest
```

`[]`, `[a]` and `[a, b]` match exactly that many elements; `[a, ..rest]` matches
at least the listed elements and binds the remaining suffix. Elements and the
rest are ordinary patterns, so literals, constructors, nested lists, repeated
captures and `[...] as whole` work as elsewhere. A length mismatch is a failed
match that tries the next rule. Patterns accept Go slices, using `len` and
indexing, and any type with `Len() int` and `At(int) E` value methods, such as
`readonly.Slice[T]`; a rest pattern on such a type also needs `Sub(int, int)`
returning the same type. Those methods are host matchers: they must be pure
reads. A rest suffix aliases the input. A nil slice matches `[]`.

## Unreachable rules and coverage

Every target's equations are checked with the usefulness algorithm of Maranget,
"Warnings for pattern matching" (JFP 2007), over the resolved, OR-expanded
patterns. The checks report diagnostics only; they never change generated code.
Hyrule has no warnings, so both findings are compile errors, like unused
pattern definitions, and a target reports all of its findings together:

- An equation, guarded branch or OR alternative written in the rule is
  *unreachable* when earlier unguarded rules, and earlier alternatives of the
  same equation, match every input it matches:
  `rule Late: unreachable rule: rule First matches every input it matches`.
  Alternatives of reusable pattern definitions are judged only as part of the
  whole rule, since other uses may need them.
- An `exhaustive type` input must be covered by unguarded rules. Each uncovered
  variant is named with a witness, `non-exhaustive union Event: missing Channel
  { capacity: ChannelCapacity { Kind: UnboundedCapacity } }`; `_` stands for values
  outside the listed ones, and omitted fields are unconstrained. An input that
  only guards, host matchers, accessors or repeated captures might cover is
  reported as `unproven` rather than missing. `total fn` declares its own
  fallback and partial APIs return false, so neither requires coverage; `case`
  and `do` decisions are complete by construction.

The analysis is sound in both directions: it never reports a reachable rule or
a covered input. Patterns it cannot see through count as covering nothing and
as possibly matching anything:

- Closed domains can be covered: Booleans; nil and non-nil pointers; products
  (records, and views over their own struct input); union variants where the
  generated code validates tags, namely the root input and active fields reached
  through records and unions; and Go slice lengths, where `[]`, `[_]` and
  `[_, _, .._]` cover every slice.
- Open domains leave a residual, so a fallback after all of their heads stays
  reachable: integer, float and string literals; typed Go constants, since Go
  admits other values of the type (the declared constants only name witnesses);
  interface inputs, which remain open to other dynamic types and typed nil pointers;
  unions reached through views or host matchers, which are not validated; and
  host sequences, whose `Len` may be negative.
- Opaque tests never cover or subsume other rows: guards and pattern guards (a
  `let` alone does not guard), refutable accessor projections, repeated
  captures, `nil` of slices and maps, constants matched against interfaces and
  list rests other than captures or lists. A host `?` matcher is deterministic
  under the hook contract, so it subsumes a later use of the same matcher whose
  children it subsumes, and nothing else.

Literals and constants compare by exact value. Each query is bounded to 65,536
steps; an exhausted query answers conservatively.

## Ordered queries and result choice

```text
host
  ready! = Ready
  plan = BuildPlan
  conservative = ConservativePlan

@resolve_L6_F9193c53f
resolve request =
  do
    require ready request
    plan request
  <|> conservative request
```

A `require expression` step inside `do` evaluates a Boolean query or pure
Boolean expression once. True continues to the next step; false declines the
current strategy. It is shorthand for `true <- expression` with
`else false -> decline`, and requires an enclosing non-final `<|>`
alternative. It does not retry another rule or undo earlier query effects.
It has no `else` clause; use an explicit binding or `case` for other outcomes.
A `do` block still needs a final result after its requirements.

Queries execute once when reached, after a rule commits. A pattern bind explicitly
handles other outcomes in `else` arms; a whole-result bind needs no else. A `do`
block ends in its result expression. `case query arguments of` handles complete
outcomes directly. Invalid tags stop conservatively; they do not become fallback
answers. `<|>` tries strategies lazily within the committed result. Only `decline`
tries the next strategy, without undoing effects or retrying another equation.
The final alternative is a pure total expression. See [transition semantics](TRANSITIONS.md).

A `do` step or `case` may also decide on a pure Boolean or union value, such as
`false <- proof.Exhausted` or `case kind value of`. The value is evaluated once at
that step, in written order, and may not call `!` queries. Declare a host `!` only
when it spends budget, records evidence or reads state that earlier queries
change; a pure check does not need `!` to branch.

The formatter keeps an equation's guarded alternatives together under one
pattern, with any `where` block after the last alternative. Long `if` expressions
put `then` and `else` on indented lines; an `else if` chain stays at that same
indentation.

## Host and compiler contracts

Declare the generated API's types in the rules, or a named function type
directly in the configured binding file:

```text
fn rewrite(value: ssa.Instruction) -> Copy

partial fn classify(value: ssa.Value, kind: Kind) -> ImmediateKind

total fn transfer(request: TransferRequest) -> TransferPlan
  fallback outputs TransferMissing()
```

Types use Go syntax (`*T`, `[]T`, `map[K]V`, `pkg.T`, `pkg.T[A]`) with the
binding file's import names. The target's YAML `signature:` setting remains
optional when a header declares types; it names a Go function type, not a rule
declaration. If both are present their types must agree. If annotations are
omitted, the binding file supplies the signature. Generic/variadic generated
APIs and hooks remain unsupported. Imported names use binding-file imports,
then unambiguous imports from other handwritten files. Dot imports are
unsupported. Queries may return Boolean, struct-union or tuple outcomes;
ordinary pure functions return one value. See [host interfaces](HOST_INTERFACES.md).

Matchers and guards must not mutate inputs, host state or externally observable
state. Equal arguments against the same stable snapshot give equal answers.
Hyrule may skip, repeat or share pure matching work; call counts are not guaranteed.
Guard prerequisites remain ordered. Builders likewise leave existing state
unchanged, may allocate fresh results, and run only on reached construction paths;
their results are not cached. Hyrule cannot prove callback purity. Ordered queries
have a separate effect contract and are never shared or hoisted as predicates.

```text
source → syntax AST → owned semantic model → declaration collection
       → Go hook/storage resolution → matching DAG + typed value/helper plans
       → bottom-up matching-DAG hash-consing
       → Go emission → complete-package validation → publication
```

`syntax` owns parsing/layout/formatting. `frontend` resolves names/scopes into
`semantic`; `compiler` owns ordering and bounded reusable/OR expansion.
`goresolve` produces a `goplan.Program`, portable `matchir` and `valueir` plus Go
binding tables. The portable IRs contain opaque IDs, not Go types or syntax.
`gogen` and `valuegen` emit resolved plans without parsing or name resolution.
`recordresolve` checks derivations; `recordgen` emits helper plans.

The matching DAG shares adjacent identical pure prefixes while retaining failure
and guard fallthrough. A bottom-up hash-consing pass then shares identical pure subgraphs without
mutating the input or merging construction/allocation nodes. Union schemas allow
checked exclusive dispatch. View extractions are emitted inline. Consecutive
alternatives asserting concrete view types on one value share a Go type switch:
each type's alternatives keep source order, typed nil pointers never match, and
any other rule ends the run. Cases with identical bodies that bind no fields
share one clause. Generated
functions use typed locals and direct branches, without runtime graphs, capture
maps, boxing or reflection. Go is the only implemented backend. See
[decision-graph measurements](DECISION_GRAPH.md) for the original benchmark scope.

## Commands and configuration

```sh
go run ./cmd/hyrule check
go run ./cmd/hyrule generate
go run ./cmd/hyrule generate --check
go run ./cmd/hyrule fmt --write path/to/pass.rules
```

On Xenia prefix direct Go workloads with `agent-test`; Make targets already use
it. `check` validates without publication. `generate --check` checks freshness.
Both accept `--config`, `--target`, `--format text|json`. `fmt FILE` prints one file;
`--write FILES...` replaces files and `--check FILES...` checks canonical spelling.
Formatting retains comments, moving internal comments to declaration boundaries.

Version 1 YAML declares explicit rule paths, package, binding file, function,
output and, unless the rules declare their types, signature. An optional
top-level `views:` list names `.views` accessor files. Paths remain inside the config directory; no globs/includes.
Outputs must be `.go` files in their package. Hosts load with in-memory signature
stubs, permitting handwritten callers before first generation. Stubs are never
published. Generated packages are type-checked before any output writes.

`.hyrule-manifest.json` records output ownership and hashes. Generated Go and
manifests are ignored artifacts in this repository. Renamed/removed outputs are
deleted only if unchanged; handwritten files and output symlinks are protected.
Publication is atomic per file, not per batch. Do not publish concurrently for
the same config. `targets: []` retires previously owned outputs.

Inputs are bounded to 4 MiB, 100,000 tokens and 64 nesting levels. Generated files
are limited to 500 lines. `make generate` generates all standalone examples;
`make generated-check` verifies their freshness. Run `make build` to generate
and compile, and `make test` for compiler and example regressions.

Field-complete representation maintenance (snapshots, read-only views, keys,
visitors and transforms) is not a semantic decision and belongs to
Boiler, not to Hyrule schemas.
The public library provides `Load`, `Compile`, `Publish` and `Format`; the CLI is
an adapter over those APIs.

## Readable, checked-in output

Generated Go is intended to be read, reviewed and debugged. The compiler preserves
capture and parameter names, chooses names within each lexical scope, and avoids
collisions with Go keywords, imports and host declarations. Native APIs retain
their declared signatures. Structural extraction omits unused fields; simple
conditions use Go's short-circuit operators. Local output cleanup removes
redundant copies and result carriers when their storage cannot be observed.

These changes preserve first-match precedence, query order, captured read
snapshots, typed assignment conversions and invalid-domain handling. A temporary
remains when removing it could change those contracts. Embedded Go blocks retain
their own scopes; only proven expression-only wrappers are simplified.

Each file records the compiler release and a regeneration command relative to
its configuration directory. Rule comments retain labels and source files without
line numbers, so moving a labeled rule does not churn the generated output.
Use explicit `@Labels` for rules whose names should survive source reorganization.

Consumers may check generated `.go` files into Git. Their normal builds and
`go install` then need only Go and the consumer's runtime dependencies, provided
all required generated files are committed. Keep Hyrule in a separate pinned
tool module when its Go version is newer than the consumer's supported version.
CI should run `generate --check` before regenerating files to detect stale output.
Hyrule's own generated examples remain local build artifacts.

The small reviewed goldens in `testdata/readability` cover records, native APIs,
and ordered queries. When output intentionally changes, regenerate the examples,
run `go test -run 'TestReadable(Output|NativeQueries)$' -update-readability`,
and review the corresponding `.golden` changes alongside the emitter change.
On Xenia, prefix that command with `agent-test`. Behavioral regressions cover the semantic constraints as well;
matching a golden is not evidence of semantic preservation by itself.

## Module and consumer integration

The module is `github.com/kojah/hyrule`; the CLI is `cmd/hyrule`.
This repository preserves the history of `tools/hyrule` extracted from Lockdoc.

Clone it beside a consumer and add `../hyrule` to that consumer's local
`go.work`. A `tools/hyrule -> ../../hyrule` symlink provides convenient source
access. Consumers should pin the compiler as a Go tool dependency for clean
clones and CI, and run the compiled command from the consumer directory.
Hyrule needs Go 1.27; a separate tool module can keep that requirement out of
a consumer supporting an older Go version. Generated code has no Hyrule runtime
dependency. Consumer rules and integration regressions stay with the consumer;
language tests and standalone examples live here.

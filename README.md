# Hyrule

Hyrule compiles ordered structural rules into typed Go functions with no Hyrule
runtime dependency. The host supplies types and domain operations; Hyrule owns
local matching, decisions and construction. Graph traversal, mutable storage,
worklists, fixed points, budgets and applying effect plans remain host concerns.

The frontend is a handwritten lexer and parser. The checked-in
[EBNF grammar](grammar.ebnf) and [layout specification](LAYOUT.md) define its
surface syntax. It uses equations, whitespace application and layout blocks;
there are no braces or semicolons. This is Haskell-inspired syntax over strict
local decisions, without currying, lazy values, unrestricted recursion or loops.

## Equations and host bindings

```text
host
  Extract? = MatchExtract
  Lookup? = MatchLookup
  ready = IsReady
  payload = BuildPayload

@LookupPayload
rewrite (Extract lookup@(Lookup _ _ true) 0)
  | ready lookup = payload lookup
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
append (Append header count)
  | fits header count = reuseBacking header count
  | otherwise = allocateBacking header count
```

Bare pattern names bind captures unless they name declared constructors or
patterns. `whole@Pattern` captures an entire matched value; `_` ignores it.
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
classify (Entry (node: node))
  | Found (size: size) <- lookup node
  , size > 0 && not (excluded node)
  =
  if size > limit then outputs large else outputs small
  where
    limit = base * 2
    large = Big (size: size)
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

Named groups use `Header (length: length, capacity: capacity)`. Matcher labels
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
pattern FiniteWrite request resident =
  Write (request: request, allocated: true, resident: resident,
         evidence: ExactEvidence)
```

Reusable patterns have explicit capture parameters and exact arity. All captures
must be parameters; unused parameters, unknown references, hidden captures and
recursive definitions are errors, including unused definitions. Definitions can
refer forward and across configured files. A whole-capture parameter accepts a
capture or `_`. Expansion is bounded to 64 levels and 10,000 nodes before the OR
limit is applied. Reuse introduces no runtime registry or traversal.

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
Patterns may also name Go types directly, `ChannelSnapshot (Fill: fill)`, as
derived read-only views over exact Go field names, and typed Go constants by
name, `SendChannel` or the tagged variant `FiniteCapacity (Bound: bound)`.
Opted-in accessor methods project as `Name(): pattern` labels.

Guard qualifiers are strict and ordered. Commas or `&&` separate requirements;
`let` binds an immutable value at its exact position:

```text
@Fits
append request@(Request (length: length, capacity: capacity, count: count))
  | count >= 0
  , let end = length + count
  , end >= length
  , end <= capacity
  = request with (length: end)
```

Locals cannot shadow captures or earlier locals, and are visible only to later
qualifiers and the result of that alternative. Failed earlier guards prevent
local evaluation. Arithmetic uses typed unchecked integer `+ - * / %`; overflow
and division follow Go behavior. Establish nonzero divisors and container bounds
with explicit guards where required. Comparison operators are `== != < <= > >=`;
types must be compatible and equality must be safely comparable. Boolean
`&&`, `||` and `not` are described under "Guards, conditionals and where".

`request with (field: value)` copies a captured product and preserves unmentioned
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
captures and `whole@[...]` work as elsewhere. A length mismatch is a failed
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
  (capacity: ChannelCapacity (Kind: UnboundedCapacity))`; `_` stands for values
  outside the listed ones, and omitted fields are unconstrained. An input that
  only guards, host matchers, accessors or repeated captures might cover is
  reported as `unproven` rather than missing. `signature total` declares its own
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
  interface inputs, whose closed implementer set limits what patterns may name
  but not what values hold, such as other dynamic types and typed nil pointers;
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

resolve request =
  do
    true <- ready request
      else false -> decline
    plan request
  <|> conservative request
```

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
signature ssa.Instruction -> Copy
signature Event, *Host -> []Effect
signature partial value: ssa.Value, kind: Kind -> ImmediateKind
signature total TransferRequest -> TransferPlan = outputs TransferMissing()
```

Types use Go syntax (`*T`, `[]T`, `map[K]V`, `pkg.T`, `pkg.T[A]`) with the
binding file's import names. A signature without a mode keeps the implicit API
`func(Input[, Host]) (Output, bool)`; `partial` appends the success Boolean;
`total` returns the outputs alone. `name: T` names a native input or output
field. With declared types the target's `signature:` setting is optional: when
named and absent from the binding file, the generator declares that function
type; when the binding file declares it, it must be identical. Without
declared types, the binding file must declare the named signature. Native `signature partial`
or `signature total = outputs ...` supports multiple parameters/results; see
[host interfaces](HOST_INTERFACES.md). Generic/variadic signatures and hooks are
unsupported. Imported names use the binding file's import aliases, then names
other handwritten files of the package import unambiguously; dot imports are
unsupported. Queries may return Boolean, struct-union or tuple
outcomes. Ordinary pure functions return one value.

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
go run ./tools/hyrule/cmd/hyrule check
go run ./tools/hyrule/cmd/hyrule generate
go run ./tools/hyrule/cmd/hyrule generate --check
go run ./tools/hyrule/cmd/hyrule fmt --write path/to/pass.rules
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
are limited to 500 lines. `make hyrule-check`, `make hyrule-generate` and
`make hyrule-generated-check` include the production/example configurations.
Run `make generate-go` before raw Go commands in a fresh checkout.

Field-complete representation maintenance (snapshots, read-only views, keys,
visitors and transforms) is not a semantic decision and belongs to
[Boiler](../boiler/README.md), not to Hyrule schemas.
The public library provides `Load`, `Compile`, `Publish` and `Format`; the CLI is
an adapter over those APIs.

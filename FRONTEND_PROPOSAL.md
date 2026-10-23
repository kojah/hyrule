# A Haskell-inspired Hyrule frontend

Status: implemented, with Lockdoc and GoHawk rules migrated. The production parser
uses equations. This document records the design rationale; the maintained
[grammar](grammar.ebnf), [layout specification](LAYOUT.md) and [README](README.md)
define current syntax. The legacy parser and migration command have since been removed.

The direction is **compact pattern equations first, indentation for actual
sequences and branches**. Keep `do` and `<|>`. Use whitespace application, guards
and `case ... of` as inspiration from Haskell. Avoid mandatory stacks of rule
labels, pattern blocks and result blocks. No braces or semicolons.

This is Haskell-inspired syntax over Hyrule's existing execution model, not an
attempt to implement Haskell. It does not introduce lazy evaluation, currying,
higher-order functions, typeclasses or unrestricted recursion.

## Equations, not nested rule blocks

A simple illustrative ruleset:

```text
append (Append header count)
  | fits header count = reuseBacking header count
  | otherwise        = allocateBacking header count
```

Each equation begins with a ruleset name and its input pattern. Its right-hand
side follows `=`. A simple equation can fit on one line:

```text
release (Event (lock: lock, release: true, deferred: false)) = [Release lock]
```

Guards use `| condition = result`; `otherwise` means an unconditional true guard.
Multiple guarded right-hand sides are ordered attempts. If none succeeds,
matching proceeds to the next equation. Declared rule priority and lexical file
order are unchanged.

The equation family identifies the source ruleset for a target. It is not a
new callable DSL function: `append` above does not become available for recursive
calls from another rule. Each target has one family, inferred from its equations;
the YAML configuration still determines the exported Go function and signature.
Shared source files must agree on the family when they contain equations.

Individual clauses can retain an optional diagnostic label, e.g. `@Fits` on the
line before an equation. The migration can preserve every existing rule name
this way without making labels a required extra nesting level. Unlabelled
clauses get deterministic source-based identities. Labels remain metadata,
never extra matching conditions.

## Application, named fields and updates

Ordinary application is `makePlan header`, with nested applications grouped as
`wrap (makePlan header)`. Calls are saturated: whitespace does not imply currying
or partial application. Commas are not ordinary argument separators.

Named arguments use a parenthesized named group:

```text
Copy (offset: length, count: count)
Header (length: length, capacity: capacity)
```

The named group is argument syntax, not a positional tuple argument. A group
containing colons is unambiguously named. Named and positional arguments cannot
be mixed where the current language prohibits it. All existing unknown,
duplicate and missing-field checks remain.

Use `request@Pattern` for a whole-value capture. `header with (length: end)`
copies the established record and updates only the named fields. Parenthesized
named groups can wrap across lines; those line breaks are formatting, not extra
structural blocks. Named fields remain explicit in this revision; there is no
second indentation-only constructor syntax or ambiguous field punning.

Here is the full conversion of [the append example](examples/localplans/append.rules):

```text
@Fits
append request@(Request
    (header: header@(Header (length: length, capacity: capacity)), count: count))
  | length >= 0
  , count >= 0
  , let end = length + count
  , end >= length
  , end <= capacity
  = request with
      (header: header with (length: end),
       outcome: Reuse,
       copies: [Copy (offset: length, count: count)])

@Decline
append request = request with (outcome: Refused, copies: [])
```

Comma-separated guard qualifiers execute left to right. `let name = expression`
binds an immutable local at that exact point. Failed earlier guards prevent its
evaluation. This retains arbitrary interleaving of guards and locals, including
partial arithmetic or domain operations that require earlier evidence.
A qualifier list may begin with `let` if the old clause sequence does.

The example preserves negative-input guards, unchecked arithmetic, overflow
admission, the capacity guard, backing identity, source evidence and non-nil
empty lists. It does not move locals into a `where` block whose evaluation order
would need a new contract. `Decline` is a diagnostic label here; lowercase
`decline` is the separate result-control expression below.

## Keep `do` and `<|>`

The terminal expression in `do` supplies its result without `pure`:

```text
resolve (Request value) =
  do
    Known header <- resolveHeader value
      else Unknown -> decline
    makePlan header
  <|> conservativePlan value
```

`else` attached to a pattern binding is a Hyrule extension. It preserves explicit
handling of unsuccessful outcomes. This example assumes a two-variant outcome;
all other valid variants would need their own arms. A mismatch does not silently
become `decline`.

The complete `Main` rule from [the sequencing fixture](sequencing_test.go) becomes:

```text
@Main
rewrite (inputs input n host) =
  do
    Proven (reason: first) <- ask host n 0
      else
        Disproven -> decline
        Unavailable (reason: why) -> outputs why 3
    Proven (reason: second) <- ask host host.Offset.Value first
      else
        Disproven -> decline
        Unavailable (reason: why) -> outputs why 4
    outputs (mark host second) 1
  <|> do
    true <- ready host "ok"
      else false -> decline
    outputs (mark host 20) 2
  <|> outputs (mark host 99) 5

@Later
rewrite (inputs input n host) = outputs (mark host 100) 6
```

The host aliases, tuple schema and native signature are declared separately.
This preserves the sequencing fixture's early answers, query order and fallback
behavior; it does not permit the `Later` equation to recover from a declined
result continuation.

Execution rules remain:

- Matching and guard qualifiers finish before the selected result begins.
- Each reached query executes once. Later bindings can use earlier captures.
- Remaining valid outcomes need explicit else arms. Boolean steps can use a
  compact single-expression else arm.
- A whole-result binding, `proof <- ask host n 0`, needs no else. Boolean and
  tuple outcomes retain their host types.
- `decline` tries the next alternative in the nearest enclosing `<|>` chain.
  It never retries another equation or rolls back an executed query.
- The final alternative must be a pure total result expression, without queries
  or decline. Invalid runtime tags stop conservatively, not via fallback.
- Nested `do` and choice retain lexical scopes and local decline behavior.

`do` is a result expression, not an arbitrary statement language. The first
frontend version retains dependent query steps and a terminal expression; it
need not introduce general imperative statements or mutable local variables.

## `case ... of` for outcome decisions

Use one familiar branching form in place of `query ... decide`:

```text
transition (Event (lock: lock, release: false, deferred: false)) =
  case Exclusive lock of
    Proven -> [Acquire lock]
    Disproven -> [Acquire lock, Order lock]
    Unknown (reason: reason) -> [Acquire lock, Uncertain lock reason]
```

This translates [the acquisition transition](examples/transitions/transitions.rules).
The declaration identifies `Exclusive` as a query. The case evaluates it once
and requires exhaustive outcomes. In the first frontend version, case supports
these existing query decisions, not arbitrary newly computed pure scrutinees.
Named query captures use `proof <- Exclusive lock` followed immediately by
`case proof of ...`, lowering to the current query node and whole-result capture.

Selective query outcomes become explicit bindings with declining else arms:

```text
acquire (Request lock) =
  do
    (Proven | Disproven) <- Exclusive lock
      else Unknown (reason: reason) -> decline
    [Acquire lock]
  <|> []
```

Pattern OR alternatives must expose identical capture names and types. One OR
selection runs the query once. This version retains complete variant/literal
case arms and rejects refinements that would weaken exhaustiveness.

The three choice forms are distinct:

- `|` after an equation head introduces a guarded right-hand side.
- Parenthesized `|` inside a pattern denotes ordered OR patterns.
- `<|>` combines result strategies after the equation has committed.

This context distinction prevents guard alternatives from being mistaken for
query fallback. There is no general Boolean OR introduced by this proposal.

## Host declarations

Retain a compact declaration block so effect distinctions appear once:

```text
host
  Event? = MatchEvent
  Exclusive! = Exclusive
  mark = mark
  Reuse = Reuse
  Refused = Refused
```

`?` declares an extractor whose final host result is a match Boolean. `!`
declares an ordered query. They mark declarations, not names at the use site:
write `Exclusive lock`. Unmarked aliases bind pure value functions or typed
constants, distinguished by the host symbol. Pure Boolean functions can appear
in guards without a separate guard declaration. A Boolean builder result remains
data, not a success bit.

Queries remain forbidden in patterns, guard qualifiers, their locals and pure
fallbacks. Pure matchers/guards keep their optimization contract. Builders stay
on the winning path and their allocating results are not shared. Host context
and receiver arguments remain explicit where the current API requires them.

Forwarding keeps whitespace application and lexical evidence:

```text
host
  transferred! instruction obligation =
    transfersResource evidence instruction obligation.target
```

Forwarded values are still captures, projections, constants or literals; no
hidden nested calls or effects. Qualified functions and explicit receiver methods
remain supported. Same-name aliases can omit `= Name`, e.g. `Exclusive!`.

The `?`/`!` declaration marks are a recommended spelling, still open to review.
They are separate from the agreed equations, application, guards and `do`/choice
style. No runtime effect classification is inferred from a function's name.

## Schemas and reusable patterns

Schemas bind existing host storage. These declaration blocks use indentation
because they are genuinely lists of fields or constructors:

```text
type Header = Header
  backing: Backing
  length: Length
  capacity: Capacity

type Proof = Proof
  tag Kind
  Proven = Proven
  Disproven = Disproven
  Unknown (reason: Reason) = Unknown

exhaustive type Capacity = Capacity
  tag Kind
  Nil = NilCapacity
  Finite (bound: Bound) = FiniteCapacity
  Unknown = UnknownCapacity
```

`tag` distinguishes a union from a product. Constructor payload entries bind
logical names to host storage fields. Same-name field/tag aliases can be omitted.
`exhaustive` retains the existing unconditional input-variant coverage check;
it is separate from exhaustive query decisions. Ordinary unions still support
partial recognizers. Tuple schemas keep `type Proof = (Kind, int)`, numeric tag
positions and numeric payload mappings.

Read-only `view` schemas retain checked interface/pointer extraction and their
current nil behavior. Native APIs retain `signature partial` or
`signature total = outputs 0 -1`; the total expression is invoked at the same
failure boundary as today. Legacy signatures, `inputs`/`outputs` products and
YAML target bindings remain supported.

```text
pattern FiniteHeader backing length capacity =
  Header (backing: backing, length: length, capacity: capacity)

pattern SafeKey = (ExactEvidence | SafeUnknownEvidence)
```

Reusable patterns remain hygienic, typed, bounded and non-recursive. Nested
constructors, omitted named fields, `_`, `@` and repeated captures retain their
contracts. Repeated captures perform checked equality, not alias inference.
Bare names bind in patterns; `^Limit` explicitly selects a declared constant in
patterns or expressions, including when a local shadows it. Constructors are
recognized by declarations, not capitalization. Nullary host functions use
`Name()` explicitly; the frontend does not guess a call from a bare identifier.
Keywords are contextual at field labels; backtick escapes preserve ambiguous
identifiers when dropping `$`.

## Schema-derived helpers

Record derivations were later removed from Hyrule. Field-complete snapshots,
views, keys, visitors and transforms are generated by Boiler
([tools/boiler](../boiler/README.md)), which keeps the per-field policy
discipline outside the rule language.

## Layout and evaluation

- Equations, guards and expressions form the normal surface syntax. There are
  no mandatory label, input-pattern and `=>` blocks.
- Guards after a head use `|`; comma-separated qualifiers combine requirements
  in written order. Boolean tests and `let` bindings can interleave arbitrarily.
  Separate guarded right-hand sides are separate attempts with private locals.
- Local values are strict, evaluated at their qualifier position. There is no
  lazy `where` semantics or hoisting across guards. Guard `&&` can desugar to
  ordered Boolean qualifiers, retaining short-circuiting.
- Whitespace application binds tighter than arithmetic and `<|>`. All calls
  retain their declared arity. Parentheses group nested applications; a named
  group is recognized by its colon-separated fields. `with` updates an explicit
  captured record; parenthesize an update used as a call argument, e.g.
  `use (header with (length: end))`.
- A case scrutinee or query right-hand side consumes the complete application,
  e.g. `case ask host n 0 of`. All arguments are evaluated in written order,
  even when named arguments map to a different host parameter order.
- Infix `+ - * / %` means existing unchecked integer arithmetic. Overflow and
  division behavior remain target-defined as today. Bounds stay explicit guards.
- Record updates use `with (field: value, ...)`. Fixed lists use `[...]`,
  including multiline lists. Neither requires a new indentation-based field
  block. No comprehensions, list matching or implicit dynamic loops are added.
- A `do` block and `case` arms use layout. A dedent ends the block. A `<|>` aligned
  with the opening `do` combines complete strategies; parentheses can make
  grouping explicit. `<|>` associates in written order and is left-biased.
- Indentation inside parentheses/brackets is cosmetic. Else arms have their own
  layout block. The formatter chooses two-space block indentation and rejects
  mixed tab/space indentation. Source locations and comments are retained.
- File ordering, rule/guard priority and bounded expansion remain deterministic.
  Equation syntax does not add recursive DSL calls or a general FP runtime.

## Expressiveness checklist

| Current responsibility | Proposed representation |
|---|---|
| Ordered rules / guards / guard failure | Pattern equations, guarded right-hand sides, ordered qualifiers |
| Individual rule names and tracing labels | Optional `@Name` clause metadata |
| Captures / whole captures / ignored inputs / constants | Bare bindings, `@`, `_`, `^Constant` |
| Named and positional nested matchers/builders | Whitespace application, grouping, named argument groups |
| OR and reusable patterns | Parenthesized pattern `|` and `pattern` equations |
| Typed constants / pure builders / guards | Unmarked `host` aliases; guards establish Boolean context |
| Matcher failure | `host` extractor declaration with `?` |
| Tagged unions / products / read-only views | `type`, `tag`, `view`, explicit host mappings |
| Coverage checks / invalid runtime tags | Existing contracts; `exhaustive` where requested |
| Arbitrarily interleaved locals and guards | Ordered Boolean and `let` guard qualifiers |
| Comparisons / unchecked arithmetic | Typed infix expressions |
| Typed lists / effect bundles / record updates | Lists and `with (...)` |
| Committed queries / exhaustive outcome decisions | `!` declaration and `case ... of` |
| Named, Boolean and tuple query results | `<-`, typed schemas, literal/constructor arms |
| Dependent sequencing / early outcome answers | Layout `do` with explicit `else` arms |
| Selective query / OR selections | Pattern bind plus explicit declining else |
| Lazy alternatives / nested decline | `<|>` and `decline`, unchanged lexical semantics |
| Field projection / host forwarding / receiver methods | Field paths and `host` aliases/forwarders |
| Legacy and native partial/total APIs | Existing YAML/signatures; compact total fallback |
| Formatting / bounded expansion / source diagnostics | Layout frontend and formatter; retained limits/metadata |

This is the design-level expressiveness mapping used to audit the migration.
No existing capability is intentionally removed. The Hyrule-specific named
groups, record updates, pattern OR, identifier escapes and bind-else syntax are
explicit extensions;
they are not presented as standard Haskell.

## Migration evidence before replacement

Implement the frontend against the semantic model; retain matching/value/helper
plans and emitters as the execution boundary. Translate the complete configured
rule inventory, not just these examples. Reuse public compiler/generated-code
behavior tests for precedence, guard/local evaluation order, purity boundaries,
query call order, decline, invalid tags, named argument order, native interfaces
and derivation ownership. Preserve existing rule labels where used by diagnostics.

Add new-syntax fixtures for layout, equation grouping, guard-versus-pattern bars,
named-group-versus-positional applications, backtick names, formatting and limits.
All production/example targets must generate and type-check. Compare translated
behavior with the old frontend before deleting it. A pretty example or a successful
parse alone does not establish expressive or behavioral equivalence.

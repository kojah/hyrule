# Native Go host interfaces

Hyrule uses existing Go signatures, fields and query results directly. Host code
owns searches, mutable state and ownership. Without a `signature` declaration,
the implicit API remains `func(Input[, Host]) (Output, bool)`.

## Boolean and tuple query outcomes

```text
host
  transferred!

transfer request =
  case transferred request of
    true -> Settled
    false -> Live
```

Boolean queries, including named Boolean types, require literal `true` and
`false` outcomes. Wildcards and refinements are errors. A query runs once when
reached, after matching and guards. An explicit declining arm inside `<|>` can
continue to another strategy; see [transition semantics](TRANSITIONS.md).

Multiple Go results can bind directly to a tuple schema:

```go
func classify(instruction Instruction) (Status, Reason)
```

```text
host
  classify!

type Decision = (Status, Reason)
  tag 0
  Live (reason: 1) = statusLive
  Settled (reason: 1) = statusSettled
  Unknown (reason: 1) = statusUnknown
```

Types are local/imported non-generic named types, optionally pointers. Indices
are zero-based result positions. Tags are integer/string/Boolean values with
exact typed constants; Boolean tags can use `true`/`false`. Duplicate tags and
invalid positions are rejected. Outcomes cover every declared variant.

Tuple storage is generated internally. A whole capture retains the complete
host tuple and can return it when its types match the native outputs. Invalid
runtime tags stop immediately; they never retry rules or undo consumed budgets.

## Field projections and forwarding

`request.obligation.target` and `host.evidence` are checked, read-only field paths.
They work in guards, locals, query arguments and construction. Fields must be
accessible. Methods, indexing and arbitrary Go expressions are excluded. Pointer
projection has Go's nil behavior; it does not establish non-nil evidence.

```text
host
  transferred! instruction obligation =
    transfersResource evidence instruction obligation.target
```

Parameters are positional values. The body calls an existing host function with
captures, projections, constants or literals; it cannot hide nested calls or
queries. Free captures such as `evidence` come from the calling rule's lexical
scope. Every invocation is type-checked. Forwarding applies to ordered queries
and pure functions, not matchers or constants.

Qualified imported functions use the binding file's actual import aliases.
`Classifier.method` binds a local receiver method as a method expression; pass
the receiver explicitly first. Value methods take `Classifier`, pointer-only
methods take `*Classifier`. This works for matchers, pure functions and queries,
without adding implicit state or changing their execution phase.

## Native generated signatures

Declare a named function type directly in the configured binding file:

```go
type Signature func(*Evidence, Probe, Instruction, Obligation) (Status, Reason)
```

Then declare the failure contract:

```text
signature total = outputs statusUnknown reasonNone

classify (inputs evidence probe instruction obligation) =
  case transferred instruction obligation of
    true -> outputs statusSettled reasonTransferred
    false -> outputs statusLive reasonNone
```

The example assumes aliases for constants and the query. `inputs` matches the
parameter list; `outputs` constructs the result list. A matching tuple-union
constructor can construct those outputs too. Positional fields always work;
named fields use Go parameter/result names, or `value0`, `value1`, ... for unnamed
slots. Native targets reserve `inputs` and `outputs`.

Total fallback runs for unmatched input or invalid query/schema outcomes. It
must construct outputs without rule captures or queries. It is distinct from
retrying another equation after an effectful query. A final Boolean in a total
API is ordinary data.

`signature partial` instead reserves the last Go result as a success Boolean.
Failure returns zero outputs and false; success returns outputs and true. At
least one parameter and one output are required. Generic/variadic APIs are not
supported. Native rules pass context explicitly; implicit host methods belong
to the implicit two-parameter API.

## Read-only views

```text
view Assertion = *ssa.TypeAssert
  value: X
  commaOK: CommaOk

asserted (Assertion (value: value, commaOK: false)) = value
```

Views name local/imported structs or pointers and can expose a subset of direct
accessible fields. Interface input uses a checked type assertion. Other dynamic
types and nil pointers fail the match; incompatible static types are errors.
Promoted fields require an explicit captured field path.

Views cannot construct, update or derive representation helpers. They introduce
no mutation, recursive traversal or implicit semantic evidence. Pure extraction
may be shared by the matching graph; ordered queries remain separate.

A whole view capture, `deferred@Deferred`, retains the concrete storage type that
passed the assertion. A hook requiring `*ssa.Defer` can consume it even when the
original input was `ssa.Instruction`. Identity is preserved and no extra field
is added. Repeated captures keep ordinary equality restrictions. Bind around a
pattern OR instead when the whole value should retain its original interface type.

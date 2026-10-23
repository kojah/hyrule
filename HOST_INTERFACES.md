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

Declare the parameter and result types with the failure contract:

```text
signature total evidence: *Evidence, probe: Probe, instruction: Instruction,
  obligation: Obligation -> Status, Reason = outputs statusUnknown reasonNone
```

Alternatively declare a named function type directly in the configured binding
file and only the failure contract in the rules:

```go
type Signature func(*Evidence, Probe, Instruction, Obligation) (Status, Reason)
```

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
named fields use the declared `name: T` or Go parameter/result names, or
`value0`, `value1`, ... for unnamed slots. Native targets reserve `inputs` and `outputs`.

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

Views cannot construct or update values. They introduce
no mutation, recursive traversal or implicit semantic evidence. Pure extraction
may be shared by the matching graph; ordered queries remain separate.

### Derived views

A pattern may name a Go type directly, without a `view` declaration:

```text
rewrite (ChannelSnapshot (Capacity: ChannelCapacity (Bound: bound), Fill: fill)) = ...
rewrite source@(ssa.Lookup (X: map, Index: key, CommaOk: commaOK)) = ...
```

Labels are exact Go field names, including fields promoted from structs
embedded by value; other packages' types expose only exported fields. The
pattern matches values of that type, pointers to it (nil never matches) and,
on an interface input, the dynamic type that implements the interface: the
value type when its method set suffices, otherwise the pointer type.

An interface input has a closed implementer set: by default the concrete
types declared in the interface's own package, so `ssa.Call` works on an
`ssa.Instruction`. A rule file may instead list the set explicitly, which an
interface from another package or `any` requires:

```text
implementers fmt.Stringer = *Name, Code
```

Naming a type outside the set is a compile error. At run time other dynamic
types, typed nil pointers and nil interfaces match no implementer, and
consecutive implementer alternatives share one Go type switch. `Name()`
with no fields tests the type alone. Pointer-typed fields match nested
patterns through the same nil check, while a field promoted through an
embedded pointer is an error; match the embedded field explicitly. A derived
view introduces no runtime registry and shares the inline extraction and type
switch of declared views. Declare a `view` to rename fields.

### Accessor methods

A derived view label may call a method, `Name(): "copy"`, when the method is
opted in as a pure accessor: a configured `.views` file lists methods of
packages the repository does not own, and a `//hyrule:view` comment marks a
method in its own source. Accessors take no arguments and return one value.

```text
// hyrule.views, named by the top-level "views:" list in hyrule.yaml.
golang.org/x/tools/go/ssa
  Builtin.Name
  Value.Type
```

An unindented line names an import path; indented `Type.Method` lines name
methods, including interface methods. Every entry must resolve in a loaded
package. Any other method in a pattern is an error. Calls share the view's
nil checks: nil pointers and nil interfaces never reach them, but an accessor
must tolerate whatever else its receiver can hold, such as a typed nil inside
an interface. Accessors are matchers: they must not change state.

### Typed Go constants and tagged structs

A constant of a named Go type is usable by name without a host alias. As a
pattern, `SendChannel` or `SendChannel()` on an input of exactly its type
tests Go `==`; in values and guards, `OperationEnabled` or `action ==
UnknownAction` reads it. Qualified names use the binding file's imports.

Applied to a struct whose unique field has the constant's type, the name is a
tagged variant: `FiniteCapacity (Bound: bound)` on a `ChannelCapacity{Kind
CapacityKind; Bound int}` input requires `Kind == FiniteCapacity` and matches
the named Go fields. The constants of the tag type are the variant set; a tag
outside them matches no variant. Two fields of the tag type are ambiguous.
Captures keep priority: a name already bound in the rule, or a backtick-quoted
capture, never reads a constant. Declare a `type ... tag` union schema for
renamed fields, construction or exhaustiveness checks.

A whole view capture, `deferred@Deferred`, retains the concrete storage type that
passed the assertion. A hook requiring `*ssa.Defer` can consume it even when the
original input was `ssa.Instruction`. Identity is preserved and no extra field
is added. Repeated captures keep ordinary equality restrictions. Bind around a
pattern OR instead when the whole value should retain its original interface type.

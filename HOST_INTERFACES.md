# Native Go host interfaces

Hyrule can use existing Go signatures, fields and query results directly. These
features remove mechanical adapters; searches, mutation and ownership contracts
remain Go responsibilities. Existing rules and `func(Input[, Host]) (Output,
bool)` signatures retain their behavior without a signature declaration.

## Boolean and tuple query outcomes

A query returning `bool` (including a named Boolean type) uses literal arms:

```text
query transferred;

rule Transfer:
  $request
  => query transferred $request {
       true => Settled;
       false => Live;
     };
```

Both arms are required exactly once. Wildcards and refinements are errors.
Named query captures remain available, with the hook's exact Boolean type.
Queries still execute once when reached, after matching and guards succeed.

A Go function returning multiple values can bind its result domain directly:

```go
func classify(instruction Instruction) (Status, Reason)
```

```text
query classify;
union Decision = (Status, Reason) tag 0 {
  Live = statusLive { reason:1; };
  Settled = statusSettled { reason:1; };
  Unknown = statusUnknown { reason:1; };
};
```

Type names are local or imported non-generic named types, optionally pointers.
Indices are zero-based result positions. The discriminator must be an integer,
string or Boolean; each variant binds an exact typed Go constant, or `true` /
`false` for Boolean discriminators. Payloads refer to other result positions.
Duplicate tags and invalid positions are errors. Query decisions use these
constructors and must cover every declared variant, just like struct unions.

Tuple storage is generated internally; no handwritten result struct is needed.
A named capture denotes the complete tuple, and can be returned when its types
match the target's native outputs. Invalid runtime tags stop the selected
continuation immediately. They never retry another rule or roll back a budget.

## Field projections and forwarding bindings

A captured value supports checked read-only field paths:

```text
$request.obligation.target
$host.evidence
```

These work in query arguments, guards, immutable locals and construction.
Fields must be accessible from the host package. Methods, indexing, arbitrary Go
expressions and implicit calls are excluded. Pointer projections have ordinary
Go nil behavior; a field path does not establish that its pointers are non-nil.

Forwarding declarations express argument selection and context explicitly:

```text
query transferred($instruction, $obligation) =
  transfersResource $evidence $instruction $obligation.target;
```

The left side defines positional value parameters. The right side names an
existing Go function or legacy host method; qualified imported functions use the
binding file's actual import aliases. `Classifier.method` binds a local Go
receiver method as an explicit method expression: pass its receiver as the first
argument. Value methods take `Classifier`; pointer-only methods take
`*Classifier`. This also works for matchers, guards and builders. It adds no
implicit state or invocation before the binding's ordinary execution phase.
Its arguments are captures, projections,
constants or literals. Parameter captures are substituted, while other captures
(such as `$evidence`) come from the calling rule's lexical scope. Every invocation
is type checked. Forwarded arguments cannot hide nested calls or query execution.
`query` remains explicit at the call site. `build` and `guard` also support value
forwarding; matchers and constants do not.

## Native generated function signatures

Declare the Go function type in the binding file as usual:

```go
type Signature func(*Evidence, Probe, Instruction, Obligation) (Status, Reason)
```

Then opt into a total decision with an explicit failure result:

```text
signature total fallback Unknown reasonNone;

rule Classify:
  inputs $evidence $probe $instruction $obligation
  => query transferred $instruction $obligation {
       true => Settled reasonTransferred;
       false => Live reasonNone;
     };
```

`inputs` matches the complete parameter list. `outputs` constructs the result
list, for example `outputs statusUnknown reasonNone`. A matching tuple union
constructor can construct that list as well. Positional fields are always
available; named fields use Go parameter/result names, or `value0`, `value1`, ...
for unnamed slots. `inputs` and `outputs` are reserved in a native ruleset.

A total fallback runs on unmatched input or invalid query/schema outcomes. It
must construct the declared outputs without rule captures or further queries.
This makes conservative failure policy visible in the rules. It is distinct
from trying another rule after an effectful query.

For a recognizer with a success flag, use `signature partial;` and declare a
final Go `bool` after the output values. Failed recognition returns zero outputs
and false; matched recognition returns the outputs and true. In a total API a
final Boolean is ordinary data. Multiple parameters and results need no request,
host or result wrapper types. At least one parameter and one output are required;
generic and variadic APIs are not supported. Legacy implicit host methods apply
to the legacy signature; native rules pass context as explicit captures.

## Read-only views

A view generates structural extraction from existing storage:

```text
view Assertion = *ssa.TypeAssert {
  value:X;
  commaOK:CommaOk;
};

rule AssertedValue:
  Assertion value:$value commaOK:false
  => $value;
```

Views may name local or imported structs, or pointers to them, and select a
subset of accessible direct fields. They use the same named/positional pattern
syntax as records. Interface inputs use a checked Go type assertion. Incompatible
types are compilation errors; other dynamic types and nil pointers fail the
match. Promoted fields must be read through an explicit captured field path.

Views cannot construct or update values. They add no mutation, recursive
traversal, implicit unwrapping or semantic proof. Pure extraction can be shared
by the matching graph, while ordered query calls remain separate continuations.

A whole view capture, such as `$deferred:(Deferred)`, binds the concrete storage
value that passed the assertion. A hook taking `*ssa.Defer` can therefore use
`$deferred` directly even when the original input was `ssa.Instruction`. Identity
is preserved. View fields and positional arity are unchanged; the concrete
whole value is not an additional field. Repeated whole captures compare the
asserted values under the ordinary identical/comparable capture contract.

For alternatives with different storage types, bind the surrounding `or` when
the whole capture should retain the original input interface type instead.

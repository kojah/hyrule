# Hyrule

Hyrule compiles ordered structural rules into ordinary typed Go calls. It lives
under `tools/hyrule` and has no Lockdoc analyzer dependencies. Generated code
has no Hyrule runtime dependency. The pipeline example demonstrates both lifting and lowering. Production rules
live under `internal/lower/memory`: shared loads/stores, field/index projections
stay in that package; `mapop`, `interfaceop`, `channelop` and `sliceop` own their typed family operations and original-source projections.
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
`$name` binds a pattern value and references it on the right; `$name:(Op ...)`
binds the entire input to that subpattern. `_` ignores a pattern value.
Parenthesized calls nest. Literals use Go spelling for numbers, strings, raw
strings, `true`, `false`, and `nil`; negative numeric literals are supported.
A rule has at most one guard call after `&&`; implement compound predicates in Go.
Declarations and rules end with `;`. Line and block comments are accepted.

Files are sorted lexically; rules retain source order within each file. The first
successful rule wins. Unknown operations, failed matchers and false guards fall
through; exhausting the rules returns the zero output and `false`. There is no
implicit traversal or fixed-point iteration.

Repeated captures compare concrete comparable values using Go equality. Types
containing interfaces are rejected because their dynamic values could make `==`
panic. Use distinct captures and a typed equality guard for those cases. Matching
does not infer structural equality, aliasing, pointer-analysis facts or semantics.

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

Match and guard callbacks should be read-only. Builders should construct complete
results; callers apply their effects after matching. Hyrule cannot prove callback
purity. Host code owns memory identity, soundness, unknown-value policy, lifecycle,
control flow, budgets and scheduling. No effects should be applied speculatively
inside matchers or guards.

Generated functions use typed locals and per-rule helpers, with no term boxing,
reflection or runtime capture maps. The initial compiler does not share matching
work across rules. Its output is easy to inspect before considering dispatch
optimization. Hook errors and malformed patterns carry source locations; Go type
errors identify generated code and its nearby rule comment.

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
so the existing architecture workflow checks committed generated code. Normal
builds compile committed code without invoking the generator.

The public library surface is `Load`, `Config.Compile`, `Config.Publish`, and
`Format`. The CLI is a small adapter. Syntax and Go emission are private packages.
To extract Hyrule later, move this directory into its own module, change internal
import prefixes, and retain the public contract tests. The only external library
dependencies are YAML configuration decoding and Go package loading.

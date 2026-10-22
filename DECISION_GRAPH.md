# Ordered decision graph

Hyrule compiles resolved patterns into `internal/matchir` before emitting Go.
`internal/compiler` owns source priority, reusable-pattern expansion and OR
alternatives. The Go adapter resolves hook signatures and capture types, then
supplies opaque hook/type/literal/expression IDs to the graph. The graph builder owns canonical extraction value IDs, shared prefixes and
explicit success/fallback edges. The hash-consing pass consumes that completed
IR and returns a new graph of the same type, without mutating its input. Go syntax and `go/types` stay in the adapter. Go is the only implemented
backend; another backend needs its own hook/type resolution and source emitter.

Matchers and guards are pure by language contract, without annotations. Their
inputs and host snapshot must remain stable during matching. Hyrule cannot
verify arbitrary callback purity. Builders may allocate and are never shared.
Graph construction folds adjacent identical matcher/test prefixes. A separate
bottom-up `matchir.HashCons` pass then canonicalizes structurally identical pure
decisions before emission. It does
not search past intervening predicates, reorder partial guards, infer operation
kind exclusivity for opaque callbacks. Declared union schemas do permit
structural switch dispatch. Guard expression IDs
are currently distinct, so the resolver does not deduplicate guard calls.
Expression IDs now name resolved `valueir` plans, never semantic expression nodes
or capture-name maps. Committed query/choice continuations are also resolved
before emission and remain outside the pure matching graph.

Failure of a shared extraction skips its entire group. On success, child
attempts run in source priority; an exhausted child rejoins the shared fallback.
OR variants remain adjacent attempts, including guard failure. Replacement
builders execute only on the winning path. A terminal builder retains an
unreachable suffix for static validation; the Go backend emits that suffix in
an `if false` block before the return so invalid unreachable rules still fail
Go type checking. There is no runtime graph interpreter.

## Bottom-up hash-consing

The pass visits successors before parents and interns pure decisions using their
kind, operands, hook/expression IDs, capture IDs, schema identity, and canonical
success/fallback/case edges. Ordered dispatch cases remain ordered. Value IDs
must belong to one identity namespace; the Go adapter runs the pass separately
for each graph, before any variant dispatch combines separately numbered graphs.

Construction terminals and evaluations stay distinct, retaining rule diagnostics
and fresh allocation identity. Query continuations remain behind those terminals
and are not inspected or memoized. Shared pure nodes retain the first visited
origin, consistent with prefix sharing. The input graph is immutable; the pass
clones retained nodes and rejects cycles with an iterative traversal.

This is structural canonicalization, not globally optimal decision-tree search.
The current Go backend emits structured branches and common fallbacks; it does
not introduce join labels for every shared suffix. Distinct guard expression IDs
and construction leaves also limit opportunities in current production graphs.
No generated-program speedup is claimed from hash-consing alone.

`BenchmarkHashConsRepeatedSuffix` is a synthetic 64-branch, eight-test-suffix
workload. On the development EPYC 9645 it measured approximately 151 microseconds,
43.7 KB and 54 allocations per pass (100 ms sample). This measures compiler work,
not generated runtime behavior. Reproduce with:

```sh
agent-test go test ./tools/hyrule/internal/matchir -run '^TestHashCons' \
  -bench BenchmarkHashConsRepeatedSuffix -benchtime=100ms
```

## Measurements

Baseline: `d4865b4a`, the generator with per-rule helpers. Both versions used the
same benchmark inputs and public plan APIs, Go 1.27 on an AMD EPYC 9645, four Go
processors, three 200 ms samples per case. Values below are sample medians.
The baseline and graph were built in isolated checkouts; analyzer changes in
the shared working tree were excluded.

| Production plan API workload | Before ns/op | Graph ns/op | Before/graph | Allocations/op, both |
| --- | ---: | ---: | ---: | ---: |
| Slice append reuse | 138.70 | 13.83 | 10.03× | 1 (1 byte) |
| Slice append growth | 167.80 | 14.27 | 11.76× | 1 (1 byte) |
| Slice view | 98.70 | 16.04 | 6.15× | 1 (1 byte) |
| Map replacement | 95.27 | 32.80 | 2.90× | 0 |
| Map insertion | 117.50 | 34.39 | 3.42× | 0 |

The small `testdata/decisionbench` slice-state fixture retains the actual legacy
generator's output in `legacy_test.go.txt`. `make generate-go` copies it to the
ignored `legacy_test.go` and regenerates the graph version. Its
counter is observational benchmark instrumentation and never affects decisions.
It checks both versions on 243 inputs before benchmarking.

| Fixture workload | Matcher calls before → graph | Median ns/op before → graph |
| --- | ---: | ---: |
| Miss | 3 → 1 | 6.684 → 2.099 |
| Empty | 2 → 2 | 3.699 → 2.279 |
| Reuse | 4 → 2 | 9.486 → 2.304 |
| Growth | 6 → 2 | 8.161 → 2.610 |

Both fixture versions allocate zero bytes. Generated fixture source shrank from
1,410 to 640 bytes. Across the 31 targets present at the baseline, generated
source shrank from 75,529 to 42,488 bytes (3,447 to 1,883 lines). This includes
removing helper/dispatcher boilerplate as well as sharing extractions; it does
not isolate the contribution of each optimization.

These short microbenchmarks measure plan dispatch, not full model construction,
solving, search, compiled machine-code size or end-to-end NATS performance.
They are not whole-analyzer acceptance or evidence that the earlier slowdown
is fixed. Repeat on representative full workloads before making that claim.

Run from the repository root, through `agent-test` on Xenia:

```sh
make generate-go
go test -p=1 -parallel=4 ./tools/hyrule/... ./tools/hyrule/testdata/decisionbench
go test -p=1 -parallel=4 -run '^$' -bench BenchmarkDecisions -benchtime=200ms -count=3 ./tools/hyrule/testdata/decisionbench
go test -p=1 -parallel=4 -run '^$' -bench 'Benchmark(Header|Map)RulePlans' -benchtime=200ms -count=3 ./internal/lower/memory/sliceheader ./internal/lower/mapstate
```

For a fresh production baseline, copy only the two public benchmark test files
onto `d4865b4a` in an isolated checkout and run the same command. The fixture's
legacy file was produced by that revision's CLI using the same bindings and
rules, with generated function `Legacy`. Its graph config generates `Optimized`.

## Hook audit

The 43 working-tree targets were enumerated: 126 distinct matchers and 28 guards.
No direct analyzer input/host/external writes, budget consumption, I/O, time or
randomness were found. This source audit is not proof of transitive purity.
Go's type APIs can lazily memoize cold metadata: `Implements`, `Unalias` and
`Named.Underlying` are examples. No cold production instance was established.
Hosts must supply a stable type/fact snapshot; the optimizer relies on that
contract. In particular, purity does not remove guard preconditions: assertion
checks still precede guards that require a supported non-nil dynamic type.

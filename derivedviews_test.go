package hyrule_test

import (
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

const derivedViewHost = `package fixture
import "strings"
var _ strings.Builder
type Capacity struct { Kind uint8; Bound int }
type Inner struct { Depth int }
type Outer struct { *Inner }
type Snapshot struct { Capacity Capacity; Fill int; Closed bool; Inner; Next *Snapshot; Outer Outer }
type Node interface{ node() }
type Leaf struct{ Value int }
func (*Leaf) node() {}
type Pair struct{ Left, Right Node }
func (Pair) node() {}
func Width(pair Pair) int { if pair.Left == nil { return 9 }; return 7 }
`

func derivedViewConfig(t *testing.T, root string) {
	t.Helper()
	write(t, root, "hyrule.yaml", `version: 1
targets:
 - name: nodes
   package: .
   bindings: bindings.go
   function: Nodes
   rules: [nodes.rules]
   out: nodes_gen.go
 - name: snapshots
   package: .
   bindings: bindings.go
   function: Snapshots
   rules: [snapshots.rules]
   out: snapshots_gen.go
`)
}

// Patterns can name Go types directly: struct fields, promoted fields,
// pointers and concrete implementers of an interface input need no view.
func TestDerivedViews(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", derivedViewHost)
	derivedViewConfig(t, root)
	write(t, root, "nodes.rules", `fn rewrite(arg0: Node) -> int

@Leaf
rewrite (Leaf (Value: value)) = value

@Leaves
rewrite (Pair (Left: Leaf (Value: left), Right: Leaf (Value: right))) = left + right

host
  Width

@Pair
rewrite pair@(Pair ()) = Width pair
`)
	write(t, root, "snapshots.rules", `fn rewrite(arg0: Snapshot) -> int

@Next
rewrite (Snapshot (Next: Snapshot (Fill: fill))) = fill * 100

@Room
rewrite (Snapshot (Capacity: Capacity (Bound: bound), Fill: fill, Depth: depth, Closed: false))
  | fill < bound
  = fill + depth

@Closed
rewrite (Snapshot (Closed: true)) = -1
`)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestViews(t *testing.T) {
 var nilLeaf *Leaf
 for _, test := range []struct{input Node; want int; ok bool}{
  {&Leaf{3}, 3, true},
  {nilLeaf, 0, false},
  {Pair{&Leaf{1}, &Leaf{2}}, 3, true},
  {Pair{&Leaf{1}, nilLeaf}, 7, true},
  {Pair{nil, &Leaf{1}}, 9, true},
  {nil, 0, false},
 } {
  if got, ok := Nodes(test.input); ok != test.ok || got != test.want { t.Fatalf("%#v => %d %t", test.input, got, ok) }
 }
 for _, test := range []struct{input Snapshot; want int; ok bool}{
  {Snapshot{Next: &Snapshot{Fill: 2}}, 200, true},
  {Snapshot{Capacity: Capacity{Bound: 3}, Fill: 1, Inner: Inner{4}}, 5, true},
  {Snapshot{Capacity: Capacity{Bound: 1}, Fill: 1}, 0, false},
  {Snapshot{Closed: true}, -1, true},
 } {
  if got, ok := Snapshots(test.input); ok != test.ok || got != test.want { t.Fatalf("%+v => %d %t", test.input, got, ok) }
 }
}`)
	runUnionFixture(t, root, "TestViews")
}

func TestDerivedViewDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, pattern, want string }{
		{"unknown field", "(Snapshot (Missing: x))", "unknown Go field Missing"},
		{"positional", "(Snapshot x)", "requires named Go fields"},
		{"embedded pointer", "(Snapshot (Outer: Outer (Depth: x)))", "promoted through embedded pointer Inner"},
		{"foreign unexported", "(Snapshot (Capacity: strings.Builder (addr: x)))", "does not match input type"},
		{"wrong type", "(Snapshot (Capacity: Inner (Depth: x)))", "does not match input type"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", derivedViewHost)
			derivedViewConfig(t, root)
			write(t, root, "nodes.rules", "fn rewrite(arg0: Node) -> int\n\nrewrite _ = 0\n")
			write(t, root, "snapshots.rules", "fn rewrite(arg0: Snapshot) -> int\n\n@Check\nrewrite "+test.pattern+" = 0\n")
			expectUnionCompile(t, root, test.want)
		})
	}
}

const constantHost = `package fixture
type Action uint8
const (
 UnknownAction Action = iota
 Send
 Receive
)
type CapacityKind uint8
const (
 NilCapacity CapacityKind = iota
 FiniteCapacity
 UnboundedCapacity
)
type Capacity struct { Kind CapacityKind; Bound int }
type Request struct { Action Action; Capacity Capacity }
`

// Typed Go constants match by name with Go equality. Applied to a struct
// whose unique field has the constant's type, they match a tagged variant.
func TestTypedConstantPatterns(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", constantHost)
	signatureConfig(t, root, "")
	write(t, root, "input.rules", `fn rewrite(arg0: Request) -> int

@FiniteSend
rewrite (Request (Action: Send, Capacity: FiniteCapacity (Bound: bound))) = bound

@NilReceive
rewrite (Request (Action: Receive(), Capacity: NilCapacity())) = -1

@Unbounded
rewrite (Request (Capacity: c@UnboundedCapacity)) = -2

@Other
rewrite (Request (Action: action))
  | action == UnknownAction
  = -3
`)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestConstants(t *testing.T) {
 for _, test := range []struct{input Request; want int; ok bool}{
  {Request{Send, Capacity{FiniteCapacity, 4}}, 4, true},
  {Request{Send, Capacity{NilCapacity, 4}}, 0, false},
  {Request{Receive, Capacity{NilCapacity, 4}}, -1, true},
  {Request{Receive, Capacity{UnboundedCapacity, 0}}, -2, true},
  {Request{Send, Capacity{CapacityKind(9), 4}}, 0, false},
  {Request{Action(7), Capacity{CapacityKind(9), 4}}, 0, false},
 } {
  if got, ok := Rewrite(test.input); ok != test.ok || got != test.want { t.Fatalf("%+v => %d %t", test.input, got, ok) }
 }
}`)
	runUnionFixture(t, root, "TestConstants")
}

func TestTypedConstantDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, pattern, want string }{
		{"fields on a plain constant", "(Request (Action: Send (Bound: 1)))", "constant pattern Send takes no fields"},
		{"no tag field", "(FiniteCapacity (Bound: b))", "constant FiniteCapacity does not match input type"},
		{"positional variant", "(Request (Capacity: FiniteCapacity b))", "requires named Go fields"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", constantHost)
			signatureConfig(t, root, "")
			write(t, root, "input.rules", "fn rewrite(arg0: Request) -> int\n\n@Check\nrewrite "+test.pattern+" = 0\n")
			expectUnionCompile(t, root, test.want)
		})
	}
}

const closedHost = `package fixture
import "fmt"
var _ fmt.Stringer
type Name struct{ Text string }
func (n *Name) String() string { return n.Text }
type Code struct{ Value int }
func (c Code) String() string { return "code" }
type Other struct{}
func (Other) String() string { return "other" }
`

// Interface patterns admit compatible concrete types without a separate list.
// Other dynamic types and typed nil fall through; adjacent alternatives share
// one type switch.
func TestInterfacePatternsWithoutImplementerLists(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", closedHost)
	signatureConfig(t, root, "")
	write(t, root, "input.rules", `fn rewrite(arg0: fmt.Stringer) -> string

@Name
rewrite (Name (Text: text)) = text

@Code
rewrite (Code (Value: 0)) = "zero"
`)
	config, artifacts := compile(t, root)
	if !strings.Contains(string(artifacts[0].Data), ".(type)") {
		t.Fatalf("closed alternatives do not share a type switch:\n%s", artifacts[0].Data)
	}
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import ("fmt"; "testing")
func TestClosed(t *testing.T) {
 var nilName *Name
 for _, test := range []struct{input fmt.Stringer; want string; ok bool}{
  {&Name{"n"}, "n", true}, {nilName, "", false}, {Code{0}, "zero", true}, {Code{1}, "", false},
  {Other{}, "", false}, {nil, "", false},
 } {
  if got, ok := Rewrite(test.input); ok != test.ok || got != test.want { t.Fatalf("%#v => %q %t", test.input, got, ok) }
 }
}`)
	runUnionFixture(t, root, "TestClosed")
}

func TestInterfacePatternRejectsIncompatibleType(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", closedHost+"\ntype Plain struct{ Text string }\n")
	signatureConfig(t, root, "")
	write(t, root, "input.rules", `fn rewrite(value: fmt.Stringer) -> string
  match value
    Plain(Text: text) -> text
`)
	expectUnionCompile(t, root, "does not match input type")
}

func TestRetiredImplementersDeclaration(t *testing.T) {
	if _, err := hyrule.Format("input.rules", []byte("implementers fmt.Stringer = Code\n")); err == nil {
		t.Fatal("accepted retired implementers declaration")
	}
}

// Rules may use import names from any handwritten file of the package when
// the binding file does not import them and the name is unambiguous.
func TestPackageImportNames(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", "package fixture\n")
	write(t, root, "other.go", "package fixture\nimport \"go/token\"\ntype Op struct{ Kind token.Token }\n")
	signatureConfig(t, root, "")
	write(t, root, "input.rules", "fn rewrite(arg0: Op) -> bool\n\n@Add\nrewrite (Op (Kind: token.ADD)) = true\n")
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import ("go/token"; "testing")
func TestImports(t *testing.T) {
 if got, ok := Rewrite(Op{token.ADD}); !ok || !got { t.Fatal("ADD") }
 if _, ok := Rewrite(Op{token.SUB}); ok { t.Fatal("SUB") }
}`)
	runUnionFixture(t, root, "TestImports")
}

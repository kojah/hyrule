package hyrule_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/hyrule"
)

var updateReadability = flag.Bool("update-readability", false, "update reviewed generated-output goldens")

// These few reviewed examples specify readable public output across records,
// native APIs, embedded Go, closed unions and ordered host queries.
func TestReadableOutput(t *testing.T) {
	for _, fixture := range []struct{ name, config, target string }{
		{"records", "examples/localplans/hyrule.yaml", "append_plan"},
		{"native", "examples/inlinego/hyrule.yaml", "resolve"},
		{"queries", "examples/transitions/hyrule.yaml", "transitions"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			config, err := hyrule.Load(fixture.config)
			if err != nil {
				t.Fatal(err)
			}
			artifacts, err := config.Compile(context.Background(), fixture.target)
			if err != nil {
				t.Fatal(err)
			}
			if len(artifacts) != 1 {
				t.Fatalf("got %d artifacts", len(artifacts))
			}
			readabilityGolden(t, fixture.name, artifacts[0].Data)
		})
	}
}

func readabilityGolden(t *testing.T, name string, source []byte) {
	t.Helper()
	path := filepath.Join("testdata", "readability", name+".golden")
	if *updateReadability {
		if err := os.WriteFile(path, source, 0644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(golden, source) {
		t.Fatalf("generated output differs from reviewed %s golden:\n%s", name, source)
	}
}

func TestReadableNativeQueries(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", nativeHost+`func mark(h *Host,n int)int{h.Calls=append(h.Calls,"mark");return n}`)
	write(t, root, "input.rules", doRules)
	_, artifacts := compile(t, root)
	readabilityGolden(t, "native-queries", artifacts[0].Data)
}

func TestReadableNamesAndStableProvenance(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Request struct { Type, Value int }
 type Signature func(request Request) (int, bool)
 func Sum(a,b int) int { return a+b }
 `)
	rules := `type Request
 host
   sum = Sum

 @Positive
 rewrite (Request { type: typeValue, value })
   | typeValue > 0
   = sum typeValue value

 @Fallback
 rewrite _ = 0
 `
	rules = strings.ReplaceAll(rules, "\n ", "\n")
	write(t, root, "input.rules", rules)
	_, first := compile(t, root)
	if !strings.Contains(string(first[0].Data), "typeValue") || strings.Contains(string(first[0].Data), "hv1") {
		t.Fatalf("missing readable names:\n%s", first[0].Data)
	}
	write(t, root, "input.rules", "\n\n"+rules)
	_, second := compile(t, root)
	if !bytes.Equal(first[0].Data, second[0].Data) {
		t.Fatal("moving labeled rules changed output")
	}
	if bytes.Contains(first[0].Data, []byte(root)) {
		t.Fatal("output contains checkout-specific path")
	}
	if !bytes.Contains(first[0].Data, []byte("@v"+hyrule.Version+" generate")) {
		t.Fatal("missing reproducible regeneration command")
	}
}

func TestReadableEmissionPreservesObservableBehavior(t *testing.T) {
	for _, example := range []struct{ name, host, rules, behavior string }{
		{"snapshot and hook collision", `package fixture
 type Cell struct { Value int }
 type Host struct { Cell *Cell; Calls int }
 type Signature func(cell *Cell, host *Host) (int,bool)
 func Read(cell *Cell)(int,bool){if cell==nil{return 0,false};return cell.Value,true}
 func value(n int) int { return n+1 }
 func(h *Host) Mutate(n int)bool{h.Calls++;h.Cell.Value=99;return n>0}
 `, `host
  read? = Read
  mutate! = Mutate
  adjusted = value

@Snapshot
rewrite (read value) =
  case mutate value of
    true -> adjusted value
    false -> 0

@NoInput
rewrite _ = -1
`, `cell:=&Cell{Value:7};host:=&Host{Cell:cell}
 got,ok:=Rewrite(cell,host)
 if !ok||got!=8||cell.Value!=99||host.Calls!=1 {t.Fatalf("snapshot/call order: %d,%v %+v %+v",got,ok,cell,host)}
 got,ok=Rewrite(nil,host)
 if !ok||got!=-1||host.Calls!=1 {t.Fatal("matching failure ran a query")}
`},
		{"native arithmetic and interface", `package fixture
 type Signature func(value int8)(any,int8,bool)
`, `partial fn rewrite(input)

@Typed
rewrite (inputs value) = outputs value (value + 1)
`, `boxed,incremented,ok:=Rewrite(127)
 if !ok||boxed!=int8(127)||incremented!=int8(-128){t.Fatalf("typed result: %T %v %v %v",boxed,boxed,incremented,ok)}
`},

		{"host fields keep their spelling", `package fixture
 type Node struct { hv1 int; Type int }
 type Signature func(node Node)(Node,bool)
`, `type Node

@Fields
rewrite (Node { hv1: value, type: kind }) = Node { hv1: value, type: kind }
`, `input:=Node{hv1:7,Type:3};got,ok:=Rewrite(input)
 if !ok||got!=input {t.Fatalf("host fields changed: %+v %v",got,ok)}
`},
		{"embedded Go keeps private-looking locals", `package fixture
 type Node struct { N int }
 type Signature func(node Node)(int,bool)
`, `@Embedded
rewrite node = go! { func(hv1 int) int { local := hv1 + 1; return local }(node.N) }
`, `got,ok:=Rewrite(Node{N:7});if !ok||got!=8 {t.Fatalf("embedded locals changed: %d %v",got,ok)}
`},
		{"unused view projections", `package fixture
 type A struct{ N int };type B struct{ N int }
 type Signature func(any)(int,bool)
`, `view First = A
  n: N

view Second = B
  n: N

@One
rewrite (First {}) = 1

@Two
rewrite (Second {}) = 2
`, `for index,input:=range []any{A{N:9},B{N:9}}{got,ok:=Rewrite(input);if !ok||got!=index+1{t.Fatalf("%v: %d %v",input,got,ok)}}
 if _,ok:=Rewrite(nil);ok{t.Fatal("unknown input admitted")}
`},
		{"validator names avoid host and schema collisions", `package fixture
 type Kind int
 const Yes Kind = 0
 type Proof struct{ Kind Kind }
 type First struct{ Proof Proof };type Second struct{ Proof Proof }
 type Input struct{ A First;B Second }
 type Signature func(Input)(int,bool)
 var Wrong int
 func hyruleRewriteValidBox(First)bool{Wrong++;return true}
`, `type Proof = Proof
  tag Kind
  Valid = Yes

type Box = First
  proof: Proof

type box = Second
  proof: Proof

type Input = Input
  a: A
  b: B

@Valid
rewrite input = 1
`, `if got,ok:=Rewrite(Input{});!ok||got!=1{t.Fatal("valid input rejected")}
 for _,input:=range []Input{{A:First{Proof{Kind:99}}},{B:Second{Proof{Kind:99}}}}{if _,ok:=Rewrite(input);ok{t.Fatal("invalid input admitted")}}
 if Wrong!=0{t.Fatal("handwritten helper shadowed generated validation")}
`},
		{"embedded map keys remain variable references", `package fixture
 type Signature func(int)(map[int]int,bool)
 func n(){}
`, `@Map
rewrite n = go! { map[int]int{n: n + 1} }
`, `got,ok:=Rewrite(7);if !ok||got[7]!=8||len(got)!=1{t.Fatalf("map keys changed: %v %v",got,ok)}
`},
		{"embedded struct keys remain field names", `package fixture
 type Box struct{ n int }
 type Signature func(int)(Box,bool)
`, `@Struct
rewrite n = go! { Box{n: n + 1} }
`, `got,ok:=Rewrite(7);if !ok||got.n!=8{t.Fatalf("struct keys changed: %+v %v",got,ok)}
`},
		{"inlined Go keeps free package names", `package fixture
 var hv1=11
 var harg0=Node{N:13}
 type Node struct{N int}
 type Plan struct{N int}
 type Signature func(Node)(Plan,bool)
`, `partial fn rewrite(input)

type Node

@Global
rewrite (inputs (Node n)) = outputs (go! { Plan{N: n + hv1 + harg0.N} })
`, `got,ok:=Rewrite(Node{N:7});if !ok||got.N!=31{t.Fatalf("global name captured: %+v %v",got,ok)}
`},
		{"named map keys retain typed scope", `package fixture
 type Table map[int]int
 type Signature func(int)(Table,bool)
 func n(){}
`, `@Map
rewrite n = go! { Table{n: n + 1} }
`, `got,ok:=Rewrite(7);if !ok||got[7]!=8||len(got)!=1{t.Fatalf("named map keys changed: %v %v",got,ok)}
`},
		{"typed nil", `package fixture
 type Signature func(pointer *int)(any,bool)
`, `partial fn rewrite(input)

@TypedNil
rewrite (inputs pointer) = outputs pointer
`, `boxed,ok:=Rewrite(nil)
 if !ok||boxed==nil {t.Fatal("lost typed nil")}
 if pointer,valid:=boxed.(*int);!valid||pointer!=nil {t.Fatalf("got %T %v",boxed,boxed)}
`},
	} {
		t.Run(example.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", example.host)
			write(t, root, "input.rules", example.rules)
			config, artifacts := compile(t, root)
			if err := config.Publish(artifacts, false); err != nil {
				t.Fatal(err)
			}
			write(t, root, "behavior_test.go", "package fixture\nimport \"testing\"\nfunc TestBehavior(t *testing.T){"+example.behavior+"}\n")
			runGeneratedTransitionTest(t, root)
		})
	}
}

func TestReadableArgumentsKeepWrittenOrder(t *testing.T) {
	for _, example := range []struct{ name, build string }{
		{"named", `Plan { ready: first n && second n, count: count n }`},
		{"positional", `build (first n && second n) (if n >= 0 then count n else count 0)`},
	} {
		t.Run(example.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", `package fixture
 type Node struct { N int }
 type Plan struct { Count int; Ready bool }
 type Host struct { Calls []string }
 type Signature func(Node,*Host)(Plan,bool)
 // Calls observe evaluation order only; they do not affect the answers.
 func(h *Host) First(n int)bool{h.Calls=append(h.Calls,"first");return n>0}
 func(h *Host) Second(n int)bool{h.Calls=append(h.Calls,"second");return true}
 func(h *Host) Count(n int)int{h.Calls=append(h.Calls,"count");return n}
 func Build(ready bool,count int)Plan{return Plan{Count:count,Ready:ready}}
 `)
			write(t, root, "input.rules", `type Node = Node
  n: N

type Plan = Plan
  count: Count
  ready: Ready

host
  first = First
  second = Second
  count = Count
  build = Build

@Order
rewrite (Node n) = `+example.build+"\n")
			config, artifacts := compile(t, root)
			if err := config.Publish(artifacts, false); err != nil {
				t.Fatal(err)
			}
			write(t, root, "behavior_test.go", `package fixture
import("testing";"reflect")
func TestBehavior(t *testing.T){for _,n:=range []int{-1,0,1}{
 host:=&Host{};got,ok:=Rewrite(Node{N:n},host)
 wantCalls:=[]string{"first","count"};if n>0{wantCalls=[]string{"first","second","count"}}
 wantCount:=n;if n<0 && "`+example.name+`"=="positional"{wantCount=0}
 if !ok||got.Ready!=(n>0)||got.Count!=wantCount||!reflect.DeepEqual(host.Calls,wantCalls){t.Fatalf("n=%d got=%+v,%v calls=%v",n,got,ok,host.Calls)}
}}`)
			runGeneratedTransitionTest(t, root)
		})
	}
}

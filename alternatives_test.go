package hyrule_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/hyrule"
)

const alternativesRules = `total fn rewrite(input)
  fallback outputs 0 (-1)

view Entry = *goast.Ident
  name: Name

host
  ready! name = Ready host name
  ask! n = Ask host n host.Offset.Value
  mark

type Proof = (Kind, int)
  tag 0
  Proven (reason: 1) = Yes
  Disproven = No
  Unavailable (reason: 1) = Unknown

@Main
rewrite (inputs (Entry { name }) n host) =
  case ask n of
    Proven { reason: why } -> outputs (mark host why) 1
    Disproven() -> decline
    Unavailable() -> decline
  <|> case ready name of
    false -> outputs (mark host 10) 2
    true -> decline
  <|> case ask host.Offset.Value of
    Unavailable { reason: why } -> outputs (mark host why) 3
    Proven() -> decline
    Disproven() -> decline
  <|> outputs (mark host 20) 4
`

func TestOrderedAlternatives(t *testing.T) {
	for _, mode := range []string{"total", "partial"} {
		t.Run(mode, func(t *testing.T) {
			root := fixture(t)
			host := nativeHost + `
func mark(h *Host,n int)int{h.Calls=append(h.Calls,"mark");return n}
`
			rules := alternativesRules
			if mode == "partial" {
				host = replaceFixture(t, host, "(int, int)", "(int, int, bool)")
				rules = replaceFixture(t, rules, `total fn rewrite(input)
  fallback outputs 0 (-1)
`, `partial fn rewrite(input)
`)
			}
			formatted, err := hyrule.Format("input.rules", []byte(rules))
			if err != nil {
				t.Fatal(err)
			}
			again, err := hyrule.Format("input.rules", formatted)
			if err != nil || string(again) != string(formatted) {
				t.Fatalf("format roundtrip: %v\n%s\n%s", err, formatted, again)
			}
			write(t, root, "bindings.go", host)
			write(t, root, "input.rules", string(formatted))
			c, artifacts := compile(t, root)
			if err := c.Publish(artifacts, false); err != nil {
				t.Fatal(err)
			}
			invocation, failure := "a,b:=Rewrite(input,tt.n,h);ok:=tt.ok", "-1"
			if mode == "partial" {
				invocation, failure = "a,b,ok:=Rewrite(input,tt.n,h)", "0"
			}
			write(t, root, "behavior_test.go", `package fixture
import("go/ast";"reflect";"testing")
func TestBehavior(t *testing.T){
 for _,tt:=range []struct{name string;n,offset,a,b int;calls []string;ok bool}{
 {"ok",0,2,9,1,[]string{"ask","mark"},true},
 {"skip",1,2,10,2,[]string{"ask","ready","mark"},true},
 {"ok",1,2,9,3,[]string{"ask","ready","ask","mark"},true},
 {"ok",1,1,20,4,[]string{"ask","ready","ask","mark"},true},
 {"ok",99,2,0,`+failure+`,[]string{"ask"},false},
 {"ok",1,99,0,`+failure+`,[]string{"ask","ready","ask"},false},
 }{
 h:=&Host{};h.Offset.Value=tt.offset;input:=&ast.Ident{Name:tt.name};`+invocation+`
 if a!=tt.a||b!=tt.b||ok!=tt.ok||!reflect.DeepEqual(h.Calls,tt.calls){t.Fatalf("%+v: %d,%d,%v calls %v",tt,a,b,ok,h.Calls)}
 }
}`)
			runGeneratedTransitionTest(t, root)
		})
	}
}

func TestOrderedAlternativeDiagnostics(t *testing.T) {
	for _, tt := range []struct{ name, old, replacement, want string }{
		{"fallback required", "  <|> outputs (mark host 20) 4", "", "final pure fallback"},
		{"early answer", "case ready name of\n    false -> outputs (mark host 10) 2\n    true -> decline", "outputs 0 0", "ordered alternative requires"},
		{"fresh payload", "Proven { reason: why }", "Proven { reason: n }", "fresh captures"},
		{"no refinement", "Proven { reason: why }", "Proven { reason: 7 }", "fresh captures"},
		{"wrong variant", "Proven { reason: why }", "Missing { reason: why }", "decision arm must name a variant"},
		{"bool selection", "false -> outputs (mark host 10) 2", "_ -> outputs (mark host 10) 2", "Boolean decision arms"},
		{"scope", "outputs (mark host 20) 4", "outputs why 4", "unbound capture"},
		{"typed answers", "outputs (mark host 10) 2", "outputs true 2", "cannot use"},
		{"query fallback", "outputs (mark host 20) 4", "case ready name of\n    true -> outputs 1 1\n    false -> outputs 2 2", "final pure fallback"},
		{"exhaustive remains", "    true -> decline\n", "", "require true and false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", nativeHost+"\nfunc mark(h *Host,n int)int{return n}\n")
			write(t, root, "input.rules", replaceFixture(t, alternativesRules, tt.old, tt.replacement))
			c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Compile(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q: %v", tt.want, err)
			}
		})
	}
}

func TestOrderedAlternativesValidateUnselectedPayload(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", queryHost+`
type Observation struct{ Kind Kind; Data Proof }
func(h *Host) Observe(kind,inner int)Observation{
 h.Calls=append(h.Calls,kind);return Observation{Kind:Kind(kind),Data:Proof{Kind:Kind(inner)}}
}
`)
	write(t, root, "input.rules", queryDeclarations+`host
  Observe!

type Observation = Observation
  tag Kind
  Known (value: Data) = Yes
  Skipped (value: Data) = No

@Main
rewrite (Pair x y) =
  case Observe x y of
    Known { value: proof } -> [Mark 7]
    Skipped() -> decline
  <|> [Mark 99]

@OtherRule
rewrite _ = [Mark 100]
`)
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import("reflect";"testing")
func TestBehavior(t *testing.T){
 for _,tt:=range []struct{outer,inner int;want []int;ok bool}{
 {0,0,[]int{7},true},{1,0,[]int{99},true},
 {0,99,nil,false},{1,99,nil,false},{99,0,nil,false},
 }{
 h:=&Host{};got,ok:=Rewrite(Node{tt.outer,tt.inner},h)
 if ok!=tt.ok||!reflect.DeepEqual([]int(got),tt.want)||!reflect.DeepEqual(h.Builds,tt.want)||!reflect.DeepEqual(h.Calls,[]int{tt.outer}){
 t.Fatalf("%+v: %v,%v calls %v builds %v",tt,got,ok,h.Calls,h.Builds)
 }
 }
}`)
	runGeneratedTransitionTest(t, root)
}

func TestOrderedAlternativesInDecisionArms(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", queryHost)
	write(t, root, "input.rules", queryDeclarations+`@Main
rewrite (Pair x y) =
  case Ask x of
    Proven() -> case Ask y of
      Unavailable { reason: why } -> [Mark why]
      Proven() -> decline
      Disproven() -> decline
    <|> [Mark 7]
    Disproven() -> []
    Unavailable() -> []
`)
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import("reflect";"testing")
func TestBehavior(t *testing.T){
 for _,tt:=range []struct{x,y int;want,calls []int;ok bool}{
 {0,2,[]int{42},[]int{0,2},true},{0,1,[]int{7},[]int{0,1},true},
 {1,99,[]int{},[]int{1},true},{0,99,nil,[]int{0,99},false},
 }{
 h:=&Host{};got,ok:=Rewrite(Node{tt.x,tt.y},h)
 if ok!=tt.ok||!reflect.DeepEqual([]int(got),tt.want)||!reflect.DeepEqual(h.Calls,tt.calls){t.Fatalf("%+v: %v,%v calls %v",tt,got,ok,h.Calls)}
 }
}`)
	runGeneratedTransitionTest(t, root)
}

func TestOrderedAlternativesKeepFalseAnswer(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
type Signature func(int,*Host)bool
type Host struct{ Calls int }
func ask(h *Host,n int)bool{h.Calls++;return n==0}
func fallback(h *Host)bool{h.Calls++;return true}
`)
	write(t, root, "input.rules", `total fn rewrite(input)
  fallback outputs true

host
  ask!
  fallback

@Main
rewrite (inputs n host) =
  case ask host n of
    true -> outputs false
    false -> decline
  <|> outputs (fallback host)
`)
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestBehavior(t *testing.T){
 for _,tt:=range []struct{n,calls int;want bool}{{0,1,false},{1,2,true}}{
 h:=&Host{};got:=Rewrite(tt.n,h)
 if got!=tt.want||h.Calls!=tt.calls{t.Fatalf("%+v: %v calls %d",tt,got,h.Calls)}
 }
}`)
	runGeneratedTransitionTest(t, root)
}

func TestOrderedSelectionAlternation(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", nativeHost+`func mark(h *Host,n int)int{h.Calls=append(h.Calls,"mark");return n}`)
	rules := replaceFixture(t, alternativesRules, "Proven { reason: why }", "(Proven { reason: why } | Unavailable { reason: why })")
	rules = replaceFixture(t, rules, "    Unavailable() -> decline\n", "")
	formatted, err := hyrule.Format("input.rules", []byte(rules))
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("input.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format roundtrip: %v", err)
	}
	write(t, root, "input.rules", string(formatted))
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import("go/ast";"reflect";"testing")
func TestBehavior(t *testing.T){
 for _,tt:=range []struct{n,a,b int;calls []string}{
 {0,9,1,[]string{"ask","mark"}},
 {2,9,1,[]string{"ask","mark"}},
 {1,20,4,[]string{"ask","ready","ask","mark"}},
 {99,0,-1,[]string{"ask"}},
 }{
 h:=&Host{};h.Offset.Value=2;if tt.n==1{h.Offset.Value=1}
 a,b:=Rewrite(&ast.Ident{Name:"ok"},tt.n,h)
 if a!=tt.a||b!=tt.b||!reflect.DeepEqual(h.Calls,tt.calls){t.Fatalf("%+v: %d,%d calls %v",tt,a,b,h.Calls)}
 }
}`)
	runGeneratedTransitionTest(t, root)
}

func TestOrderedSelectionAlternationDiagnostics(t *testing.T) {
	for _, tt := range []struct{ name, selection, want string }{
		{"duplicate", "(Proven { reason: why } | Proven { reason: why })", "duplicate decision variant"},
		{"capture names", "(Proven { reason: why } | Unavailable { reason: other })", "same captures with identical types"},
		{"missing capture", "(Proven { reason: why } | Unavailable())", "same captures with identical types"},
		{"refinement", "(Proven { reason: why } | Unavailable { reason: 7 })", "fresh captures"},
		{"nested", "(Proven { reason: why } | (Unavailable { reason: why } | Disproven()))", "decision arm must name a variant"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", nativeHost+"\nfunc mark(h *Host,n int)int{return n}\n")
			write(t, root, "input.rules", replaceFixture(t, alternativesRules, "Proven { reason: why }", tt.selection))
			c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Compile(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q: %v", tt.want, err)
			}
		})
	}
}

func TestNamedOrderedSelection(t *testing.T) {
	root := fixture(t)
	host := replaceFixture(t, nativeHost, "type Signature func(any, int, *Host) (int, int)", "type Signature func(int,*Host)(Kind,int)")
	write(t, root, "bindings.go", host)
	rules := `total fn rewrite(input)
  fallback outputs ^No (-1)

host
  No
  ask! = Ask

type Proof = (Kind, int)
  tag 0
  Proven = Yes
  Disproven = No
  Unavailable = Unknown

@Main
rewrite (inputs n host) =
  do
    answer <- ask host n 0
    case answer of
      Proven() -> answer
      Unavailable() -> answer
      Disproven() -> decline
  <|> outputs ^No 0
`
	formatted, err := hyrule.Format("input.rules", []byte(rules))
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("input.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format roundtrip: %v", err)
	}
	write(t, root, "input.rules", string(formatted))
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import("reflect";"testing")
 func TestBehavior(t *testing.T){
 for _,tt:=range []struct{n int;kind Kind;value int}{{0,Yes,7},{2,Unknown,7},{1,No,0},{99,No,-1}}{
 h:=&Host{};kind,value:=Rewrite(tt.n,h)
 if kind!=tt.kind||value!=tt.value||!reflect.DeepEqual(h.Calls,[]string{"ask"}){t.Fatalf("%+v: %v,%d calls %v",tt,kind,value,h.Calls)}
 }
 }`)
	runGeneratedTransitionTest(t, root)
}

func TestOrderedSelectionCaptureTypes(t *testing.T) {
	root := fixture(t)
	host := replaceFixture(t, nativeHost, "func Ask(h *Host,n,offset int)(Kind,int)", "func Ask(h *Host,n,offset int)(Kind,int,string)")
	host = replaceFixture(t, host, "return Kind(n),offset+7", "return Kind(n),offset+7,\"\"")
	write(t, root, "bindings.go", host+"\nfunc mark(h *Host,n int)int{return n}\n")
	rules := replaceFixture(t, alternativesRules, "(Kind, int)", "(Kind, int, string)")
	rules = replaceFixture(t, rules, "Unavailable (reason: 1) = Unknown", "Unavailable (reason: 2) = Unknown")
	rules = replaceFixture(t, rules, "Proven { reason: why }", "(Proven { reason: why } | Unavailable { reason: why })")
	write(t, root, "input.rules", rules)
	c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Compile(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "same captures with identical types") {
		t.Fatalf("incompatible shared payload: %v", err)
	}
}

func TestNamedBooleanSelectionAlternation(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
type Signature func(bool,*Host)bool
type Host struct{Calls int}
func ask(h *Host,value bool)bool{h.Calls++;return value}
func fallback(h *Host)bool{h.Calls++;return true}
`)
	write(t, root, "input.rules", `total fn rewrite(input)
  fallback outputs true

host
  ask!
  fallback

@Main
rewrite (inputs value host) =
  do
    answer <- ask host value
    case answer of
      true -> outputs answer
      false -> outputs answer
  <|> outputs (fallback host)
`)
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestBehavior(t *testing.T){for _,value:=range []bool{true,false}{
 h:=&Host{};got:=Rewrite(value,h)
 if got!=value||h.Calls!=1{t.Fatalf("%v: %v calls %d",value,got,h.Calls)}
 }} `)
	runGeneratedTransitionTest(t, root)
}

package hyrule_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

const nativeHost = `package fixture
import goast "go/ast"
import "strings"
var _ = strings.HasPrefix
var _ = goast.Ident{}
type Signature func(any, int, *Host) (int, int)
type Host struct { Calls []string; Offset struct { Value int } }
type Kind int
const (Yes Kind = iota; No; Unknown)
func Ready(h *Host, name string) bool { h.Calls=append(h.Calls,"ready"); return name!="skip" }
func Ask(h *Host,n,offset int)(Kind,int) { h.Calls=append(h.Calls,"ask");return Kind(n),offset+7 }
`
const nativeRules = `total fn rewrite(input)
  fallback outputs 0 (-1)

view Entry = *goast.Ident
  name: Name

host
  ready! name = Ready host name
  ask! n context = Ask context n context.Offset.Value
  named name = strings.HasPrefix name ""

type Proof = (Kind, int)
  tag 0
  Proven (reason: 1) = Yes
  Disproven = No
  Unavailable (reason: 1) = Unknown

@Main
rewrite (inputs (Entry (name: name)) n host)
  | let offset = host.Offset.Value
  , offset >= 0
  , named name
  =
  case ready name of
    false -> outputs 10 20
    true -> case ask n host of
      Proven (reason: why) -> outputs why 1
      Disproven() -> outputs 0 2
      Unavailable (reason: why) -> outputs why 3
`

func TestNativeHostInterfaces(t *testing.T) {
	for _, mode := range []string{"total", "partial"} {
		t.Run(mode, func(t *testing.T) {
			root := fixture(t)
			host, rules := nativeHost, nativeRules
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
				t.Fatalf("format roundtrip: %v", err)
			}
			write(t, root, "bindings.go", host)
			write(t, root, "input.rules", string(formatted))
			c, a := compile(t, root)
			if err := c.Publish(a, false); err != nil {
				t.Fatal(err)
			}
			invocation := "a,b:=Rewrite(tt.input,tt.n,h);ok:=tt.ok"
			failure := "-1"
			if mode == "partial" {
				invocation = "a,b,ok:=Rewrite(tt.input,tt.n,h)"
				failure = "0"
			}
			write(t, root, "behavior_test.go", `package fixture
import("go/ast";"reflect";"testing")
func TestBehavior(t *testing.T){
 for _,tt:=range []struct{input any;n,a,b int;calls []string;ok bool}{
 {&ast.Ident{Name:"ok"},0,12,1,[]string{"ready","ask"},true},
 {&ast.Ident{Name:"ok"},1,0,2,[]string{"ready","ask"},true},
 {&ast.Ident{Name:"ok"},2,12,3,[]string{"ready","ask"},true},
 {&ast.Ident{Name:"skip"},0,10,20,[]string{"ready"},true},
 {&ast.Ident{Name:"ok"},99,0,`+failure+`,[]string{"ready","ask"},false},
 {(*ast.Ident)(nil),0,0,`+failure+`,nil,false},
 {nil,0,0,`+failure+`,nil,false},
 {42,0,0,`+failure+`,nil,false},
 }{
 h:=&Host{};h.Offset.Value=5;`+invocation+`
 if a!=tt.a||b!=tt.b||ok!=tt.ok||!reflect.DeepEqual(h.Calls,tt.calls){t.Fatalf("%#v: %d,%d,%v calls %v",tt.input,a,b,ok,h.Calls)}
 }
}`)
			runGeneratedTransitionTest(t, root)
			cmd := exec.Command("go", "vet", ".")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOWORK=off")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated native API vet: %v\n%s", err, output)
			}
		})
	}
}

func TestNativeInterfaceDiagnostics(t *testing.T) {
	for _, tt := range []struct{ name, rules, want string }{
		{"bool missing", replaceFixture(t, nativeRules, "false -> outputs 10 20", ""), "non-exhaustive Boolean"},
		{"bool duplicate", replaceFixture(t, nativeRules, "false -> outputs 10 20", "false -> outputs 10 20\n    false -> outputs 0 0"), "duplicate Boolean"},
		{"tuple missing", replaceFixture(t, nativeRules, "Disproven() -> outputs 0 2", ""), "non-exhaustive query"},
		{"tuple slot", replaceFixture(t, nativeRules, "reason: 1", "reason: 9"), "missing or invalid direct variant"},
		{"projection", replaceFixture(t, nativeRules, "Offset.Value", "Missing.Value"), "missing or inaccessible field"},
		{"view field", replaceFixture(t, nativeRules, "name: Name", "name: Missing"), "missing or inaccessible direct record field"},
		{"view construction", replaceFixture(t, nativeRules, "outputs 10 20", "Entry (name: 1)"), "read-only"},
		{"nested forwarding", replaceFixture(t, nativeRules, "Ready host name", "Ready host (Ready host name)"), "captures, constants or literals"},
		{"typed forwarding", replaceFixture(t, nativeRules, "Ready host name", "Ready host 99"), "cannot use"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", nativeHost)
			write(t, root, "input.rules", tt.rules)
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

func TestNativeParametersAreNotQueryOutcomes(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Kind int
 const (Yes Kind=iota;No)
 type Signature func(Kind,int) int
 `)
	write(t, root, "input.rules", `total fn rewrite(input)
  fallback outputs (-1)

type Proof = (Kind, int)
  tag 0
  Proven = Yes
  Disproven = No

@Any
rewrite (inputs _ _) = outputs 42
`)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestBehavior(t *testing.T){if got:=Rewrite(Kind(99),7);got!=42{t.Fatalf("ordinary argument constrained by unused outcome schema: %d",got)}}`)
	runGeneratedTransitionTest(t, root)
}

func TestNativeViewRejectsRecursivePointerStorage(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", nativeHost+"\ntype Recursive *Recursive\n")
	write(t, root, "input.rules", replaceFixture(t, nativeRules, "*goast.Ident", "Recursive"))
	c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Compile(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "record storage must be a Go struct") {
		t.Fatalf("recursive pointer storage: %v", err)
	}
}

func TestNativeBooleanResultIsData(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Host struct{ Calls int }
 type Signature func(bool,*Host) bool
 func Observe(host *Host,value bool)bool{host.Calls++;return !value}
 `)
	write(t, root, "input.rules", `total fn rewrite(input)
  fallback outputs false

host
  observe! value = Observe host value

@Invert
rewrite (inputs input host) =
  do
    answer <- observe input
    case answer of
      true -> outputs answer
      false -> outputs answer
`)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestBehavior(t *testing.T){h:=&Host{};
if !Rewrite(false,h)||Rewrite(true,h)||h.Calls!=2{t.Fatalf("Boolean output confused with success flag: calls=%d",h.Calls)}}`)
	runGeneratedTransitionTest(t, root)
}

func TestNativeExplicitReceiverMethods(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Host struct { Calls int; Offset int }
 type Signature func(int,*Host)(int,bool)
 func(h *Host) ready(n int)bool{h.Calls++;return n>0}
 func(h Host) adjusted(n int)int{return n+h.Offset}
 `)
	write(t, root, "input.rules", `partial fn rewrite(input)

host
  ready! n = Host.ready host n
  adjusted = Host.adjusted

@Main
rewrite (inputs n host) =
  case ready n of
    true -> outputs (adjusted (copy host) n)
    false -> outputs 0

host
  copy = copyHost
`)
	// A value method's receiver stays a value; the explicit copy is ordinary Go.
	write(t, root, "copy.go", `package fixture;func copyHost(h *Host)Host{return *h}`)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestBehavior(t *testing.T){
 h:=&Host{Offset:7};a,ok:=Rewrite(3,h);b,again:=Rewrite(0,h)
 if !ok||!again||a!=10||b!=0||h.Calls!=2{t.Fatalf("receiver binding: %d,%d,%v,%v calls=%d",a,b,ok,again,h.Calls)}
 }`)
	runGeneratedTransitionTest(t, root)
}

func TestNativeWholeViewCaptureKeepsConcreteTypeAndIdentity(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 import goast "go/ast"
 type Signature func(any,any)(*goast.Ident,bool)
 var Calls int
 func valid(node *goast.Ident)bool{Calls++;return node.Name!=""}
 `)
	write(t, root, "input.rules", `partial fn rewrite(input)

view Entry = *goast.Ident
  name: Name

host
  valid!

@Same
rewrite (inputs node@(Entry (name: name)) node@(Entry())) =
  case valid node of
    true -> outputs node
    false -> outputs nil
`)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import("go/ast";"testing")
 func TestBehavior(t *testing.T){
 node:=&ast.Ident{Name:"ok"};other:=&ast.Ident{Name:"ok"};empty:=&ast.Ident{}
 for _,tt:=range []struct{a,b any;want *ast.Ident;ok bool;calls int}{
 {node,node,node,true,1},{node,other,nil,false,0},{nil,nil,nil,false,0},
 {(*ast.Ident)(nil),(*ast.Ident)(nil),nil,false,0},{42,42,nil,false,0},{empty,empty,nil,true,1},
 }{Calls=0;got,ok:=Rewrite(tt.a,tt.b);if got!=tt.want||ok!=tt.ok||Calls!=tt.calls{t.Fatalf("%v,%v: %p,%v calls=%d",tt.a,tt.b,got,ok,Calls)}}
 }`)
	runGeneratedTransitionTest(t, root)
}

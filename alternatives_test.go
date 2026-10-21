package hyrule_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

const alternativesRules = `signature total fallback outputs 0 -1;
view Entry = *goast.Ident { name:Name; };
query ready($name) = Ready $host $name;
query ask($n) = Ask $host $n $host.Offset.Value;
build mark;
union Proof = (Kind, int) tag 0 {
 Proven = Yes { reason:1; };
 Disproven = No;
 Unavailable = Unknown { reason:1; };
};
rule Main:
 inputs (Entry name:$name) $n $host
 => query ask $n select Proven reason:$why => outputs (mark $host $why) 1
 <|> query ready $name select false => outputs (mark $host 10) 2
 <|> query ask $host.Offset.Value select Unavailable reason:$why => outputs (mark $host $why) 3
 <|> pure outputs (mark $host 20) 4;
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
				host = strings.Replace(host, "(int, int)", "(int, int, bool)", 1)
				rules = strings.Replace(rules, "signature total fallback outputs 0 -1;", "signature partial;", 1)
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
		{"fallback required", "<|> pure outputs (mark $host 20) 4", "", "final pure fallback"},
		{"early pure", "query ready $name select false => outputs (mark $host 10) 2", "pure outputs 0 0", "pure must be the final"},
		{"fresh payload", "Proven reason:$why", "Proven reason:$n", "fresh captures"},
		{"no refinement", "Proven reason:$why", "Proven reason:7", "fresh captures"},
		{"wrong variant", "select Proven", "select Missing", "decision arm must name a variant"},
		{"bool selection", "select false", "select _", "Boolean decision arms"},
		{"scope", "pure outputs (mark $host 20) 4", "pure outputs $why 4", "unbound capture"},
		{"typed answers", "outputs (mark $host 10) 2", "outputs true 2", "cannot use"},
		{"query in pure", "pure outputs (mark $host 20) 4", "pure query ready $name { true => outputs 1 1; false => outputs 2 2; }", "pure cannot execute a query"},
		{"exhaustive remains", "query ready $name select false => outputs (mark $host 10) 2",
			"query ready $name { false => outputs 1 1; }", "ordered alternative requires"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", nativeHost+"\nfunc mark(h *Host,n int)int{return n}\n")
			write(t, root, "input.rules", strings.Replace(alternativesRules, tt.old, tt.replacement, 1))
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
	write(t, root, "input.rules", queryDeclarations+`
query Observe;
union Observation = Observation tag Kind {
 Known = Yes { value:Data; };
 Skipped = No { value:Data; };
};
rule Main:
 Pair $x $y
 => query Observe $x $y select Known value:$proof => effects [(Mark 7)]
 <|> pure effects [(Mark 99)];
rule OtherRule: _ => effects [(Mark 100)];
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
	write(t, root, "input.rules", queryDeclarations+`
rule Main:
 Pair $x $y
 => query Ask $x {
  Proven => query Ask $y select Unavailable reason:$why => effects [(Mark $why)]
    <|> pure effects [(Mark 7)];
  Disproven => effects [];
  Unavailable => effects [];
 };
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
	write(t, root, "input.rules", `signature total fallback outputs true;
query ask;
build fallback;
rule Main: inputs $n $host
 => query ask $host $n select true => outputs false
 <|> pure outputs (fallback $host);
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

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

const queryHost = `package fixture
 type Node struct{ A,B int }
 type Plan []int
 type Signature func(Node,*Host)(Plan,bool)
 type Host struct { Calls []int; Builds []int }
 type Kind int
 const ( Yes Kind = iota; No; Unknown )
 type Proof struct { Kind Kind; Reason int }
 func Pair(n Node)(left,right int,ok bool){return n.A,n.B,true}
 func Positive(n int)bool{return n>0}
 func(h *Host) Ask(n int)Proof{h.Calls=append(h.Calls,n);return Proof{Kind:Kind(n),Reason:42}}
 func(h *Host) Mark(n int)int{h.Builds=append(h.Builds,n);return n}
 func Wrap(items []int)[]int{return items}
 func Wrong(n int)bool{return true}
 `
const queryDeclarations = `match Pair = Pair;
 guard Positive = Positive;
 query Ask = Ask;
 build Mark = Mark;
 build Wrap = Wrap;
 union Proof = Proof tag Kind {
  Proven = Yes {};
  Disproven = No {};
  Unavailable = Unknown { reason:Reason; };
 };
 `

func TestOrderedQueryOutcomesAndEffects(t *testing.T) {
	t.Run("named", func(t *testing.T) { testOrderedQueryOutcomesAndEffects(t, false) })
	t.Run("anonymous", func(t *testing.T) { testOrderedQueryOutcomesAndEffects(t, true) })
}

func testOrderedQueryOutcomesAndEffects(t *testing.T, anonymous bool) {
	root := fixture(t)
	write(t, root, "bindings.go", queryHost)
	rules := queryDeclarations + `
 rule Skip: (Pair -1 _) => effects [];
 rule Main: (or (Pair $x $y) (Pair $x $y)) && (Positive $y)
  => query $proof = (Ask $x) decide $proof {
   (Proven) => effects [(Mark 1), (Mark 2)];
   (Disproven) => query $next = (Ask $y) decide $next {
    (Proven) => effects [10];
    (Disproven) => effects [11];
    (Unavailable reason:$why) => (Wrap effects [(Mark $why), (Mark 3)]);
   };
   (Unavailable reason:$why) => effects [(Mark $why)];
  };
 rule Fallback: _ => effects [(Mark 99)];
 `
	if anonymous {
		rules = strings.NewReplacer(
			"query $proof = (Ask $x) decide $proof", "query Ask $x",
			"query $next = (Ask $y) decide $next", "query (Ask $y)",
		).Replace(rules)
	}
	formatted, err := hyrule.Format("input.rules", []byte(rules))
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("input.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format roundtrip: %v", err)
	}
	write(t, root, "input.rules", string(formatted))
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(a, true); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import("reflect";"testing")
 func TestBehavior(t *testing.T){
  for _,tt:=range []struct{n Node;want,calls,builds []int;ok bool}{
   {Node{-1,1},[]int{},nil,nil,true},
   {Node{0,2},[]int{1,2},[]int{0},[]int{1,2},true},
   {Node{1,2},[]int{42,3},[]int{1,2},[]int{42,3},true},
   {Node{2,1},[]int{42},[]int{2},[]int{42},true},
   {Node{0,0},[]int{99},nil,[]int{99},true},
   {Node{9,1},nil,[]int{9},nil,false},
   {Node{1,9},nil,[]int{1,9},nil,false},
  }{
   h:=&Host{};got,ok:=Rewrite(tt.n,h)
   if ok!=tt.ok||!reflect.DeepEqual([]int(got),tt.want)||!reflect.DeepEqual(h.Calls,tt.calls)||!reflect.DeepEqual(h.Builds,tt.builds){
    t.Fatalf("%v: got=%v,%v calls=%v builds=%v",tt.n,got,ok,h.Calls,h.Builds)
   }
  }
 }
 `)
	runGeneratedTransitionTest(t, root)
}

func runGeneratedTransitionTest(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("go", "test", "-run", "TestBehavior", ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated transitions: %v\n%s", err, out)
	}
}

func TestQueryDecisionDiagnostics(t *testing.T) {
	complete := `(Proven) => effects []; (Disproven) => effects []; (Unavailable) => effects [];`
	for _, tt := range []struct{ name, result, want string }{
		{"anonymous missing", `query Ask 0 {Proven => effects [];}`, "missing Disproven, Unavailable"},
		{"anonymous non-query", `query Mark 0 {` + complete + `}`, "expected query binding"},
		{"anonymous unbound result", `query Ask 0 {Proven => effects [$proof]; Disproven => effects []; Unavailable => effects [];}`, "unbound capture"},
		{"missing", `query $p = (Ask 0) decide $p {(Proven)=>effects [];}`, "missing Disproven, Unavailable"},
		{"duplicate", `query $p = (Ask 0) decide $p {` + complete + `(Proven)=>effects [];}`, "duplicate decision variant"},
		{"catchall", `query $p = (Ask 0) decide $p {_=>effects [];}`, "decision arm must name"},
		{"refined", `query $p = (Ask 0) decide $p {(Proven)=>effects [];(Disproven)=>effects [];(Unavailable reason:42)=>effects [];}`,
			"refinements are not exhaustive"},
		{"wrong capture", `query $p = (Ask 0) decide $q {` + complete + `}`, "immediately preceding"},
		{"not query", `query $p = (Mark 0) decide $p {` + complete + `}`, "expected query binding"},
		{"wrong arity", `query $p = (Ask) decide $p {` + complete + `}`, "one parameter per argument"},
		{"argument type", `query $p = (Ask "bad") decide $p {` + complete + `}`, "cannot use"},
		{"builder argument", `query $p = (Ask (Mark 0)) decide $p {` + complete + `}`, "captures or literals"},
		{"escaped payload", `query $p = (Ask 0) decide $p {(Proven)=>effects [$why];(Disproven)=>effects [];(Unavailable reason:$why)=>effects [];}`,
			"unbound capture"},
		{"shadow query", `query $x = (Ask 0) decide $x {` + complete + `}`, "query capture shadows"},
		{"shadow payload", `query $p = (Ask 0) decide $p {(Proven)=>effects [];(Disproven)=>effects [];(Unavailable reason:$x)=>effects [];}`, "fresh captures"},
		{"effect type", `effects ["bad"]`, "cannot use"},
		{"query as builder", `(Ask 0)`, "expected build binding"},
		{"nested query", `(Wrap query $p = (Ask 0) decide $p {` + complete + `})`, "only allowed as a rule result"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", queryHost)
			write(t, root, "input.rules", queryDeclarations+`rule R: (Pair $x _) => `+tt.result+`;`)
			c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Compile(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
			if _, err := os.Stat(filepath.Join(root, "rewrite_gen.go")); !os.IsNotExist(err) {
				t.Fatal("failed compilation published code")
			}
		})
	}
}

func TestEffectsRequireSliceContext(t *testing.T) {
	root := fixture(t)
	write(t, root, "input.rules", `rule R: _ => effects [1];`)
	c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Compile(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "effects requires a slice") {
		t.Fatalf("%v", err)
	}
}

func TestEffectsPreserveNamedArgumentOrderAndTypes(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Node struct{A,B int}
 type Plan [][]*int
 type Signature func(Node,*Host)(Plan,bool)
 type Host struct{Steps []int}
 func(h *Host) Mark(n int)*int{h.Steps=append(h.Steps,n);return &n}
 func(h *Host) Pack(left *int,right []*int)Plan{h.Steps=append(h.Steps,9);return Plan{right,{left}}}
 `)
	write(t, root, "input.rules", `build Mark = Mark; build Pack = Pack;
 rule R: _ => (Pack right:effects [(Mark 2), nil, (Mark 3)] left:(Mark 1));`)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import("reflect";"testing")
 func TestBehavior(t *testing.T){
 h:=&Host{};got,ok:=Rewrite(Node{},h)
 if !ok||len(got)!=2||len(got[0])!=3||got[0][1]!=nil||*got[0][0]!=2||*got[0][2]!=3||*got[1][0]!=1{t.Fatalf("%v,%v",got,ok)}
 if !reflect.DeepEqual(h.Steps,[]int{2,3,1,9}){t.Fatal(h.Steps)}
 }`)
	runGeneratedTransitionTest(t, root)
	write(t, root, "input.rules", `build Mark = Mark; rule R: _ => effects [effects [nil, (Mark 1)], effects []];`)
	_, nested := compile(t, root)
	if err := c.Publish(nested, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestBehavior(t *testing.T){
 h:=&Host{};got,ok:=Rewrite(Node{},h)
 if !ok||len(got)!=2||got[0][0]!=nil||*got[0][1]!=1||got[1]==nil{t.Fatalf("%v,%v",got,ok)}
 }`)
	runGeneratedTransitionTest(t, root)
}

func TestQueryRequiresUnionAndAllBranchesTypeCheck(t *testing.T) {
	for _, tt := range []struct{ declarations, result, want string }{
		{`query Wrong = Wrong;`, `query $p = (Wrong 1) decide $p {(Proven)=>effects [];}`, "query result must have a declared union"},
		{``, `query $p = (Ask 0) decide $p {
    (Proven)=>effects []; (Disproven)=>effects []; (Unavailable)=>effects ["bad"];
   }`, "cannot use"},
	} {
		root := fixture(t)
		write(t, root, "bindings.go", queryHost)
		write(t, root, "input.rules", queryDeclarations+tt.declarations+`rule R: _ => `+tt.result+`;`)
		c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Compile(context.Background(), "")
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("want %q, got %v", tt.want, err)
		}
	}
}

func TestTransitionExampleCurrent(t *testing.T) {
	c, err := hyrule.Load(filepath.Join("examples", "transitions", "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := c.Compile(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(artifacts, true); err != nil {
		t.Fatal(err)
	}
}

func TestResultKeywordsRemainUsableAsFieldNames(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Node struct{A,B int}
 type Signature func(Node)(int,bool)
 func Match(n Node)(effects,query int,ok bool){return n.A,n.B,true}
 func Build(effects,query int)int{return effects+query}
 `)
	write(t, root, "input.rules", `match M = Match; build B = Build;
 rule R: (M effects:$a query:$b) => (B effects:$a query:$b);`)
	compile(t, root)
}

func TestInvalidEffectStopsBeforeLaterBuildersAndFallback(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Node struct{A,B int}
 type Signature func(Node,*Host)([]Effect,bool)
 type Kind int
 const Valid Kind=0
 type Effect struct{Kind Kind}
 type Host struct{Calls int}
 func(h *Host) Invalid()Effect{h.Calls++;return Effect{Kind:9}}
 func(h *Host) Later()Effect{h.Calls+=10;return Effect{Kind:Valid}}
 `)
	write(t, root, "input.rules", `union Effect = Effect tag Kind {Valid = Valid {};};
 build Invalid = Invalid; build Later = Later;
 rule First: _ => effects [(Invalid), (Later)];
 rule Fallback: _ => effects [(Later)];`)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestBehavior(t *testing.T){h:=&Host{};got,ok:=Rewrite(Node{},h);if ok||got!=nil||h.Calls!=1{t.Fatalf("%v,%v calls=%d",got,ok,h.Calls)}}`)
	runGeneratedTransitionTest(t, root)
}

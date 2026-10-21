package hyrule_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

const doRules = `signature total fallback outputs 0 -1;
query ask = Ask;
query ready = Ready;
build mark;
union Proof = (Kind, int) tag 0 {
 Proven = Yes { reason:1; };
 Disproven = No;
 Unavailable = Unknown { reason:1; };
};
rule Main:
 inputs $input $n $host
 => do {
   Proven reason:$first <- query ask $host $n 0
     else {
       Disproven => decline;
       Unavailable reason:$why => outputs $why 3;
     };
   Proven reason:$second <- query ask $host $host.Offset.Value $first
     else {
       Disproven => decline;
       Unavailable reason:$why => outputs $why 4;
     };
   pure outputs (mark $host $second) 1;
 }
 <|> do {
   true <- query ready $host "ok" else { false => decline; };
   pure outputs (mark $host 20) 2;
 }
 <|> pure outputs (mark $host 99) 5;
rule Later: inputs $input $n $host => outputs (mark $host 100) 6;
`

func TestDoSequencingAndAlternatives(t *testing.T) {
	for _, mode := range []string{"total", "partial"} {
		t.Run(mode, func(t *testing.T) {
			root := fixture(t)
			host := strings.Replace(nativeHost, `return name!="skip"`, `return name!="skip" && h.Offset.Value != -1`, 1)
			host += `func mark(h *Host,n int)int{h.Calls=append(h.Calls,"mark");return n}`
			rules := doRules
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
				t.Fatalf("unstable formatting: %v\n%s\n%s", err, formatted, again)
			}
			write(t, root, "bindings.go", host)
			write(t, root, "input.rules", string(formatted))
			c, artifacts := compile(t, root)
			if err := c.Publish(artifacts, false); err != nil {
				t.Fatal(err)
			}
			if err := c.Publish(artifacts, true); err != nil {
				t.Fatal(err)
			}
			invoke, failure := "a,b:=Rewrite(nil,tt.n,h);ok:=tt.ok", "-1"
			if mode == "partial" {
				invoke, failure = "a,b,ok:=Rewrite(nil,tt.n,h)", "0"
			}
			write(t, root, "behavior_test.go", `package fixture
import("reflect";"testing")
func TestBehavior(t *testing.T){
 for _,tt:=range []struct{n,offset,a,b int;calls []string;ok bool}{
  {0,0,14,1,[]string{"ask","ask","mark"},true},
  {1,0,20,2,[]string{"ask","ready","mark"},true},
  {1,-1,99,5,[]string{"ask","ready","mark"},true},
  {0,1,20,2,[]string{"ask","ask","ready","mark"},true},
  {2,0,7,3,[]string{"ask"},true},
  {0,2,14,4,[]string{"ask","ask"},true},
  {99,0,0,`+failure+`,[]string{"ask"},false},
  {0,99,0,`+failure+`,[]string{"ask","ask"},false},
 }{
  h:=&Host{};h.Offset.Value=tt.offset;`+invoke+`
  if a!=tt.a||b!=tt.b||ok!=tt.ok||!reflect.DeepEqual(h.Calls,tt.calls){t.Fatalf("%+v: %d,%d,%v calls=%v",tt,a,b,ok,h.Calls)}
 }
}`)
			runGeneratedTransitionTest(t, root)
		})
	}
}

func TestDoDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement, want string }{
		{"missing outcome", "Disproven => decline;", "", "missing Disproven"},
		{"duplicate outcome", "Disproven => decline;", "Proven => decline; Disproven => decline;", "duplicate decision variant"},
		{"refinement", "Proven reason:$first", "Proven reason:7", "fresh captures"},
		{"shadow", "Proven reason:$second", "Proven reason:$first", "fresh captures"},
		{"later use", "$n 0", "$second 0", "unbound capture"},
		{"else scope", "outputs $why 3", "outputs $first 3", "unbound capture"},
		{"alternative scope", "mark $host 20", "mark $host $first", "unbound capture"},
		{"wrong result type", "(mark $host $second)", "true", "cannot use"},
		{"missing final result", "pure outputs (mark $host $second) 1;", "", "final pure result"},
		{"pure cannot query", "pure outputs (mark $host $second) 1;",
			`pure query ready $host "ok" {true => outputs 1 1; false => outputs 2 2;};`, "pure cannot execute a query"},
		{"boolean exhaustive", "else { false => decline; }", "", "require true and false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", nativeHost+`func mark(h *Host,n int)int{return n}`)
			write(t, root, "input.rules", strings.Replace(doRules, tc.old, tc.replacement, 1))
			c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Compile(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestStandaloneDo(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", queryHost)
	write(t, root, "input.rules", queryDeclarations+`
 rule Main: Pair $x $y => do {
   Proven <- query Ask $x else {
     Disproven => effects [1];
     Unavailable reason:$why => effects [$why];
   };
   Proven <- query Ask $y else {
     Disproven => effects [2];
     Unavailable reason:$why => effects [$why];
   };
   pure effects [3];
 };
 `)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import("testing";"reflect")
func TestBehavior(t *testing.T){for _,tt:=range []struct{n Node;want int;calls []int}{
 {Node{0,0},3,[]int{0,0}},{Node{1,0},1,[]int{1}},{Node{0,1},2,[]int{0,1}},{Node{2,0},42,[]int{2}},
}{h:=&Host{};p,ok:=Rewrite(tt.n,h);if !ok||len(p)!=1||p[0]!=tt.want||!reflect.DeepEqual(h.Calls,tt.calls){t.Fatalf("%+v: %v %v %v",tt,p,ok,h.Calls)}}}`)
	runGeneratedTransitionTest(t, root)
	// A decline without a surrounding choice has no implicit matching fallback.
	write(t, root, "input.rules", queryDeclarations+`rule Main: Pair $x $y => do {
 Proven <- query Ask $x else {Disproven => decline; Unavailable => effects [];}; pure effects []; };`)
	_, err := c.Compile(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "decline requires") {
		t.Fatalf("want explicit decline context error: %v", err)
	}
}

func TestDoNestedChoiceDeclinesLocally(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", nativeHost+`func mark(h *Host,n int)int{return n}`)
	replacement := `Disproven => do {
   true <- query ready $host "skip" else { false => decline; };
   pure outputs 11 1;
 } <|> pure outputs 77 7;`
	write(t, root, "input.rules", strings.Replace(doRules, "Disproven => decline;", replacement, 1))
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import("testing";"reflect")
 func TestBehavior(t *testing.T){
 h:=&Host{};a,b:=Rewrite(nil,1,h)
 if a!=77||b!=7||!reflect.DeepEqual(h.Calls,[]string{"ask","ready"}){t.Fatalf("%d %d %v",a,b,h.Calls)}
 }`)
	runGeneratedTransitionTest(t, root)
}

func TestDoRejectsUnboundedSyntaxAndImpurePlacement(t *testing.T) {
	for _, source := range []string{
		`rule X: _ => do {};`,
		`rule X: _ => do { pure 1; pure 2; };`,
		`rule X: _ => do {` + strings.Repeat(`true <- query Ready else { false => decline; };`, 70) + `pure 0;};`,
	} {
		if _, err := hyrule.Format("input.rules", []byte(source)); err == nil {
			t.Fatalf("accepted invalid block: %s", source)
		}
	}
	for _, source := range []string{
		`rule X: Pair $x $y let $z = do { pure 1; } => effects [];`,
		`rule X: Pair $x $y => pure do { pure effects []; };`,
		`rule X: Pair $x $y => decline;`,
	} {
		root := fixture(t)
		write(t, root, "bindings.go", queryHost)
		write(t, root, "input.rules", queryDeclarations+source)
		c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Compile(context.Background(), ""); err == nil {
			t.Fatalf("accepted invalid placement: %s", source)
		}
	}
}

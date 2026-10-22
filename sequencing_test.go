package hyrule_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

const doRules = `signature total = outputs 0 (-1)

host
  ask! = Ask
  ready! = Ready
  mark

type Proof = (Kind, int)
  tag 0
  Proven (reason: 1) = Yes
  Disproven = No
  Unavailable (reason: 1) = Unknown

@Main
rewrite (inputs input n host) =
  do
    Proven (reason: first) <- ask host n 0
      else
        Disproven() -> decline
        Unavailable (reason: why) -> outputs why 3
    Proven (reason: second) <- ask host host.Offset.Value first
      else
        Disproven() -> decline
        Unavailable (reason: why) -> outputs why 4
    outputs (mark host second) 1
  <|> do
    true <- ready host "ok"
      else
        false -> decline
    outputs (mark host 20) 2
  <|> outputs (mark host 99) 5

@Later
rewrite (inputs input n host) = outputs (mark host 100) 6
`

func TestDoSequencingAndAlternatives(t *testing.T) {
	for _, mode := range []string{"total", "partial"} {
		t.Run(mode, func(t *testing.T) {
			root := fixture(t)
			host := replaceFixture(t, nativeHost, `return name!="skip"`, `return name!="skip" && h.Offset.Value != -1`)
			host += `func mark(h *Host,n int)int{h.Calls=append(h.Calls,"mark");return n}`
			rules := doRules
			if mode == "partial" {
				host = replaceFixture(t, host, "(int, int)", "(int, int, bool)")
				rules = replaceFixture(t, rules, `signature total = outputs 0 (-1)
`, `signature partial
`)
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
		{"missing outcome", "        Disproven() -> decline\n", "", "missing Disproven"},
		{"duplicate outcome", "Disproven() -> decline", "Proven -> decline\n        Disproven() -> decline", "duplicate decision variant"},
		{"refinement", "Proven (reason: first)", "Proven (reason: 7)", "fresh captures"},
		{"shadow", "Proven (reason: second)", "Proven (reason: first)", "fresh captures"},
		{"later use", "n 0", "second 0", "unbound capture"},
		{"else scope", "outputs why 3", "outputs first 3", "unbound capture"},
		{"alternative scope", "mark host 20", "mark host first", "unbound capture"},
		{"wrong result type", "(mark host second)", "true", "cannot use"},
		{"missing final result", "    outputs (mark host second) 1\n", "", "final result"},
		{"query fallback", "outputs (mark host 99) 5", "case ready host \"ok\" of\n    true -> outputs 1 1\n    false -> outputs 2 2", "final pure fallback"},
		{"boolean exhaustive", "      else\n        false -> decline\n", "", "require true and false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", nativeHost+`func mark(h *Host,n int)int{return n}`)
			write(t, root, "input.rules", replaceFixture(t, doRules, tc.old, tc.replacement))
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
	write(t, root, "input.rules", queryDeclarations+`@Main
rewrite (Pair x y) =
  do
    Proven <- Ask x
      else
        Disproven -> [1]
        Unavailable (reason: why) -> [why]
    Proven <- Ask y
      else
        Disproven -> [2]
        Unavailable (reason: why) -> [why]
    [3]
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
	write(t, root, "input.rules", queryDeclarations+`@Main
rewrite (Pair x y) =
  do
    Proven <- Ask x
      else
        Disproven() -> decline
        Unavailable -> []
    []
`)
	_, err := c.Compile(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "decline requires") {
		t.Fatalf("want explicit decline context error: %v", err)
	}
}

func TestDoNestedChoiceDeclinesLocally(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", nativeHost+`func mark(h *Host,n int)int{return n}`)
	replacement := `Disproven() -> do
          true <- ready host "skip"
            else false -> decline
          outputs 11 1
        <|> outputs 77 7`
	write(t, root, "input.rules", replaceFixture(t, doRules, "Disproven() -> decline", replacement))
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
		"rewrite _ = do\n",
		"rewrite _ = do\n  1\n  2\n",
		"rewrite _ = do\n" + strings.Repeat("  true <- Ready()\n    else false -> decline\n", 70) + "  0\n",
	} {
		if _, err := hyrule.Format("input.rules", []byte(source)); err == nil {
			t.Fatalf("accepted invalid block: %s", source)
		}
	}
	for _, source := range []string{
		`@X
rewrite (Pair x y)
  | let z = do
    1
  =
  []
`,
		`@X
rewrite (Pair x y) =
  do
    []
  <|> do
    []
`,
		`@X
rewrite (Pair x y) = decline
`,
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

func TestDoBooleanExpressionElse(t *testing.T) {
	root := fixture(t)
	host := nativeHost + `
type Flag bool
func Gate(h *Host,n,stage int)Flag {
 h.Calls=append(h.Calls,[]string{"first","second","last"}[stage])
 return Flag(n & (1<<stage) != 0)
}`
	rules := `signature total = outputs 0 (-1)

host
  gate! = Gate
  ready! = Ready

@Main
rewrite (inputs _ n host) =
  do
    true <- gate host n 0
      else
        false -> outputs 10 1
    false <- gate host n 1
      else
        true -> decline
    true <- gate host n 2
      else
        false -> case ready host "ok" of
          true -> outputs 50 5
          false -> outputs 60 6
    outputs 30 3
  <|> outputs 40 4
`
	formatted, err := hyrule.Format("input.rules", []byte(rules))
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("input.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("unstable expression else formatting: %v\n%s", err, formatted)
	}
	if !strings.Contains(string(formatted), "false -> outputs 10 1") || !strings.Contains(string(formatted), "true -> decline") {
		t.Fatalf("formatter lost expression else shorthand:\n%s", formatted)
	}
	write(t, root, "bindings.go", host)
	write(t, root, "input.rules", string(formatted))
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import("reflect";"testing")
func TestBehavior(t *testing.T){
 for _,tt:=range []struct{n,a,b int;calls []string}{
  {0,10,1,[]string{"first"}},
  {1,50,5,[]string{"first","second","last","ready"}},
  {3,40,4,[]string{"first","second"}},
  {5,30,3,[]string{"first","second","last"}},
 }{
  h:=&Host{};a,b:=Rewrite(nil,tt.n,h)
  if a!=tt.a||b!=tt.b||!reflect.DeepEqual(h.Calls,tt.calls){t.Fatalf("%+v: %d,%d calls=%v",tt,a,b,h.Calls)}
 }
}`)
	runGeneratedTransitionTest(t, root)
}

func TestDoExpressionElseRequiresBooleanPattern(t *testing.T) {
	for _, pattern := range []string{"Proven (reason: why)", "_", "0", "(true | false)"} {
		t.Run(pattern, func(t *testing.T) {
			_, err := hyrule.Format("input.rules", []byte(`rewrite input = do
  `+pattern+` <- Ask input
    else outputs 0
  outputs 1
`))
			if err == nil || !strings.Contains(err.Error(), "single-expression else requires a true or false do binding") {
				t.Fatalf("want Boolean-only expression else error, got %v", err)
			}
		})
	}
}

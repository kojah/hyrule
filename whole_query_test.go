package hyrule_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/hyrule"
)

const wholeQueryRules = `total fn rewrite(input)
  fallback outputs 0 (-1)

host
  advance!
  Ready!
  keepGoing!
  pair!

@Main
rewrite (inputs _ n host) =
  do
    unused <- advance host n
    step <- advance host n
    ready <- Ready host "ok"
    true <- keepGoing host step.Number
      else
        false -> decline
    answer <- pair host step.Number ready
    answer
  <|> outputs 9 10
`

const wholeQueryHost = nativeHost + `
type Step struct { Number int }
func advance(h *Host,n int)Step{h.Calls=append(h.Calls,"advance");return Step{n+1}}
func keepGoing(h *Host,n int)bool{h.Calls=append(h.Calls,"continue");return n>0}
func pair(h *Host,n int,ready bool)(int,int){h.Calls=append(h.Calls,"pair");if ready{return n,1};return n,0}
`

func TestWholeQueryBindings(t *testing.T) {
	root := fixture(t)
	formatted, err := hyrule.Format("input.rules", []byte(wholeQueryRules))
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("input.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format roundtrip: %v", err)
	}
	write(t, root, "bindings.go", wholeQueryHost)
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
 for _,tt:=range []struct{n,a,b int;calls []string}{
  {2,3,1,[]string{"advance","advance","ready","continue","pair"}},
  {-1,9,10,[]string{"advance","advance","ready","continue"}},
 }{
  h:=&Host{};a,b:=Rewrite(nil,tt.n,h)
  if a!=tt.a||b!=tt.b||!reflect.DeepEqual(h.Calls,tt.calls){t.Fatalf("%+v: %d,%d calls=%v",tt,a,b,h.Calls)}
 }
}`)
	runGeneratedTransitionTest(t, root)
}

func TestWholeQueryBindingDiagnostics(t *testing.T) {
	for _, tt := range []struct{ old, new, want string }{
		{"step <-", "n <-", "fresh capture"},
		{"step <-", "step.Number <-", "fresh capture"},
		{"step <- advance host n", "step <- advance host n\n      else true -> outputs 0 0", "cannot have else"},
		{"step <- advance host n", "step <- advance host n\n      else outputs 0 0", "single-expression else requires"},
		{"outputs 9 10", "outputs step.Number 10", "unbound capture"},
		{"step.Number ready", "step.Number step", "cannot use"},
	} {
		t.Run(tt.want+tt.new, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", wholeQueryHost)
			write(t, root, "input.rules", replaceFixture(t, wholeQueryRules, tt.old, tt.new))
			c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err == nil {
				_, err = c.Compile(t.Context(), "")
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v; want %s", err, tt.want)
			}
		})
	}
}

func TestWholeQueryBindingValidatesDeclaredUnion(t *testing.T) {
	root := fixture(t)
	rules := replaceFixture(t, doRules, "  do\n", "  do\n    complete <- ask host n 0\n")
	// The retained whole tuple is unused; even then its invalid tag must stop
	// before the next question, rather than silently forwarding opaque data.
	write(t, root, "bindings.go", nativeHost+`func mark(h *Host,n int)int{h.Calls=append(h.Calls,"mark");return n}`)
	write(t, root, "input.rules", rules)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import("reflect";"testing")
func TestBehavior(t *testing.T){
 for _,tt:=range []struct{n,a,b int;calls []string}{
  {99,0,-1,[]string{"ask"}},
  {0,14,1,[]string{"ask","ask","ask","mark"}},
 }{
  h:=&Host{};a,b:=Rewrite(nil,tt.n,h)
  if a!=tt.a||b!=tt.b||!reflect.DeepEqual(h.Calls,tt.calls){t.Fatalf("%+v: %d,%d calls=%v",tt,a,b,h.Calls)}
 }
}`)
	runGeneratedTransitionTest(t, root)
}

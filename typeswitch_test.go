package hyrule_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Consecutive concrete view assertions share one type switch. Alternatives of
// one type keep source order, typed nil pointers never match, a non-view rule
// still ends the run, and identical binding-free cases share a clause.
const typeSwitchRules = `signature total = outputs 0

view First = *A
  n: N

view Second = *B

view Third = *C

host
  Small
  Probe

@SmallFirst
rewrite (inputs (First (n: n)) host)
  | Small host n
  =
  outputs 1

@Second
rewrite (inputs (Second() | Third()) host) = outputs 2

@LargeFirst
rewrite (inputs (First (n: n)) host) = outputs n

@Probe
rewrite (inputs shape host)
  | Probe host
  =
  outputs 4

@AfterProbe
rewrite (inputs (Second()) host) = outputs 5
`

const typeSwitchHost = `package fixture
type Shape interface{ shape() }
type A struct{ N int }
type B struct{}
type C struct{}
func (*A) shape() {}
func (*B) shape() {}
func (*C) shape() {}
type Host struct { Calls []string; Probe bool }
type Signature func(Shape, *Host) int
func Small(h *Host, n int) bool { h.Calls = append(h.Calls, "small"); return n < 10 }
func Probe(h *Host) bool { h.Calls = append(h.Calls, "probe"); return h.Probe }
`

func TestTypeSwitchPreservesRuleOrder(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", typeSwitchHost)
	write(t, root, "input.rules", typeSwitchRules)
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(root + "/rewrite_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), ".(type)") || !strings.Contains(string(generated), "case *B, *C:") {
		t.Fatalf("expected a fused type switch with a shared clause:\n%s", generated)
	}
	write(t, root, "behavior_test.go", `package fixture
import ("reflect"; "testing")
func TestBehavior(t *testing.T) {
 var none *A
 for _, tt := range []struct{ name string; shape Shape; probe bool; want int; calls []string }{
  {"small first", &A{N: 3}, false, 1, []string{"small"}},
  {"large first after other types", &A{N: 30}, false, 30, []string{"small"}},
  {"shared second", &B{}, false, 2, nil},
  {"shared third", &C{}, false, 2, nil},
  {"typed nil reaches probe", none, true, 4, []string{"probe"}},
  {"typed nil fails probe", none, false, 0, []string{"probe"}},
  {"nil interface", nil, false, 0, []string{"probe"}},
 } {
  h := &Host{Probe: tt.probe}
  if got := Rewrite(tt.shape, h); got != tt.want || !reflect.DeepEqual(h.Calls, tt.calls) {
   t.Fatalf("%s: got %d calls %v, want %d calls %v", tt.name, got, h.Calls, tt.want, tt.calls)
  }
 }
}`)
	command := exec.Command("go", "test", "-run", "TestBehavior", ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated type switch: %v\n%s\n%s", err, out, generated)
	}
}

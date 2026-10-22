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

// Boolean operators, pattern guards, if/then/else and where bindings keep
// Haskell's evaluation order: && and || short-circuit, if evaluates one branch,
// a pattern guard falls through on mismatch, and a where binding is evaluated
// only where it is used.
const languageRules = `signature total = outputs 0

view First = *A
  n: N

host
  Small
  Probe
  Pick
  ask! = Ask

@Lazy
rewrite (inputs shape host)
  | Probe host 1 || Probe host 2
  , not (Probe host 3)
  =
  if Probe host 4 then outputs shared else outputs 7
  where
    shared = 10 + offset
    offset = 5

@Guarded
rewrite (inputs shape host)
  | First (n: n) <- Pick host shape
  , n > 0 && Small host n
  =
  outputs n

@Sequenced
rewrite (inputs shape host) =
  do
    answer <- ask host
    if answer > 3 then outputs answer else decline
  <|> outputs 99
`

const languageHost = `package fixture
type Shape interface{ shape() }
type A struct{ N int }
type B struct{}
func (*A) shape() {}
func (*B) shape() {}
type Host struct { Calls []string; Probes map[int]bool; Answer int }
type Signature func(Shape, *Host) int
func Small(h *Host, n int) bool { h.Calls = append(h.Calls, "small"); return n < 10 }
func Probe(h *Host, n int) bool { h.Calls = append(h.Calls, "probe"+string(rune('0'+n))); return h.Probes[n] }
func Pick(h *Host, s Shape) Shape { h.Calls = append(h.Calls, "pick"); return s }
func Ask(h *Host) int { h.Calls = append(h.Calls, "ask"); return h.Answer }
`

func TestHaskellLanguageForms(t *testing.T) {
	formatted, err := hyrule.Format("input.rules", []byte(languageRules))
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("input.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format roundtrip: %v\n%s\n%s", err, formatted, again)
	}
	root := fixture(t)
	write(t, root, "bindings.go", languageHost)
	write(t, root, "input.rules", string(formatted))
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import ("reflect"; "testing")
func TestBehavior(t *testing.T) {
 for _, tt := range []struct{ name string; shape Shape; probes map[int]bool; answer, want int; calls []string }{
  {"or short-circuits, where in then", &B{}, map[int]bool{1: true, 4: true}, 0, 15, []string{"probe1", "probe3", "probe4"}},
  {"or tries right, else branch", &B{}, map[int]bool{2: true}, 0, 7, []string{"probe1", "probe2", "probe3", "probe4"}},
  {"not rejects", &B{}, map[int]bool{1: true, 3: true}, 2, 99, []string{"probe1", "probe3", "pick", "ask"}},
  {"pattern guard and conjunction", &A{N: 3}, nil, 0, 3, []string{"probe1", "probe2", "pick", "small"}},
  {"and short-circuits", &A{N: 0}, nil, 5, 5, []string{"probe1", "probe2", "pick", "ask"}},
  {"pattern guard mismatch, if declines", &B{}, nil, 2, 99, []string{"probe1", "probe2", "pick", "ask"}},
 } {
  h := &Host{Probes: tt.probes, Answer: tt.answer}
  if got := Rewrite(tt.shape, h); got != tt.want || !reflect.DeepEqual(h.Calls, tt.calls) {
   t.Fatalf("%s: got %d calls %v, want %d calls %v", tt.name, got, h.Calls, tt.want, tt.calls)
  }
 }
}`)
	command := exec.Command("go", "test", "-run", "TestBehavior", ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if out, err := command.CombinedOutput(); err != nil {
		generated, _ := os.ReadFile(filepath.Join(root, "rewrite_gen.go"))
		t.Fatalf("generated language forms: %v\n%s\n%s", err, out, generated)
	}
}

func TestHaskellLanguageDiagnostics(t *testing.T) {
	for _, tt := range []struct{ name, old, replacement, want string }{
		{"where cycle", "offset = 5", "offset = shared", "refers to itself"},
		{"where scope", "offset = 5", "offset = n", "not an equation capture"},
		{"where collision", "    offset = 5", "    offset = 5\n    host = 1", "collides"},
		{"where projection", "10 + offset", "10 + offset.N", "cannot be projected"},
		{"bool operands", "n > 0 && Small host n", "n > 0 && not n", "require bool operands"},
		{"bool guard", "n > 0 && Small host n", "n", "guards require bool"},
		{"impure pattern guard", "First (n: n) <- Pick host shape", "First (n: n) <- ask host", "must be pure"},
		{"decline needs choice", "else outputs 7", "else decline", "enclosing ordered alternative"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", languageHost)
			write(t, root, "input.rules", replaceFixture(t, languageRules, tt.old, tt.replacement))
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

package hyrule_test

import (
	"strings"
	"testing"

	"github.com/kojah/hyrule"
)

const listHost = `package fixture
import "fmt"
type Values struct{ values []int }
func (v Values) Len() int { return len(v.values) }
func (v Values) At(i int) int { return v.values[i] }
func (v Values) Sub(low, high int) Values { return Values{v.values[low:high]} }
type Fixed struct{ values []int }
func (v Fixed) Len() int { return len(v.values) }
func (v Fixed) At(i int) int { return v.values[i] }
type Input struct { Slice []int; Seq Values; Fixed Fixed; Count int }
type Signature func(Input) (string, bool)
func Describe(head int, tail Values) string { return fmt.Sprint(head, tail.values) }
func Suffix(tail []int) string { return fmt.Sprint(tail) }
`

// List patterns match Go slices and host sequences by exact arity, or by a
// minimum arity with a rest suffix. A length mismatch falls through.
func TestListPatterns(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", listHost)
	write(t, root, "input.rules", `type Input

host
  Describe
  Suffix

@Empty
rewrite (Input { slice: [], count: 0 }) = "empty"

@Literal
rewrite (Input { slice: [1] }) = "one"

@Same
rewrite (Input { slice: [a, a] }) = "same"

@Nested
rewrite (Input { slice: [_, _, ..([_, _] as rest)] }) = Suffix rest

@Ascending
rewrite (Input { seq: [x, y] })
  | x < y
  =
  "ascending"

@SequenceRest
rewrite (Input { seq: [x, ..tail] }) = Describe x tail

@FixedSequence
rewrite (Input { fixed: [_] as whole }) = "fixed"

@Other
rewrite _ = "other"
`)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestLists(t *testing.T) {
 for _, test := range []struct{input Input; want string}{
  {Input{}, "empty"},
  {Input{Count: 1}, "other"},
  {Input{Slice: []int{1}}, "one"},
  {Input{Slice: []int{2}}, "other"},
  {Input{Slice: []int{3, 3}}, "same"},
  {Input{Slice: []int{3, 4}}, "other"},
  {Input{Slice: []int{1, 2, 3, 4}}, "[3 4]"},
  {Input{Slice: []int{1, 2, 3}}, "other"},
  {Input{Slice: []int{1, 2, 3, 4, 5}}, "other"},
  {Input{Slice: []int{9}, Seq: Values{[]int{1, 2}}}, "ascending"},
  {Input{Slice: []int{9}, Seq: Values{[]int{2, 1}}}, "2 [1]"},
  {Input{Slice: []int{9}, Seq: Values{[]int{5}}}, "5 []"},
  {Input{Slice: []int{9}, Fixed: Fixed{[]int{7}}}, "fixed"},
  {Input{Slice: []int{9}, Fixed: Fixed{[]int{7, 8}}}, "other"},
 } {
  if got, ok := Rewrite(test.input); !ok || got != test.want { t.Fatalf("%+v => %q %t, want %q", test.input, got, ok, test.want) }
 }
}`)
	runUnionFixture(t, root, "TestLists")
}

func TestListPatternDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, pattern, want string }{
		{"not a sequence", `Input { count: [x] }`, "list pattern requires a slice"},
		{"rest needs Sub", `(Input { fixed: [x, ..rest] })`, "Sub(int, int)"},
		{"arity is fixed", `(Input { slice: [x, ..rest, y] })`, `expected "]"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", listHost)
			write(t, root, "input.rules", "type Input\n\n@Check\nrewrite "+test.pattern+" = \"x\"\n")
			expectUnionCompile(t, root, test.want)
		})
	}
}

func TestListPatternFormatting(t *testing.T) {
	input := "@rewrite_L1_Fdecd027b\nrewrite (Wrap ([a, b, ..rest] as whole)) = a\n\n@rewrite_L3_Fdecd027b\nrewrite (Wrap []) = 0\n"
	formatted, err := hyrule.Format("lists.rules", []byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(formatted), "[a, b, ..rest] as whole") || !strings.Contains(string(formatted), "(Wrap [])") {
		t.Fatalf("list pattern formatting:\n%s", formatted)
	}
	again, err := hyrule.Format("lists.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format is not idempotent: %v\n%s", err, again)
	}
}

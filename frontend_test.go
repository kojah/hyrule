package hyrule_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

// Guarded equations must preserve strict qualifier order and isolate each
// branch's locals; remainder also checks the new infix spelling's semantics.
func TestEquationBranchesAndArithmetic(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Signature func(int) int
 func identity(n int)int{return n}
 `)
	write(t, root, "input.rules", `signature total = outputs (-99)
host
  identity
@classify
classify (inputs value)
  | value < 0 = outputs (-1)
  | let remainder = value % 2
  , remainder == 0 = outputs (identity value)
  | otherwise = outputs 1
`)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestEquations(t *testing.T){for _,tt:=range []struct{in,want int}{{-3,-1},{0,0},{3,1},{4,4}}{
 if got:=Rewrite(tt.in);got!=tt.want{t.Fatalf("%d: %d want %d",tt.in,got,tt.want)}
 }}`)
	runUnionFixture(t, root, "TestEquations")
}

func TestEquationLayoutAndEscapes(t *testing.T) {
	good := []string{
		"host\n  Match?\n@Escaped\nrewrite (Match `Match`) = `Match`\n",
		"rewrite (Header (length: length,\n capacity: capacity)) = [length,\n capacity]\n",
		"rewrite value = do\n  true <- ready value\n    else false -> decline\n  value\n<|> 0\n",
	}
	for _, source := range good {
		formatted, err := hyrule.Format("layout.rules", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		again, err := hyrule.Format("layout.rules", formatted)
		if err != nil || string(again) != string(formatted) {
			t.Fatalf("unstable layout: %v\n%s\n%s", err, formatted, again)
		}
	}
	for _, source := range []string{
		"host\n\tMatch?\n", "rewrite _ = [1, 2\n", "rewrite _ = do\n",
		"rewrite _ = 0;\n", "host\n  function argument\n",
		"rewrite " + strings.Repeat("value@", 70) + "_ = 0\n", "rule Old: _ => 0;", "rewrite _ = 0\n  unexpected\n",
	} {
		if _, err := hyrule.Format("bad.rules", []byte(source)); err == nil {
			t.Fatalf("accepted invalid layout %q", source)
		}
	}
}

func TestTargetRejectsMixedEquationFamilies(t *testing.T) {
	root := fixture(t)
	write(t, root, "input.rules", "first _ = 0\n")
	write(t, root, "second.rules", "second _ = 0\n")
	write(t, root, "hyrule.yaml", `version: 1
targets:
  - name: rewrite
    package: .
    rules: [input.rules, second.rules]
    bindings: bindings.go
    signature: Signature
    function: Rewrite
    out: rewrite_gen.go
`)
	c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Compile(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "equation families") {
		t.Fatalf("mixed target families: %v", err)
	}
}

func TestLegacyMigrationPreservesQueryFallback(t *testing.T) {
	// Old spelling is intentional: only the migration API accepts it.
	legacy := `signature total fallback outputs true;
 query ask; build fallback;
 rule First: inputs $value $host
 => query ask $host $value select true => outputs false
 <|> pure outputs (fallback $host);`
	migrated, err := hyrule.Migrate("input.rules", []byte(legacy), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hyrule.Format("input.rules", []byte(legacy)); err == nil {
		t.Fatal("production formatter accepted legacy syntax")
	}
	again, err := hyrule.Format("input.rules", migrated)
	if err != nil || string(again) != string(migrated) {
		t.Fatalf("migration not canonical: %v", err)
	}
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Host struct {Calls int}
 type Signature func(bool,*Host)bool
 func ask(h *Host,value bool)bool{h.Calls++;return value}
 func fallback(h *Host)bool{h.Calls++;return true}
 `)
	write(t, root, "input.rules", string(migrated))
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestMigration(t *testing.T){for _,v:=range []bool{true,false}{
 h:=&Host{};got:=Rewrite(v,h);wantCalls:=2;if v {wantCalls=1}
 if got==v || h.Calls!=wantCalls{t.Fatalf("input %v: %v calls %d",v,got,h.Calls)}
 }}`)
	runUnionFixture(t, root, "TestMigration")
}

func TestCaseFormattingPreservesFollowingCommentsOnce(t *testing.T) {
	source := []byte("rewrite value =\n  case ask value of\n    true -> 1\n    false -> 0\n\n// Next rule.\n// Keep this evidence.\n@Next\nrewrite _ = 2\n")
	first, err := hyrule.Format("comments.rules", source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hyrule.Format("comments.rules", first)
	if err != nil || string(second) != string(first) {
		t.Fatalf("unstable case comments: %v\n%s\n%s", err, first, second)
	}
	for _, comment := range []string{"// Next rule.", "// Keep this evidence."} {
		if strings.Count(string(first), comment) != 1 {
			t.Fatalf("comment duplicated or lost: %s", first)
		}
	}
}

func TestHostFormattingKeepsBindingComments(t *testing.T) {
	source := []byte("host\n  first!\n  // Evidence belongs to second.\n  second!\nrewrite _ = 0\n")
	first, err := hyrule.Format("host.rules", source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hyrule.Format("host.rules", first)
	if err != nil || string(second) != string(first) {
		t.Fatalf("host comments moved: %v\n%s\n%s", err, first, second)
	}
	if strings.Count(string(first), "// Evidence belongs to second.") != 1 {
		t.Fatalf("comment lost or duplicated: %s", first)
	}
}

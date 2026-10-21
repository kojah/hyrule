package hyrule_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

func TestOptionalApplicationParentheses(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", queryHost+"\nconst Count = 7\nfunc Zero() Plan { return Plan{7} }\n")
	explicit := queryDeclarations + `const Count; build Zero;
 pattern Both($x, $y) = (Pair $x $y);
 rule Zero: (Pair 99 _) => (Zero);
 rule Skip: (Pair -1 _) => effects [];
 rule Main: (Both $x $y) when (Positive $y)
 => query $proof = (Ask $x) decide $proof {
  (Proven) => (Wrap effects [(Mark Count)]);
  (Disproven) => (Wrap effects [Count]);
  (Unavailable reason:$why) => query $next = (Ask $y) decide $next {
   (Proven) => effects [1];
   (Disproven) => effects [2];
   (Unavailable reason:$nextWhy) => (Wrap effects [$nextWhy]);
  };
 };
 rule Fallback: _ => effects [Count];
 `
	write(t, root, "input.rules", explicit)
	_, want := compile(t, root)
	shorthand := strings.NewReplacer(
		"(Pair $x $y)", "Pair $x $y", "(Pair 99 _)", "Pair 99 _", "(Zero)", "Zero",
		"(Pair -1 _)", "Pair -1 _", "(Both $x $y)", "Both $x $y", "(Positive $y)", "Positive $y",
		"(Ask $x)", "Ask $x", "(Ask $y)", "Ask $y", "(Proven)", "Proven", "(Disproven)", "Disproven",
		"(Unavailable reason:$why)", "Unavailable reason:$why", "(Unavailable reason:$nextWhy)", "Unavailable reason:$nextWhy",
		"(Wrap effects [(Mark Count)])", "Wrap effects [(Mark Count)]",
		"(Wrap effects [Count])", "Wrap effects [Count]", "(Wrap effects [$nextWhy])", "Wrap effects [$nextWhy]",
	).Replace(explicit)
	write(t, root, "input.rules", shorthand)
	_, got := compile(t, root)
	if !bytes.Equal(got[0].Data, want[0].Data) {
		t.Fatal("optional parentheses changed generated execution")
	}
	for _, source := range []string{explicit, shorthand} {
		formatted, err := hyrule.Format("input.rules", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		again, err := hyrule.Format("input.rules", formatted)
		if err != nil || !bytes.Equal(formatted, again) {
			t.Fatalf("format not idempotent: %v", err)
		}
		if !bytes.Contains(formatted, []byte("(Mark Count)")) {
			t.Fatal("formatter removed nested grouping")
		}
		if source == shorthand && !bytes.Contains(formatted, []byte("query $proof = Ask $x")) {
			t.Fatal("formatter restored optional parentheses")
		}
		if source == explicit && !bytes.Contains(formatted, []byte("query $proof = (Ask $x)")) {
			t.Fatal("formatter removed explicit parentheses")
		}
	}
}

func TestOptionalApplicationKeepsNestedGrouping(t *testing.T) {
	root := fixture(t)
	write(t, root, "input.rules", `match Pair; build Sum; rule Nested: Pair $a $b => Sum (Sum $a 1) $b;`)
	compile(t, root)
	// An unparenthesized nested name is an argument, not an inferred call. The
	// compiler must not guess grouping from a Go hook's arity.
	write(t, root, "input.rules", `match Pair; build Sum; rule Nested: Pair $a $b => Sum Sum $a 1 $b;`)
	config, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Compile(context.Background(), ""); err == nil {
		t.Fatal("inferred grouping for a nested application")
	}
}

func TestBareCallsResolveAcrossFilesAndPreserveConstants(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Signature func(int)(int,bool)
 const Zero = 0
 func One() int { return 1 }
 `)
	write(t, root, "input.rules", `rule Zero: Zero => One; rule Other: _ => Zero;`)
	write(t, root, "schema.rules", `const Zero; build One;`)
	config(t, root, "rewrite_gen.go")
	path := filepath.Join(root, "hyrule.yaml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "hyrule.yaml", strings.Replace(string(contents), "[input.rules]", "[input.rules, schema.rules]", 1))
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestBare(t *testing.T) {
  for _, test := range []struct{in,want int}{{0,1},{2,0}} {
   got,ok:=Rewrite(test.in)
   if !ok || got!=test.want {t.Fatalf("%d => %d,%t",test.in,got,ok)}
  }
 }`)
	runUnionFixture(t, root, "TestBare")
}

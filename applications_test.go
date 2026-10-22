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
	explicit := queryDeclarations + `host
  Count
  Zero

pattern Both x y = Pair x y

@Zero
rewrite (Pair 99 _) = Zero()

@Skip
rewrite (Pair (-1) _) = []

@Main
rewrite (Both x y)
  | Positive y
  =
  do
    proof <- Ask x
    case proof of
      Proven() -> Wrap [Mark ^Count]
      Disproven() -> Wrap [^Count]
      Unavailable (reason: why) -> do
        next <- Ask y
        case next of
          Proven() -> [1]
          Disproven() -> [2]
          Unavailable (reason: nextWhy) -> Wrap [nextWhy]

@Fallback
rewrite _ = [^Count]
`
	write(t, root, "input.rules", explicit)
	_, want := compile(t, root)
	shorthand := strings.NewReplacer(
		"rewrite (Pair 99 _)", "rewrite Pair 99 _",
		"rewrite (Pair (-1) _)", "rewrite Pair (-1) _",
		"rewrite (Both x y)", "rewrite Both x y",
		"Proven()", "Proven", "Disproven()", "Disproven",
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
		if !bytes.Contains(formatted, []byte("[Mark ^Count]")) {
			t.Fatal("formatter changed nested construction")
		}
	}
}

func TestOptionalApplicationKeepsNestedGrouping(t *testing.T) {
	root := fixture(t)
	write(t, root, "input.rules", `host
  Pair?
  Sum

@Nested
rewrite (Pair a b) = Sum (Sum a 1) b
`)
	compile(t, root)
	// An unparenthesized nested name is an argument, not an inferred call. The
	// compiler must not guess grouping from a Go hook's arity.
	write(t, root, "input.rules", `host
  Pair?
  Sum

@Nested
rewrite (Pair a b) = Sum ^Sum a 1 b
`)
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
	write(t, root, "input.rules", `@Zero
rewrite ^Zero = One()

@Other
rewrite _ = ^Zero
`)
	write(t, root, "schema.rules", `host
  Zero
  One
`)
	config(t, root, "rewrite_gen.go")
	path := filepath.Join(root, "hyrule.yaml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "hyrule.yaml", replaceFixture(t, string(contents), "[input.rules]", "[input.rules, schema.rules]"))
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

package hyrule_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/hyrule"
)

func TestReusablePatternsPreserveTypedOrderedMatches(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Node struct{A,B int}
 type Signature func(Node)(int,bool)
 var Builds int
 func Pair(input Node)(left,right int,matched bool){return input.A,input.B,true}
 func Positive(input int)bool{return input>0}
 func Sum(left,right int)int{Builds++;return left+right}
 `)
	write(t, root, "input.rules", `host
  Pair? = Pair
  Positive = Positive
  Sum = Sum

pattern Whole source left right = (Choice left right) as source

pattern Choice left right = (Pair { left, right } | Pair { left: right, right: left })

@Pick
rewrite ((Whole inner x y) as outer)
  | Positive x
  =
  Sum x y

@Literal
rewrite (First Small()) = Sum 0 0
`)
	write(t, root, "second.rules", `// Forward reference in another configured file.
pattern First value = Pair { left: value }
pattern Small = (0 | 1)
`)
	data, err := os.ReadFile(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "hyrule.yaml", replaceFixture(t, string(data), "[input.rules]", "[second.rules, input.rules]"))
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestReuse(t *testing.T){
  for _,input:=range []Node{{2,-1},{-1,2},{0,0}}{
   Builds=0;got,ok:=Rewrite(input);want:=input.A+input.B
   if !ok||got!=want||Builds!=1{t.Fatalf("%v => %d,%v builds=%d",input,got,ok,Builds)}
  }
 }
 `)
	command := exec.Command("go", "test", "-run", "TestReuse", ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated reuse behavior: %v\n%s", err, output)
	}
}

func TestReusablePatternDiagnostics(t *testing.T) {
	cases := []struct{ name, rules, want string }{
		{"duplicate", `pattern Shared = _

pattern Shared = _

@R
rewrite _ = 0
`, "duplicate pattern"},
		{"duplicate parameter", `pattern Shared x x = x

@R
rewrite _ = 0
`, "duplicate pattern parameter"},
		{"unused parameter", `pattern Shared x = _

@R
rewrite _ = 0
`, "unused pattern parameter"},
		{"free capture", `pattern Shared = Pair secret _

@R
rewrite _ = 0
`, "free capture $secret"},
		{"free whole capture", `pattern Shared = (Pair _ _) as secret

@R
rewrite _ = 0
`, "free capture $secret"},
		{"unknown unused body", `pattern Shared = Missing()

@R
rewrite _ = 0
`, "unknown match pattern Missing"},
		{"direct recursion", `pattern Shared = Shared()

@R
rewrite _ = 0
`, "recursive pattern"},
		{"indirect recursion", `pattern Shared = Other()

pattern Other = Shared()

@R
rewrite _ = 0
`, "recursive pattern"},
		{"hook alias collision", `pattern Pair = _

@R
rewrite _ = 0
`, "collides with Go hook Pair"},
		{"hook function collision", `host
  Alias? = Pair

pattern Pair = _

@R
rewrite _ = 0
`, "collides with Go hook Pair"},
		{"arity", `pattern Shared x = Pair x _

@R
rewrite Shared() = 0
`, "reusable pattern arity mismatch"},
		{"named actual", `pattern Shared x = Pair x _

@R
rewrite (Shared { x }) = 0
`, "arguments are positional"},
		{"build use", `pattern Shared = _

@R
rewrite _ = Shared()
`, "expected build binding"},
		{"guard use", `pattern Shared = _

@R
rewrite _
  | Shared()
  =
  0
`, "expected guard binding"},
		{"whole input actual", `pattern Shared x = (Pair _ _) as x

@R
rewrite (Shared 1) = 0
`, "requires a capture or wildcard"},
		{"typed actual", `pattern Shared x = Pair x _

@R
rewrite (Shared "wrong") = 0
`, "mismatched types"},
		{"capture join", `pattern Shared x = (Pair x _ | Pair _ _)

@R
rewrite (Shared x) = 0
`, "identical capture names and types"},
		{"repeated capture", `pattern Shared x = Pair x x

@R
rewrite (Shared x) = x
`, ""},
		{"wildcard actual", `pattern Shared x = (Pair _ _) as x

@R
rewrite (Shared _) = 0
`, ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "input.rules", `host
  Pair? = Pair
`+test.rules)
			config, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = config.Compile(context.Background(), "")
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestReusablePatternExpansionLimits(t *testing.T) {
	var doubling strings.Builder
	doubling.WriteString(`pattern Level0 x = x
`)
	for level := 1; level <= 14; level++ {
		fmt.Fprintf(&doubling, "pattern Level%d x = Pair (Level%d x) (Level%d x)\n", level, level-1, level-1)
	}
	doubling.WriteString(`@R
rewrite _ = 0
`) // oversized unused declaration rejects too
	var chain strings.Builder
	chain.WriteString(`pattern Level0 = _
`)
	for level := 1; level <= 35; level++ {
		fmt.Fprintf(&chain, "pattern Level%d = Level%d()\n", level, level-1)
	}
	chain.WriteString("@R\nrewrite " + strings.Repeat("(Pair ", 35) + "Level35()" + strings.Repeat(" _)", 35) + " = 0\n")
	var alternatives strings.Builder
	alternatives.WriteString(`pattern Choice = (1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9)
`)
	alternatives.WriteString(`@R
rewrite (Pair Choice() Choice()) = 0
`)
	for _, test := range []struct{ name, rules, want string }{
		{"non OR growth", doubling.String(), "exceeds 10000 nodes"},
		{"composed depth", chain.String(), "nesting exceeds 64"},
		{"retained OR bound", alternatives.String(), "exceeds 64 alternatives"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "input.rules", `host
  Pair? = Pair
`+test.rules)
			config, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = config.Compile(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestReusablePatternFormatting(t *testing.T) {
	original := []byte(`// Explicit captures are part of the reusable contract.
pattern Entry request present = Pair { left: request, right: present }

pattern Ready = (true | false)

@R
rewrite (Entry x Ready()) = x
`)
	formatted, err := hyrule.Format("reuse.rules", original)
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("reuse.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format not idempotent: %v\n%s", err, again)
	}
	if !strings.Contains(string(formatted), "pattern Entry request present") {
		t.Fatalf("lost explicit parameter contract: %s", formatted)
	}
}

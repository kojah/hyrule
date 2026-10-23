package hyrule_test

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/hyrule"
)

func write(t *testing.T, root, path, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/fixture\n\ngo 1.27\n")
	write(t, root, "bindings.go", `package fixture
 type Node struct { A, B int }
 type Signature func(Node) (int, bool)
 func Pair(n Node)(int,int,bool){return n.A,n.B,true}
 func Sum(a,b int)int{return a+b}
 func Positive(n int)bool{return n>0}
 // The host can refer to the generated API before its first generation.
 func Use(n Node)(int,bool){return Rewrite(n)}
 `)
	write(t, root, "input.rules", `host
  Pair? = Pair
  Sum = Sum
  Positive = Positive

@Equal
rewrite (Pair x x)
  | Positive x
  =
  Sum x 10

@Fallback
rewrite (Pair a b) = Sum a b
`)
	config(t, root, "rewrite_gen.go")
	return root
}
func config(t *testing.T, root, out string) {
	t.Helper()
	write(t, root, "hyrule.yaml", `version: 1
targets:
 - name: rewrite
   package: .
   bindings: bindings.go
   signature: Signature
   function: Rewrite
   rules: [input.rules]
   out: `+out+"\n")
}
func compile(t *testing.T, root string) (*hyrule.Config, []hyrule.Artifact) {
	t.Helper()
	c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.Compile(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	return c, a
}

func TestGeneratedBehaviorAndOwnership(t *testing.T) {
	root := fixture(t)
	c, a := compile(t, root)
	if err := c.Publish(a, true); err == nil {
		t.Fatal("missing outputs reported fresh")
	}
	if _, err := os.Stat(filepath.Join(root, "rewrite_gen.go")); !os.IsNotExist(err) {
		t.Fatal("check wrote output")
	}
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(a, true); err != nil {
		t.Fatal(err)
	}
	_, again := compile(t, root)
	if !bytes.Equal(a[0].Data, again[0].Data) {
		t.Fatal("nondeterministic generation")
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestRewrite(t *testing.T){for _,n:=range []Node{{2,2},{-1,-1},{3,4}}{
got,ok:=Use(n);want:=n.A+n.B;
if n.A==n.B && n.A>0{want=n.A+10};if !ok||got!=want{t.Fatalf("%v => %d,%v",n,got,ok)}}}
 `)
	cmd := exec.Command("go", "test", "-run", "TestRewrite", ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated behavior: %v\n%s", err, output)
	}
	config(t, root, "renamed_gen.go")
	c, a = compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "rewrite_gen.go")); !os.IsNotExist(err) {
		t.Fatal("obsolete output retained")
	}
	write(t, root, "renamed_gen.go", "package fixture\n// handwritten\n")
	if err := c.Publish(a, false); err == nil || !strings.Contains(err.Error(), "handwritten") {
		t.Fatalf("overwrite safety: %v", err)
	}
}

func TestRuleDiagnostics(t *testing.T) {
	cases := []struct{ name, rules, want string }{
		{"unbound", `@Bad
rewrite _ = missing
`, "unbound capture"},
		{"arity", `host
  Pair? = Pair

@Bad
rewrite (Pair a) = 0
`, "matcher requires"},
		{"literal type", `host
  Pair? = Pair

@Bad
rewrite (Pair "bad" _) = 0
`, "mismatched types"},
		{"overflow", `@Bad
rewrite _ = 999999999999999999999999999999999999
`, "overflows"},
		{"duplicate", `@Bad
rewrite _ = 0

@Bad
rewrite _ = 1
`, "duplicate rule"},
		{"syntax", `rewrite (Pair a = 0`, "input.rules:1:"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "input.rules", tt.rules)
			c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Compile(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %s, got %v", tt.want, err)
			}
		})
	}
}

func TestFormatContract(t *testing.T) {
	input := []byte("// description\nhost\n  Pair? = Pair\n\n@R\nrewrite (Pair a b) = Sum a b\n/* preserve me */\n// tail\n")
	formatted, err := hyrule.Format("test.rules", input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("test.rules", formatted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(formatted, again) {
		t.Fatalf("format not idempotent:\n%s\n%s", formatted, again)
	}
	for _, comment := range []string{"// description", "/* preserve me */", "// tail"} {
		if !bytes.Contains(formatted, []byte(comment)) {
			t.Fatal("lost comment", comment)
		}
	}
}

func TestNestedPatternsAndLiteralFidelity(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 import "go/ast"
 type Signature func(ast.Expr) (string, bool)
 func Binary(x ast.Expr)(ast.Expr,ast.Expr,bool){n,ok:=x.(*ast.BinaryExpr);if !ok{return nil,nil,false};return n.X,n.Y,true}
 func Ident(x ast.Expr)(string,bool){n,ok:=x.(*ast.Ident);if !ok{return "",false};return n.Name,true}
 func Named(x ast.Expr)bool{_,ok:=x.(*ast.Ident);return ok}
 func Text(s string)string{return s}
 `)
	write(t, root, "input.rules", `host
  Binary? = Binary
  Ident? = Ident
  Named = Named
  Text = Text

@Nested
rewrite (Binary ((Ident "x") as lhs) _)
  | Named lhs
  =
  Text "line\nquoted\"\\end"
`)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import("testing";"go/ast")
 func TestNested(t *testing.T){
 got,ok:=Rewrite(&ast.BinaryExpr{X:ast.NewIdent("x")});if !ok||got!="line\nquoted\"\\end"{t.Fatalf("%q %v",got,ok)}
 if _,ok=Rewrite(ast.NewIdent("x"));ok{t.Fatal("unknown shape matched")}
 if _,ok=Rewrite(&ast.BinaryExpr{X:ast.NewIdent("y")});ok{t.Fatal("literal mismatch matched")}
 }
 `)
	cmd := exec.Command("go", "test", "-run", "TestNested", ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated behavior: %v\n%s", err, output)
	}
}

func TestUnsafeEqualityAndEditedObsoleteOutput(t *testing.T) {
	root := fixture(t)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(root, "rewrite_gen.go"), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("// edited\n"); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	config(t, root, "renamed_gen.go")
	c, err = hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Compile(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "edited obsolete") {
		t.Fatalf("edited obsolete file: %v", err)
	}
	root = fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Signature func(any) (int,bool)
 func Pair(x any)(any,any,bool){return x,x,true}
 `)
	write(t, root, "input.rules", `host
  Pair? = Pair

@Unsafe
rewrite (Pair x x) = 1
`)
	c, err = hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Compile(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "interface") {
		t.Fatalf("unsafe equality: %v", err)
	}
}

func TestRetireAllTargets(t *testing.T) {
	root := fixture(t)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "hyrule.yaml", "version: 1\ntargets: []\n")
	c, a = compile(t, root)
	if err := c.Publish(a, true); err == nil {
		t.Fatal("obsolete output reported fresh")
	}
	if _, err := os.Stat(filepath.Join(root, "rewrite_gen.go")); err != nil {
		t.Fatal("check removed output", err)
	}
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "rewrite_gen.go")); !os.IsNotExist(err) {
		t.Fatal("retired output retained")
	}
}

func TestMultipleGuardsShortCircuitAndFallThrough(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Node struct{ A,B int }
 type Signature func(Node)(int,bool)
 var Events []string
 func Pair(n Node)(int,int,bool){return n.A,n.B,true}
 func First(n int)bool{Events=append(Events,"first");return n>0}
 func Second(n int)bool{Events=append(Events,"second");return n<3}
 func Make(n int)int{Events=append(Events,"build");return n}
 func Fallback(n int)int{Events=append(Events,"fallback");return n+10}
 `)
	input := []byte(`host
  Pair? = Pair
  First = First
  Second = Second
  Make = Make
  Fallback = Fallback

@Both
rewrite (Pair a _)
  | First a
  , Second a
  =
  Make a

@Other
rewrite (Pair a _) = Fallback a
`)
	formatted, err := hyrule.Format("input.rules", input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("input.rules", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("multiple-guard formatting: %v", err)
	}
	write(t, root, "input.rules", string(formatted))
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "guards_test.go", `package fixture
 import("testing";"reflect")
 func TestGuards(t *testing.T){
  for _,test:=range []struct{in,want int;events []string}{
   {-1,9,[]string{"first","fallback"}},
   {3,13,[]string{"first","second","fallback"}},
   {2,2,[]string{"first","second","build"}},
  }{
   Events=nil;got,ok:=Rewrite(Node{A:test.in})
   if !ok||got!=test.want||!reflect.DeepEqual(Events,test.events){t.Fatalf("%+v => %d,%v,%v",test,got,ok,Events)}
  }
 }
 `)
	command := exec.Command("go", "test", "-run", "TestGuards", ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("guard behavior: %v\n%s", err, output)
	}
}

// The ownership marker must remain a generated-code marker without becoming a
// second package doc when the host supplies its own package documentation.
func TestGeneratedMarkerIsNotPackageDocumentation(t *testing.T) {
	_, artifacts := compile(t, fixture(t))
	file, err := parser.ParseFile(token.NewFileSet(), "generated.go", artifacts[0].Data, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if !ast.IsGenerated(file) || file.Doc != nil {
		t.Fatal("generated marker is missing or attached as package documentation")
	}
}

// replaceFixture makes mutation-based regressions fail when their source fixture
// changes, rather than silently testing the unmodified happy path.
func replaceFixture(t *testing.T, source, old, replacement string) string {
	t.Helper()
	found := strings.Count(source, old)
	if found == 0 {
		t.Fatalf("fixture mutation missing %q", old)
	}
	return strings.Replace(source, old, replacement, 1)
}

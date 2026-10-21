package hyrule_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
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
	write(t, root, "input.rules", `match Pair = Pair;
 build Sum = Sum;
 guard Positive = Positive;
 rule Equal: (Pair $x $x) && (Positive $x) => (Sum $x 10);
 rule Fallback: (Pair $a $b) => (Sum $a $b);
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
		{"unbound", `rule Bad: _ => $missing;`, "unbound capture"},
		{"arity", `match Pair = Pair; rule Bad: (Pair $a) => 0;`, "matcher requires"},
		{"literal type", `match Pair = Pair; rule Bad: (Pair "bad" _) => 0;`, "mismatched types"},
		{"overflow", `rule Bad: _ => 999999999999999999999999999999999999;`, "overflows"},
		{"duplicate", `rule Bad: _ => 0; rule Bad: _ => 1;`, "duplicate rule"},
		{"syntax", `rule Bad: (Pair $a => 0;`, "input.rules:1:"},
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
	input := []byte("// description\nmatch Pair=Pair;\nrule R:(Pair $a /* preserve me */ $b)=>(Sum $a $b);\n// tail\n")
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
	write(t, root, "input.rules", `match Binary = Binary;
 match Ident = Ident;
 guard Named = Named;
 build Text = Text;
 rule Nested: (Binary $lhs:(Ident "x") _) && (Named $lhs) => (Text "line\nquoted\"\\end");
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
	write(t, root, "input.rules", `match Pair = Pair; rule Unsafe: (Pair $x $x) => 1;`)
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

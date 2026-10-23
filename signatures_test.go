package hyrule_test

import (
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

func signatureConfig(t *testing.T, root, signature string) {
	t.Helper()
	line := ""
	if signature != "" {
		line = "\n   signature: " + signature
	}
	write(t, root, "hyrule.yaml", `version: 1
targets:
 - name: rewrite
   package: .
   bindings: bindings.go`+line+`
   function: Rewrite
   rules: [input.rules]
   out: rewrite_gen.go
`)
}

const signatureRules = `host
  Pair? = Pair
  Sum = Sum

@Sum
rewrite (Pair a b) = Sum a b
`

// Rule-declared types generate the API, with or without a named Go type.
// The host may refer to the function and the named type before generation.
func TestInferredSignatures(t *testing.T) {
	for _, test := range []struct{ name, signature, host, declaration string }{
		{"anonymous", "", "func Use(n Node)(int,bool){return Rewrite(n)}", ""},
		{"generated type", "Signature", "var Use Signature = Rewrite", "type Signature func(arg0 Node) (int, bool)"},
		{"handwritten type", "Signature", "type Signature func(Node)(int,bool)\nvar Use Signature = Rewrite", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", `package fixture
type Node struct { A, B int }
func Pair(n Node)(int,int,bool){return n.A,n.B,true}
func Sum(a,b int)int{return a+b}
`+test.host+"\n")
			write(t, root, "input.rules", "fn rewrite(arg0: Node) -> int\n"+signatureRules)
			signatureConfig(t, root, test.signature)
			config, artifacts := compile(t, root)
			generated := string(artifacts[0].Data)
			if test.declaration != "" && !strings.Contains(generated, test.declaration) ||
				test.declaration == "" && strings.Contains(generated, "type Signature") {
				t.Fatalf("signature declaration want %q:\n%s", test.declaration, generated)
			}
			if err := config.Publish(artifacts, false); err != nil {
				t.Fatal(err)
			}
			write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestInferred(t *testing.T) {
 if got, ok := Use(Node{2, 3}); !ok || got != 5 { t.Fatalf("got %d %t", got, ok) }
}`)
			runUnionFixture(t, root, "TestInferred")
		})
	}
}

// Native rule signatures name their inputs and outputs fields.
func TestInferredNativeSignatures(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
func Positive(n int) bool { return n > 0 }
`)
	write(t, root, "input.rules", `partial fn rewrite(left: int, right: int) -> sum: int, fits: bool

host
  Positive

@Positive
rewrite (inputs { left: a, right: b })
  | Positive a
  =
  outputs { sum: a + b, fits: (a + b) > a }
`)
	write(t, root, "hyrule.yaml", `version: 1
targets:
 - name: add
   package: .
   bindings: bindings.go
   signature: Adder
   function: Add
   rules: [input.rules]
   out: add_gen.go
 - name: valid
   package: .
   bindings: bindings.go
   function: Valid
   rules: [valid.rules]
   out: valid_gen.go
`)
	write(t, root, "valid.rules", `total fn rewrite(arg0: int) -> bool
  fallback outputs false

host
  Positive

@Valid
rewrite (inputs n)
  | Positive n
  =
  outputs true
`)
	config, artifacts := compile(t, root)
	if !strings.Contains(string(artifacts[0].Data), "type Adder func(left int, right int) (sum int, fits bool, _ bool)") {
		t.Fatalf("named signature:\n%s", artifacts[0].Data)
	}
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestNative(t *testing.T) {
 var add Adder = Add
 if sum, fits, ok := add(2, 3); !ok || sum != 5 || !fits { t.Fatalf("got %d %t %t", sum, fits, ok) }
 if _, _, ok := add(-2, 3); ok { t.Fatal("guard failure matched") }
 if !Valid(1) || Valid(-1) { t.Fatal("total signature") }
}`)
	runUnionFixture(t, root, "TestNative")
}

func TestInferredSignatureDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, signature, host, rules, want string }{
		{"mismatched handwritten type", "Signature", "type Signature func(Node)(Node,bool)", "fn rewrite(arg0: Node) -> int\n",
			"but its rules declare func(arg0 Node) (int, bool)"},
		{"no signature", "", "", "", "signature requires declared rule types or a Go signature type"},
		{"unknown type", "", "", "fn rewrite(arg0: Missing) -> int\n", "undefined: Missing"},
		{"implicit arity", "", "", "fn rewrite(arg0: Node, arg1: Node, arg2: Node) -> int\n", "signature must be func(Input[, Host]) (Output, bool)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", `package fixture
type Node struct { A, B int }
func Pair(n Node)(int,int,bool){return n.A,n.B,true}
func Sum(a,b int)int{return a+b}
`+test.host+"\n")
			write(t, root, "input.rules", test.rules+signatureRules)
			signatureConfig(t, root, test.signature)
			expectUnionCompile(t, root, test.want)
		})
	}
}

func TestSignatureTypeFormatting(t *testing.T) {
	input := "partial fn rewrite(value: ssa.Value, arg1: []*Kind, arg2: readonly.Slice[ssa.Value]) -> map[string]Kind\n\n@rewrite_L3_Fdecd027b\nrewrite x = x\n"
	formatted, err := hyrule.Format("signature.rules", []byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := "partial fn rewrite(value: ssa.Value, arg1: []*Kind, arg2: readonly.Slice[ssa.Value]) -> map[string]Kind\n"
	if !strings.HasPrefix(string(formatted), want) {
		t.Fatalf("signature formatting:\n%s", formatted)
	}
	wrapped := "total fn rewrite(left: int, right: int) -> int\n  fallback outputs 0\n\n@rewrite_L6_Fdecd027b\nrewrite x = x\n"
	if formatted, err := hyrule.Format("signature.rules", []byte(wrapped)); err != nil ||
		!strings.HasPrefix(string(formatted), "total fn rewrite(left: int, right: int) -> int\n  fallback outputs 0\n") {
		t.Fatalf("continued signature: %v\n%s", err, formatted)
	}
	if _, err := hyrule.Format("signature.rules", []byte("signature\n\nrewrite x = x\n")); err == nil {
		t.Fatal("bare signature parsed")
	}
}

// Function headers bind named parameters independently of arm captures; each
// calling convention retains its observable Go result and failure contract.
func TestFunctionHeadersAndMatchBodies(t *testing.T) {
	for _, mode := range []string{"", "partial", "total"} {
		t.Run(mode, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", "package fixture\ntype Node struct { A, B int }\n")
			signatureConfig(t, root, "")
			prefix, fallback := mode, ""
			if prefix != "" {
				prefix += " "
			}
			if mode == "total" {
				fallback = "  fallback -1\n"
			}
			rules := prefix + `fn rewrite(request: Node) -> int
` + fallback + `  match request
    @Positive
    Node { A: left, B: right } when left > 0 ->
      go! { request.A + right }
    @Other
    _ -> 0
`
			formatted, err := hyrule.Format("input.rules", []byte(rules))
			if err != nil {
				t.Fatal(err)
			}
			again, err := hyrule.Format("input.rules", formatted)
			if err != nil || string(again) != string(formatted) {
				t.Fatalf("unstable fn format: %v", err)
			}
			write(t, root, "input.rules", string(formatted))
			c, artifacts := compile(t, root)
			if err := c.Publish(artifacts, false); err != nil {
				t.Fatal(err)
			}
			call := "got,ok:=Rewrite(tt.node)"
			if mode == "total" {
				call = "got:=Rewrite(tt.node);ok:=true"
			}
			write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestFunction(t *testing.T){
 for _,tt:=range []struct{node Node;want int}{{Node{2,3},5},{Node{-1,4},0}}{
  `+call+`
  if !ok||got!=tt.want{t.Fatalf("%+v: %d %v",tt,got,ok)}
 }
}`)
			runUnionFixture(t, root, "TestFunction")
		})
	}
}

func TestFunctionBodyWithoutMatch(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", "package fixture\n")
	signatureConfig(t, root, "")
	write(t, root, "input.rules", `total fn rewrite(value: int) -> int
  fallback -1
  do
    require go! { value > 0 }
    go! { value * 2 }
  <|> 0
`)
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestDirect(t *testing.T) {
 if Rewrite(3)!=6||Rewrite(0)!=0{t.Fatal("function body changed strategy behavior")}
}`)
	runUnionFixture(t, root, "TestDirect")
}

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
		{"generated type", "Signature", "var Use Signature = Rewrite", "type Signature func(Node) (int, bool)"},
		{"handwritten type", "Signature", "type Signature func(Node)(int,bool)\nvar Use Signature = Rewrite", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", `package fixture
type Node struct { A, B int }
func Pair(n Node)(int,int,bool){return n.A,n.B,true}
func Sum(a,b int)int{return a+b}
`+test.host+"\n")
			write(t, root, "input.rules", "signature Node -> int\n\n"+signatureRules)
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
	write(t, root, "input.rules", `signature partial left: int, right: int -> sum: int, fits: bool

host
  Positive

@Positive
rewrite (inputs (left: a, right: b))
  | Positive a
  = outputs (sum: a + b, fits: a + b > a)
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
	write(t, root, "valid.rules", `signature total int -> bool = outputs false

host
  Positive

@Valid
rewrite (inputs n)
  | Positive n
  = outputs true
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
		{"mismatched handwritten type", "Signature", "type Signature func(Node)(Node,bool)", "signature Node -> int\n\n",
			"but its rules declare func(Node) (int, bool)"},
		{"no signature", "", "", "", "signature requires declared rule types or a Go signature type"},
		{"unknown type", "", "", "signature Missing -> int\n\n", "undefined: Missing"},
		{"implicit arity", "", "", "signature Node, Node, Node -> int\n\n", "signature must be func(Input[, Host]) (Output, bool)"},
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
	input := "signature   partial  value:ssa.Value,  []*Kind, readonly.Slice[ssa.Value] ->  map[string]Kind\n\nrewrite x = x\n"
	formatted, err := hyrule.Format("signature.rules", []byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := "signature partial value: ssa.Value, []*Kind, readonly.Slice[ssa.Value] -> map[string]Kind\n"
	if !strings.HasPrefix(string(formatted), want) {
		t.Fatalf("signature formatting:\n%s", formatted)
	}
	wrapped := "signature total left: int,\n  right: int\n  -> int = outputs 0\n\nrewrite x = x\n"
	if formatted, err := hyrule.Format("signature.rules", []byte(wrapped)); err != nil ||
		!strings.HasPrefix(string(formatted), "signature total left: int, right: int -> int = outputs 0\n") {
		t.Fatalf("continued signature: %v\n%s", err, formatted)
	}
	if _, err := hyrule.Format("signature.rules", []byte("signature\n\nrewrite x = x\n")); err == nil {
		t.Fatal("bare signature parsed")
	}
}

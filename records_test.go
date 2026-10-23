package hyrule_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

const recordHost = `package fixture
type Kind uint8
const Known Kind=1
type Other uint8
const Foreign Other=1
var Mutable Kind
type Box struct { Kind Kind; Count int; Source string; Any any }
type Signature func(Box)(Box,bool)
`

const recordSchema = `type Box = Box
  kind: Kind
  count: Count
  source: Source
  any: Any

host
  Known = Known
`

func TestTypedConstantPatternAndRecordGuardFallthrough(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", recordHost)
	write(t, root, "input.rules", recordSchema+`@Known
rewrite box@(Box (kind: ^Known, count: n))
  | n >= 0
  =
  box with (count: 7)

@Fallback
rewrite box = box with (count: -1)
`)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestRecords(t *testing.T) {
 for _, test := range []struct{kind Kind; count, want int}{
  {Known,3,7}, {Known,-3,-1}, {Kind(2),3,-1},
 } {
  input:=Box{Kind:test.kind,Count:test.count,Source:"original"}
  got,ok:=Rewrite(input)
  if !ok || got.Count!=test.want || got.Source!=input.Source || got.Kind!=input.Kind || input.Count!=test.count {
   t.Fatalf("input=%+v got=%+v matched=%t",input,got,ok)
  }
 }
}`)
	runUnionFixture(t, root, "TestRecords")
}

func TestStructuralRuleDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, schema, rule, want string }{
		{"constant must be immutable", recordSchema + `host
  Bad = Mutable
`, `box = box`, "function or constant"},
		{"typed constant mismatch", recordSchema + `host
  Bad = Foreign
`,
			`(Box (kind: ^Bad)) = Box (kind: ^Known, count: 0, source: "", any: nil)`, "constant pattern type mismatch"},
		{"unknown constant", recordSchema, `_ = ^Missing`, "unknown constant Missing"},
		{"incomplete schema", `type Box = Box
  kind: Kind
`, `box = box`, "omits Go field"},
		{"duplicate field", `type Box = Box
  kind: Kind
  count: Kind
`, `box = box`, "duplicate record field"},
		{"unknown field", recordSchema, `(Box (missing: 0)) = Box (kind: ^Known, count: 0, source: "", any: nil)`, "unknown matcher field"},
		{"missing construction field", recordSchema, `_ = Box (kind: ^Known)`, "missing named builder arguments"},
		{"unknown update field", recordSchema, `box = box with (missing: 0)`, "unknown update field"},
		{"duplicate update field", recordSchema, `box = box with (count: 0, count: 1)`, "unique named fields"},
		{"update needs captured base", recordSchema, `_ = 0 with (count: 0)`, "explicit captured record base"},
		{"unsafe interface equality", recordSchema, `box@(Box (any: any))
  | any == any
  =
  box`, "safe comparable operands"},
		{"unordered scalar", recordSchema, `box@(Box (count: n))
  | true < false
  =
  box`, "ordered scalar types"},
		{"incompatible comparison", recordSchema, `box@(Box (count: n, source: source))
  | n < source
  =
  box`, "compatible operand types"},
		{"list requires slice", recordSchema, `_ = []`, "slice type"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", recordHost)
			write(t, root, "input.rules", test.schema+"@Check\nrewrite "+test.rule+"\n")
			config, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = config.Compile(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want %q", err, test.want)
			}
		})
	}
}

func TestStructuralSyntaxFormatting(t *testing.T) {
	input := []byte(recordSchema + `@Copy
rewrite box@(Box (kind: ^Known, count: n))
  | n >= 0
  =
  box with (count: n)
`)
	first, err := hyrule.Format("records.rules", input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hyrule.Format("records.rules", first)
	if err != nil || string(first) != string(second) {
		t.Fatalf("format is not idempotent: %v\n%s", err, second)
	}
}

// Effect lists inherit their slice type from both record fields and updates.
// These are complete public results, with no test-only compiler access.
func TestRecordEffectListsRetainTypedContext(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
type Effects []int
type Box struct { Count int; Effects Effects }
type Signature func(Box)(Box,bool)
`)
	write(t, root, "input.rules", `type Box = Box
  count: Count
  effects: Effects

@Construct
rewrite (Box (count: 0)) = Box (count: 1, effects: [7, 8])

@Update
rewrite box@(Box (count: 1)) = box with (effects: [])

@Preserve
rewrite box = box
`)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestEffectFields(t *testing.T) {
 input:=Box{Effects:Effects{3}}
 built,ok:=Rewrite(input)
 if !ok || built.Count!=1 || len(built.Effects)!=2 || built.Effects[0]!=7 || built.Effects[1]!=8 {
  t.Fatalf("constructed=%+v matched=%t",built,ok)
 }
 cleared,ok:=Rewrite(built)
 if !ok || cleared.Count!=1 || cleared.Effects==nil || len(cleared.Effects)!=0 {
  t.Fatalf("updated=%+v matched=%t",cleared,ok)
 }
 if len(input.Effects)!=1 || input.Effects[0]!=3 || len(built.Effects)!=2 {
  t.Fatal("construction mutated an input")
 }
}`)
	runUnionFixture(t, root, "TestEffectFields")
}

// A bodiless type derives its schema from every direct Go field, so rules can
// construct, match and update host structs without a handwritten builder.
// Like a Go composite literal, named construction may omit derived fields.
func TestDerivedRecordSchemas(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
type Node struct { A, B int }
type Copy struct { Source string; Destination, Input int; ID int }
type Signature func(Node)(Copy,bool)
func Pair(n Node)(int,int,bool){return n.A,n.B,true}
`)
	write(t, root, "input.rules", `type Copy

host
  Pair?

@Partial
rewrite (Pair 0 b) = Copy (destination: b)

@Zero
rewrite (Pair 1 1) = Copy()

@Positional
rewrite (Pair a b)
  | a > 0
  = Copy "positional" a b 1

@Named
rewrite (Pair a b)
  | b > 0
  = Copy (id: 2, input: b, destination: a, source: "named")

@Updated
rewrite (Pair a b)
  | let base = Copy "base" a b 3
  = base with (source: "updated")
`)
	if formatted, err := hyrule.Format("input.rules", []byte("type Copy\n\nrewrite x = x\n")); err != nil || !strings.HasPrefix(string(formatted), "type Copy\n") {
		t.Fatalf("implicit storage type was not preserved: %v\n%s", err, formatted)
	}
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(artifacts[0].Data), "Copy{Source: ") {
		t.Fatalf("derived record is not a composite literal:\n%s", artifacts[0].Data)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestDerived(t *testing.T) {
 for _, test := range []struct{input Node; want Copy}{
  {Node{0,5}, Copy{Destination: 5}},
  {Node{1,1}, Copy{}},
  {Node{1,2}, Copy{"positional",1,2,1}},
  {Node{-1,2}, Copy{"named",-1,2,2}},
  {Node{-1,-2}, Copy{"updated",-1,-2,3}},
 } {
  if got,ok:=Rewrite(test.input); !ok || got!=test.want { t.Fatalf("%+v => %+v %t", test.input, got, ok) }
 }
}`)
	runUnionFixture(t, root, "TestDerived")
}

func TestDerivedRecordDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, host, schema, want string }{
		{"ambiguous labels", "type Box struct { Source, source int }", "type Box", "ambiguous"},
		{"inaccessible field", "type Box = strings.Builder", "type Box", "inaccessible direct record field"},
		{"not a struct", "type Box int", "type Box", "must be a Go struct"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", "package fixture\nimport \"strings\"\nvar _ strings.Builder\n"+test.host+"\ntype Signature func(Box)(Box,bool)\n")
			write(t, root, "input.rules", test.schema+"\n\n@Check\nrewrite box = box\n")
			expectUnionCompile(t, root, test.want)
		})
	}
}

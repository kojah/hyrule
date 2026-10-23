package hyrule_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

const unionHost = `package fixture
 type Tag uint8
 const(NumberTag Tag=iota;EmptyTag;WrapTag)
 type Atom struct{Kind Tag;Number int}
 type Value struct{Kind Tag;Number int;Atom Atom}
 type Signature func(Value)(Value,bool)
 func Positive(number int)bool{return number>0}
 func Increment(number int)int{return number+1}
 `
const unionSchema = `type Atom = Atom
  tag Kind
  AtomNumber (value: Number) = NumberTag
  AtomEmpty = EmptyTag

exhaustive type Value = Value
  tag Kind
  Number (value: Number) = NumberTag
  Empty = EmptyTag
  Wrapped (atom: Atom) = WrapTag
`

func TestTaggedUnionMatchingConstructionAndGuardFallback(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", unionHost)
	write(t, root, "input.rules", unionSchema+`host
  Positive = Positive
  Increment = Increment

pattern Numeric number = (Number { value: number } | Wrapped { atom: AtomNumber { value: number } })

@Positive
rewrite (Numeric number)
  | Positive number
  =
  Number { value: Increment number }

@Number
rewrite (Number { value: number }) = Number { value: number }

@Wrapped
rewrite (Wrapped { atom }) = Wrapped { atom }

@Empty
rewrite Empty() = Empty()
`)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestUnions(t *testing.T){
 cases:=[]struct{input,want Value;ok bool}{
  {Value{Kind:NumberTag,Number:3},Value{Kind:NumberTag,Number:4},true},
  {Value{Kind:NumberTag,Number:-2},Value{Kind:NumberTag,Number:-2},true},
  {Value{Kind:WrapTag,Atom:Atom{Kind:NumberTag,Number:5}},Value{Kind:NumberTag,Number:6},true},
  {Value{Kind:WrapTag,Atom:Atom{Kind:EmptyTag}},Value{Kind:WrapTag,Atom:Atom{Kind:EmptyTag}},true},
  {Value{Kind:EmptyTag,Number:99},Value{Kind:EmptyTag},true},
  {Value{Kind:99},Value{},false},
  {Value{Kind:WrapTag,Atom:Atom{Kind:99}},Value{},false},
 }
 for _,c:=range cases{got,ok:=Rewrite(c.input);if ok!=c.ok||got!=c.want{t.Fatalf("%+v => %+v,%v want %+v,%v",c.input,got,ok,c.want,c.ok)}}
 }
 `)
	runUnionFixture(t, root, "TestUnions")
	// A checked root dispatch reads the root discriminator once; nested union
	// validation and matching refer to their own fields and preserve correlation.
	if count := strings.Count(string(artifacts[0].Data), "harg0.Kind"); count != 1 {
		t.Fatalf("root discriminator inspected %d times", count)
	}
}

func TestTaggedUnionFallbackKeepsClosedDomain(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", unionHost)
	write(t, root, "input.rules", unionSchema+`@Keep
rewrite whole = whole
`)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestClosed(t *testing.T){
 if got,ok:=Rewrite(Value{Kind:EmptyTag});!ok||got.Kind!=EmptyTag{t.Fatal("catchall lost declared variant")}
 if _,ok:=Rewrite(Value{Kind:99});ok{t.Fatal("catchall admitted undeclared tag")}
 }`)
	runUnionFixture(t, root, "TestClosed")
}

func TestTaggedUnionPartialRecognizerAndCoverage(t *testing.T) {
	for _, test := range []struct{ name, schema, rules, want string }{
		{"partial allowed", replaceFixture(t, unionSchema, "exhaustive type Value", "type Value"), `@One
rewrite Number() = Empty()
`, ""},
		{"missing variant", unionSchema, `@One
rewrite Number() = Empty()
`, "non-exhaustive union Value: missing Empty, Wrapped"},
		{"guard is not coverage", unionSchema, `host
  Positive = Positive

@One
rewrite (Number { value: n })
  | Positive n
  =
  Empty()

@Rest
rewrite (Empty() | Wrapped()) = Empty()
`, "non-exhaustive union Value: unproven Number"},
		{"literal is not coverage", unionSchema, `@One
rewrite (Number { value: 1 }) = Empty()

@Rest
rewrite (Empty() | Wrapped()) = Empty()
`, "non-exhaustive union Value: missing Number { value: _ }"},
		{"unconditional fallback", unionSchema, `@Any
rewrite _ = Empty()
`, ""},
		{"guarded fallback", unionSchema, `host
  Positive = Positive

@Any
rewrite _
  | Positive 1
  =
  Empty()
`, "non-exhaustive union Value: unproven Number, Empty, Wrapped"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", unionHost)
			write(t, root, "input.rules", test.schema+test.rules)
			expectUnionCompile(t, root, test.want)
		})
	}
}

func TestTaggedUnionRepresentationDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, schema, rule, want string }{
		{"duplicate tag", `type V = Value
  tag Kind
  A = NumberTag
  B = NumberTag
`, `_`, "duplicate variant tag value"},
		{"unknown tag", `type V = Value
  tag Kind
  A = Missing
`, `_`, "variant tag requires"},
		{"missing selector", `type V = Value
  tag Missing
  A = NumberTag
`, `_`, "missing direct union tag"},
		{"missing storage", `type V = Value
  tag Kind
  A (value: Missing) = NumberTag
`, `_`, "invalid direct variant field"},
		{"tag is not payload", `type V = Value
  tag Kind
  A (value: Kind) = NumberTag
`, `_`, "invalid direct variant field"},
		{"duplicate logical field", `type V = Value
  tag Kind
  A (value: Number, value: Atom) = NumberTag
`, `_`, "duplicate union field"},
		{"duplicate storage field", `type V = Value
  tag Kind
  A (first: Number, second: Number) = NumberTag
`,
			`_`, "duplicate union storage field"},
		{"constructor collision", `host
  A? = Positive

type V = Value
  tag Kind
  A = NumberTag
`, `_`, "colliding constructor"},
		{"empty union", `type V = Value
  tag Kind
`, `_`, "at least one variant"},
		{"duplicate union", `type V = Value
  tag Kind
  A = NumberTag

type V = Atom
  tag Kind
  B = EmptyTag
`, `_`, "duplicate union"},
		{"wrong child type", unionSchema, `Wrapped { atom: Number() }`, "matcher input type mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", unionHost)
			write(t, root, "input.rules", test.schema+"@Any\nrewrite "+test.rule+" = Empty()\n")
			expectUnionCompile(t, root, test.want)
		})
	}
}

func TestTaggedUnionFormat(t *testing.T) {
	original := []byte(unionSchema + `@Any
rewrite _ = Empty()
`)
	formatted, err := hyrule.Format("union.rules", original)
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("union.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format not idempotent: %v\n%s", err, again)
	}
	if !strings.Contains(string(formatted), "exhaustive type Value = Value\n  tag Kind") {
		t.Fatal("lost checked schema contract")
	}
}

func expectUnionCompile(t *testing.T, root, want string) {
	t.Helper()
	config, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = config.Compile(context.Background(), "")
	if want == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func runUnionFixture(t *testing.T, root, name string) {
	t.Helper()
	command := exec.Command("go", "test", "-run", name, ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("union behavior: %v\n%s", err, output)
	}
}

func TestEmptyVariantAliasOptional(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", unionHost)
	schema := replaceFixture(t, unionSchema, "\n  Empty = EmptyTag", "\n  EmptyTag = EmptyTag")
	rules := `@Number
rewrite (Number { value: n }) = Number { value: n }

@Empty
rewrite EmptyTag = EmptyTag

@Wrapped
rewrite (Wrapped { atom }) = Wrapped { atom }
`
	write(t, root, "input.rules", schema+rules)
	_, expected := compile(t, root)
	shorthand := replaceFixture(t, schema, "EmptyTag = EmptyTag", "EmptyTag")
	write(t, root, "input.rules", shorthand+rules)
	_, actual := compile(t, root)
	if string(actual[0].Data) != string(expected[0].Data) {
		t.Fatal("omitting same-name variant alias changed generated Go")
	}
	for _, input := range []string{schema + rules, shorthand + rules} {
		formatted, err := hyrule.Format("union.rules", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		again, err := hyrule.Format("union.rules", formatted)
		if err != nil || string(again) != string(formatted) {
			t.Fatalf("format not idempotent: %v", err)
		}
		want := "EmptyTag = EmptyTag"
		if input == shorthand+rules {
			want = "  EmptyTag\n"
		}
		if !strings.Contains(string(formatted), want) {
			t.Fatalf("formatter changed variant spelling:\n%s", formatted)
		}
		if !strings.Contains(string(formatted), "Number (value: Number) = NumberTag") {
			t.Fatal("formatter removed nonempty variant fields")
		}
	}
	for _, input := range []string{
		"type Value = Value\n  tag Kind\n  EmptyTag;\n",
		"type Value = Value\n  tag Kind\n  EmptyTag Wrapped\n",
		"type Value = Value\n  tag Kind\n  Number = NumberTag value: Number\n",
	} {
		if _, err := hyrule.Format("union.rules", []byte(input)); err == nil {
			t.Fatalf("accepted malformed declaration %q", input)
		}
	}
}

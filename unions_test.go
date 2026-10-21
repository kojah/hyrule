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
const unionSchema = `union Atom = Atom tag Kind {
 AtomNumber = NumberTag { value: Number; };
 AtomEmpty = EmptyTag {};
 };
 exhaustive union Value = Value tag Kind {
 Number = NumberTag { value: Number; };
 Empty = EmptyTag {};
 Wrapped = WrapTag { atom: Atom; };
 };
 `

func TestTaggedUnionMatchingConstructionAndGuardFallback(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", unionHost)
	write(t, root, "input.rules", unionSchema+`guard Positive = Positive; build Increment = Increment;
 pattern Numeric($number) = (or (Number value:$number) (Wrapped atom:(AtomNumber value:$number)));
 rule Positive: (Numeric $number) && (Positive $number) => (Number value:(Increment $number));
 rule Number: (Number value:$number) => (Number value:$number);
 rule Wrapped: (Wrapped atom:$atom) => (Wrapped atom:$atom);
 rule Empty: (Empty) => (Empty);`)
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
	write(t, root, "input.rules", unionSchema+`rule Keep: $whole => $whole;`)
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
		{"partial allowed", strings.Replace(unionSchema, "exhaustive union Value", "union Value", 1), `rule One: (Number) => (Empty);`, ""},
		{"missing variant", unionSchema, `rule One: (Number) => (Empty);`, "missing unconditional variants Empty, Wrapped"},
		{"guard is not coverage", unionSchema, `guard Positive = Positive;
 rule One: (Number value:$n) && (Positive $n) => (Empty);
 rule Rest: (or (Empty) (Wrapped)) => (Empty);`, "missing unconditional variants Number"},
		{"literal is not coverage", unionSchema, `rule One: (Number value:1) => (Empty);
 rule Rest: (or (Empty) (Wrapped)) => (Empty);`, "missing unconditional variants Number"},
		{"unconditional fallback", unionSchema, `rule Any: _ => (Empty);`, ""},
		{"guarded fallback", unionSchema, `guard Positive = Positive; rule Any: _ && (Positive 1) => (Empty);`, "missing unconditional variants"},
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
		{"duplicate tag", `union V = Value tag Kind { A = NumberTag {}; B = NumberTag {}; };`, `_`, "duplicate variant tag value"},
		{"unknown tag", `union V = Value tag Kind { A = Missing {}; };`, `_`, "variant tag requires"},
		{"missing selector", `union V = Value tag Missing { A = NumberTag {}; };`, `_`, "missing direct union tag"},
		{"missing storage", `union V = Value tag Kind { A = NumberTag { value: Missing; }; };`, `_`, "invalid direct variant field"},
		{"tag is not payload", `union V = Value tag Kind { A = NumberTag { value: Kind; }; };`, `_`, "invalid direct variant field"},
		{"duplicate logical field", `union V = Value tag Kind { A = NumberTag { value: Number; value: Atom; }; };`, `_`, "duplicate union field"},
		{"duplicate storage field", `union V = Value tag Kind { A = NumberTag { first: Number; second: Number; }; };`,
			`_`, "duplicate union storage field"},
		{"constructor collision", `match A = Positive; union V = Value tag Kind { A = NumberTag {}; };`, `_`, "colliding constructor"},
		{"empty union", `union V = Value tag Kind {};`, `_`, "at least one variant"},
		{"duplicate union", `union V = Value tag Kind { A = NumberTag {}; }; union V = Atom tag Kind { B = EmptyTag {}; };`, `_`, "duplicate union"},
		{"wrong child type", unionSchema, `(Wrapped atom:(Number))`, "matcher input type mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", unionHost)
			write(t, root, "input.rules", test.schema+`rule Any: `+test.rule+` => (Empty);`)
			expectUnionCompile(t, root, test.want)
		})
	}
}

func TestTaggedUnionFormat(t *testing.T) {
	original := []byte(unionSchema + `rule Any:_=>(Empty);`)
	formatted, err := hyrule.Format("union.rules", original)
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("union.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format not idempotent: %v\n%s", err, again)
	}
	if !strings.Contains(string(formatted), "exhaustive union Value = Value tag Kind") {
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

package hyrule_test

import (
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

// Named fields select by label, nested aliases preserve unmentioned fields,
// and updates leave the original record intact.
func TestBracedRecordsAndAliases(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Header struct { Length, Capacity int }
 type Request struct { Header Header; Count int; Source string }
 type Signature func(Request) (Request, bool)
 `)
	source := `type Request
 type Header
 fn rewrite(input: Request) -> Request
   match input
     Request {
       count,
       header: Header { length } as header,
     } as request when count >= 0 ->
       request with { header: header with { length: length + count }, count }
 `
	// Declarations begin at column one; nested indentation stays relative.
	source = strings.ReplaceAll(source, "\n ", "\n")
	formatted, err := hyrule.Format("input.rules", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("input.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("unstable formatting: %v\n%s", err, again)
	}
	write(t, root, "input.rules", string(formatted))
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestRewrite(t *testing.T) {
  original := Request{Header: Header{Length: 2, Capacity: 10}, Count: 3, Source: "keep"}
  got, ok := Rewrite(original)
  want := original; want.Header.Length = 5
  if !ok || got != want || original.Header.Length != 2 { t.Fatalf("got %+v, %v; input %+v", got, ok, original) }
  original.Count = -1
  if _, ok := Rewrite(original); ok { t.Fatal("negative count matched") }
 }
 `)
	runUnionFixture(t, root, "TestRewrite")
}

func TestRecordSyntaxRejectsRetiredForms(t *testing.T) {
	for _, source := range []string{
		"rewrite request@(Request count) = request\n",
		"rewrite (Request (count: count)) = count\n",
		"rewrite request = request with (count: 0)\n",
		"rewrite (Request { count } as first as second) = first\n",
		"rewrite _ = Request { count: 0\n",
		"rewrite (Request { Size() }) = 0\n",
	} {
		if _, err := hyrule.Format("invalid.rules", []byte(source)); err == nil {
			t.Fatalf("accepted invalid syntax: %s", source)
		}
	}
}

// Escaped captures must stay captures even when a constructor has the same name.
func TestFieldPunningPreservesEscapedCaptures(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Box struct { Count int }
 type Signature func(Box) (int, bool)
 `)
	source := "type Box\npattern count value = Box { count: value }\nrewrite (Box { count: `count` }) = `count`\n"
	formatted, err := hyrule.Format("input.rules", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "input.rules", string(formatted))
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestCapture(t *testing.T) {
  if got, ok := Rewrite(Box{Count: 7}); !ok || got != 7 { t.Fatalf("%d %v", got, ok) }
 }
 `)
	runUnionFixture(t, root, "TestCapture")
}

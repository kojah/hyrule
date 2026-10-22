package hyrule_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

func TestAliasFreeBindings(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", queryHost+"\nconst Count = 4\n")
	explicit := queryDeclarations + `host
  Count = Count

@Main
rewrite (Pair x y)
  | Positive y
  =
  do
    proof <- Ask x
    case proof of
      Proven() -> [Mark ^Count]
      Disproven() -> []
      Unavailable (reason: why) -> Wrap [why]
`
	// The same host declarations and rule must emit identical Go, including the
	// ordered query continuation, regardless of how the bindings are spelled.
	write(t, root, "input.rules", explicit)
	_, want := compile(t, root)
	implicit := explicit
	for _, name := range []string{"Pair", "Positive", "Ask", "Mark", "Wrap", "Count"} {
		implicit = strings.ReplaceAll(implicit, " = "+name+"\n", "\n")
	}
	write(t, root, "input.rules", implicit)
	_, got := compile(t, root)
	if len(got) != len(want) {
		t.Fatalf("artifact count: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if !bytes.Equal(got[i].Data, want[i].Data) {
			t.Fatalf("alias-free binding changed generated artifact %d", i)
		}
	}

}

func TestBindingShorthandFormatting(t *testing.T) {
	for _, mark := range []string{"?", "!", ""} {
		t.Run("mark"+mark, func(t *testing.T) {
			input := "host\n  exactName" + mark + "\n  first" + mark + "\n  second" + mark + "\n  Alias" + mark + " = exactName\n"
			got, err := hyrule.Format("bindings.rules", []byte(input))
			if err != nil || string(got) != input {
				t.Fatalf("format = %q, %v; want %q", got, err, input)
			}
			for _, bad := range []string{"host\n", "host\n  = Missing\n", "host\n  Name" + mark + " =\n", "host\n  Name" + mark + ", Other\n"} {
				if _, err := hyrule.Format("bindings.rules", []byte(bad)); err == nil {
					t.Errorf("accepted malformed declaration %q", bad)
				}
			}
		})
	}
}

func TestSameNameUnionVariants(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", unionHost+"\nconst (Number = NumberTag; Empty = EmptyTag; Wrapped = WrapTag)\n")
	explicit := strings.NewReplacer(
		"\n  Number (value: Number) = NumberTag", "\n  Number (value: Number) = Number",
		"\n  Empty = EmptyTag", "\n  Empty = Empty",
		"Wrapped (atom: Atom) = WrapTag", "Wrapped (atom: Atom) = Wrapped").Replace(unionSchema)
	explicit += `@Number
rewrite (Number n) = Number n

@Empty
rewrite Empty() = Empty()

@Wrapped
rewrite (Wrapped atom) = Wrapped atom
`
	write(t, root, "input.rules", explicit)
	_, want := compile(t, root)
	implicit := strings.NewReplacer("\n  Number (value: Number) = Number", "\n  Number (value: Number)", "\n  Empty = Empty", "\n  Empty",
		"Wrapped (atom: Atom) = Wrapped", "Wrapped (atom: Atom)").Replace(explicit)
	write(t, root, "input.rules", implicit)
	_, got := compile(t, root)
	if !bytes.Equal(got[0].Data, want[0].Data) {
		t.Fatal("same-name variants changed generated code")
	}
	formatted, err := hyrule.Format("input.rules", []byte(implicit))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(formatted, []byte("  Number (value: Number)\n")) || !bytes.Contains(formatted, []byte("AtomNumber (value: Number) = NumberTag")) {
		t.Fatalf("formatter lost shorthand or explicit alias:\n%s", formatted)
	}
	again, err := hyrule.Format("input.rules", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("format not idempotent: %v", err)
	}
	for _, bad := range []string{"Number =", "Number | Empty", "Number, Empty"} {
		if _, err := hyrule.Format("input.rules", []byte("type Value = Value\n  tag Kind\n  "+bad+"\n")); err == nil {
			t.Errorf("accepted malformed variant %q", bad)
		}
	}
}

func TestSameNameStorageFields(t *testing.T) {
	for _, test := range []struct{ name, schema string }{
		{"record", `type Value = Value
  kind: Kind
  Number: Number
  Atom: Atom
`},
		{"union", `type Value = Value
  tag Kind
  NumberTag (Number: Number)
  EmptyTag
  WrapTag (Atom: Atom)
`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", unionHost)
			rule := `@Identity
rewrite value = value
`
			write(t, root, "input.rules", test.schema+rule)
			_, want := compile(t, root)
			shorthand := strings.NewReplacer("Number: Number", "Number", "Atom: Atom", "Atom").Replace(test.schema)
			write(t, root, "input.rules", shorthand+rule)
			_, got := compile(t, root)
			if !bytes.Equal(got[0].Data, want[0].Data) {
				t.Fatal("same-name fields changed generated code")
			}
			formatted, err := hyrule.Format("input.rules", []byte(shorthand+rule))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(formatted, []byte("Number")) || bytes.Contains(formatted, []byte("Number: Number")) {
				t.Fatalf("formatter lost field shorthand:\n%s", formatted)
			}
			if test.name == "record" && !bytes.Contains(formatted, []byte("kind: Kind")) {
				t.Fatal("formatter lost explicit mapping")
			}
			again, err := hyrule.Format("input.rules", formatted)
			if err != nil || !bytes.Equal(formatted, again) {
				t.Fatalf("format not idempotent: %v", err)
			}
			for _, invalid := range []string{"number", "NumberDuplicate"} {
				write(t, root, "input.rules", replaceFixture(t, shorthand, "Number", invalid)+rule)
				config, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := config.Compile(context.Background(), ""); err == nil {
					t.Errorf("accepted invalid field mapping %q", invalid)
				}
			}
		})
	}
}

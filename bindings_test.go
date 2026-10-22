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
	explicit := queryDeclarations + `const Count = Count;
 rule Main: (Pair $x $y) && (Positive $y)
 => query $proof = (Ask $x) decide $proof {
  (Proven) => effects [(Mark Count)];
  (Disproven) => effects [];
  (Unavailable reason:$why) => (Wrap effects [$why]);
 };
 `
	// The same host declarations and rule must emit identical Go, including the
	// ordered query continuation, regardless of how the bindings are spelled.
	write(t, root, "input.rules", explicit)
	_, want := compile(t, root)
	implicit := explicit
	for _, name := range []string{"Pair", "Positive", "Ask", "Mark", "Wrap", "Count"} {
		implicit = strings.ReplaceAll(implicit, name+" = "+name+";", name+";")
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
	grouped := strings.ReplaceAll(implicit, "build Mark;\n build Wrap;", "build Mark, Wrap;\n")
	write(t, root, "input.rules", grouped)
	_, groupedArtifacts := compile(t, root)
	for i := range groupedArtifacts {
		if !bytes.Equal(groupedArtifacts[i].Data, want[i].Data) {
			t.Fatalf("grouped bindings changed generated artifact %d", i)
		}
	}
}

func TestBindingShorthandFormatting(t *testing.T) {
	for _, kind := range []string{"match", "guard", "build", "query", "const"} {
		t.Run(kind, func(t *testing.T) {
			input := kind + " exactName;\n" + kind + " first, second, third;\n" + kind + " Alias = exactName;\n"
			got, err := hyrule.Format("bindings.rules", []byte(input))
			if err != nil || string(got) != input {
				t.Fatalf("format = %q, %v; want %q", got, err, input)
			}
			for _, bad := range []string{kind + ";", kind + " Name = ;", kind + " Name Other;", kind + " Name,;",
				kind + " Name, Other = Hook;", kind + " Name = Hook, Other;"} {
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
	explicit := strings.NewReplacer("\n Number = NumberTag", "\n Number = Number",
		"\n Empty = EmptyTag", "\n Empty = Empty",
		"\n Wrapped = WrapTag", "\n Wrapped = Wrapped").Replace(unionSchema)
	explicit += `rule Number: (Number $n) => (Number $n);
 rule Empty: (Empty) => (Empty);
 rule Wrapped: (Wrapped $atom) => (Wrapped $atom);`
	write(t, root, "input.rules", explicit)
	_, want := compile(t, root)
	implicit := strings.NewReplacer("Number = Number {", "Number {", "Empty = Empty {", "Empty {", "Wrapped = Wrapped {", "Wrapped {").Replace(explicit)
	write(t, root, "input.rules", implicit)
	_, got := compile(t, root)
	if !bytes.Equal(got[0].Data, want[0].Data) {
		t.Fatal("same-name variants changed generated code")
	}
	formatted, err := hyrule.Format("input.rules", []byte(implicit))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(formatted, []byte("  Number {")) || !bytes.Contains(formatted, []byte("AtomNumber = NumberTag")) {
		t.Fatalf("formatter lost shorthand or explicit alias:\n%s", formatted)
	}
	again, err := hyrule.Format("input.rules", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("format not idempotent: %v", err)
	}
	for _, bad := range []string{"Number = {};", "Number | Empty {};", "Number, Empty {};"} {
		if _, err := hyrule.Format("input.rules", []byte("union Value = Value tag Kind {"+bad+"};")); err == nil {
			t.Errorf("accepted malformed variant %q", bad)
		}
	}
}

func TestSameNameStorageFields(t *testing.T) {
	for _, test := range []struct{ name, schema string }{
		{"record", `record Value = Value { kind: Kind; Number: Number; Atom: Atom; };`},
		{"union", `union Value = Value tag Kind { NumberTag { Number: Number; }; EmptyTag {}; WrapTag { Atom: Atom; }; };`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", unionHost)
			rule := "\nrule Identity: $value => $value;\n"
			write(t, root, "input.rules", test.schema+rule)
			_, want := compile(t, root)
			shorthand := strings.NewReplacer("Number: Number;", "Number;", "Atom: Atom;", "Atom;").Replace(test.schema)
			write(t, root, "input.rules", shorthand+rule)
			_, got := compile(t, root)
			if !bytes.Equal(got[0].Data, want[0].Data) {
				t.Fatal("same-name fields changed generated code")
			}
			formatted, err := hyrule.Format("input.rules", []byte(shorthand+rule))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(formatted, []byte("Number;")) || bytes.Contains(formatted, []byte("Number: Number")) {
				t.Fatalf("formatter lost field shorthand:\n%s", formatted)
			}
			if test.name == "record" && !bytes.Contains(formatted, []byte("kind: Kind;")) {
				t.Fatal("formatter lost explicit mapping")
			}
			again, err := hyrule.Format("input.rules", formatted)
			if err != nil || !bytes.Equal(formatted, again) {
				t.Fatalf("format not idempotent: %v", err)
			}
			for _, invalid := range []string{"number;", "Number; Number;"} {
				write(t, root, "input.rules", strings.Replace(shorthand, "Number;", invalid, 1)+rule)
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

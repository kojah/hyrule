package hyrule_test

import (
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

const accessorHost = `package fixture
import "go/ast"
type Box struct{ n int }
//hyrule:view
func (b Box) Size() int { return b.n }
func (b Box) Hidden() int { return b.n }
func (b Box) Scaled(k int) int { return b.n * k }
type Input struct { Expr ast.Expr; Box Box }
`

func accessorConfig(t *testing.T, root, views string) {
	t.Helper()
	write(t, root, "ast.views", views)
	write(t, root, "hyrule.yaml", `version: 1
views: [ast.views]
targets:
 - name: rewrite
   package: .
   bindings: bindings.go
   function: Rewrite
   rules: [input.rules]
   out: rewrite_gen.go
`)
}

const accessorViews = `// Pure go/ast accessors.
go/ast
  Ident.IsExported
  Node.Pos
`

// Patterns project methods opted in by a .views file or //hyrule:view.
func TestAccessorProjections(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", accessorHost)
	accessorConfig(t, root, accessorViews)
	write(t, root, "input.rules", `fn rewrite(arg0: Input) -> string

@Exported
rewrite (Input { Expr: ast.Ident { Name: name, IsExported(): true } }) = name

@Three
rewrite (Input { Box: Box { Size(): 3 } }) = "three"

@Seven
rewrite (Input { Expr: ast.Expr { Pos(): 7 } }) = "seven"
`)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import ("go/ast"; "testing")
func TestAccessors(t *testing.T) {
 var nilIdent *ast.Ident
 for _, test := range []struct{input Input; want string; ok bool}{
  {Input{Expr: &ast.Ident{Name: "Go"}}, "Go", true},
  {Input{Expr: &ast.Ident{Name: "go"}}, "", false},
  {Input{Expr: &ast.Ident{Name: "go", NamePos: 7}}, "seven", true},
  {Input{Expr: nilIdent, Box: Box{3}}, "three", true},
  {Input{Box: Box{4}}, "", false},
 } {
  if got, ok := Rewrite(test.input); ok != test.ok || got != test.want { t.Fatalf("%+v => %q %t", test.input, got, ok) }
 }
}`)
	runUnionFixture(t, root, "TestAccessors")
	if formatted, err := hyrule.Format("input.rules", []byte("@rewrite_L1_Fdecd027b\nrewrite (Box { Size(): 3 }) = 0\n")); err != nil ||
		!strings.Contains(string(formatted), "Box { Size(): 3 }") {
		t.Fatalf("accessor formatting: %v\n%s", err, formatted)
	}
}

func TestAccessorDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, views, pattern, want string }{
		{"not opted in", accessorViews, "(Input { Box: Box { Hidden(): x } })", "Hidden is not an opted-in accessor"},
		{"arguments", accessorViews, "(Input { Box: Box { Scaled(): x } })", "not an opted-in accessor"},
		{"unknown method", accessorViews, "(Input { Box: Box { Missing(): x } })", "unknown Go method Missing"},
		{"bad views entry", "go/ast\n  Ident.Missing\n", "_", "unknown accessor method go/ast.Ident.Missing"},
		{"views method arguments", "go/ast\n  CommentGroup.Text\n  Scope.Lookup\n", "_", "must take no arguments"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", accessorHost)
			accessorConfig(t, root, test.views)
			write(t, root, "input.rules", "fn rewrite(arg0: Input) -> string\n\n@Check\nrewrite "+test.pattern+" = \"\"\n")
			expectUnionCompile(t, root, test.want)
		})
	}
}

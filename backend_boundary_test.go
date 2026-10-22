package hyrule_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The matching graph and shared source compiler must remain usable without
// inheriting Go's source emitter, type model or package loader.
func TestSharedCompilerHasNoBackendDependency(t *testing.T) {
	for _, directory := range []string{"internal/compiler", "internal/matchir", "internal/semantic"} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			count++
			name := filepath.Join(directory, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range file.Imports {
				dependency, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(dependency, "go/") || strings.Contains(dependency, "/gogen") ||
					strings.HasSuffix(dependency, "/syntax") || strings.HasSuffix(dependency, "/frontend") ||
					strings.HasPrefix(dependency, "golang.org/x/tools") {
					t.Errorf("shared compiler imports backend machinery: %s -> %s", name, dependency)
				}
			}
		}
		if count == 0 {
			t.Fatalf("empty shared compiler package: %s", directory)
		}
	}
}

// Parser representation must not leak into planning or host code generation.
// This contract lets the surface language be replaced independently.
func TestSemanticConsumersDoNotImportParser(t *testing.T) {
	for _, directory := range []string{"internal/compiler", "internal/gogen", "internal/recordgen", "internal/semantic", "internal/matchir"} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			name := filepath.Join(directory, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range file.Imports {
				dependency, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(dependency, "/syntax") || strings.HasSuffix(dependency, "/frontend") {
					t.Errorf("semantic consumer imports surface frontend: %s -> %s", name, dependency)
				}
			}
		}
	}
}

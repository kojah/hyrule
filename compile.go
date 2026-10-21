package hyrule

import (
	"bytes"
	"context"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/gogen"
	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
	"golang.org/x/tools/go/packages"
)

// Artifact is a validated generated file. Path is absolute; Data is deterministic.
type Artifact struct {
	Target string
	Path   string
	Data   []byte
}

// Compile validates all configured hosts under in-memory stubs, renders the
// selected targets, and type-checks the complete generated package before
// returning artifacts. An empty target selects all targets. It writes no files.
func (c *Config) Compile(ctx context.Context, target string) ([]Artifact, error) {
	overlay, err := c.obsoleteOverlay()
	if err != nil {
		return nil, err
	}
	selected := []Target{}
	parsed := map[string][]*syntax.File{}
	for _, t := range c.Targets {
		files, err := c.rules(t)
		if err != nil {
			return nil, err
		}
		parsed[t.Name] = files
		functions := []hostapi.FunctionStub{}
		if !t.Schema {
			functions = append(functions, hostapi.FunctionStub{Signature: t.Signature, Function: t.Function})
		}
		for _, file := range files {
			for _, item := range file.Items {
				if item.Derivation != nil {
					for _, operation := range item.Derivation.Operations {
						functions = append(functions, hostapi.FunctionStub{Signature: operation.Signature, Function: operation.Function})
					}
				}
			}
		}
		data, err := hostapi.Stubs(filepath.Join(c.root, t.Bindings), functions)
		if err != nil {
			return nil, fmt.Errorf("target %s: %w", t.Name, err)
		}
		overlay[filepath.Join(c.root, t.Out)] = data
		if target == "" || target == t.Name {
			selected = append(selected, t)
		}
	}
	if len(selected) == 0 && target == "" {
		return nil, nil
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("unknown target %q", target)
	}
	loaded, err := c.loadPackages(ctx, overlay)
	if err != nil {
		return nil, err
	}
	artifacts := []Artifact{}
	for _, t := range selected {
		pkg := loaded[filepath.Join(c.root, t.Package)]
		if pkg == nil {
			return nil, fmt.Errorf("target %s: package not loaded", t.Name)
		}
		var sig *types.Signature
		if !t.Schema {
			obj := pkg.Types.Scope().Lookup(t.Signature)
			if obj == nil {
				return nil, fmt.Errorf("target %s: missing signature %s", t.Name, t.Signature)
			}
			var ok bool
			sig, ok = obj.Type().Underlying().(*types.Signature)
			if !ok {
				return nil, fmt.Errorf("%s is not a function signature", t.Signature)
			}
		}
		files := parsed[t.Name]
		names := map[string]*types.Package{}
		for _, file := range pkg.Syntax {
			if filepath.Clean(pkg.Fset.Position(file.Pos()).Filename) != filepath.Join(c.root, t.Bindings) {
				continue
			}
			for _, spec := range file.Imports {
				obj := pkg.TypesInfo.Implicits[spec]
				if spec.Name != nil {
					obj = pkg.TypesInfo.Defs[spec.Name]
				}
				if imported, ok := obj.(*types.PkgName); ok {
					names[imported.Name()] = imported.Imported()
				}
			}
		}
		data, err := gogen.Generate(gogen.Host{Package: pkg.Types, Signature: sig, Function: t.Function, ImportNames: names}, files)
		if err != nil {
			return nil, fmt.Errorf("target %s: %w", t.Name, err)
		}
		path := filepath.Join(c.root, t.Out)
		overlay[path] = data
		artifacts = append(artifacts, Artifact{t.Name, path, data})
	}
	if _, err = c.loadPackages(ctx, overlay); err != nil {
		return nil, fmt.Errorf("generated Go validation: %w", err)
	}
	return artifacts, nil
}

func (c *Config) rules(t Target) ([]*syntax.File, error) {
	paths := append([]string(nil), t.Rules...)
	sort.Strings(paths)
	var files []*syntax.File
	seen := map[string]bool{}
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			return nil, fmt.Errorf("duplicate rule input %s", path)
		}
		seen[path] = true
		data, err := os.ReadFile(filepath.Join(c.root, path))
		if err != nil {
			return nil, err
		}
		if len(data) > 4<<20 {
			return nil, fmt.Errorf("rule input exceeds 4 MiB: %s", path)
		}
		file, err := syntax.Parse(filepath.ToSlash(path), bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func (c *Config) loadPackages(ctx context.Context, overlay map[string][]byte) (map[string]*packages.Package, error) {
	patterns := []string{}
	seen := map[string]bool{}
	for _, t := range c.Targets {
		p := "./" + filepath.ToSlash(filepath.Clean(t.Package))
		if !seen[p] {
			patterns = append(patterns, p)
			seen[p] = true
		}
	}
	pkgs, err := packages.Load(&packages.Config{Context: ctx, Dir: c.root, Overlay: overlay,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps |
			packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo}, patterns...)
	if err != nil {
		return nil, err
	}
	var messages []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			messages = append(messages, e.Error())
		}
	})
	if len(messages) > 0 {
		sort.Strings(messages)
		return nil, fmt.Errorf("go host validation:\n%s", strings.Join(messages, "\n"))
	}
	result := map[string]*packages.Package{}
	for _, p := range pkgs {
		if len(p.GoFiles) > 0 {
			result[filepath.Dir(p.GoFiles[0])] = p
		}
	}
	return result, nil
}

// Format canonicalizes a rules file, preserving comments and rule order.
// It checks syntax only; Compile checks the host-dependent contracts.
func Format(name string, input []byte) ([]byte, error) { return syntax.Format(name, input) }

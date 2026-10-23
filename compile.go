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

	"github.com/kojah/hyrule/internal/gogen"
	"github.com/kojah/hyrule/internal/goresolve"
	"github.com/kojah/hyrule/internal/syntax"
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
	prepared, err := c.prepare(target)
	if err != nil {
		return nil, err
	}
	if len(prepared.targets) == 0 {
		return nil, nil
	}
	overlay := prepared.overlay
	loaded, err := c.loadPackages(ctx, overlay)
	if err != nil {
		return nil, err
	}
	accessors, err := c.accessors(loaded)
	if err != nil {
		return nil, err
	}
	artifacts := []Artifact{}
	for _, selected := range prepared.targets {
		t := selected.target
		pkg := loaded[filepath.Join(c.root, t.Package)]
		if pkg == nil {
			return nil, fmt.Errorf("target %s: package not loaded", t.Name)
		}
		data, err := c.compileTarget(pkg, selected, accessors)
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

// compileTarget resolves one prepared program against an already loaded host,
// then emits its typed dispatcher.
func (c *Config) compileTarget(pkg *packages.Package, selected preparedTarget, accessors map[string]bool) ([]byte, error) {
	t := selected.target
	program := selected.program
	sig, declare, err := c.targetSignature(pkg, t, program.API)
	if err != nil {
		return nil, err
	}
	names := c.importNames(pkg, t)
	host := goresolve.Host{Package: pkg.Types, Signature: sig, Function: t.Function, ImportNames: names, Accessors: accessors}
	if declare != "" {
		host.Declare, host.Declared = declare, sig
	}
	resolved, err := goresolve.Resolve(host, program)
	if err != nil {
		return nil, err
	}
	data, err := gogen.Generate(resolved)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// importNames maps the binding file's import names to packages, then adds
// names other handwritten files of the package import unambiguously. Rules
// can therefore name ssa.Call or token.ARROW where the package imports them.
func (c *Config) importNames(pkg *packages.Package, t Target) map[string]*types.Package {
	bindings, others, ambiguous := map[string]*types.Package{}, map[string]*types.Package{}, map[string]bool{}
	for _, file := range pkg.Syntax {
		filename := filepath.Clean(pkg.Fset.Position(file.Pos()).Filename)
		generated := false
		for _, target := range c.Targets {
			generated = generated || filename == filepath.Join(c.root, target.Out)
		}
		if generated {
			continue
		}
		for _, spec := range file.Imports {
			obj := pkg.TypesInfo.Implicits[spec]
			if spec.Name != nil {
				obj = pkg.TypesInfo.Defs[spec.Name]
			}
			imported, ok := obj.(*types.PkgName)
			if !ok {
				continue
			}
			name := imported.Name()
			switch {
			case filename == filepath.Join(c.root, t.Bindings):
				bindings[name] = imported.Imported()
			case others[name] != nil && others[name] != imported.Imported():
				ambiguous[name] = true
			default:
				others[name] = imported.Imported()
			}
		}
	}
	for name, imported := range others {
		if bindings[name] == nil && !ambiguous[name] {
			bindings[name] = imported
		}
	}
	return bindings
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

// Package hyrule compiles declarative, ordered tree patterns into typed Go.
// Pure host matchers expose structure; pure guards establish facts; builders return complete
// results. Generated code has no dependency on Hyrule. The host owns traversal,
// effects, scheduling and any fixed point. Compile never executes host callbacks.
package hyrule

import (
	"bytes"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Config describes generation targets. Paths are relative to the config file.
type Config struct {
	Version int      `yaml:"version"`
	Targets []Target `yaml:"targets"`
	root    string
}

// Target connects one ordered ruleset with a Go host signature.
type Target struct {
	Schema    bool     `yaml:"schema"`
	Name      string   `yaml:"name"`
	Package   string   `yaml:"package"`
	Rules     []string `yaml:"rules"`
	Bindings  string   `yaml:"bindings"`
	Signature string   `yaml:"signature"`
	Function  string   `yaml:"function"`
	Out       string   `yaml:"out"`
}

// Load reads a strict, versioned configuration. Unknown fields are errors.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err = dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("config must contain exactly one document")
	}
	if c.Version != 1 {
		return nil, fmt.Errorf("config requires version 1")
	}
	c.root, err = filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	names, outputs := map[string]bool{}, map[string]bool{}
	for _, t := range c.Targets {
		if !token.IsIdentifier(t.Name) || !t.Schema && (!token.IsIdentifier(t.Signature) || !token.IsIdentifier(t.Function) || t.Function == "_") || t.Schema && (t.Signature != "" || t.Function != "") {
			return nil, fmt.Errorf("target %q: invalid name, signature or function", t.Name)
		}
		if names[t.Name] {
			return nil, fmt.Errorf("duplicate target %q", t.Name)
		}
		names[t.Name] = true
		if t.Package == "" || t.Bindings == "" || t.Out == "" || len(t.Rules) == 0 {
			return nil, fmt.Errorf("target %s: package, bindings, rules and out are required", t.Name)
		}
		for _, p := range append([]string{t.Package, t.Bindings, t.Out}, t.Rules...) {
			if filepath.IsAbs(p) || !filepath.IsLocal(p) {
				return nil, fmt.Errorf("target %s: path must stay inside config directory: %s", t.Name, p)
			}
		}
		out := filepath.Clean(t.Out)
		if outputs[out] {
			return nil, fmt.Errorf("duplicate output %s", out)
		}
		outputs[out] = true
		if filepath.Ext(out) != ".go" || filepath.Dir(out) != filepath.Clean(t.Package) {
			return nil, fmt.Errorf("target %s: output must be a Go file in package directory", t.Name)
		}
	}
	return &c, nil
}

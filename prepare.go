package hyrule

import (
	"fmt"
	"github.com/kojah/hyrule/internal/compiler"
	"github.com/kojah/hyrule/internal/frontend"
	"github.com/kojah/hyrule/internal/hostapi"
	"path/filepath"
)

// Preparation is the surface-language boundary. All configured declarations
// are collected before loading host packages; stubs exist only in the overlay.
type preparedTarget struct {
	target  Target
	program *compiler.Program
}
type preparedCompilation struct {
	targets []preparedTarget
	overlay map[string][]byte
}

func (c *Config) prepare(target string) (*preparedCompilation, error) {
	overlay, err := c.obsoleteOverlay()
	if err != nil {
		return nil, err
	}
	selected := []preparedTarget{}
	for _, t := range c.Targets {
		files, err := c.rules(t)
		if err != nil {
			return nil, err
		}
		lowered, err := frontend.Lower(files)
		if err != nil {
			return nil, fmt.Errorf("target %s: %w", t.Name, err)
		}
		program, err := compiler.Collect(lowered)
		if err != nil {
			return nil, fmt.Errorf("target %s: %w", t.Name, err)
		}
		functions := []hostapi.FunctionStub{{Signature: t.Signature, Function: t.Function, Type: inferredType(program.API)}}
		data, err := hostapi.Stubs(filepath.Join(c.root, t.Bindings), functions)
		if err != nil {
			return nil, fmt.Errorf("target %s: %w", t.Name, err)
		}
		overlay[filepath.Join(c.root, t.Out)] = data
		if target == "" || target == t.Name {
			selected = append(selected, preparedTarget{t, program})
		}
	}
	if len(selected) == 0 && target == "" {
		return &preparedCompilation{overlay: overlay}, nil
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("unknown target %q", target)
	}
	return &preparedCompilation{targets: selected, overlay: overlay}, nil
}

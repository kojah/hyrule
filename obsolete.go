package hyrule

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
)

// Hide unchanged obsolete generated declarations during bootstrap so renaming
// an output does not produce duplicate functions before publication can clean it.
func (c *Config) obsoleteOverlay() (map[string][]byte, error) {
	m, _, err := c.readManifest()
	if err != nil {
		return nil, err
	}
	outputs := map[string]string{}
	for _, t := range c.Targets {
		outputs[t.Name] = filepath.ToSlash(filepath.Clean(t.Out))
	}
	overlay := map[string][]byte{}
	for name, entry := range m.Targets {
		if outputs[name] == entry.Path {
			continue
		}
		removes := map[string]bool{}
		if err = c.obsolete(entry, removes); err != nil {
			return nil, err
		}
		for path := range removes {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, data, parser.PackageClauseOnly)
			if err != nil {
				return nil, err
			}
			overlay[path] = []byte(fmt.Sprintf("package %s\n", file.Name.Name))
		}
	}
	return overlay, nil
}

package hyrule

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// MigrateRules computes every configured file's new spelling before changing
// anything. Shared files must produce identical output in every target context.
func (c *Config) MigrateRules() (map[string][]byte, error) {
	inputs := map[string][]byte{}
	for _, target := range c.Targets {
		for _, path := range target.Rules {
			absolute := filepath.Join(c.root, path)
			if _, ok := inputs[absolute]; ok {
				continue
			}
			data, err := os.ReadFile(absolute)
			if err != nil {
				return nil, err
			}
			inputs[absolute] = data
		}
	}
	outputs := map[string][]byte{}
	for _, target := range c.Targets {
		scope := map[string][]byte{}
		for _, path := range target.Rules {
			absolute := filepath.Join(c.root, path)
			scope[absolute] = inputs[absolute]
		}
		paths := make([]string, 0, len(scope))
		for path := range scope {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			output, err := Migrate(path, scope[path], scope)
			if err != nil {
				return nil, fmt.Errorf("target %s: %w", target.Name, err)
			}
			if previous, ok := outputs[path]; ok && !bytes.Equal(previous, output) {
				return nil, fmt.Errorf("shared rules %s migrate differently across targets", path)
			}
			outputs[path] = output
		}
	}
	return outputs, nil
}

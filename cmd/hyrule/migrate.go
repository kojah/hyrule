package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/kojah/lockdoc/tools/hyrule"
)

func migrateConfig(args []string, out, stderr io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	config := fs.String("config", "hyrule.yaml", "target configuration")
	write := fs.Bool("write", false, "replace all configured rules files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("--config migration does not accept individual files")
	}
	c, err := hyrule.Load(*config)
	if err != nil {
		return err
	}
	outputs, err := c.MigrateRules()
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(outputs))
	for path := range outputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if *write {
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, outputs[path], info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	fmt.Fprintf(out, "Hyrule migrate: %d file(s) converted; written=%v\n", len(paths), *write)
	return nil
}

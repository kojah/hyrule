// Command hyrule checks, formats and compiles typed lifting/lowering rules.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kojah/lockdoc/tools/hyrule"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hyrule {check|generate|fmt} [options]")
	}
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	if cmd == "fmt" {
		return formatFiles(fs, args[1:], out)
	}
	if cmd != "check" && cmd != "generate" {
		return fmt.Errorf("unknown command %q", cmd)
	}
	config := fs.String("config", "hyrule.yaml", "target configuration")
	target := fs.String("target", "", "select one target (default all)")
	format := fs.String("format", "text", "diagnostics: text or json")
	check := fs.Bool("check", false, "check output freshness without writing (generate only)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unknown diagnostics format %q", *format)
	}
	if cmd == "check" && *check {
		return fmt.Errorf("--check applies to generate")
	}
	c, err := hyrule.Load(*config)
	var artifacts []hyrule.Artifact
	if err == nil {
		artifacts, err = c.Compile(ctx, *target)
	}
	if err == nil && cmd == "generate" {
		err = c.Publish(artifacts, *check)
	}
	if *format == "json" {
		result := struct {
			OK      bool   `json:"ok"`
			Error   string `json:"error,omitempty"`
			Targets int    `json:"targets"`
		}{OK: err == nil, Targets: len(artifacts)}
		if err != nil {
			result.Error = err.Error()
		}
		if encodeErr := json.NewEncoder(out).Encode(result); encodeErr != nil {
			return encodeErr
		}
	} else if err == nil {
		fmt.Fprintf(out, "Hyrule %s: %d target(s) valid\n", cmd, len(artifacts))
	}
	return err
}
func formatFiles(fs *flag.FlagSet, args []string, out io.Writer) error {
	write := fs.Bool("write", false, "replace input files")
	check := fs.Bool("check", false, "fail if files need formatting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *write && *check {
		return fmt.Errorf("--write and --check are mutually exclusive")
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("fmt requires .rules files")
	}
	if !*write && !*check && fs.NArg() != 1 {
		return fmt.Errorf("stdout formatting accepts exactly one file")
	}
	outputs := map[string][]byte{}
	inputs := map[string][]byte{}
	if err := convertFiles(fs.Args(), inputs, outputs, *check); err != nil {
		return err
	}
	for _, path := range fs.Args() {
		if *write {
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			if err = os.WriteFile(path, outputs[path], info.Mode().Perm()); err != nil {
				return err
			}
		} else if !*check {
			if _, err := out.Write(outputs[path]); err != nil {
				return err
			}
		}
	}
	return nil
}

func convertFiles(paths []string, inputs, outputs map[string][]byte, check bool) error {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		inputs[path] = data
	}
	for _, path := range paths {
		data := inputs[path]
		formatted, err := hyrule.Format(path, data)
		if err != nil {
			return err
		}
		if check && !bytes.Equal(data, formatted) {
			return fmt.Errorf("%s needs formatting", path)
		}
		outputs[path] = formatted
	}
	return nil
}

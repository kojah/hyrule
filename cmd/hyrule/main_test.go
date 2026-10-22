package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIFormattingAndErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.rules")
	input := []byte("@R\nrewrite _=0")
	if err := os.WriteFile(path, input, 0644); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := run(context.Background(), []string{"fmt", "--check", path}, &out, &stderr); err == nil {
		t.Fatal("accepted unformatted file")
	}
	if err := run(context.Background(), []string{"fmt", "--write", path}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"fmt", "--check", path}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"check", "--config", path, "--format", "json"}, &out, &stderr); err == nil {
		t.Fatal("accepted invalid config")
	}
	if !strings.Contains(out.String(), `"ok":false`) {
		t.Fatal("missing JSON failure", out.String())
	}
	for _, args := range [][]string{{}, {"wat"}, {"fmt", "--write", "--check", path}, {"generate", "--format", "xml"}} {
		if err := run(context.Background(), args, &out, &stderr); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

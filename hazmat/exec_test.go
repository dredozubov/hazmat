package main

import (
	"path/filepath"
	"testing"
)

func TestCommandStdoutMissingFileReturnsNoStderrContent(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-file")

	out, err := commandStdout("cat", missing)
	if err == nil {
		t.Fatal("expected missing file read to fail")
	}
	if out != "" {
		t.Fatalf("expected stdout to stay empty for missing file read, got %q", out)
	}
}

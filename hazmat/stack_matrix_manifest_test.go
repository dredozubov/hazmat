package main

import (
	"path/filepath"
	"testing"
)

func TestStackMatrixManifestIsWellFormed(t *testing.T) {
	path := filepath.Join("..", "testdata", "stack-matrix", "repos.yaml")
	if _, err := loadStackMatrixManifest(path); err != nil {
		t.Fatalf("loadStackMatrixManifest(%s): %v", path, err)
	}
}

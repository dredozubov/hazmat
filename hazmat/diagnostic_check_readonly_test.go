package hazmat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHomebrewToolForAgentReturnsExecutablePath(t *testing.T) {
	prefix := t.TempDir()
	tool := "golangci-lint"
	binary := buildFakeCellarBinary(t, prefix, tool, "2.11.4", tool)
	formulaRoot := filepath.Dir(filepath.Dir(binary))
	linkHomebrewOpt(t, prefix, tool, formulaRoot)
	wantBinary, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatalf("canonicalize binary: %v", err)
	}

	got := resolveHomebrewToolForAgentInPrefixes(tool, []string{prefix}, func(path string) bool {
		return path == wantBinary
	})
	if got != wantBinary {
		t.Fatalf("resolveHomebrewToolForAgentInPrefixes() = %q, want %q", got, wantBinary)
	}
}

func TestResolveHomebrewToolForAgentDoesNotRepairInaccessibleTool(t *testing.T) {
	prefix := t.TempDir()
	tool := "golangci-lint"
	binary := buildFakeCellarBinary(t, prefix, tool, "2.11.4", tool)
	formulaRoot := filepath.Dir(filepath.Dir(binary))
	linkHomebrewOpt(t, prefix, tool, formulaRoot)

	savedRepair := repairHomebrewToolAccess
	repairHomebrewToolAccess = func(string) bool {
		t.Fatal("read-only check resolver must not call Homebrew permission repair")
		return true
	}
	t.Cleanup(func() { repairHomebrewToolAccess = savedRepair })

	got := resolveHomebrewToolForAgentInPrefixes(tool, []string{prefix}, func(string) bool {
		return false
	})
	if got != "" {
		t.Fatalf("resolveHomebrewToolForAgentInPrefixes() = %q, want empty for inaccessible tool", got)
	}
}

func linkHomebrewOpt(t *testing.T, prefix, tool, formulaRoot string) {
	t.Helper()
	optDir := filepath.Join(prefix, "opt")
	if err := os.MkdirAll(optDir, 0o755); err != nil {
		t.Fatalf("mkdir opt dir: %v", err)
	}
	if err := os.Symlink(formulaRoot, filepath.Join(optDir, tool)); err != nil {
		t.Fatalf("symlink opt tool: %v", err)
	}
}

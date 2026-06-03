package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostCredentialHardeningSpecsCoverCredentialDenySubs(t *testing.T) {
	t.Parallel()

	specs := make(map[string]struct{})
	for _, spec := range hostCredentialHardeningSpecs {
		specs[filepath.ToSlash(spec.rel)] = struct{}{}
	}
	for _, sub := range credentialDenySubs {
		rel := strings.TrimPrefix(sub, "/")
		if _, ok := specs[rel]; !ok {
			t.Fatalf("credential deny path %q is not covered by host hardening specs", sub)
		}
	}
}

func TestHostCredentialHardeningTargetsRestrictExistingPaths(t *testing.T) {
	home := t.TempDir()

	mustMkdirMode(t, filepath.Join(home, ".aws"), 0o755)
	mustMkdirMode(t, filepath.Join(home, ".config", "gh"), 0o751)
	mustMkdirMode(t, filepath.Join(home, ".ssh"), 0o700)
	mustWriteMode(t, filepath.Join(home, ".netrc"), 0o644)

	linkTarget := filepath.Join(home, "kube-target")
	mustMkdirMode(t, linkTarget, 0o755)
	if err := os.Symlink(linkTarget, filepath.Join(home, ".kube")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	targets, skipped := hostCredentialHardeningTargets(home)
	got := make(map[string]os.FileMode)
	for _, target := range targets {
		got[target.path] = target.mode
	}

	for path, want := range map[string]os.FileMode{
		filepath.Join(home, ".aws"):       0o700,
		filepath.Join(home, ".config/gh"): 0o700,
		filepath.Join(home, ".netrc"):     0o600,
	} {
		if got[path] != want {
			t.Fatalf("target %s mode = %04o, want %04o", path, got[path], want)
		}
	}
	if _, ok := got[filepath.Join(home, ".ssh")]; ok {
		t.Fatalf("already-restricted .ssh should not be returned as a chmod target")
	}
	if len(skipped) != 1 || skipped[0] != filepath.Join(home, ".kube") {
		t.Fatalf("skipped symlinks = %v, want [.kube]", skipped)
	}
}

func mustMkdirMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func mustWriteMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("test\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

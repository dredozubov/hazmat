package pathpolicy

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCanonicalizeResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	got, err := Canonicalize(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Canonicalize(%q) = %q, want %q", link, got, want)
	}
}

func TestDenyPolicyCredentialParentAndSibling(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	policy := DefaultDenyPolicy("/Users/agent", home)

	if !policy.CredentialDenyPath(filepath.Join(home, ".ssh")) {
		t.Fatal("expected exact credential path to be denied")
	}
	if !policy.CredentialDenyPath(home) {
		t.Fatal("expected credential parent to be denied")
	}
	if policy.CredentialDenyPath(filepath.Join(home, ".m2", "repository")) {
		t.Fatal("expected sibling of credential file to remain allowed")
	}
}

func TestDenyPolicyHostStateOverlap(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	policy := DefaultDenyPolicy("/Users/agent", home)

	if !policy.HostStateDenyPath(filepath.Join(home, ".codex")) {
		t.Fatal("expected host-state parent to be denied")
	}
	if !policy.HostStateDenyPath(filepath.Join(home, ".codex", "sqlite", "codex.db")) {
		t.Fatal("expected host-state child to be denied")
	}
	if policy.HostStateDenyPath(filepath.Join(home, ".codex", "prompts")) {
		t.Fatal("expected narrow Codex assets to remain allowed")
	}
}

func TestAppendUniqueDirsCopiesAndReportsAdded(t *testing.T) {
	existing := []string{"/a"}
	merged, added := AppendUniqueDirs(existing, []string{"/a", "/b", "/c"})
	if !slices.Equal(merged, []string{"/a", "/b", "/c"}) {
		t.Fatalf("merged = %v", merged)
	}
	if !slices.Equal(added, []string{"/b", "/c"}) {
		t.Fatalf("added = %v", added)
	}
	merged[0] = "/mutated"
	if existing[0] != "/a" {
		t.Fatal("AppendUniqueDirs returned storage aliasing existing")
	}
}

func TestSubtractResolvedDirsDedupesSymlinkAliases(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	link := filepath.Join(root, "link")
	other := filepath.Join(root, "other")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	got := SubtractResolvedDirs([]string{link, real, other}, []string{real})
	if !slices.Equal(got, []string{other}) {
		t.Fatalf("SubtractResolvedDirs = %v, want [%q]", got, other)
	}
}

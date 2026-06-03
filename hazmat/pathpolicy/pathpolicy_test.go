package pathpolicy

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestValidatedConstructorsRequireDenyPolicy(t *testing.T) {
	dir := CanonicalDir{path: t.TempDir()}

	_, err := NewProjectRoot(dir, DenyPolicy{})
	if err == nil {
		t.Fatal("expected zero deny policy to fail closed")
	}
	if !strings.Contains(err.Error(), "deny policy is required") {
		t.Fatalf("error = %v, want deny policy requirement", err)
	}
}

func TestValidatedConstructorsRejectZeroCanonicalDir(t *testing.T) {
	policy := DefaultDenyPolicy("/Users/agent", t.TempDir())

	_, err := NewProjectRoot(CanonicalDir{}, policy)
	if err == nil {
		t.Fatal("expected zero canonical dir to be rejected")
	}
	if !strings.Contains(err.Error(), "canonical dir is required") {
		t.Fatalf("error = %v, want canonical dir requirement", err)
	}
}

func TestValidatedConstructorsRejectCredentialDenyZones(t *testing.T) {
	home := t.TempDir()
	policy := DefaultDenyPolicy("/Users/agent", home)

	tests := []struct {
		name    string
		resolve func() error
		want    string
	}{
		{
			name: "project",
			resolve: func() error {
				_, err := ResolveProjectRoot(home, false, policy)
				return err
			},
			want: "project dir",
		},
		{
			name: "read",
			resolve: func() error {
				_, err := ResolveReadOnlyGrant(home, policy)
				return err
			},
			want: "read dir",
		},
		{
			name: "write",
			resolve: func() error {
				_, err := ResolveReadWriteGrant(home, policy)
				return err
			},
			want: "write dir",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.resolve()
			if err == nil {
				t.Fatal("expected credential deny zone rejection")
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "credential deny zone") {
				t.Fatalf("error = %v, want %q credential deny zone", err, tt.want)
			}
		})
	}
}

func TestValidatedConstructorsRejectHostStateDenyZones(t *testing.T) {
	home := t.TempDir()
	hostState := filepath.Join(home, ".codex")
	if err := os.MkdirAll(hostState, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", hostState, err)
	}
	policy := DefaultDenyPolicy("/Users/agent", home)

	tests := []struct {
		name    string
		resolve func() error
		want    string
	}{
		{
			name: "project",
			resolve: func() error {
				_, err := ResolveProjectRoot(hostState, false, policy)
				return err
			},
			want: "project dir",
		},
		{
			name: "read",
			resolve: func() error {
				_, err := ResolveReadOnlyGrant(hostState, policy)
				return err
			},
			want: "read dir",
		},
		{
			name: "write",
			resolve: func() error {
				_, err := ResolveReadWriteGrant(hostState, policy)
				return err
			},
			want: "write dir",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.resolve()
			if err == nil {
				t.Fatal("expected host-state deny zone rejection")
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "host-state deny zone") {
				t.Fatalf("error = %v, want %q host-state deny zone", err, tt.want)
			}
		})
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

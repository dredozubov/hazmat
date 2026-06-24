package hazmat

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestChownChmodDirNoFollowRefusesSymlinkComponents proves the TOCTOU/parent-
// symlink privilege-escalation vector is closed: a symlink at the final OR a
// parent component below the anchor must make the operation fail rather than
// redirect chown/chmod onto the symlink target. Runs without root because the
// open fails before any chown is attempted.
func TestChownChmodDirNoFollowRefusesSymlinkComponents(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("O_NOFOLLOW directory walk is POSIX-only")
	}
	// Resolve symlinks in the temp base (macOS /var -> /private/var) so the
	// anchor itself is a real path; the safety we test is below the anchor.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp base: %v", err)
	}
	anchor := filepath.Join(base, "agenthome")
	mustMkdirAll(t, filepath.Join(anchor, "a"))

	// A sensitive target the attacker wants the privileged op redirected onto.
	sensitive := filepath.Join(base, "sensitive")
	mustMkdirAll(t, sensitive)

	t.Run("parent component is a symlink", func(t *testing.T) {
		// anchor/a/b -> sensitive ; target anchor/a/b/leaf resolves into sensitive
		link := filepath.Join(anchor, "a", "b")
		if err := os.Symlink(sensitive, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		mustMkdirAll(t, filepath.Join(sensitive, "leaf"))
		target := filepath.Join(anchor, "a", "b", "leaf")
		if err := chownChmodDirNoFollow(anchor, target, os.Getuid(), os.Getgid(), 0o755); err == nil {
			t.Fatal("expected refusal when a parent component is a symlink")
		}
		_ = os.Remove(link)
	})

	t.Run("final component is a symlink", func(t *testing.T) {
		link := filepath.Join(anchor, "a", "leaflink")
		if err := os.Symlink(sensitive, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if err := chownChmodDirNoFollow(anchor, link, os.Getuid(), os.Getgid(), 0o755); err == nil {
			t.Fatal("expected refusal when the final component is a symlink")
		}
		_ = os.Remove(link)
	})

	t.Run("path escaping the anchor is refused", func(t *testing.T) {
		if err := chownChmodDirNoFollow(anchor, sensitive, os.Getuid(), os.Getgid(), 0o755); err == nil {
			t.Fatal("expected refusal for a path outside the anchor")
		}
	})

	t.Run("real directory below the anchor succeeds and preserves setgid", func(t *testing.T) {
		real := filepath.Join(anchor, "a", "realdir")
		mustMkdirAll(t, real)
		// chown to our own uid/gid is permitted without root; 2750 carries setgid.
		if err := chownChmodDirNoFollow(anchor, real, os.Getuid(), os.Getgid(), 0o2750); err != nil {
			t.Fatalf("repair of a legitimate dir failed: %v", err)
		}
		info, err := os.Stat(real)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode()&os.ModeSetgid == 0 {
			t.Fatalf("expected setgid preserved, mode=%v", info.Mode())
		}
		if info.Mode().Perm() != 0o750 {
			t.Fatalf("expected perm 0750, got %o", info.Mode().Perm())
		}
	})
}

func TestParseAgentRepairModePreservesSpecialBits(t *testing.T) {
	got, err := parseAgentRepairMode("2770")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != 0o2770 {
		t.Fatalf("expected 0o2770, got %o", got)
	}
	if _, err := parseAgentRepairMode("not-octal"); err == nil {
		t.Fatal("expected error for non-octal mode")
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

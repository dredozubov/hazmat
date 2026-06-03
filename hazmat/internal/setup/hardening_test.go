package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	targets, skipped := HostCredentialHardeningTargets(home)
	got := make(map[string]os.FileMode)
	for _, target := range targets {
		got[target.Path] = target.Mode
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

func TestSetupHardeningGapsRestrictsCredentialsAndWritesAgentUmask(t *testing.T) {
	home := t.TempDir()
	mustMkdirMode(t, filepath.Join(home, ".aws"), 0o755)
	mustWriteMode(t, filepath.Join(home, ".netrc"), 0o644)

	env := HardeningEnv{
		AgentUser:       "agent",
		AgentHome:       "/Users/agent",
		HostHome:        home,
		UmaskBlockStart: "# >>> hazmat umask >>>",
		UmaskBlockEnd:   "# <<< hazmat umask <<<",
		DockerSocket:    filepath.Join(home, "missing-docker.sock"),
	}
	runner := newFakeToolingRunner(t)
	agentZshrc := filepath.Join(env.AgentHome, ".zshrc")
	runner.sudoOutput[agentZshrc] = "export KEEP=1\n"

	if err := SetupHardeningGaps(env, &fakeToolingUI{}, runner); err != nil {
		t.Fatalf("SetupHardeningGaps: %v", err)
	}

	for path, want := range map[string]os.FileMode{
		filepath.Join(home, ".aws"):   0o700,
		filepath.Join(home, ".netrc"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %04o, want %04o", path, got, want)
		}
	}

	got := runner.sudoWrites[agentZshrc]
	if !strings.Contains(got, env.UmaskBlockStart) || !strings.Contains(got, "umask 007") || !strings.Contains(got, "export KEEP=1") {
		t.Fatalf("agent zshrc content = %q, want preserved content plus managed umask block", got)
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

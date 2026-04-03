package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitMetadataDirRequiresDirectory(t *testing.T) {
	projectDir := t.TempDir()
	if got := gitMetadataDir(projectDir); got != "" {
		t.Fatalf("gitMetadataDir() = %q, want empty", got)
	}

	gitDir := filepath.Join(projectDir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := gitMetadataDir(projectDir); got != gitDir {
		t.Fatalf("gitMetadataDir() = %q, want %q", got, gitDir)
	}
}

func TestCollectGitPermissionProblemsFlagsBrokenPaths(t *testing.T) {
	projectDir := t.TempDir()
	gitDir := filepath.Join(projectDir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(gitDir, "index")
	if err := os.WriteFile(indexPath, []byte("index"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(indexPath, 0); err != nil {
		t.Fatal(err)
	}

	problems := collectGitPermissionProblems(gitDir)
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"host user cannot read " + indexPath,
		"host user cannot write " + indexPath,
		"agent user cannot write with inheritable dev ACL " + gitDir,
		"agent user cannot write with inheritable dev ACL " + filepath.Join(gitDir, "objects"),
		"agent user cannot write with inheritable dev ACL " + filepath.Join(gitDir, "refs"),
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("collectGitPermissionProblems() missing %q in:\n%s", want, joined)
		}
	}
}

func TestCollectGitPermissionProblemsDetectsReadOnlyForHost(t *testing.T) {
	projectDir := t.TempDir()
	gitDir := filepath.Join(projectDir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	headPath := filepath.Join(gitDir, "HEAD")
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0o444); err != nil {
		t.Fatal(err)
	}

	problems := collectGitPermissionProblems(gitDir)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "host user cannot write "+headPath) {
		t.Fatalf("expected host write problem for read-only HEAD, got:\n%s", joined)
	}
}

func TestRepairGitAfterSessionNoGitDir(t *testing.T) {
	// Should not panic on a project without .git.
	repairGitAfterSession(t.TempDir())
}

func TestRepairGitAfterSessionHealthyGitDir(t *testing.T) {
	projectDir := t.TempDir()
	gitDir := filepath.Join(projectDir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should not panic or error on a healthy .git tree (ACL checks will
	// report problems in CI since the dev group doesn't exist, but the
	// function must not crash).
	repairGitAfterSession(projectDir)
}

func TestCollectGitPermissionProblemsSkipsOptionalMissingPaths(t *testing.T) {
	projectDir := t.TempDir()
	gitDir := filepath.Join(projectDir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	problems := collectGitPermissionProblems(gitDir)
	for _, problem := range problems {
		if strings.Contains(problem, filepath.Join(gitDir, "logs")) || strings.Contains(problem, filepath.Join(gitDir, "index")) {
			t.Fatalf("optional missing path should not be reported: %s", problem)
		}
	}
}

func TestGitRepairCommandIncludesChownAndACLRepair(t *testing.T) {
	cmd := gitRepairCommand("/tmp/project/.git")
	for _, want := range []string{
		`sudo chown -R "$(id -un)":staff /tmp/project/.git`,
		"find /tmp/project/.git -type d -exec chmod +a",
		"find /tmp/project/.git -type f -exec chmod +a",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("gitRepairCommand() missing %q in %q", want, cmd)
		}
	}
}

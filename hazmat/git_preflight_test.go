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

func TestGitMetadataACLTargetsSkipObjectFiles(t *testing.T) {
	projectDir := t.TempDir()
	gitDir := filepath.Join(projectDir, ".git")
	for _, dir := range []string{
		filepath.Join(gitDir, "objects", "ab"),
		filepath.Join(gitDir, "objects", "pack"),
		filepath.Join(gitDir, "refs", "heads"),
		filepath.Join(gitDir, "logs", "refs", "heads"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, contents := range map[string]string{
		filepath.Join(gitDir, "HEAD"):                              "ref: refs/heads/main\n",
		filepath.Join(gitDir, "index"):                             "index",
		filepath.Join(gitDir, "packed-refs"):                       "",
		filepath.Join(gitDir, "objects", "ab", "object"):           "object",
		filepath.Join(gitDir, "objects", "pack", "pack-test.pack"): "pack",
		filepath.Join(gitDir, "refs", "heads", "main"):             "ref",
		filepath.Join(gitDir, "logs", "HEAD"):                      "log",
		filepath.Join(gitDir, "logs", "refs", "heads", "main"):     "log",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dirs, files, failures := gitMetadataACLTargets(gitDir)
	if len(failures) != 0 {
		t.Fatalf("gitMetadataACLTargets failures = %v", failures)
	}
	for _, want := range []string{
		gitDir,
		filepath.Join(gitDir, "objects"),
		filepath.Join(gitDir, "objects", "ab"),
		filepath.Join(gitDir, "objects", "pack"),
		filepath.Join(gitDir, "refs", "heads"),
		filepath.Join(gitDir, "logs", "refs", "heads"),
	} {
		if !containsPath(dirs, want) {
			t.Fatalf("dirs missing %s in %v", want, dirs)
		}
	}
	for _, want := range []string{
		filepath.Join(gitDir, "HEAD"),
		filepath.Join(gitDir, "index"),
		filepath.Join(gitDir, "packed-refs"),
		filepath.Join(gitDir, "refs", "heads", "main"),
		filepath.Join(gitDir, "logs", "HEAD"),
		filepath.Join(gitDir, "logs", "refs", "heads", "main"),
	} {
		if !containsPath(files, want) {
			t.Fatalf("files missing %s in %v", want, files)
		}
	}
	for _, forbidden := range []string{
		filepath.Join(gitDir, "objects", "ab", "object"),
		filepath.Join(gitDir, "objects", "pack", "pack-test.pack"),
	} {
		if containsPath(files, forbidden) || containsPath(dirs, forbidden) {
			t.Fatalf("object file target should be skipped: %s", forbidden)
		}
	}
}

func TestApplyGitMetadataACLsDoesNotChmodObjectFiles(t *testing.T) {
	projectDir := t.TempDir()
	gitDir := filepath.Join(projectDir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects", "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	objectPath := filepath.Join(gitDir, "objects", "ab", "object")
	if err := os.WriteFile(objectPath, []byte("object"), 0o644); err != nil {
		t.Fatal(err)
	}

	backend := &batchRecordingACLBackend{}
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return backend
	}
	t.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	if failures := applyGitMetadataACLs(gitDir); len(failures) != 0 {
		t.Fatalf("applyGitMetadataACLs failures = %v", failures)
	}
	var allArgs []string
	for _, args := range backend.chmods {
		allArgs = append(allArgs, args...)
	}
	if containsPath(allArgs, objectPath) {
		t.Fatalf("applyGitMetadataACLs must not chmod object file %s; args=%v", objectPath, backend.chmods)
	}
	if !containsPath(allArgs, filepath.Join(gitDir, "objects", "ab")) {
		t.Fatalf("applyGitMetadataACLs should chmod object fanout dir; args=%v", backend.chmods)
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
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

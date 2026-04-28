package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHooksInstallReportsConflictAndSupportsReplace(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
	setFlagYesAllForTest(t, true)

	projectDir := initGitHookProject(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: sh
`,
		files: map[string]string{
			"pre-commit.sh": "#!/bin/sh\nexit 0\n",
		},
	})

	if err := writeLocalGitHooksPath(projectDir, filepath.Join(projectDir, ".husky")); err != nil {
		t.Fatal(err)
	}
	if err := runHooksInstall(projectDir, false); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("expected replace hint, got %v", err)
	}
	if err := runHooksInstall(projectDir, true); err != nil {
		t.Fatal(err)
	}

	runtime, err := buildProjectHookRuntime(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if hooksPath, err := readLocalGitHooksPath(projectDir); err != nil || hooksPath != runtime.ManagedDir {
		t.Fatalf("expected managed hooksPath %q, got %q err=%v", runtime.ManagedDir, hooksPath, err)
	}
}

func TestMaybePromptProjectHooksInstallsWhenYesAll(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
	setFlagYesAllForTest(t, true)

	projectDir := initGitHookProject(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-push
    script: pre-push.sh
    purpose: fast local gate
    interpreter: sh
`,
		files: map[string]string{
			"pre-push.sh": "#!/bin/sh\nexit 0\n",
		},
	})

	maybePromptProjectHooks(projectDir)

	approval, err := loadProjectHookApproval(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if approval == nil {
		t.Fatal("expected approval to be recorded")
	}
	if _, err := validateProjectHookRuntime(projectDir); err != nil {
		t.Fatalf("expected runtime install, got %v", err)
	}
}

func TestMaybePromptProjectHooksSkipsForeignHooksPathOwner(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
	setFlagYesAllForTest(t, true)

	projectDir := initGitHookProject(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-push
    script: pre-push.sh
    purpose: fast local gate
    interpreter: sh
`,
		files: map[string]string{
			"pre-push.sh": "#!/bin/sh\nexit 0\n",
		},
	})

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordProjectHookApproval(bundle); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalGitHooksPath(projectDir, ".beads/hooks"); err != nil {
		t.Fatal(err)
	}

	stderr := captureStderr(t, func() {
		maybePromptProjectHooks(projectDir)
	})
	if stderr != "" {
		t.Fatalf("expected launch hook prompt to stay quiet for foreign hooksPath owner, got %q", stderr)
	}
	if hooksPath, err := readLocalGitHooksPath(projectDir); err != nil || hooksPath != ".beads/hooks" {
		t.Fatalf("expected foreign hooksPath to remain .beads/hooks, got %q err=%v", hooksPath, err)
	}
}

func TestMaybePromptProjectHooksReportsForeignHooksPathAfterManagedInstall(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
	setFlagYesAllForTest(t, true)

	projectDir := initGitHookProject(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-push
    script: pre-push.sh
    purpose: fast local gate
    interpreter: sh
`,
		files: map[string]string{
			"pre-push.sh": "#!/bin/sh\nexit 0\n",
		},
	})

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordProjectHookApproval(bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := installProjectHookRuntime(projectDir, "/usr/local/bin/hazmat"); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalGitHooksPath(projectDir, ".beads/hooks"); err != nil {
		t.Fatal(err)
	}

	stderr := captureStderr(t, func() {
		maybePromptProjectHooks(projectDir)
	})
	if !strings.Contains(stderr, "repo hooks need install/repair") {
		t.Fatalf("expected launch hook repair warning for installed runtime drift, got %q", stderr)
	}
	if hooksPath, err := readLocalGitHooksPath(projectDir); err != nil || hooksPath != ".beads/hooks" {
		t.Fatalf("expected foreign hooksPath to remain .beads/hooks, got %q err=%v", hooksPath, err)
	}
}

func TestRunHooksUninstallRemovesApprovalAndSnapshots(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
	setFlagYesAllForTest(t, true)

	projectDir := initGitHookProject(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: commit-msg
    script: commit-msg.sh
    purpose: enforce commit shape
    interpreter: sh
`,
		files: map[string]string{
			"commit-msg.sh": "#!/bin/sh\nexit 0\n",
		},
	})

	if err := runHooksInstall(projectDir, false); err != nil {
		t.Fatal(err)
	}
	approval, err := loadProjectHookApproval(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if approval == nil {
		t.Fatal("expected approval before uninstall")
	}
	snapshotDir := approval.SnapshotDir

	if err := runHooksUninstall(projectDir); err != nil {
		t.Fatal(err)
	}

	approval, err = loadProjectHookApproval(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if approval != nil {
		t.Fatalf("expected approval removal, got %+v", approval)
	}
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Fatalf("expected snapshot removal, stat err=%v", err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = old
	}()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(data)
}

func setFlagYesAllForTest(t *testing.T, value bool) {
	t.Helper()

	oldYesAll := flagYesAll
	oldDryRun := flagDryRun
	flagYesAll = value
	flagDryRun = false
	t.Cleanup(func() {
		flagYesAll = oldYesAll
		flagDryRun = oldDryRun
	})
}

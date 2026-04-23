package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRollbackProjectHooksRemovesRepoState(t *testing.T) {
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

	if err := runHooksInstall(projectDir, false); err != nil {
		t.Fatal(err)
	}
	runtime, err := buildProjectHookRuntime(projectDir)
	if err != nil {
		t.Fatal(err)
	}

	rollbackProjectHooks(&UI{})

	if approval, err := loadProjectHookApproval(projectDir); err != nil || approval != nil {
		t.Fatalf("expected approval removal, got %+v err=%v", approval, err)
	}
	if hooksPath, err := readLocalGitHooksPath(projectDir); err != nil || hooksPath != "" {
		t.Fatalf("expected hooksPath removal, got %q err=%v", hooksPath, err)
	}
	if _, err := os.Stat(runtime.WrapperPath); !os.IsNotExist(err) {
		t.Fatalf("expected wrapper removal, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(runtime.FallbackDir, "pre-commit")); !os.IsNotExist(err) {
		t.Fatalf("expected fallback dispatcher removal, stat err=%v", err)
	}
	if _, err := os.Stat(projectHookApprovalsFilePath); !os.IsNotExist(err) {
		t.Fatalf("expected approvals file removal, stat err=%v", err)
	}
	if _, err := os.Stat(projectHookSnapshotsRootDir); !os.IsNotExist(err) {
		t.Fatalf("expected snapshots root removal, stat err=%v", err)
	}
}

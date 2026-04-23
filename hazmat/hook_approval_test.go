package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordProjectHookApprovalStoresSnapshotAndSummary(t *testing.T) {
	setProjectHookApprovalTestPaths(t)

	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: scripts/pre-commit.sh
    purpose: keep staged files clean
    interpreter: bash
    requires: [bash, gitleaks]
`,
		files: map[string]string{
			"scripts/pre-commit.sh": "#!/usr/bin/env bash\necho pre-commit\n",
		},
	})

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := recordProjectHookApproval(bundle)
	if err != nil {
		t.Fatal(err)
	}

	canonicalProjectDir, err := canonicalizePath(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if record.ProjectDir != canonicalProjectDir {
		t.Fatalf("ProjectDir = %q, want %q", record.ProjectDir, canonicalProjectDir)
	}
	if record.BundleHash != bundle.BundleHash {
		t.Fatalf("BundleHash = %q, want %q", record.BundleHash, bundle.BundleHash)
	}
	if got, want := len(record.Summary.Hooks), 1; got != want {
		t.Fatalf("len(Summary.Hooks) = %d, want %d", got, want)
	}
	if got, want := record.Summary.Hooks[0].Purpose, "keep staged files clean"; got != want {
		t.Fatalf("Purpose = %q, want %q", got, want)
	}

	manifestPath := filepath.Join(record.SnapshotDir, "hooks.yaml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(manifestData) != string(bundle.ManifestData) {
		t.Fatalf("snapshot manifest mismatch:\n%s\nwant:\n%s", manifestData, bundle.ManifestData)
	}

	scriptPath := filepath.Join(record.SnapshotDir, "scripts", "pre-commit.sh")
	scriptData, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(scriptData) != string(bundle.Hooks[0].ScriptData) {
		t.Fatalf("snapshot script mismatch:\n%s\nwant:\n%s", scriptData, bundle.Hooks[0].ScriptData)
	}

	loaded, err := loadProjectHookApproval(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected approval record")
	}
	if !isProjectHookBundleApproved(projectDir, bundle.BundleHash) {
		t.Fatal("expected bundle to be approved")
	}
}

func TestRecordProjectHookApprovalReplacesStaleSnapshot(t *testing.T) {
	setProjectHookApprovalTestPaths(t)

	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-push
    script: pre-push.sh
    purpose: fast local gate
    interpreter: sh
`,
		files: map[string]string{
			"pre-push.sh": "#!/bin/sh\necho one\n",
		},
	})

	bundleA, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	recordA, err := recordProjectHookApproval(bundleA)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, projectHooksDirRel, "pre-push.sh"), []byte("#!/bin/sh\necho two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundleB, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	recordB, err := recordProjectHookApproval(bundleB)
	if err != nil {
		t.Fatal(err)
	}

	if recordA.BundleHash == recordB.BundleHash {
		t.Fatalf("expected bundle hash to change, got %s", recordA.BundleHash)
	}
	if _, err := os.Stat(recordA.SnapshotDir); !os.IsNotExist(err) {
		t.Fatalf("expected stale snapshot %q to be removed, stat err=%v", recordA.SnapshotDir, err)
	}

	loaded, err := loadProjectHookApproval(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.BundleHash != bundleB.BundleHash {
		t.Fatalf("expected latest approval hash %q, got %+v", bundleB.BundleHash, loaded)
	}
}

func TestRemoveProjectHookApprovalRemovesSnapshotAndRecord(t *testing.T) {
	setProjectHookApprovalTestPaths(t)

	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: commit-msg
    script: commit-msg.sh
    purpose: enforce commit shape
    interpreter: sh
`,
		files: map[string]string{
			"commit-msg.sh": "#!/bin/sh\necho msg\n",
		},
	})

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := recordProjectHookApproval(bundle)
	if err != nil {
		t.Fatal(err)
	}

	if err := removeProjectHookApproval(projectDir); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadProjectHookApproval(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Fatalf("expected approval removal, got %+v", loaded)
	}
	if _, err := os.Stat(record.SnapshotDir); !os.IsNotExist(err) {
		t.Fatalf("expected snapshot %q to be removed, stat err=%v", record.SnapshotDir, err)
	}
}

func TestRecordProjectHookApprovalCanonicalizesProjectDir(t *testing.T) {
	setProjectHookApprovalTestPaths(t)

	root := t.TempDir()
	realProjectDir := filepath.Join(root, "real")
	if err := os.MkdirAll(realProjectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkProjectDir := filepath.Join(root, "link")
	if err := os.Symlink(realProjectDir, linkProjectDir); err != nil {
		t.Fatal(err)
	}

	hooksDir := filepath.Join(realProjectDir, projectHooksDirRel)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realProjectDir, projectHooksManifestRelPath), []byte(`version: 1
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: sh
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-commit.sh"), []byte("#!/bin/sh\necho ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bundle, err := loadProjectHookBundle(linkProjectDir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := recordProjectHookApproval(bundle)
	if err != nil {
		t.Fatal(err)
	}

	canonicalProjectDir, err := canonicalizePath(realProjectDir)
	if err != nil {
		t.Fatal(err)
	}
	if record.ProjectDir != canonicalProjectDir {
		t.Fatalf("ProjectDir = %q, want %q", record.ProjectDir, canonicalProjectDir)
	}
}

func setProjectHookApprovalTestPaths(t *testing.T) {
	t.Helper()

	oldApprovals := projectHookApprovalsFilePath
	oldSnapshots := projectHookSnapshotsRootDir

	root := t.TempDir()
	projectHookApprovalsFilePath = filepath.Join(root, "hook-approvals.yaml")
	projectHookSnapshotsRootDir = filepath.Join(root, "git-hooks")

	t.Cleanup(func() {
		projectHookApprovalsFilePath = oldApprovals
		projectHookSnapshotsRootDir = oldSnapshots
	})
}

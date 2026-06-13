package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallProjectHookRuntimeConfiguresManagedLayout(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
	projectDir := initGitHookProject(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: sh
`,
		files: map[string]string{
			"pre-commit.sh": "#!/bin/sh\necho approved > \"$HOOK_OUTPUT\"\n",
		},
	})

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordProjectHookApproval(bundle); err != nil {
		t.Fatal(err)
	}

	runtime, err := installProjectHookRuntime(projectDir, "/usr/local/bin/hazmat")
	if err != nil {
		t.Fatal(err)
	}

	hooksPath, err := readLocalGitHooksPath(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if hooksPath != runtime.ManagedDir {
		t.Fatalf("core.hooksPath = %q, want %q", hooksPath, runtime.ManagedDir)
	}
	for _, path := range []string{
		filepath.Join(runtime.ManagedDir, "pre-commit"),
		filepath.Join(runtime.FallbackDir, "pre-commit"),
		runtime.WrapperPath,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}

func TestInstallProjectHookRuntimeRejectsEphemeralHazmatBinary(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
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

	ephemeral := filepath.Join(t.TempDir(), "go-build123", "b001", "exe", "hazmat")
	canonicalProjectDir, err := canonicalizePath(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installProjectHookRuntime(projectDir, ephemeral); err == nil ||
		!strings.Contains(err.Error(), "ephemeral Hazmat binary") ||
		!strings.Contains(err.Error(), "hazmat hooks install -C "+canonicalProjectDir) {
		t.Fatalf("expected ephemeral binary reinstall guidance, got %v", err)
	}
}

func TestValidateProjectHookRuntimeRejectsHooksPathDrift(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
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
	if err := writeLocalGitHooksPath(projectDir, filepath.Join(projectDir, ".agent-hooks")); err != nil {
		t.Fatal(err)
	}

	if _, err := validateProjectHookRuntime(projectDir); err == nil || !strings.Contains(err.Error(), "core.hooksPath drifted") {
		t.Fatalf("expected hooksPath drift error, got %v", err)
	}
}

func TestRunApprovedProjectHookExecutesApprovedHook(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
	projectDir := initGitHookProject(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: sh
`,
		files: map[string]string{
			"pre-commit.sh": "#!/bin/sh\necho approved > \"$HOOK_OUTPUT\"\n",
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

	outputPath := filepath.Join(t.TempDir(), "hook-output.txt")
	t.Setenv("HOOK_OUTPUT", outputPath)
	if err := runApprovedProjectHook(projectDir, hookTypePreCommit, nil); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(raw)), "approved"; got != want {
		t.Fatalf("hook output = %q, want %q", got, want)
	}
}

func TestRunApprovedProjectHookCanReadSnapshotOwnedBundleFile(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
	projectDir := initGitHookProject(t, projectHookBundleFixture{
		manifest: `version: 1
files:
  - gitleaks.toml
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: sh
`,
		files: map[string]string{
			"gitleaks.toml": "title = \"snapshot\"\n",
			"pre-commit.sh": "#!/bin/sh\nset -eu\nSCRIPT_DIR=$(CDPATH= cd -- \"$(dirname \"$0\")\" && pwd)\ncat \"$SCRIPT_DIR/gitleaks.toml\" > \"$HOOK_OUTPUT\"\n",
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

	outputPath := filepath.Join(t.TempDir(), "hook-output.txt")
	t.Setenv("HOOK_OUTPUT", outputPath)
	if err := runApprovedProjectHook(projectDir, hookTypePreCommit, nil); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(raw)), `title = "snapshot"`; got != want {
		t.Fatalf("hook output = %q, want %q", got, want)
	}
}

func TestInstallProjectHookRuntimeChainsExistingHooksPath(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
	projectDir := initGitHookProject(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: sh
`,
		files: map[string]string{
			"pre-commit.sh": "#!/bin/sh\necho approved > \"$HOOK_OUTPUT\"\n",
		},
	})
	beadsHooksDir := filepath.Join(projectDir, ".beads", "hooks")
	if err := os.MkdirAll(beadsHooksDir, 0o700); err != nil {
		t.Fatal(err)
	}
	externalHook := "#!/bin/sh\necho beads >> \"$HOOK_OUTPUT\"\n"
	if err := os.WriteFile(filepath.Join(beadsHooksDir, "pre-commit"), []byte(externalHook), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalGitHooksPath(projectDir, ".beads/hooks"); err != nil {
		t.Fatal(err)
	}

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordProjectHookApproval(bundle); err != nil {
		t.Fatal(err)
	}
	runtime, err := installProjectHookRuntimeWithOptions(projectDir, "/usr/local/bin/hazmat", projectHookRuntimeInstallOptions{ChainExisting: true})
	if err != nil {
		t.Fatal(err)
	}

	if hooksPath, err := readLocalGitHooksPath(projectDir); err != nil || hooksPath != ".beads/hooks" {
		t.Fatalf("core.hooksPath = %q err=%v, want .beads/hooks", hooksPath, err)
	}
	approval, err := loadProjectHookApproval(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if approval == nil || approval.Chain == nil || approval.Chain.HooksPath != ".beads/hooks" {
		t.Fatalf("expected chained approval for .beads/hooks, got %+v", approval)
	}
	raw, err := os.ReadFile(filepath.Join(beadsHooksDir, "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.HasPrefix(text, "#!/bin/sh\n"+projectHookChainBeginMarker) {
		t.Fatalf("expected Hazmat block after shebang, got %q", text)
	}
	if !strings.Contains(text, "echo beads") {
		t.Fatalf("expected external hook body to be preserved, got %q", text)
	}
	if _, err := os.Stat(filepath.Join(runtime.FallbackDir, "pre-commit")); err != nil {
		t.Fatalf("expected fallback dispatcher, got %v", err)
	}
	if _, err := validateProjectHookRuntime(projectDir); err != nil {
		t.Fatalf("expected chained runtime to validate, got %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "hook-output.txt")
	t.Setenv("HOOK_OUTPUT", outputPath)
	if err := runApprovedProjectHook(projectDir, hookTypePreCommit, nil); err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(output)), "approved"; got != want {
		t.Fatalf("approved hook output = %q, want %q", got, want)
	}
}

func TestValidateProjectHookRuntimeRejectsChainedHookDrift(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
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
	beadsHooksDir := filepath.Join(projectDir, ".beads", "hooks")
	if err := os.MkdirAll(beadsHooksDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsHooksDir, "pre-push"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalGitHooksPath(projectDir, ".beads/hooks"); err != nil {
		t.Fatal(err)
	}

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordProjectHookApproval(bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := installProjectHookRuntimeWithOptions(projectDir, "/usr/local/bin/hazmat", projectHookRuntimeInstallOptions{ChainExisting: true}); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(beadsHooksDir, "pre-push")
	file, err := os.OpenFile(hookPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("# drift\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := validateProjectHookRuntime(projectDir); err == nil || !strings.Contains(err.Error(), "drifted from approved hash") {
		t.Fatalf("expected chained hook drift refusal, got %v", err)
	}
}

func TestUninstallProjectHookRuntimeRemovesChainBlockAndKeepsHooksPath(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
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
	beadsHooksDir := filepath.Join(projectDir, ".beads", "hooks")
	if err := os.MkdirAll(beadsHooksDir, 0o700); err != nil {
		t.Fatal(err)
	}
	externalHook := "#!/bin/sh\necho beads >> \"$HOOK_OUTPUT\"\n"
	hookPath := filepath.Join(beadsHooksDir, "pre-commit")
	if err := os.WriteFile(hookPath, []byte(externalHook), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalGitHooksPath(projectDir, ".beads/hooks"); err != nil {
		t.Fatal(err)
	}

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordProjectHookApproval(bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := installProjectHookRuntimeWithOptions(projectDir, "/usr/local/bin/hazmat", projectHookRuntimeInstallOptions{ChainExisting: true}); err != nil {
		t.Fatal(err)
	}
	if err := uninstallProjectHookRuntime(projectDir); err != nil {
		t.Fatal(err)
	}

	if hooksPath, err := readLocalGitHooksPath(projectDir); err != nil || hooksPath != ".beads/hooks" {
		t.Fatalf("expected chained hooksPath to remain .beads/hooks, got %q err=%v", hooksPath, err)
	}
	raw, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, projectHookChainBeginMarker) || strings.Contains(text, projectHookChainEndMarker) {
		t.Fatalf("expected Hazmat chain block removal, got %q", text)
	}
	if !strings.Contains(text, "echo beads") {
		t.Fatalf("expected external hook body to remain, got %q", text)
	}
}

func TestRunApprovedProjectHookRefusesBundleDrift(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
	projectDir := initGitHookProject(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: sh
`,
		files: map[string]string{
			"pre-commit.sh": "#!/bin/sh\necho approved > \"$HOOK_OUTPUT\"\n",
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
	if err := os.WriteFile(filepath.Join(projectDir, projectHooksDirRel, "pre-commit.sh"), []byte("#!/bin/sh\necho drift > \"$HOOK_OUTPUT\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runApprovedProjectHook(projectDir, hookTypePreCommit, nil); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("expected drift refusal, got %v", err)
	}
}

func TestFallbackProjectHookRefusalMentionsReroute(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
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

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordProjectHookApproval(bundle); err != nil {
		t.Fatal(err)
	}
	runtime, err := installProjectHookRuntime(projectDir, "/usr/local/bin/hazmat")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLocalGitHooksPath(projectDir, filepath.Join(projectDir, ".agent-hooks")); err != nil {
		t.Fatal(err)
	}

	err = fallbackProjectHookRefusal(projectDir, hookTypeCommitMsg)
	if err == nil || !strings.Contains(err.Error(), runtime.ManagedDir) {
		t.Fatalf("expected fallback reroute refusal mentioning %q, got %v", runtime.ManagedDir, err)
	}
}

func TestUninstallProjectHookRuntimeRemovesManagedState(t *testing.T) {
	setProjectHookApprovalTestPaths(t)
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
	approval, err := loadProjectHookApproval(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if approval == nil {
		t.Fatal("expected approval before uninstall")
	}
	snapshotPath := approval.SnapshotDir
	runtime, err := installProjectHookRuntime(projectDir, "/usr/local/bin/hazmat")
	if err != nil {
		t.Fatal(err)
	}

	if err := uninstallProjectHookRuntime(projectDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(runtime.WrapperPath); !os.IsNotExist(err) {
		t.Fatalf("expected wrapper removal, stat err=%v", err)
	}
	if hooksPath, err := readLocalGitHooksPath(projectDir); err != nil || hooksPath != "" {
		t.Fatalf("expected core.hooksPath to be unset, got %q err=%v", hooksPath, err)
	}
	if approval, err := loadProjectHookApproval(projectDir); err != nil || approval != nil {
		t.Fatalf("expected approval removal, got %+v err=%v", approval, err)
	}
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("expected snapshot removal, stat err=%v", err)
	}
}

func initGitHookProject(t *testing.T, fixture projectHookBundleFixture) string {
	t.Helper()

	projectDir := writeProjectHookBundle(t, fixture)
	cmd, err := hostGitCommand("init", projectDir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return projectDir
}

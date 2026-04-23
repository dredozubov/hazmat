package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectHookBundleMissingManifest(t *testing.T) {
	bundle, err := loadProjectHookBundle(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if bundle != nil {
		t.Fatalf("expected nil bundle, got %+v", bundle)
	}
}

func TestLoadProjectHookBundleValid(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
files:
  - gitleaks.toml
hooks:
  - type: pre-commit
    script: scripts/pre-commit.sh
    purpose: keep staged files clean
    interpreter: bash
    requires: [gitleaks, bash]
  - type: pre-push
    script: pre-push.sh
    purpose: fast local gate before push
    interpreter: sh
    requires: [git, gofmt]
`,
		files: map[string]string{
			"gitleaks.toml":         "title = \"test\"\n",
			"scripts/pre-commit.sh": "#!/usr/bin/env bash\necho pre-commit\n",
			"pre-push.sh":           "#!/bin/sh\necho pre-push\n",
		},
	})

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if bundle == nil {
		t.Fatal("expected bundle")
	}
	if bundle.BundleHash == "" || bundle.BundleHash[:7] != "sha256:" {
		t.Fatalf("BundleHash = %q, want sha256:...", bundle.BundleHash)
	}
	if got, want := len(bundle.Hooks), 2; got != want {
		t.Fatalf("len(Hooks) = %d, want %d", got, want)
	}
	if bundle.Hooks[0].Type != hookTypePreCommit || bundle.Hooks[1].Type != hookTypePrePush {
		t.Fatalf("hooks not sorted by type: %+v", bundle.Hooks)
	}
	if got, want := bundle.Hooks[0].ScriptPath, "scripts/pre-commit.sh"; got != want {
		t.Fatalf("ScriptPath = %q, want %q", got, want)
	}
	if got, want := len(bundle.Files), 1; got != want {
		t.Fatalf("len(Files) = %d, want %d", got, want)
	}
	if got, want := bundle.Files[0].Path, "gitleaks.toml"; got != want {
		t.Fatalf("Files[0].Path = %q, want %q", got, want)
	}
}

func TestLoadProjectHookBundleRejectsUnknownField(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: bash
    unsupported: true
`,
		files: map[string]string{
			"pre-commit.sh": "#!/usr/bin/env bash\necho ok\n",
		},
	})

	if _, err := loadProjectHookBundle(projectDir); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadProjectHookBundleRejectsUnsupportedHookType(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: post-commit
    script: post-commit.sh
    purpose: not supported
    interpreter: bash
`,
		files: map[string]string{
			"post-commit.sh": "#!/usr/bin/env bash\necho post\n",
		},
	})

	if _, err := loadProjectHookBundle(projectDir); err == nil {
		t.Fatal("expected unsupported hook type error")
	}
}

func TestLoadProjectHookBundleRejectsDuplicateHookType(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: one.sh
    purpose: one
    interpreter: bash
  - type: pre-commit
    script: two.sh
    purpose: two
    interpreter: bash
`,
		files: map[string]string{
			"one.sh": "#!/usr/bin/env bash\necho one\n",
			"two.sh": "#!/usr/bin/env bash\necho two\n",
		},
	})

	if _, err := loadProjectHookBundle(projectDir); err == nil {
		t.Fatal("expected duplicate hook type error")
	}
}

func TestLoadProjectHookBundleRejectsEscapingScriptPath(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: ../escape.sh
    purpose: bad
    interpreter: bash
`,
		files: map[string]string{
			"pre-commit.sh": "#!/usr/bin/env bash\necho ok\n",
		},
	})

	if _, err := loadProjectHookBundle(projectDir); err == nil {
		t.Fatal("expected escaping script path error")
	}
}

func TestLoadProjectHookBundleRejectsSymlinkScript(t *testing.T) {
	projectDir := t.TempDir()
	hooksDir := filepath.Join(projectDir, projectHooksDirRel)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "real.sh"), []byte("#!/bin/sh\necho real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(hooksDir, "real.sh"), filepath.Join(hooksDir, "link.sh")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.yaml"), []byte(`version: 1
hooks:
  - type: pre-commit
    script: link.sh
    purpose: bad symlink
    interpreter: sh
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := loadProjectHookBundle(projectDir); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestLoadProjectHookBundleRejectsInvalidInterpreter(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: bad interpreter
    interpreter: /bin/bash
`,
		files: map[string]string{
			"pre-commit.sh": "#!/usr/bin/env bash\necho ok\n",
		},
	})

	if _, err := loadProjectHookBundle(projectDir); err == nil {
		t.Fatal("expected invalid interpreter error")
	}
}

func TestLoadProjectHookBundleRejectsInvalidRequiredBinary(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: bad requires
    interpreter: bash
    requires: ["/usr/bin/git"]
`,
		files: map[string]string{
			"pre-commit.sh": "#!/usr/bin/env bash\necho ok\n",
		},
	})

	if _, err := loadProjectHookBundle(projectDir); err == nil {
		t.Fatal("expected invalid required binary error")
	}
}

func TestLoadProjectHookBundleRejectsDuplicateBundleFile(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
files:
  - config.toml
  - ./config.toml
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: sh
`,
		files: map[string]string{
			"config.toml":   "title = \"test\"\n",
			"pre-commit.sh": "#!/bin/sh\nexit 0\n",
		},
	})

	if _, err := loadProjectHookBundle(projectDir); err == nil {
		t.Fatal("expected duplicate bundle file error")
	}
}

func TestLoadProjectHookBundleRejectsBundleFileDuplicatingScript(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
files:
  - pre-commit.sh
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

	if _, err := loadProjectHookBundle(projectDir); err == nil {
		t.Fatal("expected bundle file/script duplication error")
	}
}

func TestLoadProjectHookBundleHashStableAcrossManifestOrder(t *testing.T) {
	projectA := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
files:
  - config.toml
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: bash
    requires: [gitleaks, bash]
  - type: pre-push
    script: scripts/pre-push.sh
    purpose: fast local gate
    interpreter: sh
    requires: [git, gofmt]
`,
		files: map[string]string{
			"config.toml":         "title = \"test\"\n",
			"pre-commit.sh":       "#!/usr/bin/env bash\necho pre-commit\n",
			"scripts/pre-push.sh": "#!/bin/sh\necho pre-push\n",
		},
	})
	projectB := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
files:
  - ./config.toml
hooks:
  - type: pre-push
    script: scripts/pre-push.sh
    purpose: fast local gate
    interpreter: sh
    requires: [gofmt, git]
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: bash
    requires: [bash, gitleaks]
`,
		files: map[string]string{
			"config.toml":         "title = \"test\"\n",
			"pre-commit.sh":       "#!/usr/bin/env bash\necho pre-commit\n",
			"scripts/pre-push.sh": "#!/bin/sh\necho pre-push\n",
		},
	})

	bundleA, err := loadProjectHookBundle(projectA)
	if err != nil {
		t.Fatal(err)
	}
	bundleB, err := loadProjectHookBundle(projectB)
	if err != nil {
		t.Fatal(err)
	}
	if bundleA.BundleHash != bundleB.BundleHash {
		t.Fatalf("BundleHash mismatch:\nA=%s\nB=%s", bundleA.BundleHash, bundleB.BundleHash)
	}
}

func TestLoadProjectHookBundleHashChangesWhenScriptChanges(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-commit
    script: pre-commit.sh
    purpose: keep staged files clean
    interpreter: bash
`,
		files: map[string]string{
			"pre-commit.sh": "#!/usr/bin/env bash\necho one\n",
		},
	})

	bundleA, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, projectHooksDirRel, "pre-commit.sh"), []byte("#!/usr/bin/env bash\necho two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundleB, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if bundleA.BundleHash == bundleB.BundleHash {
		t.Fatalf("expected hash change after script mutation, got %s", bundleA.BundleHash)
	}
}

func TestLoadProjectHookBundleHashChangesWhenBundleFileChanges(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
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
			"gitleaks.toml": "title = \"one\"\n",
			"pre-commit.sh": "#!/bin/sh\nexit 0\n",
		},
	})

	bundleA, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, projectHooksDirRel, "gitleaks.toml"), []byte("title = \"two\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundleB, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if bundleA.BundleHash == bundleB.BundleHash {
		t.Fatalf("expected hash change after bundle file mutation, got %s", bundleA.BundleHash)
	}
}

func TestSummarizeProjectHookBundle(t *testing.T) {
	projectDir := writeProjectHookBundle(t, projectHookBundleFixture{
		manifest: `version: 1
hooks:
  - type: pre-push
    script: pre-push.sh
    purpose: fast local gate
    interpreter: sh
    requires: [git]
`,
		files: map[string]string{
			"pre-push.sh": "#!/bin/sh\necho pre-push\n",
		},
	})

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		t.Fatal(err)
	}

	installSummary := summarizeProjectHookBundle(bundle, "")
	if installSummary.Kind != hookReviewInstall {
		t.Fatalf("install summary kind = %q, want %q", installSummary.Kind, hookReviewInstall)
	}
	if got, want := len(installSummary.Hooks), 1; got != want {
		t.Fatalf("len(Hooks) = %d, want %d", got, want)
	}
	if got, want := installSummary.Hooks[0].Purpose, "fast local gate"; got != want {
		t.Fatalf("Purpose = %q, want %q", got, want)
	}
	if got, want := installSummary.Hooks[0].Interpreter, "sh"; got != want {
		t.Fatalf("Interpreter = %q, want %q", got, want)
	}

	driftSummary := summarizeProjectHookBundle(bundle, "sha256:previous")
	if driftSummary.Kind != hookReviewDrift {
		t.Fatalf("drift summary kind = %q, want %q", driftSummary.Kind, hookReviewDrift)
	}
}

type projectHookBundleFixture struct {
	manifest string
	files    map[string]string
}

func writeProjectHookBundle(t *testing.T, fixture projectHookBundleFixture) string {
	t.Helper()

	projectDir := t.TempDir()
	hooksDir := filepath.Join(projectDir, projectHooksDirRel)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for relPath, raw := range fixture.files {
		path := filepath.Join(hooksDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(projectDir, projectHooksManifestRelPath), []byte(fixture.manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return projectDir
}

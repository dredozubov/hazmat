package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFindInstalledQwenBinaryWith(t *testing.T) {
	got, ok := findInstalledQwenBinaryWith(func(args ...string) (string, error) {
		if args[len(args)-1] == agentHome+qwenBinRel {
			return "", nil
		}
		return "", errors.New("missing")
	})
	if !ok {
		t.Fatal("expected Qwen binary to be detected")
	}
	if got != agentHome+qwenBinRel {
		t.Fatalf("findInstalledQwenBinaryWith() = %q, want %q", got, agentHome+qwenBinRel)
	}
}

func TestQwenLaunchScriptChecksInstalledPath(t *testing.T) {
	script := qwenLaunchScript()
	for _, want := range []string{
		`"$HOME/.local/bin/qwen"`,
		qwenMissingHelp,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("qwenLaunchScript() missing %q in %q", want, script)
		}
	}
}

func TestQwenLaunchScriptForwardsArgsAndCWDToFakeBinary(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	capture := filepath.Join(t.TempDir(), "capture.txt")
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeQwen := filepath.Join(binDir, "qwen")
	fakeScript := `#!/bin/sh
{
  pwd
  printf 'ARGS=%s\n' "$*"
} > "$CAPTURE"
`
	if err := os.WriteFile(fakeQwen, []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/bash", "-c", qwenLaunchScript(), "hazmat-qwen", "--yolo", "-p", "hello")
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"SANDBOX_PROJECT_DIR="+project,
		"CAPTURE="+capture,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run qwen launch script: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		project + "\n",
		"ARGS=--yolo -p hello",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("capture missing %q in:\n%s", want, got)
		}
	}
}

func TestQwenLaunchArgsAddsYoloWhenSkipPermissionsEnabled(t *testing.T) {
	got := qwenLaunchArgs([]string{"-p", "say ok"}, true)
	want := []string{"--yolo", "-p", "say ok"}
	if !slices.Equal(got, want) {
		t.Fatalf("qwenLaunchArgs(skip) = %v, want %v", got, want)
	}

	for _, args := range [][]string{
		{"--yolo", "-p", "say ok"},
		{"-y", "-p", "say ok"},
	} {
		if got := qwenLaunchArgs(args, true); !slices.Equal(got, args) {
			t.Fatalf("qwenLaunchArgs(%v) = %v, want unchanged", args, got)
		}
	}

	args := []string{"-p", "say ok"}
	if got := qwenLaunchArgs(args, false); !slices.Equal(got, args) {
		t.Fatalf("qwenLaunchArgs(no skip) = %v, want unchanged", got)
	}
}

func TestQwenPreparedSessionCarriesHarnessIDAndAssetPolicy(t *testing.T) {
	skipInitCheck(t)

	prepared, err := resolvePreparedSession("qwen", harnessSessionOpts{
		project:  t.TempDir(),
		planOnly: true,
	}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(qwen): %v", err)
	}
	if got := prepared.Config.HarnessID; got != HarnessQwen {
		t.Fatalf("HarnessID = %q, want %q", got, HarnessQwen)
	}
	if _, ok := prepared.Config.HarnessEnv["QWEN_HOME"]; ok {
		t.Fatalf("Qwen session should use default contained ~/.qwen, got HarnessEnv=%v", prepared.Config.HarnessEnv)
	}
	if _, ok := harnessAssetSpecs[HarnessQwen]; !ok {
		t.Fatal("Qwen must define portable prompt-asset sync specs")
	}
}

func TestBootstrapCommandIncludesQwenAndConfigImportDoesNot(t *testing.T) {
	bootstrap := newBootstrapCmd()
	if _, _, err := bootstrap.Find([]string{"qwen"}); err != nil {
		t.Fatalf("bootstrap qwen command missing: %v", err)
	}
	if !strings.Contains(bootstrap.Long, "hazmat bootstrap qwen") {
		t.Fatalf("bootstrap help does not list Qwen:\n%s", bootstrap.Long)
	}

	configImport := newConfigImportCmd()
	if commandHasSubcommand(configImport, "qwen") {
		t.Fatal("config import qwen must not exist in Phase 1")
	}
}

func TestExplainSupportsQwenTarget(t *testing.T) {
	skipInitCheck(t)
	cfg, _, err := resolveExplainSession("qwen", harnessSessionOpts{
		project:  t.TempDir(),
		planOnly: true,
	})
	if err != nil {
		t.Fatalf("resolveExplainSession(qwen): %v", err)
	}
	if cfg.HarnessID != HarnessQwen {
		t.Fatalf("explain HarnessID = %q, want %q", cfg.HarnessID, HarnessQwen)
	}
}

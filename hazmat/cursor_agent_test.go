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

func TestFindInstalledCursorAgentBinaryWith(t *testing.T) {
	got, ok := findInstalledCursorAgentBinaryWith(func(args ...string) (string, error) {
		if args[len(args)-1] == agentHome+cursorAgentBinRel {
			return "", nil
		}
		return "", errors.New("missing")
	})
	if !ok {
		t.Fatal("expected Cursor Agent binary to be detected")
	}
	if got != agentHome+cursorAgentBinRel {
		t.Fatalf("findInstalledCursorAgentBinaryWith() = %q, want %q", got, agentHome+cursorAgentBinRel)
	}
}

func TestProbeCursorAgentBinary(t *testing.T) {
	calls := [][]string{}
	read := func(args ...string) (string, error) {
		calls = append(calls, append([]string{}, args...))
		switch {
		case slices.Equal(args, []string{"test", "-x", agentHome + cursorAgentBinRel}):
			return "", nil
		case slices.Equal(args, []string{agentHome + cursorAgentBinRel, "--version"}):
			return "cursor-agent 2026.1.0\n", nil
		default:
			return "", errors.New("unexpected call")
		}
	}
	path, version, err := probeCursorAgentBinary(read, false)
	if err != nil {
		t.Fatalf("probeCursorAgentBinary: %v", err)
	}
	if path != agentHome+cursorAgentBinRel || version != "cursor-agent 2026.1.0" {
		t.Fatalf("probeCursorAgentBinary = %q, %q", path, version)
	}
	if len(calls) != 2 {
		t.Fatalf("probe calls = %v, want install check and version probe", calls)
	}
}

func TestProbeCursorAgentBinaryMissingAndDryRun(t *testing.T) {
	missing := func(args ...string) (string, error) {
		return "", errors.New("missing")
	}
	if _, _, err := probeCursorAgentBinary(missing, false); !errors.Is(err, errCursorAgentBinaryMissing) {
		t.Fatalf("missing probe err = %v, want errCursorAgentBinaryMissing", err)
	}
	path, version, err := probeCursorAgentBinary(missing, true)
	if err != nil {
		t.Fatalf("dry-run missing probe err = %v, want nil", err)
	}
	if path != "" || version != "" {
		t.Fatalf("dry-run missing probe = %q, %q; want empty", path, version)
	}
}

func TestProbeCursorAgentBinaryVersionFailure(t *testing.T) {
	read := func(args ...string) (string, error) {
		if slices.Equal(args, []string{"test", "-x", agentHome + cursorAgentBinRel}) {
			return "", nil
		}
		return "", errors.New("boom")
	}
	if _, _, err := probeCursorAgentBinary(read, false); err == nil || !strings.Contains(err.Error(), "--version") {
		t.Fatalf("version failure err = %v, want --version context", err)
	}
}

func TestCursorAgentLaunchScriptChecksInstalledPath(t *testing.T) {
	script := cursorAgentLaunchScript()
	for _, want := range []string{
		`"$HOME/.local/bin/cursor-agent"`,
		cursorAgentMissingHelp,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("cursorAgentLaunchScript() missing %q in %q", want, script)
		}
	}
	if strings.Contains(script, ".cursor") {
		t.Fatalf("cursorAgentLaunchScript() must not reference host or default .cursor state: %q", script)
	}
}

func TestCursorAgentLaunchScriptForwardsArgsAndCWDToFakeBinary(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	capture := filepath.Join(t.TempDir(), "capture.txt")
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCursorAgent := filepath.Join(binDir, "cursor-agent")
	fakeScript := `#!/bin/sh
{
  pwd
  printf 'ARGS=%s\n' "$*"
} > "$CAPTURE"
`
	if err := os.WriteFile(fakeCursorAgent, []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/bash", "-c", cursorAgentLaunchScript(), "hazmat-cursor-agent", "--print", "--output-format", "stream-json", "--force", "--trust")
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"SANDBOX_PROJECT_DIR="+project,
		"CAPTURE="+capture,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cursor-agent launch script: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		project + "\n",
		"ARGS=--print --output-format stream-json --force --trust",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("capture missing %q in:\n%s", want, got)
		}
	}
}

func TestCursorAgentPreparedSessionCarriesHarnessIDAndNoAssetPolicy(t *testing.T) {
	skipInitCheck(t)

	prepared, err := resolvePreparedSession("cursor-agent", harnessSessionOpts{
		project:  t.TempDir(),
		planOnly: true,
	}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(cursor-agent): %v", err)
	}
	if got := prepared.Config.HarnessID; got != HarnessCursorAgent {
		t.Fatalf("HarnessID = %q, want %q", got, HarnessCursorAgent)
	}
	if len(prepared.Config.HarnessEnv) != 0 {
		t.Fatalf("Cursor Agent session should not inject harness env in v1, got HarnessEnv=%v", prepared.Config.HarnessEnv)
	}
	if _, ok := harnessAssetSpecs[HarnessCursorAgent]; ok {
		t.Fatal("Cursor Agent must not define host asset sync specs in v1")
	}
}

func TestBootstrapCommandIncludesCursorAgentAndConfigImportDoesNot(t *testing.T) {
	bootstrap := newBootstrapCmd()
	if _, _, err := bootstrap.Find([]string{"cursor-agent"}); err != nil {
		t.Fatalf("bootstrap cursor-agent command missing: %v", err)
	}
	if !strings.Contains(bootstrap.Long, "hazmat bootstrap cursor-agent") {
		t.Fatalf("bootstrap help does not list Cursor Agent:\n%s", bootstrap.Long)
	}

	configImport := newConfigImportCmd()
	if commandHasSubcommand(configImport, "cursor-agent") {
		t.Fatal("config import cursor-agent must not exist in Phase 1")
	}
}

func TestExplainSupportsCursorAgentTarget(t *testing.T) {
	skipInitCheck(t)
	cfg, _, err := resolveExplainSession("cursor-agent", harnessSessionOpts{
		project:  t.TempDir(),
		planOnly: true,
	})
	if err != nil {
		t.Fatalf("resolveExplainSession(cursor-agent): %v", err)
	}
	if cfg.HarnessID != HarnessCursorAgent {
		t.Fatalf("explain HarnessID = %q, want %q", cfg.HarnessID, HarnessCursorAgent)
	}
}

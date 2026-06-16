package hazmat

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFindInstalledPiBinaryWith(t *testing.T) {
	got, ok := findInstalledPiBinaryWith(func(args ...string) (string, error) {
		if args[len(args)-1] == agentHome+piBinRel {
			return "", nil
		}
		return "", errors.New("missing")
	})
	if !ok {
		t.Fatal("expected Pi binary to be detected")
	}
	if got != agentHome+piBinRel {
		t.Fatalf("findInstalledPiBinaryWith() = %q, want %q", got, agentHome+piBinRel)
	}
}

func TestProbePiBinary(t *testing.T) {
	calls := [][]string{}
	read := func(args ...string) (string, error) {
		calls = append(calls, append([]string{}, args...))
		switch {
		case slices.Equal(args, []string{"test", "-x", agentHome + piBinRel}):
			return "", nil
		case slices.Equal(args, []string{agentHome + piBinRel, "--version"}):
			return "pi 1.2.3\n", nil
		default:
			return "", errors.New("unexpected call")
		}
	}
	path, version, err := probePiBinary(read, false)
	if err != nil {
		t.Fatalf("probePiBinary: %v", err)
	}
	if path != agentHome+piBinRel || version != "pi 1.2.3" {
		t.Fatalf("probePiBinary = %q, %q", path, version)
	}
	if len(calls) != 2 {
		t.Fatalf("probe calls = %v, want install check and version probe", calls)
	}
}

func TestProbePiBinaryMissingAndDryRun(t *testing.T) {
	missing := func(args ...string) (string, error) {
		return "", errors.New("missing")
	}
	if _, _, err := probePiBinary(missing, false); !errors.Is(err, errPiBinaryMissing) {
		t.Fatalf("missing probe err = %v, want errPiBinaryMissing", err)
	}
	path, version, err := probePiBinary(missing, true)
	if err != nil {
		t.Fatalf("dry-run missing probe err = %v, want nil", err)
	}
	if path != "" || version != "" {
		t.Fatalf("dry-run missing probe = %q, %q; want empty", path, version)
	}
}

func TestProbePiBinaryVersionFailure(t *testing.T) {
	read := func(args ...string) (string, error) {
		if slices.Equal(args, []string{"test", "-x", agentHome + piBinRel}) {
			return "", nil
		}
		return "", errors.New("boom")
	}
	if _, _, err := probePiBinary(read, false); err == nil || !strings.Contains(err.Error(), "--version") {
		t.Fatalf("version failure err = %v, want --version context", err)
	}
}

func TestPiLaunchScriptChecksInstalledPath(t *testing.T) {
	script := piLaunchScript()
	for _, want := range []string{
		`"$HOME/.local/bin/pi"`,
		piMissingHelp,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("piLaunchScript() missing %q in %q", want, script)
		}
	}
	if strings.Contains(script, ".pi/agent") {
		t.Fatalf("piLaunchScript() should not create or import Pi state directly: %q", script)
	}
}

func TestPiLaunchScriptForwardsArgsAndCWDToFakeBinary(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	capture := filepath.Join(t.TempDir(), "capture.txt")
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakePi := filepath.Join(binDir, "pi")
	fakeScript := `#!/bin/sh
{
  pwd
  printf 'ARGS=%s\n' "$*"
} > "$CAPTURE"
`
	if err := os.WriteFile(fakePi, []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/bash", "-c", piLaunchScript(), "hazmat-pi", "--mode", "rpc")
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"SANDBOX_PROJECT_DIR="+project,
		"CAPTURE="+capture,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run pi launch script: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		project + "\n",
		"ARGS=--mode rpc",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("capture missing %q in:\n%s", want, got)
		}
	}
}

func TestPiPreparedSessionCarriesHarnessIDAndNoAssetPolicy(t *testing.T) {
	skipInitCheck(t)

	prepared, err := resolvePreparedSession("pi", harnessSessionOpts{
		project:  t.TempDir(),
		planOnly: true,
	}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(pi): %v", err)
	}
	if got := prepared.Config.HarnessID; got != HarnessPi {
		t.Fatalf("HarnessID = %q, want %q", got, HarnessPi)
	}
	if len(prepared.Config.HarnessEnv) != 0 {
		t.Fatalf("Pi session should not inject harness env in v1, got HarnessEnv=%v", prepared.Config.HarnessEnv)
	}
	if _, ok := harnessAssetSpecs[HarnessPi]; ok {
		t.Fatal("Pi must not define host asset sync specs in v1")
	}
	if !sessionNotesContain(prepared.Config.SessionNotes, "host ~/.pi/agent is not imported") {
		t.Fatalf("SessionNotes missing host import boundary: %v", prepared.Config.SessionNotes)
	}
}

func TestBootstrapCommandIncludesPiAndConfigImportDoesNot(t *testing.T) {
	bootstrap := newBootstrapCmd()
	if _, _, err := bootstrap.Find([]string{"pi"}); err != nil {
		t.Fatalf("bootstrap pi command missing: %v", err)
	}
	if !strings.Contains(bootstrap.Long, "hazmat bootstrap pi") {
		t.Fatalf("bootstrap help does not list Pi:\n%s", bootstrap.Long)
	}

	configImport := newConfigImportCmd()
	if commandHasSubcommand(configImport, "pi") {
		t.Fatal("config import pi must not exist in Phase 1")
	}
}

func TestExplainSupportsPiTarget(t *testing.T) {
	skipInitCheck(t)
	cfg, _, err := resolveExplainSession("pi", harnessSessionOpts{
		project:  t.TempDir(),
		planOnly: true,
	})
	if err != nil {
		t.Fatalf("resolveExplainSession(pi): %v", err)
	}
	if cfg.HarnessID != HarnessPi {
		t.Fatalf("explain HarnessID = %q, want %q", cfg.HarnessID, HarnessPi)
	}
}

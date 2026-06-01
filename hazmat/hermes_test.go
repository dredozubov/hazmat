package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestFindInstalledHermesBinaryWith(t *testing.T) {
	got, ok := findInstalledHermesBinaryWith(func(args ...string) (string, error) {
		if args[len(args)-1] == agentHome+hermesBinRel {
			return "", nil
		}
		return "", errors.New("missing")
	})
	if !ok {
		t.Fatal("expected Hermes binary to be detected")
	}
	if got != agentHome+hermesBinRel {
		t.Fatalf("findInstalledHermesBinaryWith() = %q, want %q", got, agentHome+hermesBinRel)
	}
}

func TestProbeHermesBinary(t *testing.T) {
	calls := [][]string{}
	read := func(args ...string) (string, error) {
		calls = append(calls, append([]string{}, args...))
		switch {
		case slices.Equal(args, []string{"test", "-x", agentHome + hermesBinRel}):
			return "", nil
		case slices.Equal(args, []string{agentHome + hermesBinRel, "--version"}):
			return "hermes 1.2.3\n", nil
		default:
			return "", errors.New("unexpected call")
		}
	}
	path, version, err := probeHermesBinary(read, false)
	if err != nil {
		t.Fatalf("probeHermesBinary: %v", err)
	}
	if path != agentHome+hermesBinRel || version != "hermes 1.2.3" {
		t.Fatalf("probeHermesBinary = %q, %q", path, version)
	}
	if len(calls) != 2 {
		t.Fatalf("probe calls = %v, want install check and version probe", calls)
	}
}

func TestProbeHermesBinaryMissingAndDryRun(t *testing.T) {
	missing := func(args ...string) (string, error) {
		return "", errors.New("missing")
	}
	if _, _, err := probeHermesBinary(missing, false); !errors.Is(err, errHermesBinaryMissing) {
		t.Fatalf("missing probe err = %v, want errHermesBinaryMissing", err)
	}
	path, version, err := probeHermesBinary(missing, true)
	if err != nil {
		t.Fatalf("dry-run missing probe err = %v, want nil", err)
	}
	if path != "" || version != "" {
		t.Fatalf("dry-run missing probe = %q, %q; want empty", path, version)
	}
}

func TestProbeHermesBinaryVersionFailure(t *testing.T) {
	read := func(args ...string) (string, error) {
		if slices.Equal(args, []string{"test", "-x", agentHome + hermesBinRel}) {
			return "", nil
		}
		return "", errors.New("boom")
	}
	if _, _, err := probeHermesBinary(read, false); err == nil || !strings.Contains(err.Error(), "--version") {
		t.Fatalf("version failure err = %v, want --version context", err)
	}
}

func TestHermesLaunchScriptChecksInstalledPath(t *testing.T) {
	script := hermesLaunchScript()
	for _, want := range []string{
		`"$HOME/.local/bin/hermes"`,
		hermesMissingHelp,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("hermesLaunchScript() missing %q in %q", want, script)
		}
	}
	if strings.Contains(script, ".hermes") {
		t.Fatalf("hermesLaunchScript() must not reference host or default .hermes state: %q", script)
	}
}

func TestHermesLaunchScriptForwardsArgsEnvAndCWDToFakeBinary(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	capture := filepath.Join(t.TempDir(), "capture.txt")
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeHermes := filepath.Join(binDir, "hermes")
	fakeScript := `#!/bin/sh
{
  pwd
  printf 'HERMES_HOME=%s\n' "$HERMES_HOME"
  printf 'OPENAI_API_KEY=%s\n' "$OPENAI_API_KEY"
  printf 'ARGS=%s\n' "$*"
} > "$CAPTURE"
`
	if err := os.WriteFile(fakeHermes, []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/bash", "-c", hermesLaunchScript(), "hazmat-hermes", "chat", "--toolsets", "terminal,file")
	hermesHome := hermesProjectStateDir(project)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"SANDBOX_PROJECT_DIR="+project,
		"HERMES_HOME="+hermesHome,
		"OPENAI_API_KEY=fake-openai",
		"CAPTURE="+capture,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run hermes launch script: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		project + "\n",
		"HERMES_HOME=" + hermesHome,
		"OPENAI_API_KEY=fake-openai",
		"ARGS=chat --toolsets terminal,file",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("capture missing %q in:\n%s", want, got)
		}
	}
}

func TestRejectHermesDeferredEntrypoints(t *testing.T) {
	blocked := [][]string{
		{"gateway"},
		{"dashboard"},
		{"api"},
		{"server"},
		{"serve"},
		{"cron", "add", "daily"},
	}
	for _, args := range blocked {
		if err := rejectHermesDeferredEntrypoint(args); err == nil {
			t.Fatalf("rejectHermesDeferredEntrypoint(%v) = nil, want error", args)
		}
	}

	allowed := [][]string{
		nil,
		{"--version"},
		{"chat"},
		{"chat", "gateway"},
		{"--profile", "gateway"},
		{"--profile", "gateway", "chat"},
		{"--profile=gateway", "chat"},
		{"--model", "gateway", "chat"},
		{"-p", "gateway", "chat"},
		{"-pgateway", "chat"},
		{"--profile", "work", "chat"},
	}
	for _, args := range allowed {
		if err := rejectHermesDeferredEntrypoint(args); err != nil {
			t.Fatalf("rejectHermesDeferredEntrypoint(%v) = %v, want nil", args, err)
		}
	}

	collidingFlagValues := [][]string{
		{"--profile", "work", "gateway"},
		{"--profile=work", "gateway"},
		{"--model", "sonnet", "dashboard"},
		{"-p", "work", "cron", "add", "daily"},
		{"-pwork", "server"},
	}
	for _, args := range collidingFlagValues {
		if err := rejectHermesDeferredEntrypoint(args); err == nil {
			t.Fatalf("rejectHermesDeferredEntrypoint(%v) = nil, want error", args)
		}
	}
}

func TestHermesSessionEnvAndStateRootPlan(t *testing.T) {
	skipInitCheck(t)
	dir := t.TempDir()

	prepared, err := resolvePreparedSession("hermes", harnessSessionOpts{
		project:  dir,
		planOnly: true,
	}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(hermes): %v", err)
	}
	if got := prepared.Config.HarnessID; got != HarnessHermes {
		t.Fatalf("HarnessID = %q, want %q", got, HarnessHermes)
	}
	hermesHome := hermesProjectStateDir(prepared.Config.ProjectDir)
	if got := prepared.Config.HarnessEnv["HERMES_HOME"]; got != hermesHome {
		t.Fatalf("HERMES_HOME = %q, want %q", got, hermesHome)
	}
	if _, ok := prepared.Config.HarnessEnv["OPENAI_API_KEY"]; ok {
		t.Fatal("unexpected provider key in empty test store")
	}
	if !sessionNotesContain(prepared.Config.SessionNotes, "host ~/.hermes is not imported") {
		t.Fatalf("SessionNotes missing host import boundary: %v", prepared.Config.SessionNotes)
	}
	if !sessionMutationsContain(prepared.Config.PlannedHostMutations, "Hermes state root") {
		t.Fatalf("planned mutations missing Hermes state root: %v", prepared.Config.PlannedHostMutations)
	}
	if _, ok := harnessAssetSpecs[HarnessHermes]; ok {
		t.Fatal("Hermes must not have host harness asset sync specs in v1")
	}
}

func TestHermesPreparedSessionCarriesNetworkMetadataAndProviderGrants(t *testing.T) {
	skipInitCheck(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, spec := range harnessAPIKeyPrompts {
		if err := storeHostAPIKey(spec, "stored-"+spec.EnvVar); err != nil {
			t.Fatalf("storeHostAPIKey(%s): %v", spec.EnvVar, err)
		}
	}

	prepared, err := resolvePreparedSession("hermes", harnessSessionOpts{
		project:             t.TempDir(),
		networkMode:         "none",
		networkModeExplicit: true,
		metadataJSON:        true,
		planOnly:            true,
	}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(hermes): %v", err)
	}
	cfg := prepared.Config
	if cfg.HarnessID != HarnessHermes {
		t.Fatalf("HarnessID = %q, want Hermes", cfg.HarnessID)
	}
	if cfg.NetworkMode != sessionNetworkNone || !cfg.EmitSessionMetadataJSON {
		t.Fatalf("network/metadata = %q/%v, want none/true", cfg.NetworkMode, cfg.EmitSessionMetadataJSON)
	}
	for _, envVar := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"} {
		if got := cfg.HarnessEnv[envVar]; got != "stored-"+envVar {
			t.Fatalf("Hermes HarnessEnv[%s] = %q", envVar, got)
		}
	}
	if len(cfg.CredentialEnvGrants) != 4 {
		t.Fatalf("CredentialEnvGrants = %v, want 4 provider grants", cfg.CredentialEnvGrants)
	}
	for _, dir := range append(append([]string{}, cfg.ReadDirs...), cfg.WriteDirs...) {
		if strings.Contains(dir, "docker.sock") {
			t.Fatalf("Hermes session unexpectedly exposes Docker socket path: %v", dir)
		}
	}
}

func TestNonHermesSessionDoesNotReceiveHermesHome(t *testing.T) {
	cfg := sessionConfig{HarnessID: HarnessCodex}
	applyHarnessStaticSessionEnv(&cfg)
	if _, ok := cfg.HarnessEnv["HERMES_HOME"]; ok {
		t.Fatalf("Codex HarnessEnv includes HERMES_HOME: %v", cfg.HarnessEnv)
	}
	if plan := buildHermesStateRootMutationPlan(cfg); len(plan.Mutations) != 0 {
		t.Fatalf("Codex Hermes state plan = %v, want empty", plan.Mutations)
	}
}

func TestHermesStateRootMutationUsesManagedAgentPath(t *testing.T) {
	projectDir := "/Users/dr/workspace/project"
	expected := hermesProjectStateDir(projectDir)
	cfg := sessionConfig{HarnessID: HarnessHermes, ProjectDir: projectDir}
	plan := buildHermesStateRootMutationPlan(cfg)
	if len(plan.Mutations) != 1 {
		t.Fatalf("Hermes mutation count = %d, want 1", len(plan.Mutations))
	}

	var ensured []string
	saved := ensureHermesStateRoot
	ensureHermesStateRoot = func(path string) error {
		ensured = append(ensured, path)
		return nil
	}
	t.Cleanup(func() { ensureHermesStateRoot = saved })

	exec, err := plan.Mutations[0].Apply()
	if err != nil {
		t.Fatalf("apply Hermes state root: %v", err)
	}
	if !slices.Equal(ensured, []string{expected}) {
		t.Fatalf("ensured = %v, want %v", ensured, []string{expected})
	}
	if !strings.Contains(exec.AppliedMessage, expected) {
		t.Fatalf("AppliedMessage = %q", exec.AppliedMessage)
	}
}

func TestHermesProjectStateDirIsProjectScoped(t *testing.T) {
	projectA := "/Users/dr/workspace/project-a"
	projectB := "/Users/dr/workspace/project-b"
	gotA := hermesProjectStateDir(projectA)
	gotARepeat := hermesProjectStateDir(projectA)
	gotB := hermesProjectStateDir(projectB)
	if gotA != gotARepeat {
		t.Fatalf("hermesProjectStateDir is not stable: %q != %q", gotA, gotARepeat)
	}
	if gotA == gotB {
		t.Fatalf("project state dirs collide: %q", gotA)
	}
	if !strings.HasPrefix(gotA, hermesProjectsDir()+string(os.PathSeparator)) {
		t.Fatalf("project state dir %q is outside %q", gotA, hermesProjectsDir())
	}
	if strings.Contains(gotA, "project-a") {
		t.Fatalf("project state dir should not embed project basename: %q", gotA)
	}
}

func TestBootstrapCommandIncludesHermesAndConfigImportDoesNot(t *testing.T) {
	bootstrap := newBootstrapCmd()
	if _, _, err := bootstrap.Find([]string{"hermes"}); err != nil {
		t.Fatalf("bootstrap hermes command missing: %v", err)
	}
	if !strings.Contains(bootstrap.Long, "hazmat bootstrap hermes") {
		t.Fatalf("bootstrap help does not list Hermes:\n%s", bootstrap.Long)
	}

	configImport := newConfigImportCmd()
	if commandHasSubcommand(configImport, "hermes") {
		t.Fatal("config import hermes must not exist in Phase 1")
	}
}

func sessionNotesContain(notes []string, needle string) bool {
	for _, note := range notes {
		if strings.Contains(note, needle) {
			return true
		}
	}
	return false
}

func commandHasSubcommand(cmd *cobra.Command, name string) bool {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return true
		}
	}
	return false
}

func sessionMutationsContain(mutations []sessionMutation, summary string) bool {
	for _, mutation := range mutations {
		if mutation.Summary == summary {
			return true
		}
	}
	return false
}

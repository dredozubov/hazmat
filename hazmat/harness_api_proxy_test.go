package hazmat

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHermesMuginnAPIProxyPlanOnlyInjectsLocalProxyEnv(t *testing.T) {
	skipInitCheck(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, spec := range harnessAPIKeyPrompts {
		if err := storeHostAPIKey(spec, "stored-"+spec.EnvVar); err != nil {
			t.Fatalf("storeHostAPIKey(%s): %v", spec.EnvVar, err)
		}
	}

	restoreStart := startMuginnOpenAIProxy
	startMuginnOpenAIProxy = func(string) (muginnProxyRuntimeInfo, error) {
		t.Fatal("plan-only proxy setup must not start Muginn")
		return muginnProxyRuntimeInfo{}, nil
	}
	t.Cleanup(func() { startMuginnOpenAIProxy = restoreStart })

	prepared, err := resolvePreparedSession("hermes", harnessSessionOpts{
		project:      t.TempDir(),
		apiProxyMode: string(apiProxyModeMuginn),
		planOnly:     true,
	}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(hermes --api-proxy=muginn): %v", err)
	}

	env := prepared.Config.HarnessEnv
	if env["OPENAI_BASE_URL"] != "http://127.0.0.1:0/v1" {
		t.Fatalf("OPENAI_BASE_URL = %q", env["OPENAI_BASE_URL"])
	}
	if env["OPENAI_API_KEY"] != "muginn-local-proxy-token" {
		t.Fatalf("OPENAI_API_KEY = %q, want local proxy token placeholder", env["OPENAI_API_KEY"])
	}
	if env["OPENAI_MODEL"] != defaultMuginnProxyModel {
		t.Fatalf("OPENAI_MODEL = %q, want %q", env["OPENAI_MODEL"], defaultMuginnProxyModel)
	}
	if env["HERMES_HOME"] == "" {
		t.Fatal("HERMES_HOME missing from proxy env")
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"} {
		if _, ok := env[name]; ok {
			t.Fatalf("proxy mode exposed provider env %s in HarnessEnv=%v", name, env)
		}
	}
	if got := prepared.Config.CredentialEnvGrants; len(got) != 1 ||
		got[0].EnvVar != "OPENAI_API_KEY" ||
		got[0].Source != "Muginn local proxy session token" ||
		got[0].ConsumerHarness != HarnessHermes {
		t.Fatalf("CredentialEnvGrants = %+v", got)
	}
	if !slices.Contains(prepared.Config.ServiceAccess, "muginn-api-proxy") {
		t.Fatalf("ServiceAccess = %v, want muginn-api-proxy", prepared.Config.ServiceAccess)
	}
	if !sessionNotesContain(prepared.Config.SessionNotes, "durable provider keys are not injected") {
		t.Fatalf("SessionNotes missing proxy credential boundary: %v", prepared.Config.SessionNotes)
	}
}

func TestMuginnAPIProxyLaunchUsesStartedProxyEnv(t *testing.T) {
	restoreStart := startMuginnOpenAIProxy
	var requestedModel string
	startMuginnOpenAIProxy = func(model string) (muginnProxyRuntimeInfo, error) {
		requestedModel = model
		return muginnProxyRuntimeInfo{
			Schema:             "muginnctl.proxy.openai.v1",
			Listen:             "http://127.0.0.1:18743",
			Upstream:           "https://api.muginn.example",
			Caller:             "operator-laptop",
			OpenAIBaseURL:      "http://127.0.0.1:18743/v1",
			OpenAIAPIKey:       "local-session-token",
			OpenAIModel:        defaultMuginnProxyModel,
			WorkUnitMode:       "launch",
			WorkUnitKeyPresent: true,
		}, nil
	}
	t.Cleanup(func() { startMuginnOpenAIProxy = restoreStart })

	cfg := sessionConfig{
		HarnessID: HarnessHermes,
		HarnessEnv: map[string]string{
			"HERMES_HOME":       "/Users/agent/.hazmat/hermes/projects/example",
			"OPENAI_API_KEY":    "must-not-survive",
			"ANTHROPIC_API_KEY": "must-not-survive",
		},
	}
	if err := applyMuginnAPIProxyEnvForSession(&cfg, false); err != nil {
		t.Fatalf("applyMuginnAPIProxyEnvForSession: %v", err)
	}
	if requestedModel != defaultMuginnProxyModel {
		t.Fatalf("requested model = %q, want %q", requestedModel, defaultMuginnProxyModel)
	}
	wantEnv := map[string]string{
		"OPENAI_BASE_URL": "http://127.0.0.1:18743/v1",
		"OPENAI_API_KEY":  "local-session-token",
		"OPENAI_MODEL":    defaultMuginnProxyModel,
		"HERMES_HOME":     "/Users/agent/.hazmat/hermes/projects/example",
	}
	for key, want := range wantEnv {
		if got := cfg.HarnessEnv[key]; got != want {
			t.Fatalf("HarnessEnv[%s] = %q, want %q (env=%v)", key, got, want, cfg.HarnessEnv)
		}
	}
	for _, name := range []string{"ANTHROPIC_API_KEY"} {
		if _, ok := cfg.HarnessEnv[name]; ok {
			t.Fatalf("proxy mode preserved provider env %s in HarnessEnv=%v", name, cfg.HarnessEnv)
		}
	}
}

func TestMuginnAPIProxyRejectsUnsupportedSessions(t *testing.T) {
	skipInitCheck(t)
	_, err := resolvePreparedSession("codex", harnessSessionOpts{
		project:      t.TempDir(),
		apiProxyMode: string(apiProxyModeMuginn),
		planOnly:     true,
	}, true)
	if err == nil || !strings.Contains(err.Error(), "supported only for hazmat hermes") {
		t.Fatalf("codex proxy err = %v, want unsupported harness", err)
	}

	_, err = resolvePreparedSession("hermes", harnessSessionOpts{
		project:             t.TempDir(),
		apiProxyMode:        string(apiProxyModeMuginn),
		networkMode:         string(sessionNetworkNone),
		networkModeExplicit: true,
		planOnly:            true,
	}, true)
	if err == nil || !strings.Contains(err.Error(), "remove --network none") {
		t.Fatalf("network none proxy err = %v, want network rejection", err)
	}
}

func TestMuginnProxyStartCommandUsesLocalWorkspaceAndOps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(envHazmatMuginnctl, "")
	t.Setenv(envHazmatMuginnOpsDir, "")
	if err := os.Unsetenv(envHazmatMuginnctl); err != nil {
		t.Fatalf("unset %s: %v", envHazmatMuginnctl, err)
	}
	if err := os.Unsetenv(envHazmatMuginnOpsDir); err != nil {
		t.Fatalf("unset %s: %v", envHazmatMuginnOpsDir, err)
	}

	muginnctl := filepath.Join(home, "workspace", "muginn", "muginnctl")
	if err := os.MkdirAll(filepath.Dir(muginnctl), 0o700); err != nil {
		t.Fatalf("mkdir muginn workspace: %v", err)
	}
	if err := os.WriteFile(muginnctl, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write fake muginnctl: %v", err)
	}
	opsDir := filepath.Join(home, "ops")
	if err := os.MkdirAll(opsDir, 0o700); err != nil {
		t.Fatalf("mkdir ops: %v", err)
	}

	name, args := muginnProxyStartCommand(defaultMuginnProxyModel)
	if name != "direnv" {
		t.Fatalf("command = %q, want direnv", name)
	}
	wantPrefix := []string{"exec", opsDir, muginnctl}
	if len(args) < len(wantPrefix) || !slices.Equal(args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("args prefix = %v, want %v", args, wantPrefix)
	}
	wantSuffix := []string{"proxy", "start", "--daemon", "--model", defaultMuginnProxyModel, "--output", "json"}
	if !slices.Equal(args[len(args)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("args suffix = %v, want %v", args, wantSuffix)
	}
}

func TestValidateMuginnProxyRuntimeInfoRejectsWrongModel(t *testing.T) {
	err := validateMuginnProxyRuntimeInfo(muginnProxyRuntimeInfo{
		OpenAIBaseURL: "http://127.0.0.1:18743/v1",
		OpenAIAPIKey:  "local-session-token",
		OpenAIModel:   "muginn/codex-cli",
	}, defaultMuginnProxyModel)
	if err == nil || !strings.Contains(err.Error(), "want \"muginn/subscription-auto\"") {
		t.Fatalf("validateMuginnProxyRuntimeInfo err = %v, want wrong-model rejection", err)
	}
}

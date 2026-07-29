package hazmat

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHermesOpenAICompatibleProxyPlanUsesInvokingEnvironment(t *testing.T) {
	skipInitCheck(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	setOpenAICompatibleProxyEnv(t, "http://127.0.0.1:18743/v1", "local-session-token")
	for _, spec := range harnessAPIKeyPrompts {
		if err := storeHostAPIKey(spec, "stored-"+spec.EnvVar); err != nil {
			t.Fatalf("storeHostAPIKey(%s): %v", spec.EnvVar, err)
		}
	}

	prepared, err := resolvePreparedSession("hermes", harnessSessionOpts{
		project:              t.TempDir(),
		apiProxyMode:         string(apiProxyModeOpenAICompatible),
		apiProxyModeExplicit: true,
		planOnly:             true,
	}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(hermes --api-proxy=openai-compatible): %v", err)
	}

	env := prepared.Config.HarnessEnv
	if env["OPENAI_BASE_URL"] != "http://127.0.0.1:18743/v1" {
		t.Fatalf("OPENAI_BASE_URL = %q", env["OPENAI_BASE_URL"])
	}
	if env["OPENAI_API_KEY"] != "local-session-token" {
		t.Fatalf("OPENAI_API_KEY = %q, want invoking environment token", env["OPENAI_API_KEY"])
	}
	if _, ok := env["OPENAI_MODEL"]; ok {
		t.Fatalf("OPENAI_MODEL must remain unset: %v", env)
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
		got[0].Source != "invoking environment proxy token" ||
		got[0].ConsumerHarness != HarnessHermes {
		t.Fatalf("CredentialEnvGrants = %+v", got)
	}
	if !slices.Contains(prepared.Config.ServiceAccess, "openai-compatible-api-proxy") {
		t.Fatalf("ServiceAccess = %v, want openai-compatible-api-proxy", prepared.Config.ServiceAccess)
	}
	if !sessionNotesContain(prepared.Config.SessionNotes, "model discovery stay outside Hazmat") ||
		!sessionNotesContain(prepared.Config.SessionNotes, "durable provider keys are not injected") {
		t.Fatalf("SessionNotes missing proxy boundaries: %v", prepared.Config.SessionNotes)
	}
}

func TestOpenAICompatibleProxyEnvReplacesSensitiveValuesAndModel(t *testing.T) {
	setOpenAICompatibleProxyEnv(t, "https://facade.example.test/v1", "facade-access-token")
	cfg := sessionConfig{
		HarnessID: HarnessHermes,
		HarnessEnv: map[string]string{
			"HERMES_HOME":       "/Users/agent/.hazmat/hermes/projects/example",
			"OPENAI_API_KEY":    "must-not-survive",
			"OPENAI_MODEL":      "must-not-survive",
			"ANTHROPIC_API_KEY": "must-not-survive",
		},
	}

	if err := applyOpenAICompatibleAPIProxyEnvForSession(&cfg); err != nil {
		t.Fatalf("applyOpenAICompatibleAPIProxyEnvForSession: %v", err)
	}
	wantEnv := map[string]string{
		"OPENAI_BASE_URL": "https://facade.example.test/v1",
		"OPENAI_API_KEY":  "facade-access-token",
		"HERMES_HOME":     "/Users/agent/.hazmat/hermes/projects/example",
	}
	for key, want := range wantEnv {
		if got := cfg.HarnessEnv[key]; got != want {
			t.Fatalf("HarnessEnv[%s] = %q, want %q (env=%v)", key, got, want, cfg.HarnessEnv)
		}
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "OPENAI_MODEL"} {
		if _, ok := cfg.HarnessEnv[name]; ok {
			t.Fatalf("proxy mode preserved %s in HarnessEnv=%v", name, cfg.HarnessEnv)
		}
	}
}

func TestOpenAICompatibleProxyRejectsUnsupportedOrImplicitSessions(t *testing.T) {
	cases := []struct {
		name     string
		cfg      sessionConfig
		mode     sessionMode
		explicit bool
		want     string
	}{
		{
			name:     "implicit",
			cfg:      sessionConfig{HarnessID: HarnessHermes},
			mode:     sessionModeNative,
			explicit: false,
			want:     "must be selected explicitly",
		},
		{
			name:     "unsupported harness",
			cfg:      sessionConfig{HarnessID: HarnessCodex},
			mode:     sessionModeNative,
			explicit: true,
			want:     "supported only for hazmat hermes",
		},
		{
			name:     "unsupported backend",
			cfg:      sessionConfig{HarnessID: HarnessHermes},
			mode:     sessionModeDockerSandbox,
			explicit: true,
			want:     "supported only for native sessions",
		},
		{
			name: "network none",
			cfg: sessionConfig{
				HarnessID:   HarnessHermes,
				NetworkMode: sessionNetworkNone,
			},
			mode:     sessionModeNative,
			explicit: true,
			want:     "remove --network none",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAPIProxySession(tc.cfg, tc.mode, apiProxyModeOpenAICompatible, tc.explicit)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateAPIProxySession err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestOpenAICompatibleProxyInputRejectsPartialOrUnsafeConfiguration(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		apiKey  string
		want    string
	}{
		{name: "both missing", want: "requires OPENAI_BASE_URL and OPENAI_API_KEY together"},
		{name: "missing key", baseURL: "https://facade.example.test/v1", want: "requires OPENAI_BASE_URL and OPENAI_API_KEY together"},
		{name: "missing URL", apiKey: "token", want: "requires OPENAI_BASE_URL and OPENAI_API_KEY together"},
		{name: "relative URL", baseURL: "/v1", apiKey: "token", want: "must be an absolute HTTPS URL"},
		{name: "remote HTTP", baseURL: "http://facade.example.test/v1", apiKey: "token", want: "must use HTTPS or loopback HTTP"},
		{name: "URL credentials", baseURL: "https://user:pass@facade.example.test/v1", apiKey: "token", want: "without credentials"},
		{name: "URL query", baseURL: "https://facade.example.test/v1?token=bad", apiKey: "token", want: "without credentials, query, or fragment"},
		{name: "URL fragment", baseURL: "https://facade.example.test/v1#fragment", apiKey: "token", want: "without credentials, query, or fragment"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newOpenAICompatibleProxyInput(tc.baseURL, tc.apiKey)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("newOpenAICompatibleProxyInput err = %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), tc.apiKey) && tc.apiKey != "" {
				t.Fatalf("error leaked API key: %v", err)
			}
		})
	}
}

func TestOpenAICompatibleProxyInputAcceptsHTTPSAndLoopbackHTTP(t *testing.T) {
	for _, baseURL := range []string{
		"https://facade.example.test/v1",
		"http://localhost:18743/v1",
		"http://127.0.0.1:18743/v1",
		"http://[::1]:18743/v1",
	} {
		t.Run(baseURL, func(t *testing.T) {
			input, err := newOpenAICompatibleProxyInput(baseURL, "token")
			if err != nil {
				t.Fatalf("newOpenAICompatibleProxyInput: %v", err)
			}
			if input.baseURL != baseURL || input.apiKey != "token" {
				t.Fatalf("input = %+v", input)
			}
		})
	}
}

func TestOpenAICompatibleProxyDoesNotFallBackToStoredOpenAIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENAI_BASE_URL", "https://facade.example.test/v1")
	t.Setenv("OPENAI_API_KEY", "")
	if err := storeHostAPIKey(harnessAPIKeyPromptByEnvVar("OPENAI_API_KEY"), "stored-openai-key"); err != nil {
		t.Fatalf("storeHostAPIKey: %v", err)
	}

	cfg := sessionConfig{HarnessID: HarnessHermes}
	err := applyOpenAICompatibleAPIProxyEnvForSession(&cfg)
	if err == nil || !strings.Contains(err.Error(), "requires OPENAI_BASE_URL and OPENAI_API_KEY together") {
		t.Fatalf("applyOpenAICompatibleAPIProxyEnvForSession err = %v", err)
	}
	if len(cfg.HarnessEnv) != 0 || len(cfg.CredentialEnvGrants) != 0 {
		t.Fatalf("partial input mutated session config: %+v", cfg)
	}
}

func TestOpenAICompatibleProxyDoesNotDiscoverOrExecuteHostHelper(t *testing.T) {
	tempDir := t.TempDir()
	marker := filepath.Join(tempDir, "executed")
	helper := filepath.Join(tempDir, "openai-compatible-proxy")
	script := "#!/bin/sh\n/usr/bin/touch " + marker + "\n"
	if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake proxy helper: %v", err)
	}
	t.Setenv("PATH", tempDir)
	setOpenAICompatibleProxyEnv(t, "http://127.0.0.1:18743/v1", "token")

	cfg := sessionConfig{HarnessID: HarnessHermes}
	if err := applyOpenAICompatibleAPIProxyEnvForSession(&cfg); err != nil {
		t.Fatalf("applyOpenAICompatibleAPIProxyEnvForSession: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("host helper was executed: stat err = %v", err)
	}
}

func TestParseAPIProxyModeRejectsRemovedAndUnknownModes(t *testing.T) {
	for _, mode := range []string{"internal", "openai", "auto"} {
		if _, err := parseAPIProxyMode(mode); err == nil {
			t.Fatalf("parseAPIProxyMode(%q) succeeded", mode)
		}
	}
}

func setOpenAICompatibleProxyEnv(t *testing.T, baseURL, apiKey string) {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", baseURL)
	t.Setenv("OPENAI_API_KEY", apiKey)
}

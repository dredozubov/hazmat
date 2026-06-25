package hazmat

import (
	"encoding/json"
	"flag"
	"fmt"
	"hazmat/credentials"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hazmat/integrations"
)

var updateGoldenBaselines = flag.Bool("update-golden", false, "update golden baseline files")

func TestGoldenExplainJSONBaselines(t *testing.T) {
	restorePlatform := stubExplainPlatformReport(t, nil)
	defer restorePlatform()

	nativeCfg := goldenSessionConfig()
	dockerCfg := goldenSessionConfig()
	dockerCfg.RoutingReason = "using Docker Sandbox because --docker=sandbox was requested"
	dockerCfg.SessionNotes = []string{"Docker Sandbox uses a private daemon; integration env passthrough is not delivered in this backend yet."}

	cases := map[string]any{
		"explain/native.json": buildExplainJSON("shell", nativeCfg, sessionModeNative, false),
		"explain/docker.json": buildExplainJSON("shell", dockerCfg, sessionModeDockerSandbox, false),
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			assertGoldenJSON(t, name, value)
		})
	}
}

func TestGoldenIntegrationMergeBaselines(t *testing.T) {
	good := integrations.Spec{
		Meta: integrations.Meta{Name: "golden-go", Version: 1},
		Session: integrations.Session{
			ReadDirs:       []string{"/opt/homebrew/Cellar/go/1.2.3/libexec"},
			EnvPassthrough: []string{"GOROOT", "GOPROXY"},
		},
		Backup:   integrations.Backup{Excludes: []string{".gocache/"}},
		Warnings: []string{"Go integration warning"},
	}
	merged, err := integrations.MergeResolved([]integrations.Resolved{
		{
			Spec: good,
			ResolvedEnv: map[string]string{
				"GOROOT": "/opt/homebrew/Cellar/go/1.2.3/libexec",
			},
			AdditionalWarnings: []string{"runtime warning"},
		},
	}, integrations.MergeOptions{
		Platform: "darwin",
		ValidateReadDirs: func(_ integrations.Spec, _ string) ([]string, error) {
			return []string{"/opt/homebrew/Cellar/go/1.2.3/libexec"}, nil
		},
		Getenv: func(key string) string {
			if key == "GOPROXY" {
				return "https://proxy.golang.org,direct"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("MergeResolved good fixture: %v", err)
	}

	credentialEnvErr := goldenIntegrationMergeError(t, integrations.Spec{
		Meta: integrations.Meta{Name: "bad-env", Version: 1},
	}, integrations.Resolved{
		ResolvedEnv: map[string]string{"AWS_ACCESS_KEY_ID": "not-secret"},
	})
	readDirErr := goldenIntegrationMergeError(t, integrations.Spec{
		Meta: integrations.Meta{Name: "bad-read-dir", Version: 1},
		Session: integrations.Session{
			ReadDirs: []string{"/Users/agent/.ssh"},
		},
	}, integrations.Resolved{})

	assertGoldenJSON(t, "integrations/merge-output.json", merged)
	assertGolden(t, "integrations/reject-credential-env.txt", credentialEnvErr)
	assertGolden(t, "integrations/reject-read-dir.txt", readDirErr)
}

func TestGoldenLaunchMetadataBaseline(t *testing.T) {
	cfg := sessionConfig{
		Target:      "codex",
		ProjectDir:  "/Users/dr/workspace/project",
		NetworkMode: sessionNetworkNone,
		HarnessID:   HarnessCodex,
	}
	raw, err := marshalSessionLaunchMetadataJSON(cfg, sessionModeNative)
	if err != nil {
		t.Fatalf("marshalSessionLaunchMetadataJSON: %v", err)
	}
	assertGolden(t, "metadata/native-network-none.json", prettyJSON(t, []byte(raw))+"\n")
}

func goldenSessionConfig() sessionConfig {
	return sessionConfig{
		Target:                "shell",
		ProjectDir:            "/Users/dr/workspace/project",
		ReadDirs:              []string{"/Users/dr/workspace/reference", "/opt/homebrew/Cellar/go/1.2.3/libexec"},
		WriteDirs:             []string{"/Users/dr/workspace/project/.cache"},
		UserReadDirs:          []string{"/Users/dr/workspace/reference"},
		AutoReadDirs:          []string{"/opt/homebrew/Cellar/go/1.2.3/libexec"},
		SuggestedIntegrations: []string{"node"},
		ActiveIntegrations:    []string{"go"},
		IntegrationSources:    []string{"go (go.mod)"},
		IntegrationDetails:    []string{"go: resolved GOROOT through Homebrew"},
		IntegrationWarnings:   []string{"Go integration warning"},
		IntegrationEnv: map[string]string{
			"GOROOT":  "/opt/homebrew/Cellar/go/1.2.3/libexec",
			"GOPROXY": "https://proxy.golang.org,direct",
		},
		IntegrationRegistryKeys: []string{"GOPROXY"},
		CredentialEnvGrants: []sessionCredentialEnvGrant{
			{
				EnvVar:          "OPENAI_API_KEY",
				CredentialID:    credentials.ProviderOpenAIAPIKey,
				Source:          "host secret store",
				ConsumerHarness: HarnessCodex,
			},
		},
		PlannedHostMutations: []sessionMutation{
			{
				Summary:     "project ACL repair",
				Detail:      "may add bounded collaborative ACLs on /Users/dr/workspace/project",
				Persistence: "persistent in project",
				ProofScope:  sessionMutationProofScopeTLAModel,
			},
		},
		IntegrationExcludes: []string{".gocache/"},
		ServiceAccess:       []string{"docker"},
		GitSSH:              &sessionGitSSHConfig{DisplayName: "id_ed25519"},
		NetworkMode:         sessionNetworkDefault,
		RoutingReason:       "staying in native containment because docker: none is configured",
		SessionNotes:        []string{"Docker files detected but disabled by config"},
		RepoSetup: &repoSetupState{
			AppliedSafe: []repoSetupEffect{
				{
					ID:      "ro:/opt/homebrew/Cellar/go/1.2.3/libexec",
					Class:   repoSetupEffectClassSafe,
					Kind:    repoSetupEffectReadOnly,
					Value:   "/opt/homebrew/Cellar/go/1.2.3/libexec",
					Sources: []string{"Suggested by project files (go)"},
				},
			},
			PendingExplicit: []repoSetupEffect{
				{
					ID:      "rw:/Users/dr/workspace/project/.cache",
					Class:   repoSetupEffectClassExplicit,
					Kind:    repoSetupEffectWrite,
					Value:   "/Users/dr/workspace/project/.cache",
					Sources: []string{"Learned from previous session denial"},
				},
			},
			record: repoProfileRecord{
				Remembered: repoSetupStoredEffects{ReadOnly: []string{"/opt/homebrew/Cellar/go/1.2.3/libexec"}},
			},
		},
	}
}

func goldenIntegrationMergeError(t *testing.T, spec integrations.Spec, resolved integrations.Resolved) string {
	t.Helper()
	resolved.Spec = spec
	_, err := integrations.MergeResolved([]integrations.Resolved{resolved}, integrations.MergeOptions{
		Platform: "darwin",
		ValidateReadDirs: func(spec integrations.Spec, _ string) ([]string, error) {
			if len(spec.Session.ReadDirs) > 0 {
				return nil, fmt.Errorf("integration %q: read_dir %q is a credential deny path", spec.Meta.Name, spec.Session.ReadDirs[0])
			}
			return nil, nil
		},
	})
	if err == nil {
		t.Fatalf("MergeResolved(%s) succeeded, want error", spec.Meta.Name)
	}
	return err.Error() + "\n"
}

func assertGoldenJSON(t *testing.T, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	assertGolden(t, name, prettyJSON(t, data)+"\n")
}

func prettyJSON(t *testing.T, data []byte) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("unmarshal JSON: %v\n%s", err, string(data))
	}
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal indent JSON: %v", err)
	}
	return string(out)
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", filepath.FromSlash(name))
	if *updateGoldenBaselines {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nRun `go test -update-golden` from hazmat/ to refresh baselines.", path, err)
	}
	if got != string(want) {
		t.Fatalf("%s changed; run `go test ./... -update-golden` only after reviewing the diff.\n--- want\n%s\n--- got\n%s", name, string(want), got)
	}
}

func TestGoldenBaselinesDoNotUseHostTempDirs(t *testing.T) {
	for _, dir := range []string{"/private/tmp", "/private/var/folders"} {
		if strings.Contains(generateSBPL(sessionConfig{ProjectDir: "/Users/dr/workspace/project"}), `(allow file-read* file-write* (subpath "`+dir+`"))`) {
			t.Fatalf("unexpected broad host temp grant in golden fixture support for %s", dir)
		}
	}
}

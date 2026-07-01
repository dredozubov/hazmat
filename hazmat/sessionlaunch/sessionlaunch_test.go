package sessionlaunch

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"hazmat/sessionbackend"
	"hazmat/sessioncontract"
	"hazmat/sessionmeta"
	"hazmat/sessionplanner"
)

func TestLaunchRequestNormalizedDefensiveCopies(t *testing.T) {
	request := LaunchRequest{
		Target:       "exec",
		ProjectDir:   "/workspace/project",
		ReadOnly:     []string{"/opt/sdk"},
		ReadWrite:    []string{"/tmp/cache"},
		Integrations: []string{"go"},
	}

	normalized := request.Normalized()
	request.ReadOnly[0] = "/mutated-ro"
	request.ReadWrite[0] = "/mutated-rw"
	request.Integrations[0] = "mutated"

	if normalized.NetworkMode != sessionmeta.NetworkDefault {
		t.Fatalf("NetworkMode = %q, want default", normalized.NetworkMode)
	}
	if normalized.Options.DockerMode != "none" {
		t.Fatalf("DockerMode = %q, want none", normalized.Options.DockerMode)
	}
	if !slices.Equal(normalized.ReadOnlyDirs(), []string{"/opt/sdk"}) ||
		!slices.Equal(normalized.ReadWriteDirs(), []string{"/tmp/cache"}) ||
		!slices.Equal(normalized.IntegrationNames(), []string{"go"}) {
		t.Fatalf("normalized request shares mutable slices: %+v", normalized)
	}
}

func TestPreparedSessionDefensiveCopiesCleanupAndPlanDisclosure(t *testing.T) {
	const secret = "sk-secret"
	cleanupCalls := 0
	prepared := NewPreparedSession(PreparedSessionInput{
		Request: LaunchRequest{
			Target:      "codex",
			ProjectDir:  "/workspace/project",
			ReadOnly:    []string{"/opt/sdk"},
			ReadWrite:   []string{"/tmp/cache"},
			NetworkMode: sessionmeta.NetworkNone,
		},
		Plan: sessionplanner.Plan{
			Contract: sessioncontract.Plan{
				Target:       "codex",
				ReadOnlyDirs: []string{"/opt/sdk"},
				CredentialEnvGrants: []sessioncontract.CredentialEnvGrant{{
					EnvVar:       "OPENAI_API_KEY",
					CredentialID: "provider.openai-api-key",
					Source:       "host secret store",
					Redacted:     true,
				}},
				SessionHome: &sessioncontract.SessionHome{
					Phases:             []string{"assemble"},
					DurableBridgeRoots: []string{"/Users/agent/.cache"},
				},
			},
			Backend: sessionbackend.Plan{
				Target:       "codex",
				Backend:      sessionbackend.KindDarwinNative,
				ReadOnlyDirs: []string{"/opt/sdk"},
			},
			HarnessRequirements: []sessionplanner.HarnessRequirement{{
				ID:    "codex",
				Notes: []string{"needs auth"},
			}},
		},
		BackendPlan: sessionbackend.Plan{
			Target:       "codex",
			Backend:      sessionbackend.KindDarwinNative,
			ReadOnlyDirs: []string{"/opt/sdk"},
		},
		Mode:       sessionmeta.ModeNative,
		RuntimeEnv: []string{"OPENAI_API_KEY=" + secret},
		RuntimeDir: "/tmp/hazmat-runtime",
		Cleanup: func() {
			cleanupCalls++
		},
	})

	request := prepared.Request()
	request.ReadOnly[0] = "/mutated-ro"
	if got := prepared.Request().ReadOnlyDirs(); !slices.Equal(got, []string{"/opt/sdk"}) {
		t.Fatalf("Request shares mutable slices: %v", got)
	}

	plan := prepared.Plan()
	plan.Contract.ReadOnlyDirs[0] = "/mutated-ro"
	plan.Contract.SessionHome.Phases[0] = "mutated"
	plan.HarnessRequirements[0].Notes[0] = "mutated"
	if got := prepared.Plan(); got.Contract.ReadOnlyDirs[0] != "/opt/sdk" ||
		got.Contract.SessionHome.Phases[0] != "assemble" ||
		got.HarnessRequirements[0].Notes[0] != "needs auth" {
		t.Fatalf("Plan shares mutable state: %+v", got)
	}

	backend := prepared.BackendPlan()
	backend.ReadOnlyDirs[0] = "/mutated-ro"
	if got := prepared.BackendPlan().ReadOnlyDirs; !slices.Equal(got, []string{"/opt/sdk"}) {
		t.Fatalf("BackendPlan shares mutable slices: %v", got)
	}

	env := prepared.RuntimeEnv()
	env[0] = "OPENAI_API_KEY=mutated"
	if got := prepared.RuntimeEnv(); !slices.Equal(got, []string{"OPENAI_API_KEY=" + secret}) {
		t.Fatalf("RuntimeEnv shares mutable slices: %v", got)
	}

	planJSON, err := json.Marshal(prepared.Plan())
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if strings.Contains(string(planJSON), secret) {
		t.Fatalf("redaction-safe plan leaked runtime secret: %s", planJSON)
	}
	if !strings.Contains(string(planJSON), `"redacted":true`) {
		t.Fatalf("plan did not preserve redaction marker: %s", planJSON)
	}

	prepared.Cleanup()
	prepared.Cleanup()
	if cleanupCalls != 1 {
		t.Fatalf("Cleanup called %d times, want 1", cleanupCalls)
	}
}

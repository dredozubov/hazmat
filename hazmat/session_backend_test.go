package main

import (
	"slices"
	"testing"

	"hazmat/sessionbackend"
)

func TestBuildSessionBackendPlanUsesPreparedConfig(t *testing.T) {
	cfg := sessionConfig{
		Target:             "codex",
		ProjectDir:         "/workspace/project",
		ReadDirs:           []string{"/opt/sdk"},
		WriteDirs:          []string{"/tmp/cache"},
		NetworkMode:        sessionNetworkNone,
		ActiveIntegrations: []string{"go"},
		IntegrationEnv: map[string]string{
			"GOROOT": "/opt/go",
			"GOPATH": "/tmp/gopath",
		},
	}

	plan := buildSessionBackendPlanForGOOS(cfg, sessionModeNative, "darwin")
	if plan.Backend != sessionbackend.KindDarwinNative {
		t.Fatalf("Backend = %q", plan.Backend)
	}
	if plan.Target != "codex" || plan.ProjectDir != "/workspace/project" {
		t.Fatalf("plan identity = %+v", plan)
	}
	if plan.NetworkMode != sessionNetworkNone {
		t.Fatalf("NetworkMode = %q", plan.NetworkMode)
	}
	if !slices.Equal(plan.ReadOnlyDirs, []string{"/opt/sdk"}) ||
		!slices.Equal(plan.ReadWriteDirs, []string{"/tmp/cache"}) ||
		!slices.Equal(plan.Integrations, []string{"go"}) ||
		!slices.Equal(plan.IntegrationEnvKeys, []string{"GOPATH", "GOROOT"}) {
		t.Fatalf("plan collections = %+v", plan)
	}
}

func TestBuildSessionBackendPlanReportsDockerEnvGap(t *testing.T) {
	cfg := sessionConfig{
		Target:     "codex",
		ProjectDir: "/workspace/project",
		IntegrationEnv: map[string]string{
			"GOPROXY": "https://proxy.example",
		},
	}

	plan := buildSessionBackendPlanForGOOS(cfg, sessionModeDockerSandbox, "darwin")
	if plan.Backend != sessionbackend.KindDockerSandbox {
		t.Fatalf("Backend = %q", plan.Backend)
	}
	if len(plan.CapabilityGaps) != 1 || plan.CapabilityGaps[0].Feature != sessionbackend.GapIntegrationEnv {
		t.Fatalf("CapabilityGaps = %v", plan.CapabilityGaps)
	}
}

func TestPrepareLaunchSessionCarriesBackendPlan(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	projectDir := t.TempDir()
	prepared, err := prepareLaunchSession("codex", harnessSessionOpts{
		project:     projectDir,
		networkMode: "none",
	}, true)
	if err != nil {
		t.Fatalf("prepareLaunchSession: %v", err)
	}
	if prepared.BackendPlan.Target != "codex" {
		t.Fatalf("BackendPlan.Target = %q", prepared.BackendPlan.Target)
	}
	if prepared.BackendPlan.ProjectDir != prepared.Config.ProjectDir {
		t.Fatalf("BackendPlan.ProjectDir = %q, want %q", prepared.BackendPlan.ProjectDir, prepared.Config.ProjectDir)
	}
	if prepared.BackendPlan.NetworkMode != sessionNetworkNone {
		t.Fatalf("BackendPlan.NetworkMode = %q", prepared.BackendPlan.NetworkMode)
	}
}

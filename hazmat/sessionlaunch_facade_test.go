package hazmat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"hazmat/planescapeprovider"
	"hazmat/sessionbackend"
	launch "hazmat/sessionlaunch"
	"hazmat/sessionmeta"
)

func TestSessionLauncherPreparePlanOnly(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	projectDir := t.TempDir()
	readDir := filepath.Join(projectDir, "readonly")
	writeDir := filepath.Join(projectDir, "cache")
	for _, dir := range []string{readDir, writeDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	prepared, err := NewSessionLauncher().Prepare(context.Background(), launch.LaunchRequest{
		Target:      "exec",
		ProjectDir:  projectDir,
		ReadOnly:    []string{readDir},
		ReadWrite:   []string{writeDir},
		NetworkMode: sessionmeta.NetworkNone,
		Options: launch.LaunchOptions{
			SupportsSandbox:              true,
			PlanOnly:                     true,
			SkipAutoIntegrations:         true,
			SkipIntegrationHints:         true,
			SkipRepoSetupDiscovery:       true,
			SkipGitSafeDirectoryPlanning: true,
			SkipAmbientAccessGrants:      true,
			SkipGitHTTPSRuntime:          true,
			SkipGoModCacheEnv:            true,
			SkipProjectHooks:             true,
			SkipDockerDetection:          true,
		},
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	wantBackend := sessionbackend.BackendFor(sessionmeta.ModeNative, runtime.GOOS)
	if prepared.Mode() != sessionmeta.ModeNative || prepared.Backend() != wantBackend {
		t.Fatalf("prepared backend = mode %q backend %q, want native/%s", prepared.Mode(), prepared.Backend(), wantBackend)
	}
	request := prepared.Request()
	if request.Target != "exec" || request.ProjectDir == "" || request.NetworkMode != sessionmeta.NetworkNone {
		t.Fatalf("Request = %+v", request)
	}
	if len(request.ReadOnlyDirs()) != 1 || len(request.ReadWriteDirs()) != 1 {
		t.Fatalf("request dirs = ro:%v rw:%v", request.ReadOnlyDirs(), request.ReadWriteDirs())
	}

	plan := prepared.Plan()
	if plan.Backend.Target != "exec" || plan.Backend.ProjectDir != request.ProjectDir {
		t.Fatalf("plan backend identity = %+v", plan.Backend)
	}
	if plan.Contract.NetworkPolicy.Requested != string(sessionmeta.NetworkNone) || !plan.Contract.NetworkPolicy.DenyAllEgress {
		t.Fatalf("network policy = %+v", plan.Contract.NetworkPolicy)
	}
	if !slices.Equal(plan.Backend.ReadOnlyDirs, request.ReadOnlyDirs()) ||
		!slices.Equal(plan.Backend.ReadWriteDirs, request.ReadWriteDirs()) {
		t.Fatalf("backend dirs = ro:%v rw:%v", plan.Backend.ReadOnlyDirs, plan.Backend.ReadWriteDirs)
	}

	plan.Backend.ReadOnlyDirs[0] = "/mutated"
	if got := prepared.Plan().Backend.ReadOnlyDirs; !slices.Equal(got, request.ReadOnlyDirs()) {
		t.Fatalf("prepared plan shares mutable slices: %v", got)
	}
}

func TestSessionLauncherPrepareHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewSessionLauncher().Prepare(ctx, launch.LaunchRequest{Target: "exec"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Prepare error = %v, want context canceled", err)
	}
}

func TestSessionLauncherPrepareRejectsPlanescapeBeforeProductEffects(t *testing.T) {
	isolateConfig(t)
	if err := runConfigSet("session.execution_provider", "planescape"); err != nil {
		t.Fatal(err)
	}
	savedDependencies := planescapeProductDependenciesForSession
	t.Cleanup(func() {
		planescapeProductDependenciesForSession = savedDependencies
	})
	dependencyCalls := 0
	planescapeProductDependenciesForSession = func() (
		planescapeProductDependencies,
		error,
	) {
		dependencyCalls++
		return planescapeProductDependencies{}, nil
	}

	_, err := NewSessionLauncher().Prepare(
		context.Background(),
		launch.LaunchRequest{Target: "exec"},
	)
	requirePlanescapeProductErrorClass(t, err, planescapeprovider.ErrorUnsupported)
	if dependencyCalls != 0 {
		t.Fatalf("Planescape dependency construction calls = %d, want 0", dependencyCalls)
	}
}

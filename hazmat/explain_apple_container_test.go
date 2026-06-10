package hazmat

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	applecontainerspec "hazmat/containment/applecontainer"
)

func runExplainCmdForTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newExplainCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestExplainAppleContainerJSONEmitsPlanOnlySpec(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	dir := t.TempDir()
	canonicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", dir, err)
	}

	stdout, _, err := runExplainCmdForTest(t,
		"--backend=apple-container",
		"--image", "ghcr.io/example/hazmat-codex:sha256-abc",
		"--for", "codex",
		"--json", "-C", dir)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var spec applecontainerspec.LaunchSpec
	if err := json.Unmarshal([]byte(stdout), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v\nstdout=%s", err, stdout)
	}
	if spec.Backend != applecontainerspec.BackendAppleContainer || spec.Phase != applecontainerspec.PhasePlanOnly {
		t.Fatalf("spec identity = %q/%q", spec.Backend, spec.Phase)
	}
	if spec.Workdir != canonicalDir {
		t.Fatalf("Workdir = %q, want %q", spec.Workdir, canonicalDir)
	}
	if len(spec.Mounts) == 0 || spec.Mounts[0].Target != canonicalDir || spec.Mounts[0].Access != "read-write" {
		t.Fatalf("Mounts = %+v, want project rw first", spec.Mounts)
	}
	if spec.Environment.InheritHostEnv {
		t.Fatalf("preview inherits host env: %+v", spec.Environment)
	}
	hasGateGap := false
	for _, gap := range spec.CapabilityGaps {
		if gap.Code == "apple-container-runtime-gated" {
			hasGateGap = true
		}
	}
	if !hasGateGap {
		t.Fatalf("CapabilityGaps = %+v, want apple-container-runtime-gated", spec.CapabilityGaps)
	}
	if spec.HostIdentity != applecontainerspec.HostIdentityInvokingUser {
		t.Fatalf("HostIdentity = %q, want invoking-user", spec.HostIdentity)
	}
}

func TestExplainAppleContainerTextRendersCapabilityGaps(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	stdout, _, err := runExplainCmdForTest(t,
		"--backend=apple-container",
		"--image", "ghcr.io/example/hazmat-codex:sha256-abc",
		"--for", "codex",
		"-C", t.TempDir())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"Backend:              apple-container",
		"plan-only preview",
		"Capability gaps (why this plan cannot launch):",
		"apple-container-runtime-gated",
		"Host identity:        invoking user (host account isolation NOT provided)",
		"Guest identity:       uid 502 gid 20 (non-root)",
		"never prune",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestExplainAppleContainerRequiresImage(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	_, _, err := runExplainCmdForTest(t, "--backend=apple-container", "-C", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "requires --image") {
		t.Fatalf("err = %v, want image requirement", err)
	}
}

func TestExplainRejectsUnknownBackend(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	_, _, err := runExplainCmdForTest(t, "--backend=firecracker", "-C", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unknown preview backend") {
		t.Fatalf("err = %v, want unknown backend rejection", err)
	}
}

func TestExplainImageRequiresAppleContainerBackend(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	_, _, err := runExplainCmdForTest(t, "--image", "ubuntu:24.04", "-C", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "--image requires --backend=apple-container") {
		t.Fatalf("err = %v, want image/backend coupling error", err)
	}
}

func TestExplainAppleContainerNetworkNoneFailsClosed(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	_, _, err := runExplainCmdForTest(t,
		"--backend=apple-container",
		"--image", "ghcr.io/example/hazmat-codex:sha256-abc",
		"--network", "none",
		"-C", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not implemented for the Apple Container backend") {
		t.Fatalf("err = %v, want fail-closed network none rejection", err)
	}
}

func TestExplainAppleContainerRejectsDockerRouting(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	_, _, err := runExplainCmdForTest(t,
		"--backend=apple-container",
		"--image", "ghcr.io/example/hazmat-codex:sha256-abc",
		"--docker=sandbox",
		"-C", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with --docker") {
		t.Fatalf("err = %v, want docker routing conflict", err)
	}
}

// The experimental Apple Container backend surface is exec-only: harness
// commands like claude must not expose --backend/--image.
func TestHarnessCommandsDoNotAcceptBackendFlag(t *testing.T) {
	cmd := newClaudeCmd()
	if cmd.Flags().Lookup("backend") != nil {
		t.Fatal("claude command exposes --backend; the experimental backend is exec-only")
	}
	if cmd.Flags().Lookup("image") != nil {
		t.Fatal("claude command exposes --image; the experimental backend is exec-only")
	}
}

// Without the experimental gate env var, exec refuses to launch and explains
// the boundary bluntly.
func TestExecAppleContainerRequiresExperimentalGate(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)
	t.Setenv("HAZMAT_EXPERIMENTAL_APPLE_CONTAINER", "")

	cmd := newExecCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--backend=apple-container", "--image", "alpine:latest", "-C", t.TempDir(), "--", "true"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("exec launched without the experimental gate")
	}
	for _, want := range []string{
		"HAZMAT_EXPERIMENTAL_APPLE_CONTAINER",
		"invoking macOS user",
		"Host account isolation is",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("gate error missing %q: %v", want, err)
		}
	}
}

// With the gate set but no image, exec still refuses.
func TestExecAppleContainerRequiresImage(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)
	t.Setenv("HAZMAT_EXPERIMENTAL_APPLE_CONTAINER", "1")

	cmd := newExecCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--backend=apple-container", "-C", t.TempDir(), "--", "true"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "requires --image") {
		t.Fatalf("err = %v, want image requirement", err)
	}
}

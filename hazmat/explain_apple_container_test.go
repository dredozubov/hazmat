package hazmat

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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
	hasRuntimeGap := false
	for _, gap := range spec.CapabilityGaps {
		if gap.Code == "apple-container-runtime-missing" {
			hasRuntimeGap = true
		}
	}
	if !hasRuntimeGap {
		t.Fatalf("CapabilityGaps = %+v, want apple-container-runtime-missing", spec.CapabilityGaps)
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
		"apple-container-runtime-missing",
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

// The Apple Container backend must remain non-executable: only the plan-only
// explain command may accept --backend/--image.
func TestSessionCommandsDoNotAcceptBackendFlag(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"claude": newClaudeCmd(),
		"exec":   newExecCmd(),
	} {
		if cmd.Flags().Lookup("backend") != nil {
			t.Fatalf("%s command exposes --backend; Apple Container must stay plan-only", name)
		}
		if cmd.Flags().Lookup("image") != nil {
			t.Fatalf("%s command exposes --image; Apple Container must stay plan-only", name)
		}
	}
}

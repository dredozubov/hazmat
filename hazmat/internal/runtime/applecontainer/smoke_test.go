package applecontainer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"hazmat/containment"
	applecontainerspec "hazmat/containment/applecontainer"
	"hazmat/sessionmeta"
)

// Gated smoke coverage per the backend design's testing plan. Requires a
// macOS 26 Apple silicon host with apple/container >= 1.0.0 running:
//
//	HAZMAT_APPLE_CONTAINER_SMOKE=1 go test ./internal/runtime/applecontainer -run TestSmoke -v
func smokeGate(t *testing.T) applecontainerspec.HostReport {
	t.Helper()
	if os.Getenv("HAZMAT_APPLE_CONTAINER_SMOKE") != "1" {
		t.Skip("set HAZMAT_APPLE_CONTAINER_SMOKE=1 to run Apple Container smoke tests")
	}
	report := ProbeHost(HostRunner(), goruntime.GOOS, goruntime.GOARCH)
	if report.CLIPath == "" || !report.CLIVersionSupported || !report.APIServerHealthy {
		t.Fatalf("smoke gate set but host admission failed: %+v", report)
	}
	return report
}

func smokeSpec(t *testing.T, projectDir string, host applecontainerspec.HostReport, command ...string) applecontainerspec.LaunchSpec {
	t.Helper()
	floor, err := containment.NewCredentialFloor("/Users/agent", []string{"/.ssh", "/.aws"})
	if err != nil {
		t.Fatalf("NewCredentialFloor: %v", err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project:   containment.PathGrant{Path: projectDir, Access: containment.PathReadWrite},
		AgentHome: containment.AgentHomePolicy{Path: "/Users/agent"},
		Temp:      containment.TempPolicy{Path: filepath.Join(projectDir, ".hazmat-smoke-tmp")},
		Network:   containment.NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:   containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatalf("NewContract: %v", err)
	}
	spec, err := applecontainerspec.Compile(contract, applecontainerspec.CompileOptions{
		Harness:           "exec",
		Image:             smokeImage(),
		SessionID:         fmt.Sprintf("smoke-%s-%d", t.Name(), os.Getpid()),
		Command:           command,
		GuestUID:          os.Getuid(),
		GuestGID:          os.Getgid(),
		ExecutableRuntime: true,
		Host:              host,
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return spec
}

func smokeImage() string {
	if image := os.Getenv("HAZMAT_APPLE_CONTAINER_SMOKE_IMAGE"); image != "" {
		return image
	}
	return "alpine:latest"
}

func runSmoke(t *testing.T, spec applecontainerspec.LaunchSpec) (RunResult, string) {
	t.Helper()
	var out bytes.Buffer
	result, err := Run(spec, RunOptions{Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}
	if len(result.CleanupFailures) > 0 {
		t.Fatalf("cleanup failures: %v", result.CleanupFailures)
	}
	return result, out.String()
}

func TestSmokeProjectMountAndIdentity(t *testing.T) {
	host := smokeGate(t)
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "host-file.txt"), []byte("host-written\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := smokeSpec(t, projectDir, host, "sh", "-c",
		"id -u; cat "+projectDir+"/host-file.txt; echo guest-written > "+projectDir+"/guest-file.txt")
	result, out := runSmoke(t, spec)
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d\n%s", result.ExitCode, out)
	}
	if !strings.Contains(out, fmt.Sprintf("%d", os.Getuid())) {
		t.Fatalf("guest uid output missing invoking uid: %s", out)
	}
	if strings.Contains(out, "uid=0") || strings.HasPrefix(strings.TrimSpace(out), "0\n") {
		t.Fatalf("guest ran as root: %s", out)
	}
	if !strings.Contains(out, "host-written") {
		t.Fatalf("guest could not read project file: %s", out)
	}
	data, err := os.ReadFile(filepath.Join(projectDir, "guest-file.txt"))
	if err != nil || !strings.Contains(string(data), "guest-written") {
		t.Fatalf("guest write did not land on host: %v %q", err, data)
	}

	// The session container must be gone after Run.
	if out, err := exec.Command(ApprovedCLIPath, "ls", "--all").CombinedOutput(); err == nil {
		if strings.Contains(string(out), spec.ContainerName) {
			t.Fatalf("session container %s survived cleanup:\n%s", spec.ContainerName, out)
		}
	}
}

func TestSmokeCredentialZonesInvisible(t *testing.T) {
	host := smokeGate(t)
	projectDir := t.TempDir()
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME unset")
	}

	// The invoking user's home and the agent home are not mounted, so they
	// must not exist inside the guest at all.
	spec := smokeSpec(t, projectDir, host, "sh", "-c",
		"[ ! -e "+home+" ] && [ ! -e /Users/agent ] && echo deny-zones-invisible")
	result, out := runSmoke(t, spec)
	if result.ExitCode != 0 || !strings.Contains(out, "deny-zones-invisible") {
		t.Fatalf("credential zones visible in guest (exit %d):\n%s", result.ExitCode, out)
	}
}

func TestSmokeFailingCommandStillCleansUp(t *testing.T) {
	host := smokeGate(t)
	spec := smokeSpec(t, t.TempDir(), host, "sh", "-c", "exit 7")
	var out bytes.Buffer
	result, err := Run(spec, RunOptions{Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}
	if len(result.CleanupFailures) > 0 {
		t.Fatalf("cleanup failures: %v", result.CleanupFailures)
	}
}

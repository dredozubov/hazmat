//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hazmat/containment"
	linuxspec "hazmat/containment/linux"
	platformlinux "hazmat/platform/linux"
	"hazmat/sessionmeta"
)

const (
	agentLiveSmokeEnv      = "HAZMAT_LINUX_AGENT_USER_VM_SMOKE"
	agentLiveSmokeRootEnv  = "HAZMAT_LINUX_AGENT_USER_SMOKE_ROOT"
	agentLiveSmokeChildEnv = "HAZMAT_LINUX_AGENT_USER_VM_SMOKE_CHILD"
	agentLiveSmokeHelper   = "/usr/local/libexec/hazmat-launch"
)

func TestLinuxAgentUserPreparedHostLiveSmokeMatrix(t *testing.T) {
	if os.Getenv(agentLiveSmokeEnv) != "1" {
		t.Skipf("set %s=1 inside a disposable prepared Linux VM to run agent-user live smokes", agentLiveSmokeEnv)
	}

	scenarios := []string{
		"A4-helper-admission",
		"A5-run-metadata",
		"A6-filesystem-policy",
		"A7-network-policy",
		"A8-cancellation-cleanup",
		"A11-unsupported-host",
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			output, err := runAgentLiveSmokeChild(t, scenario)
			t.Log(strings.TrimSpace(output))
			if err != nil {
				t.Fatalf("%s child failed: %v", scenario, err)
			}
		})
	}
	t.Log("Remaining gaps: A1-A3, A9, and A10 still require setup/doctor/rollback/destructive rollback transcripts")
	t.Log("Support claim: setup-required")
}

func TestLinuxAgentUserPreparedHostLiveSmokeChild(t *testing.T) {
	scenario := os.Getenv(agentLiveSmokeChildEnv)
	if scenario == "" {
		t.Skip("agent-user live smoke child only")
	}
	switch scenario {
	case "A4-helper-admission":
		runAgentHelperAdmissionLiveSmoke(t)
	case "A5-run-metadata":
		runAgentMetadataLiveSmoke(t)
	case "A6-filesystem-policy":
		runAgentFilesystemLiveSmoke(t)
	case "A7-network-policy":
		runAgentNetworkLiveSmoke(t)
	case "A8-cancellation-cleanup":
		runAgentCancellationLiveSmoke(t)
	case "A11-unsupported-host":
		runAgentUnsupportedHostLiveSmoke(t)
	default:
		t.Fatalf("unknown agent-user live smoke child scenario %q", scenario)
	}
}

func runAgentLiveSmokeChild(t *testing.T, scenario string) (string, error) {
	t.Helper()
	cmd := exec.Command(liveSmokeTestBinary(t), "-test.run=^TestLinuxAgentUserPreparedHostLiveSmokeChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		agentLiveSmokeEnv+"=1",
		agentLiveSmokeChildEnv+"="+scenario,
		EnvExperimentalAgentUser+"=1",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

func runAgentHelperAdmissionLiveSmoke(t *testing.T) {
	scratch := newAgentLiveSmokeScratch(t)
	helper := newLiveAgentRootHelper(t)
	report := preparedAgentUserReport()
	stdout, stderr, result, err := runLiveAgentUser(t, scratch, sessionmeta.NetworkDefault, payloadCommand(t, "project-write", scratch.projectDir), helper, report, nil)
	if logGapAndReturn(t, "A4 helper admission", err) {
		return
	}
	requireNoRunError(t, err, stdout, stderr)
	requireExited(t, result, 0)

	currentUserSpec := liveAgentSmokeSpec(t, scratch, sessionmeta.NetworkDefault, payloadCommand(t, "project-write", scratch.projectDir), report)
	currentUserSpec.Identity = linuxspec.IdentityCurrentUser
	_, err = RunAgentUser(context.Background(), currentUserSpec, report, RunOptions{Sidecar: scratch.sidecar, RootHelper: helper})
	if err == nil || !strings.Contains(err.Error(), `identity "agent-user"`) {
		t.Fatalf("agent-user runner accepted current-user spec: %v", err)
	}
	t.Log("A4 helper admission: pass")
}

func runAgentMetadataLiveSmoke(t *testing.T) {
	scratch := newAgentLiveSmokeScratch(t)
	stdout, stderr, result, err := runLiveAgentUser(t, scratch, sessionmeta.NetworkDefault, payloadCommand(t, "project-write", scratch.projectDir), newLiveAgentRootHelper(t), preparedAgentUserReport(), nil)
	if logGapAndReturn(t, "A5 run metadata", err) {
		return
	}
	requireNoRunError(t, err, stdout, stderr)
	requireExited(t, result, 0)
	if _, err := os.Stat(scratch.sidecar.MetadataPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata sidecar stat err = %v, want removed after validated terminal result", err)
	}
	t.Log("A5 run metadata: pass; parent validated planned/launched/contained ordering")
}

func runAgentFilesystemLiveSmoke(t *testing.T) {
	scratch := newAgentLiveSmokeScratch(t)
	for _, tc := range []struct {
		label   string
		command []string
	}{
		{label: "project write", command: payloadCommand(t, "project-write", scratch.projectDir)},
		{label: "read-only denial", command: payloadCommand(t, "read-only-denial", scratch.readOnlyDir)},
		{label: "credential denial", command: payloadCommand(t, "credential-denial", scratch.deniedSecret)},
	} {
		stdout, stderr, result, err := runLiveAgentUser(t, scratch, sessionmeta.NetworkDefault, tc.command, newLiveAgentRootHelper(t), preparedAgentUserReport(), nil)
		if logGapAndReturn(t, "A6 filesystem policy "+tc.label, err) {
			return
		}
		requireNoRunError(t, err, stdout, stderr)
		requireExited(t, result, 0)
	}
	t.Log("A6 filesystem policy: pass")
}

func runAgentNetworkLiveSmoke(t *testing.T) {
	scratch := newAgentLiveSmokeScratch(t)
	stdout, stderr, result, err := runLiveAgentUser(t, scratch, sessionmeta.NetworkNone, payloadCommand(t, "network-none"), newLiveAgentRootHelper(t), preparedAgentUserReport(), nil)
	if logGapAndReturn(t, "A7 network policy", err) {
		return
	}
	requireNoRunError(t, err, stdout, stderr)
	requireExited(t, result, 0)
	t.Log("A7 network policy: pass")
}

func runAgentCancellationLiveSmoke(t *testing.T) {
	scratch := newAgentLiveSmokeScratch(t)
	timeout := 150 * time.Millisecond
	stdout, stderr, result, err := runLiveAgentUser(t, scratch, sessionmeta.NetworkDefault, payloadCommand(t, "sleep"), newLiveAgentRootHelper(t), preparedAgentUserReport(), &timeout)
	if logGapAndReturn(t, "A8 cancellation cleanup", err) {
		return
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation err = %v, stdout=%q stderr=%q", err, stdout, stderr)
	}
	if result.Record.Phase != PhaseCancelled {
		t.Fatalf("cancellation phase = %q, want %q", result.Record.Phase, PhaseCancelled)
	}
	if _, err := os.Stat(scratch.sidecar.MetadataPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata sidecar stat err = %v, want removed", err)
	}
	t.Log("A8 cancellation cleanup: pass")
}

func runAgentUnsupportedHostLiveSmoke(t *testing.T) {
	scratch := newAgentLiveSmokeScratch(t)
	report := preparedAgentUserReport()
	report.Features.CgroupV2 = platformlinux.FeatureReport{
		State:  platformlinux.FeatureUnavailable,
		Source: "agent-live-smoke-fixture",
		Detail: "forced missing cgroup fixture",
	}
	stdout, stderr, _, err := runLiveAgentUser(t, scratch, sessionmeta.NetworkDefault, payloadCommand(t, "project-write", scratch.projectDir), newLiveAgentRootHelper(t), report, nil)
	if err == nil {
		t.Fatalf("unsupported host fixture unexpectedly launched, stdout=%q stderr=%q", stdout, stderr)
	}
	var gapErr GapError
	if !errors.As(err, &gapErr) || !hasLiveSmokeGap(gapErr.Gaps, linuxspec.GapCgroupV2Unavailable) {
		t.Fatalf("unsupported host err = %v, want cgroup GapError", err)
	}
	if _, statErr := os.Stat(scratch.sidecar.Dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unsupported host sidecar stat err = %v, want no side effects", statErr)
	}
	t.Log("A11 unsupported host: pass typed gap before side effects")
}

func runLiveAgentUser(t *testing.T, scratch liveSmokeScratch, network sessionmeta.NetworkMode, command []string, helper AgentUserRootHelper, report platformlinux.Report, timeout *time.Duration) ([]byte, []byte, RunResult, error) {
	t.Helper()
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout != nil {
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	var stdout, stderr bytes.Buffer
	spec := liveAgentSmokeSpec(t, scratch, network, command, report)
	result, err := RunAgentUser(ctx, spec, report, RunOptions{
		Stdout:     &stdout,
		Stderr:     &stderr,
		Sidecar:    scratch.sidecar,
		RootHelper: helper,
	})
	return stdout.Bytes(), stderr.Bytes(), result, err
}

func liveAgentSmokeSpec(t *testing.T, scratch liveSmokeScratch, network sessionmeta.NetworkMode, command []string, report platformlinux.Report) linuxspec.LaunchSpec {
	t.Helper()
	testBinDir := filepath.Dir(liveSmokeTestBinary(t))
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{{Path: scratch.deniedDir, Reason: "agent-live-smoke"}})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project:      containment.PathGrant{Path: scratch.projectDir, Access: containment.PathReadWrite},
		ReadOnlyDirs: []containment.PathGrant{{Path: scratch.readOnlyDir, Access: containment.PathReadOnly}, {Path: testBinDir, Access: containment.PathReadOnly}},
		AgentHome: containment.AgentHomePolicy{
			Path: "/home/agent",
		},
		Temp:    containment.TempPolicy{Path: scratch.tempDir},
		Network: containment.NetworkPolicy{Mode: network},
		Process: containment.ProcessPolicy{},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := linuxspec.Compile(contract, linuxspec.CompileOptions{
		Platform:          report,
		Identity:          linuxspec.IdentityAgentUser,
		HelperStrategy:    linuxspec.HelperRoot,
		Command:           command,
		ExecutableRuntime: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func newAgentLiveSmokeScratch(t *testing.T) liveSmokeScratch {
	t.Helper()
	base := os.Getenv(agentLiveSmokeRootEnv)
	if strings.TrimSpace(base) == "" {
		return newLiveSmokeScratch(t)
	}
	root, err := os.MkdirTemp(base, "run-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	scratch := liveSmokeScratch{
		root:         root,
		projectDir:   filepath.Join(root, "project"),
		readOnlyDir:  filepath.Join(root, "readonly"),
		deniedDir:    filepath.Join(root, "denied"),
		deniedSecret: filepath.Join(root, "denied", "secret.txt"),
		agentHome:    "/home/agent",
		tempDir:      filepath.Join(root, "tmp"),
		sidecar:      SidecarStore{Dir: filepath.Join(root, "sidecar")},
	}
	for _, dir := range []string{scratch.projectDir, scratch.readOnlyDir, scratch.deniedDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteFile(t, filepath.Join(scratch.readOnlyDir, "readable.txt"), []byte("read-only input\n"))
	mustWriteFile(t, scratch.deniedSecret, []byte("secret\n"))
	return scratch
}

func newLiveAgentRootHelper(t *testing.T) AgentUserRootHelper {
	t.Helper()
	helperPath := strings.TrimSpace(os.Getenv("HAZMAT_LINUX_AGENT_USER_ROOT_HELPER"))
	if helperPath == "" {
		helperPath = agentLiveSmokeHelper
	}
	helper, err := NewCommandAgentUserRootHelper(helperPath)
	if err != nil {
		t.Fatal(err)
	}
	return helper
}

func preparedAgentUserReport() platformlinux.Report {
	report := platformlinux.Inspect(platformlinux.InspectOptions{
		AgentUserHelperStrategy: platformlinux.HelperRoot,
	})
	report.AgentUserBackend = platformlinux.NativeBackendStatus{CapabilityOK: true}
	return report
}

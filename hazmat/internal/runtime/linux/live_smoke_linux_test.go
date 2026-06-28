//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
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
	liveSmokeEnv      = "HAZMAT_LINUX_CURRENT_USER_VM_SMOKE"
	liveSmokeChildEnv = "HAZMAT_LINUX_CURRENT_USER_VM_SMOKE_CHILD"
)

func TestLinuxCurrentUserLiveSmokeMatrix(t *testing.T) {
	if os.Getenv(liveSmokeEnv) != "1" {
		t.Skipf("set %s=1 inside a disposable Linux VM to run current-user live smokes", liveSmokeEnv)
	}

	scenarios := []string{
		"S1-project-write",
		"S2-read-only-denial",
		"S3-credential-denial",
		"S4-network-none",
		"S5-cancellation-cleanup",
		"S6-missing-primitive",
		"S7-raw-streams",
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			output, err := runLiveSmokeChild(t, scenario)
			t.Log(strings.TrimSpace(output))
			if err != nil {
				t.Fatalf("%s child failed: %v", scenario, err)
			}
		})
	}
	t.Log("Support claim: experimental")
}

func TestLinuxCurrentUserLiveSmokeChild(t *testing.T) {
	scenario := os.Getenv(liveSmokeChildEnv)
	if scenario == "" {
		t.Skip("live smoke child only")
	}
	switch scenario {
	case "S1-project-write":
		runProjectWriteLiveSmoke(t)
	case "S2-read-only-denial":
		runReadOnlyDenialLiveSmoke(t)
	case "S3-credential-denial":
		runCredentialDenialLiveSmoke(t)
	case "S4-network-none":
		runNetworkNoneLiveSmoke(t)
	case "S5-cancellation-cleanup":
		runCancellationLiveSmoke(t)
	case "S6-missing-primitive":
		runMissingPrimitiveLiveSmoke(t)
	case "S7-raw-streams":
		runRawStreamsLiveSmoke(t)
	default:
		t.Fatalf("unknown live smoke child scenario %q", scenario)
	}
}

func TestLinuxCurrentUserLiveSmokePayload(t *testing.T) {
	args := flag.Args()
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		t.Skip("live smoke payload only")
	}
	switch args[0] {
	case "project-write":
		requirePayloadArgs(t, args, 2)
		mustWriteFile(t, filepath.Join(args[1], "s1-project-write.txt"), []byte("project write ok\n"))
	case "read-only-denial":
		requirePayloadArgs(t, args, 2)
		if err := os.WriteFile(filepath.Join(args[1], "s2-should-not-write.txt"), []byte("denied\n"), 0o600); err == nil {
			t.Fatalf("read-only write unexpectedly succeeded")
		}
	case "credential-denial":
		requirePayloadArgs(t, args, 2)
		if data, err := os.ReadFile(args[1]); err == nil {
			t.Fatalf("credential read unexpectedly succeeded: %q", data)
		}
	case "network-none":
		conn, err := net.DialTimeout("tcp", "198.51.100.1:80", 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			t.Fatalf("network=none outbound dial unexpectedly succeeded")
		}
	case "sleep":
		time.Sleep(5 * time.Second)
	default:
		t.Fatalf("unknown live smoke payload %q", args[0])
	}
}

func runLiveSmokeChild(t *testing.T, scenario string) (string, error) {
	t.Helper()
	testBin := liveSmokeTestBinary(t)
	cmd := exec.Command(testBin, "-test.run=^TestLinuxCurrentUserLiveSmokeChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		liveSmokeEnv+"=1",
		liveSmokeChildEnv+"="+scenario,
		EnvExperimentalCurrentUser+"=1",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

func runProjectWriteLiveSmoke(t *testing.T) {
	scratch := newLiveSmokeScratch(t)
	stdout, stderr, result, err := runLiveCurrentUser(t, scratch, sessionmeta.NetworkDefault, payloadCommand(t, "project-write", scratch.projectDir), nil)
	if logGapAndReturn(t, "S1 project write", err) {
		return
	}
	requireNoRunError(t, err, stdout, stderr)
	requireExited(t, result, 0)
	if data, err := os.ReadFile(filepath.Join(scratch.projectDir, "s1-project-write.txt")); err != nil || string(data) != "project write ok\n" {
		t.Fatalf("project write result = %q, %v", data, err)
	}
	t.Log("S1 project write: pass")
}

func runReadOnlyDenialLiveSmoke(t *testing.T) {
	scratch := newLiveSmokeScratch(t)
	stdout, stderr, result, err := runLiveCurrentUser(t, scratch, sessionmeta.NetworkDefault, payloadCommand(t, "read-only-denial", scratch.readOnlyDir), nil)
	if logGapAndReturn(t, "S2 read-only denial", err) {
		return
	}
	requireNoRunError(t, err, stdout, stderr)
	requireExited(t, result, 0)
	if _, err := os.Stat(filepath.Join(scratch.readOnlyDir, "s2-should-not-write.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only denial residue stat err = %v, want missing", err)
	}
	t.Log("S2 read-only denial: pass")
}

func runCredentialDenialLiveSmoke(t *testing.T) {
	scratch := newLiveSmokeScratch(t)
	stdout, stderr, result, err := runLiveCurrentUser(t, scratch, sessionmeta.NetworkDefault, payloadCommand(t, "credential-denial", scratch.deniedSecret), nil)
	if logGapAndReturn(t, "S3 credential denial", err) {
		return
	}
	requireNoRunError(t, err, stdout, stderr)
	requireExited(t, result, 0)
	t.Log("S3 credential denial: pass")
}

func runNetworkNoneLiveSmoke(t *testing.T) {
	scratch := newLiveSmokeScratch(t)
	stdout, stderr, result, err := runLiveCurrentUser(t, scratch, sessionmeta.NetworkNone, payloadCommand(t, "network-none"), nil)
	if logGapAndReturn(t, "S4 network none", err) {
		return
	}
	requireNoRunError(t, err, stdout, stderr)
	requireExited(t, result, 0)
	t.Log("S4 network none: pass")
}

func runCancellationLiveSmoke(t *testing.T) {
	scratch := newLiveSmokeScratch(t)
	timeout := 150 * time.Millisecond
	stdout, stderr, result, err := runLiveCurrentUser(t, scratch, sessionmeta.NetworkDefault, payloadCommand(t, "sleep"), &timeout)
	if logGapAndReturn(t, "S5 cancellation cleanup", err) {
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
	t.Log("S5 cancellation cleanup: pass")
}

func runMissingPrimitiveLiveSmoke(t *testing.T) {
	scratch := newLiveSmokeScratch(t)
	report := platformlinux.InspectHost()
	report.Features.UserNamespaces = platformlinux.FeatureReport{
		State:  platformlinux.FeatureUnavailable,
		Source: "live-smoke-fixture",
		Detail: "forced missing primitive fixture",
	}
	spec := liveSmokeSpec(t, scratch, sessionmeta.NetworkDefault, payloadCommand(t, "project-write", scratch.projectDir), report)
	_, err := RunCurrentUser(context.Background(), spec, report, RunOptions{Sidecar: scratch.sidecar})
	var gapErr GapError
	if !errors.As(err, &gapErr) || !hasLiveSmokeGap(gapErr.Gaps, linuxspec.GapUserNamespaceUnavailable) {
		t.Fatalf("missing primitive err = %v, want user namespace GapError", err)
	}
	if _, statErr := os.Stat(scratch.sidecar.Dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("missing primitive sidecar stat err = %v, want no side effects", statErr)
	}
	t.Log("S6 missing primitive: pass typed gap before side effects")
}

func runRawStreamsLiveSmoke(t *testing.T) {
	scratch := newLiveSmokeScratch(t)
	wantStdout := []byte("S7 stdout\x00raw\n")
	wantStderr := []byte("S7 stderr\x00raw\n")
	command := []string{"/bin/sh", "-c", "printf 'S7 stdout\\000raw\\n'; printf 'S7 stderr\\000raw\\n' >&2"}
	stdout, stderr, result, err := runLiveCurrentUser(t, scratch, sessionmeta.NetworkDefault, command, nil)
	if logGapAndReturn(t, "S7 raw streams", err) {
		return
	}
	requireNoRunError(t, err, stdout, stderr)
	requireExited(t, result, 0)
	if !bytes.Equal(stdout, wantStdout) || !bytes.Equal(stderr, wantStderr) {
		t.Fatalf("raw streams stdout=%q stderr=%q", stdout, stderr)
	}
	t.Log("S7 raw streams: pass")
}

type liveSmokeScratch struct {
	root         string
	projectDir   string
	readOnlyDir  string
	deniedDir    string
	deniedSecret string
	agentHome    string
	tempDir      string
	sidecar      SidecarStore
}

func newLiveSmokeScratch(t *testing.T) liveSmokeScratch {
	t.Helper()
	root := t.TempDir()
	scratch := liveSmokeScratch{
		root:         root,
		projectDir:   filepath.Join(root, "project"),
		readOnlyDir:  filepath.Join(root, "readonly"),
		deniedDir:    filepath.Join(root, "denied"),
		deniedSecret: filepath.Join(root, "denied", "secret.txt"),
		agentHome:    filepath.Join(root, "tmp", "home"),
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

func runLiveCurrentUser(t *testing.T, scratch liveSmokeScratch, network sessionmeta.NetworkMode, command []string, timeout *time.Duration) ([]byte, []byte, RunResult, error) {
	t.Helper()
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout != nil {
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	var stdout, stderr bytes.Buffer
	report := platformlinux.InspectHost()
	spec := liveSmokeSpec(t, scratch, network, command, report)
	result, err := RunCurrentUser(ctx, spec, report, RunOptions{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Sidecar: scratch.sidecar,
	})
	return stdout.Bytes(), stderr.Bytes(), result, err
}

func liveSmokeSpec(t *testing.T, scratch liveSmokeScratch, network sessionmeta.NetworkMode, command []string, report platformlinux.Report) linuxspec.LaunchSpec {
	t.Helper()
	testBinDir := filepath.Dir(liveSmokeTestBinary(t))
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{{Path: scratch.deniedDir, Reason: "live-smoke"}})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project:      containment.PathGrant{Path: scratch.projectDir, Access: containment.PathReadWrite},
		ReadOnlyDirs: []containment.PathGrant{{Path: scratch.readOnlyDir, Access: containment.PathReadOnly}, {Path: testBinDir, Access: containment.PathReadOnly}},
		AgentHome: containment.AgentHomePolicy{
			Path: scratch.agentHome,
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
		Identity:          linuxspec.IdentityCurrentUser,
		HelperStrategy:    linuxspec.HelperRootlessUserNS,
		Command:           command,
		ExecutableRuntime: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func payloadCommand(t *testing.T, payload string, args ...string) []string {
	t.Helper()
	command := []string{
		liveSmokeTestBinary(t),
		"-test.run=^TestLinuxCurrentUserLiveSmokePayload$",
		"--",
		payload,
	}
	return append(command, args...)
}

func liveSmokeTestBinary(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func requirePayloadArgs(t *testing.T, args []string, want int) {
	t.Helper()
	if len(args) != want {
		t.Fatalf("payload args = %v, want %d", args, want)
	}
}

func requireNoRunError(t *testing.T, err error, stdout, stderr []byte) {
	t.Helper()
	if err != nil {
		t.Fatalf("run err = %v, stdout=%q stderr=%q", err, stdout, stderr)
	}
}

func requireExited(t *testing.T, result RunResult, wantCode int) {
	t.Helper()
	if result.Record.Phase != PhaseExited || result.Record.ExitCode != wantCode {
		t.Fatalf("run result = %+v, want exited code %d", result.Record, wantCode)
	}
}

func logGapAndReturn(t *testing.T, label string, err error) bool {
	t.Helper()
	var gapErr GapError
	if errors.As(err, &gapErr) {
		var codes []string
		for _, gap := range gapErr.Gaps {
			codes = append(codes, gap.Code)
		}
		t.Logf("%s: gap %s", label, strings.Join(codes, ","))
		return true
	}
	return false
}

func hasLiveSmokeGap(gaps []linuxspec.CapabilityGap, code string) bool {
	for _, gap := range gaps {
		if gap.Code == code {
			return true
		}
	}
	return false
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func Example_linuxCurrentUserLiveSmokeCommand() {
	fmt.Println("HAZMAT_LINUX_CURRENT_USER_VM_SMOKE=1 HAZMAT_EXPERIMENTAL_LINUX_CURRENT_USER=1 go test ./internal/runtime/linux -run '^TestLinuxCurrentUserLiveSmokeMatrix$' -count=1 -v")
	// Output:
	// HAZMAT_LINUX_CURRENT_USER_VM_SMOKE=1 HAZMAT_EXPERIMENTAL_LINUX_CURRENT_USER=1 go test ./internal/runtime/linux -run '^TestLinuxCurrentUserLiveSmokeMatrix$' -count=1 -v
}

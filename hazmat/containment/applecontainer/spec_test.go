package applecontainer

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hazmat/containment"
	"hazmat/sessionmeta"
)

var updateGoldenBaselines = flag.Bool("update-golden", false, "update golden baseline files")

func healthyHost() HostReport {
	return HostReport{
		GOOS:                "darwin",
		GOARCH:              "arm64",
		MacOSMajorVersion:   26,
		CLIPath:             "/usr/local/bin/container",
		CLIVersion:          "1.0.0",
		CLIVersionSupported: true,
		APIServerHealthy:    true,
	}
}

func testOptions() CompileOptions {
	return CompileOptions{
		Harness:   "codex",
		Image:     "ghcr.io/example/hazmat-codex:sha256-abc",
		SessionID: "session-0123456789",
		Command:   []string{"codex", "--help"},
		Host:      healthyHost(),
	}
}

func testContractInput(readDirs []containment.PathGrant) containment.ContractInput {
	return containment.ContractInput{
		Project:      containment.PathGrant{Path: "/Users/dr/workspace/project", Access: containment.PathReadWrite},
		ReadOnlyDirs: readDirs,
		ReadWriteDirs: containment.PathGrants([]string{
			"/Users/dr/workspace/scratch",
		}, containment.PathReadWrite),
		AgentHome: containment.AgentHomePolicy{Path: "/Users/agent"},
		Temp:      containment.TempPolicy{Path: "/Users/agent/tmp/hazmat-session"},
		Network:   containment.NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:   containment.ProcessPolicy{AllowFork: true},
	}
}

func testFloor(t *testing.T) containment.CredentialFloor {
	t.Helper()
	floor, err := containment.NewCredentialFloor("/Users/agent", []string{"/.ssh", "/.aws"})
	if err != nil {
		t.Fatalf("NewCredentialFloor: %v", err)
	}
	return floor
}

func testContract(t *testing.T) containment.Contract {
	t.Helper()
	contract, err := containment.NewContract(testContractInput(containment.PathGrants([]string{
		"/Users/dr/reference",
		"/Users/dr/workspace/project/docs",
		"/Users/dr/reference/api",
	}, containment.PathReadOnly)), testFloor(t))
	if err != nil {
		t.Fatalf("NewContract: %v", err)
	}
	return contract
}

func TestGoldenAppleContainerLaunchSpecBaselines(t *testing.T) {
	floor, err := containment.NewCredentialFloor("/Users/agent", []string{"/.ssh", "/.aws"})
	if err != nil {
		t.Fatalf("NewCredentialFloor fixture: %v", err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project: containment.PathGrant{Path: "/Users/dr/workspace/project", Access: containment.PathReadWrite},
		ReadOnlyDirs: containment.PathGrants([]string{
			"/Users/dr/reference",
			"/Users/dr/workspace/project/docs",
		}, containment.PathReadOnly),
		AgentHome: containment.AgentHomePolicy{Path: "/Users/agent"},
		Temp:      containment.TempPolicy{Path: "/Users/agent/tmp/hazmat-session"},
		Network:   containment.NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:   containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatalf("NewContract fixture: %v", err)
	}
	spec, err := Compile(contract, CompileOptions{
		Harness:           "codex",
		Image:             "ghcr.io/example/hazmat-codex:sha256-abc",
		SessionID:         "golden-session",
		Command:           []string{"codex", "--version"},
		CredentialEnvFile: "/Users/agent/tmp/hazmat-session/credentials.env",
		Host: HostReport{
			GOOS:                "darwin",
			GOARCH:              "arm64",
			MacOSMajorVersion:   26,
			CLIPath:             "/usr/local/bin/container",
			CLIVersion:          "1.0.0",
			CLIVersionSupported: true,
			APIServerHealthy:    true,
			RunnableAsAgent:     true,
		},
	})
	if err != nil {
		t.Fatalf("Compile fixture: %v", err)
	}
	assertGoldenJSON(t, "launch/apple-container.json", spec)

	argv, err := Argv(spec)
	if err != nil {
		t.Fatalf("Argv fixture: %v", err)
	}
	assertGoldenJSON(t, "launch/apple-container-argv.json", argv)
}

func TestCompileBuildsPlanOnlyLaunchSpec(t *testing.T) {
	spec, err := Compile(testContract(t), testOptions())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if spec.FormatVersion != LaunchSpecFormatVersion || spec.Backend != BackendAppleContainer || spec.Phase != PhasePlanOnly {
		t.Fatalf("unexpected spec identity: %+v", spec)
	}
	if spec.HostIdentity != HostIdentityInvokingUser {
		t.Fatalf("HostIdentity = %q, want %q", spec.HostIdentity, HostIdentityInvokingUser)
	}
	wantMounts := []MountSpec{
		{Source: "/Users/dr/workspace/project", Target: "/Users/dr/workspace/project", Access: containment.PathReadWrite},
		{Source: "/Users/dr/reference", Target: "/Users/dr/reference", Access: containment.PathReadOnly},
		{Source: "/Users/dr/workspace/scratch", Target: "/Users/dr/workspace/scratch", Access: containment.PathReadWrite},
	}
	if len(spec.Mounts) != len(wantMounts) {
		t.Fatalf("Mounts = %+v, want %+v", spec.Mounts, wantMounts)
	}
	for i := range wantMounts {
		if spec.Mounts[i] != wantMounts[i] {
			t.Fatalf("Mounts[%d] = %+v, want %+v", i, spec.Mounts[i], wantMounts[i])
		}
	}
	if spec.Workdir != "/Users/dr/workspace/project" {
		t.Fatalf("Workdir = %q", spec.Workdir)
	}
	if spec.User != (UserSpec{UID: DefaultGuestUID, GID: DefaultGuestGID}) {
		t.Fatalf("User = %+v, want non-root default", spec.User)
	}
	if spec.Rootfs.ReadOnly || !spec.Rootfs.Ephemeral {
		t.Fatalf("Rootfs = %+v, want writable ephemeral", spec.Rootfs)
	}
	if spec.Network.Mode != sessionmeta.NetworkDefault || spec.Network.Enforcement != "apple-container-vm-network" {
		t.Fatalf("Network = %+v", spec.Network)
	}
	if !spec.Capabilities.DropAll || len(spec.Capabilities.Add) != 0 {
		t.Fatalf("Capabilities = %+v", spec.Capabilities)
	}
	if spec.Environment.InheritHostEnv {
		t.Fatalf("Environment inherits host env: %+v", spec.Environment)
	}
	if !spec.Cleanup.RemoveContainerByName || !spec.Cleanup.NeverPrune {
		t.Fatalf("Cleanup = %+v", spec.Cleanup)
	}
	if spec.Labels[LabelSessionID] != "session-0123456789" || spec.Labels[LabelHarness] != "codex" {
		t.Fatalf("Labels = %+v", spec.Labels)
	}
	if len(spec.CapabilityGaps) != 1 || spec.CapabilityGaps[0].Code != "apple-container-runtime-gated" {
		t.Fatalf("CapabilityGaps = %+v, want only the experimental-gate gap", spec.CapabilityGaps)
	}
	raw, err := MarshalJSON(spec)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"backend": "apple-container"`)) {
		t.Fatalf("marshaled spec missing backend:\n%s", string(raw))
	}
}

func TestCompileOmitsCoveredReadOnlyMounts(t *testing.T) {
	spec, err := Compile(testContract(t), testOptions())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, mount := range spec.Mounts {
		if mount.Source == "/Users/dr/workspace/project/docs" {
			t.Fatalf("project-covered read dir was mounted: %+v", spec.Mounts)
		}
		if mount.Source == "/Users/dr/reference/api" {
			t.Fatalf("read dir covered by broader grant was mounted: %+v", spec.Mounts)
		}
	}
}

func TestNewContractRejectsCredentialDenyOverlap(t *testing.T) {
	_, err := containment.NewContract(testContractInput(containment.PathGrants([]string{
		"/Users/agent",
	}, containment.PathReadOnly)), testFloor(t))
	if err == nil {
		t.Fatal("NewContract succeeded for credential deny parent grant")
	}
	if !strings.Contains(err.Error(), "overlaps credential deny path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileRejectsUnconstructedCredentialFloor(t *testing.T) {
	contract := containment.Contract{
		Project:          containment.PathGrant{Path: "/Users/dr/workspace/project", Access: containment.PathReadWrite},
		AgentHome:        containment.AgentHomePolicy{Path: "/Users/agent"},
		Temp:             containment.TempPolicy{Path: "/Users/agent/tmp/hazmat-session"},
		CredentialDenies: []containment.CredentialDeny{{Path: "/Users/agent/.ssh"}},
		Network:          containment.NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:          containment.ProcessPolicy{AllowFork: true},
	}
	_, err := Compile(contract, testOptions())
	if err == nil {
		t.Fatal("Compile succeeded without structural credential floor")
	}
	if !strings.Contains(err.Error(), "credential deny floor is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileRejectsIntegrationEnvPassthrough(t *testing.T) {
	opts := testOptions()
	opts.IntegrationEnvKeys = []string{"GOPROXY", "GOROOT"}
	_, err := Compile(testContract(t), opts)
	if err == nil {
		t.Fatal("Compile accepted integration env passthrough")
	}
	if !strings.Contains(err.Error(), "reject integration env passthrough") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileRejectsSSHForwarding(t *testing.T) {
	opts := testOptions()
	opts.SSHForwardRequested = true
	_, err := Compile(testContract(t), opts)
	if err == nil {
		t.Fatal("Compile accepted SSH agent forwarding")
	}
	if !strings.Contains(err.Error(), "SSH agent forwarding") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileRejectsSocketPublishing(t *testing.T) {
	opts := testOptions()
	opts.PublishSockets = []string{"/var/run/docker.sock"}
	_, err := Compile(testContract(t), opts)
	if err == nil {
		t.Fatal("Compile accepted socket publishing")
	}
	if !strings.Contains(err.Error(), "socket publishing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileFailsClosedOnNetworkNone(t *testing.T) {
	input := testContractInput(nil)
	input.Network = containment.NetworkPolicy{Mode: sessionmeta.NetworkNone}
	contract, err := containment.NewContract(input, testFloor(t))
	if err != nil {
		t.Fatalf("NewContract: %v", err)
	}
	_, err = Compile(contract, testOptions())
	if err == nil {
		t.Fatal("Compile accepted --network none")
	}
	if !strings.Contains(err.Error(), "not implemented for the Apple Container backend") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileRejectsRootGuestIdentity(t *testing.T) {
	opts := testOptions()
	opts.GuestRootRequested = true
	_, err := Compile(testContract(t), opts)
	if err == nil {
		t.Fatal("Compile accepted a root guest identity")
	}
	if !strings.Contains(err.Error(), "root guest identity") {
		t.Fatalf("unexpected error: %v", err)
	}

	opts = testOptions()
	opts.GuestUID = -1
	if _, err := Compile(testContract(t), opts); err == nil {
		t.Fatal("Compile accepted a negative guest uid")
	}
}

func TestCompileRequiresExplicitImage(t *testing.T) {
	opts := testOptions()
	opts.Image = ""
	if _, err := Compile(testContract(t), opts); err == nil {
		t.Fatal("Compile accepted an empty image")
	}
	opts.Image = "ubuntu latest"
	if _, err := Compile(testContract(t), opts); err == nil {
		t.Fatal("Compile accepted an image reference with whitespace")
	}
}

func TestCompileRequiresAbsoluteCredentialEnvFile(t *testing.T) {
	opts := testOptions()
	opts.CredentialEnvFile = "relative/env-file"
	if _, err := Compile(testContract(t), opts); err == nil {
		t.Fatal("Compile accepted a relative credential env-file path")
	}

	opts.CredentialEnvFile = "/Users/agent/tmp/hazmat-session/credentials.env"
	spec, err := Compile(testContract(t), opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if spec.Environment.CredentialEnvFile != opts.CredentialEnvFile {
		t.Fatalf("Environment = %+v", spec.Environment)
	}
	if len(spec.Cleanup.RemoveGeneratedFiles) != 1 || spec.Cleanup.RemoveGeneratedFiles[0] != opts.CredentialEnvFile {
		t.Fatalf("Cleanup = %+v, want generated env-file recorded for removal", spec.Cleanup)
	}
}

func TestCompileReportsAdmissionCapabilityGaps(t *testing.T) {
	opts := testOptions()
	opts.Host = HostReport{
		GOOS:              "darwin",
		GOARCH:            "amd64",
		MacOSMajorVersion: 15,
	}
	spec, err := Compile(testContract(t), opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, code := range []string{
		"apple-container-runtime-gated",
		"host-not-apple-silicon",
		"macos-version-unsupported",
		"container-cli-missing",
		"container-cli-version-unsupported",
		"container-api-server-unhealthy",
	} {
		if !hasGap(spec.CapabilityGaps, code) {
			t.Fatalf("CapabilityGaps missing %q: %+v", code, spec.CapabilityGaps)
		}
	}
	if hasGap(spec.CapabilityGaps, "host-not-darwin") {
		t.Fatalf("CapabilityGaps flagged darwin host: %+v", spec.CapabilityGaps)
	}
}

func TestContainerNameDeterministic(t *testing.T) {
	a := ContainerName("codex", "/Users/dr/workspace/My Project!", "session-0123456789")
	b := ContainerName("codex", "/Users/dr/workspace/My Project!", "session-0123456789")
	if a != b {
		t.Fatalf("container name not deterministic: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "hazmat-codex-my-project-") {
		t.Fatalf("container name = %q", a)
	}
	if c := ContainerName("codex", "/Users/dr/workspace/My Project!", "session-other"); c == a {
		t.Fatalf("container name ignores session ID: %q", c)
	}
}

func TestArgvDerivation(t *testing.T) {
	opts := testOptions()
	opts.CredentialEnvFile = "/Users/agent/tmp/hazmat-session/credentials.env"
	opts.Resources = ResourceSpec{CPUs: 4, Memory: "8g"}
	spec, err := Compile(testContract(t), opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	argv, err := Argv(spec)
	if err != nil {
		t.Fatalf("Argv: %v", err)
	}
	want := []string{
		"container", "run",
		"--name", spec.ContainerName,
		"--user", "502:20",
		"--workdir", "/Users/dr/workspace/project",
		"--network", "default",
		"--mount", "type=bind,source=/Users/dr/workspace/project,target=/Users/dr/workspace/project",
		"--mount", "type=bind,source=/Users/dr/reference,target=/Users/dr/reference,readonly",
		"--mount", "type=bind,source=/Users/dr/workspace/scratch,target=/Users/dr/workspace/scratch",
		"--cpus", "4",
		"--memory", "8g",
		"--cap-drop", "all",
		"--env-file", "/Users/agent/tmp/hazmat-session/credentials.env",
		"--label", "dev.hazmat.harness=codex",
		"--label", "dev.hazmat.session-id=session-0123456789",
		spec.Image,
		"codex", "--help",
	}
	if len(argv) != len(want) {
		t.Fatalf("argv = %q\nwant %q", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q\nfull: %q", i, argv[i], want[i], argv)
		}
	}
}

func TestArgvRefusesNonCompiledSpecs(t *testing.T) {
	if _, err := Argv(LaunchSpec{}); err == nil {
		t.Fatal("Argv accepted a zero spec")
	}
	spec, err := Compile(testContract(t), testOptions())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	spec.Environment.InheritHostEnv = true
	if _, err := Argv(spec); err == nil {
		t.Fatal("Argv accepted host env inheritance")
	}
}

func hasGap(gaps []CapabilityGap, code string) bool {
	for _, gap := range gaps {
		if gap.Code == code {
			return true
		}
	}
	return false
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
		t.Fatalf("read golden %s: %v\nRun `go test ./containment/applecontainer -update-golden` from hazmat/ to refresh baselines.", path, err)
	}
	if got != string(want) {
		t.Fatalf("%s changed; run `go test ./containment/applecontainer -update-golden` only after reviewing the diff.\n--- want\n%s\n--- got\n%s", name, string(want), got)
	}
}

func TestCompileExecutableRuntimePhase(t *testing.T) {
	opts := testOptions()
	opts.ExecutableRuntime = true
	spec, err := Compile(testContract(t), opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if spec.Phase != PhaseExperimental {
		t.Fatalf("Phase = %q, want %q", spec.Phase, PhaseExperimental)
	}
	if len(spec.CapabilityGaps) != 0 {
		t.Fatalf("CapabilityGaps = %+v, want none for a healthy executable host", spec.CapabilityGaps)
	}

	opts.Host = HostReport{}
	spec, err = Compile(testContract(t), opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(spec.CapabilityGaps) == 0 {
		t.Fatal("executable compile with uninspected host must keep admission gaps")
	}
	if hasGap(spec.CapabilityGaps, "apple-container-runtime-gated") {
		t.Fatalf("executable compile must not carry the preview gate gap: %+v", spec.CapabilityGaps)
	}
}

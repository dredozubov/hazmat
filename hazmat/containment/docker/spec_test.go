package docker

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"hazmat/containment"
	"hazmat/hostfacts"
	"hazmat/pathpolicy"
	"hazmat/sessionbackend"
	"hazmat/sessionmeta"
)

var updateGoldenBaselines = flag.Bool("update-golden", false, "update golden baseline files")

func TestCompileBuildsLaunchSpec(t *testing.T) {
	root := t.TempDir()
	project := mkdir(t, root, "workspace/project")
	readDir := mkdir(t, root, "reference")
	writeDir := mkdir(t, root, "cache")
	agentHome := mkdir(t, root, "agent")

	contract := testContract(t, project, readDir, writeDir, agentHome)
	backendPlan := sessionbackend.BuildPlan(sessionbackend.Input{
		Target:      "claude",
		Mode:        sessionmeta.ModeDockerSandbox,
		ProjectDir:  project,
		NetworkMode: sessionmeta.NetworkDefault,
		HostFacts:   hostfacts.ForGOOS("darwin"),
	})
	profile := PolicyProfile{Name: "baseline", Policy: "baseline", AllowHosts: []string{"example.com"}}

	spec, err := Compile(contract, CompileOptions{Agent: "claude", BackendPlan: backendPlan, Profile: profile})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if spec.Name != SandboxName("claude", project, []string{readDir}, []string{writeDir}, "baseline") {
		t.Fatalf("Name = %q", spec.Name)
	}
	if spec.ProjectDir != project || spec.Agent != "claude" {
		t.Fatalf("identity = %+v", spec)
	}
	if got := spec.MountReadDirs; len(got) != 1 || got[0] != readDir {
		t.Fatalf("MountReadDirs = %v", got)
	}
	if got := spec.MountWriteDirs; len(got) != 1 || got[0] != writeDir {
		t.Fatalf("MountWriteDirs = %v", got)
	}

	profile.AllowHosts[0] = "mutated.example"
	if spec.Profile.AllowHosts[0] != "example.com" {
		t.Fatalf("Profile aliases input: %+v", spec.Profile)
	}
}

func TestGoldenDockerLaunchSpecBaseline(t *testing.T) {
	agentHome := "/Users/agent"
	floor, err := containment.NewCredentialFloor(agentHome, pathpolicy.CredentialDenySubpaths())
	if err != nil {
		t.Fatalf("NewCredentialFloor: %v", err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project: containment.PathGrant{Path: "/Users/dr/workspace/project", Access: containment.PathReadWrite},
		ReadOnlyDirs: containment.PathGrants([]string{
			"/Users/dr/workspace/reference",
			"/opt/homebrew/Cellar/go/1.2.3/libexec",
		}, containment.PathReadOnly),
		AgentHome: containment.AgentHomePolicy{Path: agentHome},
		Temp:      containment.TempPolicy{Path: agentHome + "/.cache/hazmat/tmp/golden-docker"},
		Network:   containment.NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:   containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatalf("NewContract: %v", err)
	}
	backendPlan := sessionbackend.BuildPlan(sessionbackend.Input{
		Target:        "claude",
		Mode:          sessionmeta.ModeDockerSandbox,
		ProjectDir:    "/Users/dr/workspace/project",
		ReadOnlyDirs:  []string{"/Users/dr/workspace/reference", "/opt/homebrew/Cellar/go/1.2.3/libexec"},
		ReadWriteDirs: []string{"/Users/dr/workspace/project/.cache"},
		NetworkMode:   sessionmeta.NetworkDefault,
		Integrations:  []string{"go"},
		HostFacts:     hostfacts.ForGOOS("darwin"),
	})
	spec, err := Compile(contract, CompileOptions{
		Agent:       "claude",
		BackendPlan: backendPlan,
		Profile: PolicyProfile{
			Name:   "baseline",
			Policy: "deny",
			AllowHosts: []string{
				"api.anthropic.com",
				"claude.ai",
				"platform.claude.com",
				"statsig.anthropic.com",
				"*.sentry.io",
				"github.com",
				"registry.npmjs.org",
			},
		},
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	assertGoldenJSON(t, "launch/docker-sandbox.json", goldenDockerLaunchSpecFrom(spec))
}

type goldenDockerLaunchSpec struct {
	Name             string              `json:"name"`
	Agent            string              `json:"agent"`
	ProjectDir       string              `json:"project_dir"`
	BackendPlan      sessionbackend.Plan `json:"backend_plan"`
	Profile          PolicyProfile       `json:"profile"`
	MountReadDirs    []string            `json:"mount_read_dirs,omitempty"`
	MountWriteDirs   []string            `json:"mount_write_dirs,omitempty"`
	DockerCreateArgs []string            `json:"docker_create_args"`
	NetworkProxyArgs []string            `json:"network_proxy_args"`
}

func goldenDockerLaunchSpecFrom(spec LaunchSpec) goldenDockerLaunchSpec {
	return goldenDockerLaunchSpec{
		Name:             spec.Name,
		Agent:            spec.Agent,
		ProjectDir:       spec.ProjectDir,
		BackendPlan:      spec.BackendPlan,
		Profile:          spec.Profile,
		MountReadDirs:    append([]string(nil), spec.MountReadDirs...),
		MountWriteDirs:   append([]string(nil), spec.MountWriteDirs...),
		DockerCreateArgs: DockerCreateArgs(spec),
		NetworkProxyArgs: NetworkProxyArgs(spec),
	}
}

func TestCompileRejectsUnconstructedCredentialFloor(t *testing.T) {
	contract := containment.Contract{
		Project:          containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		AgentHome:        containment.AgentHomePolicy{Path: "/home/agent"},
		Temp:             containment.TempPolicy{Path: "/tmp/hazmat-session"},
		CredentialDenies: []containment.CredentialDeny{{Path: "/home/agent/.ssh"}},
		Network:          containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:          containment.ProcessPolicy{AllowFork: true},
	}
	_, err := Compile(contract, CompileOptions{})
	if err == nil {
		t.Fatal("Compile succeeded without structural credential floor")
	}
}

func testContract(t *testing.T, project, readDir, writeDir, agentHome string) containment.Contract {
	t.Helper()
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{
		{Path: filepath.Join(agentHome, ".ssh")},
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project:       containment.PathGrant{Path: project, Access: containment.PathReadWrite},
		ReadOnlyDirs:  containment.PathGrants([]string{readDir}, containment.PathReadOnly),
		ReadWriteDirs: containment.PathGrants([]string{writeDir}, containment.PathReadWrite),
		AgentHome:     containment.AgentHomePolicy{Path: agentHome},
		Temp:          containment.TempPolicy{Path: filepath.Join(agentHome, ".cache/hazmat/tmp/test")},
		Network:       containment.NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:       containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func mkdir(t *testing.T, root, rel string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
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
		t.Fatalf("read golden %s: %v\nRun `go test ./containment/docker -update-golden` from hazmat/ to refresh baselines.", path, err)
	}
	if got != string(want) {
		t.Fatalf("%s changed; run `go test ./containment/docker -update-golden` only after reviewing the diff.\n--- want\n%s\n--- got\n%s", name, string(want), got)
	}
}

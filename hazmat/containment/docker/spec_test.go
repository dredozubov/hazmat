package docker

import (
	"os"
	"path/filepath"
	"testing"

	"hazmat/containment"
	"hazmat/hostfacts"
	"hazmat/sessionbackend"
	"hazmat/sessionmeta"
)

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

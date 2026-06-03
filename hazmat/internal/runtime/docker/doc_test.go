package docker

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeProbe struct {
	outputs map[string]fakeResult
	runs    map[string]fakeResult
	calls   []string
}

type fakeResult struct {
	output string
	err    error
}

func (f *fakeProbe) Output(name string, args ...string) (string, error) {
	key := probeKey(name, args...)
	f.calls = append(f.calls, "output:"+key)
	if result, ok := f.outputs[key]; ok {
		return result.output, result.err
	}
	return "", nil
}

func (f *fakeProbe) Run(name string, args ...string) (string, error) {
	key := probeKey(name, args...)
	f.calls = append(f.calls, "run:"+key)
	if result, ok := f.runs[key]; ok {
		return result.output, result.err
	}
	return "", nil
}

func probeKey(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), "\x00")
}

func TestPrepareLaunchCreatesSandboxAndAppliesNetworkPolicy(t *testing.T) {
	var stderr bytes.Buffer
	probe := &fakeProbe{
		outputs: map[string]fakeResult{
			probeKey("docker", "sandbox", "ls", "--json"): {output: `{"sandboxes":[]}`},
		},
		runs: map[string]fakeResult{},
	}
	spec := LaunchSpec{
		Name:           "hazmat-claude-project",
		Agent:          "claude",
		ProjectDir:     "/workspace/project",
		MountReadDirs:  []string{"/workspace/ref"},
		MountWriteDirs: []string{"/workspace/cache"},
		Profile: PolicyProfile{
			Name:       "baseline",
			Policy:     "deny",
			AllowHosts: []string{"github.com"},
		},
	}

	if err := NewBackend(&stderr).PrepareLaunch(probe, spec); err != nil {
		t.Fatalf("PrepareLaunch(): %v", err)
	}

	wantCalls := []string{
		"output:" + probeKey("docker", "sandbox", "ls", "--json"),
		"run:" + probeKey("docker", "sandbox", "create", "--name", "hazmat-claude-project", "claude", "/workspace/project", "/workspace/cache", "/workspace/ref:ro"),
		"run:" + probeKey("docker", "sandbox", "network", "proxy", "hazmat-claude-project", "--policy", "deny", "--allow-host", "github.com"),
	}
	if !reflect.DeepEqual(probe.calls, wantCalls) {
		t.Fatalf("calls = %q, want %q", probe.calls, wantCalls)
	}
	if !strings.Contains(stderr.String(), "creating Docker Sandbox hazmat-claude-project") {
		t.Fatalf("stderr missing create message: %q", stderr.String())
	}
}

func TestRunAgentSessionReportsClosedPipeHint(t *testing.T) {
	probe := &fakeProbe{
		outputs: map[string]fakeResult{
			probeKey("docker", "desktop", "status"): {output: "Status running\n"},
		},
		runs: map[string]fakeResult{
			probeKey("docker", "sandbox", "run", "hazmat-claude-project"): {
				output: "io: read/write on closed pipe",
				err:    errors.New("exit status 1"),
			},
		},
	}

	err := NewBackend(nil).RunAgentSession(probe, "claude", "hazmat-claude-project", nil)
	if err == nil || !strings.Contains(err.Error(), "Docker Desktop failed unexpectedly") {
		t.Fatalf("RunAgentSession() error = %v", err)
	}
}

func TestRemoveManagedSandboxesIgnoresMissingSandbox(t *testing.T) {
	probe := &fakeProbe{
		outputs: map[string]fakeResult{
			probeKey("docker", "sandbox", "rm", "missing"): {
				output: "not found",
				err:    errors.New("exit status 1"),
			},
		},
	}

	if err := NewBackend(nil).RemoveManagedSandboxes(probe, []ManagedSandbox{{Name: "missing"}}); err != nil {
		t.Fatalf("RemoveManagedSandboxes(): %v", err)
	}
}

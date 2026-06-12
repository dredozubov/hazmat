package hostexec

import (
	"reflect"
	"testing"
)

func testEnv() Env {
	return Env{
		SudoPath:         "/usr/bin/sudo",
		TeePath:          "/usr/bin/tee",
		AgentUser:        "agent",
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
	}
}

func TestNewSudoCommandStartsFromRoot(t *testing.T) {
	cmd := NewSudoCommand(testEnv(), "test", "-x", "/Users/agent/.local/bin/claude")

	if cmd.Dir != "/" {
		t.Fatalf("NewSudoCommand().Dir = %q, want %q", cmd.Dir, "/")
	}
	wantArgs := []string{"/usr/bin/sudo", "test", "-x", "/Users/agent/.local/bin/claude"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("NewSudoCommand().Args = %v, want %v", cmd.Args, wantArgs)
	}
}

func TestNewAgentCommandUsesLaunchHelper(t *testing.T) {
	cmd := NewAgentCommand(testEnv(), "bash", "/tmp/bootstrap.sh")

	if cmd.Dir != "/" {
		t.Fatalf("NewAgentCommand().Dir = %q, want %q", cmd.Dir, "/")
	}
	wantArgs := []string{
		"/usr/bin/sudo",
		"-n",
		"-u", "agent",
		"-H", "/usr/local/libexec/hazmat-launch",
		"exec",
		"bash", "/tmp/bootstrap.sh",
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("NewAgentCommand().Args = %v, want %v", cmd.Args, wantArgs)
	}
}

func TestNewSudoNoPromptCommandAddsNonInteractiveFlag(t *testing.T) {
	cmd := NewSudoNoPromptCommand(testEnv(), "-u", "agent", "whoami")

	wantArgs := []string{"/usr/bin/sudo", "-n", "-u", "agent", "whoami"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("NewSudoNoPromptCommand().Args = %v, want %v", cmd.Args, wantArgs)
	}
}

func TestSudoOutputCommandAddsNonInteractiveFlag(t *testing.T) {
	cmd := newSudoOutputCommand(testEnv(), "cat", "/etc/sudoers.d/agent")

	wantArgs := []string{"/usr/bin/sudo", "-n", "cat", "/etc/sudoers.d/agent"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("newSudoOutputCommand().Args = %v, want %v", cmd.Args, wantArgs)
	}
}

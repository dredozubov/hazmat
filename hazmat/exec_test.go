package main

import (
	"reflect"
	"testing"
)

func TestNewSudoCommandUsesRootWorkingDir(t *testing.T) {
	cmd := newSudoCommand("test", "-x", "/Users/agent/.local/bin/claude")

	if cmd.Dir != "/" {
		t.Fatalf("newSudoCommand().Dir = %q, want %q", cmd.Dir, "/")
	}

	wantArgs := []string{
		"sudo",
		"test",
		"-x",
		"/Users/agent/.local/bin/claude",
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("newSudoCommand().Args = %v, want %v", cmd.Args, wantArgs)
	}
}

func TestNewAgentCommandUsesRootWorkingDir(t *testing.T) {
	cmd := newAgentCommand("bash", "/tmp/bootstrap.sh")

	if cmd.Dir != "/" {
		t.Fatalf("newAgentCommand().Dir = %q, want %q", cmd.Dir, "/")
	}

	wantArgs := []string{
		"sudo",
		"-u",
		agentUser,
		"-H",
		launchHelperPath(),
		"exec",
		"bash",
		"/tmp/bootstrap.sh",
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("newAgentCommand().Args = %v, want %v", cmd.Args, wantArgs)
	}
}

func TestNewSudoNoPromptCommandUsesNoPromptFlag(t *testing.T) {
	cmd := newSudoNoPromptCommand("-u", agentUser, "whoami")

	wantArgs := []string{
		"sudo",
		"-n",
		"-u",
		agentUser,
		"whoami",
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("newSudoNoPromptCommand().Args = %v, want %v", cmd.Args, wantArgs)
	}
}

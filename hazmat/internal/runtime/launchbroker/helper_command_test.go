package launchbroker

import (
	"context"
	"reflect"
	"testing"
)

func TestNewHelperCommandBuildsDirectExecInvocation(t *testing.T) {
	plan := validChildPlan(t)
	cmd, err := NewHelperCommand(plan, HelperCommandConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
	})
	if err != nil {
		t.Fatalf("NewHelperCommand: %v", err)
	}

	wantArgs := []string{
		"/usr/local/libexec/hazmat-launch",
		"/private/tmp/hazmat-123.sb",
		"--hazmat-session-temp", "/Users/agent/.cache/hazmat/tmp/123-456",
		"--hazmat-direct-exec",
		"--hazmat-working-dir", "/Users/dr/workspace/project",
		"--hazmat-env", "HOME=/Users/agent",
		"--hazmat-env", "PATH=/usr/bin",
		"--",
		"/usr/bin/true",
	}
	if !reflect.DeepEqual(cmd.Args(), wantArgs) {
		t.Fatalf("Args = %#v, want %#v", cmd.Args(), wantArgs)
	}
	if !reflect.DeepEqual(cmd.Env(), []string{"SUDO_UID=501"}) {
		t.Fatalf("Env = %#v", cmd.Env())
	}
}

func TestNewHelperCommandBuildsShellInvocation(t *testing.T) {
	peer, err := AuthenticatePeer(501, 501)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyLaunchRequest(peer, LaunchRequest{
		PolicyPath:      "/private/tmp/hazmat-123.sb",
		MetadataJSON:    `{"kind":"hazmat.session"}`,
		EnvPairs:        []string{"HOME=/Users/agent", "PATH=/usr/bin"},
		RuntimeEnvPairs: []string{"GIT_SSH_COMMAND=helper"},
		Script:          `exec "$@"`,
		Args:            []string{"claude", "-p", "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewChildPlan(verified, ChildFDPolicyCloseInherited)
	if err != nil {
		t.Fatal(err)
	}

	cmd, err := NewHelperCommand(plan, HelperCommandConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
		Profile:          true,
	})
	if err != nil {
		t.Fatalf("NewHelperCommand: %v", err)
	}

	wantArgs := []string{
		"/usr/local/libexec/hazmat-launch",
		"--hazmat-launch-profile",
		"/private/tmp/hazmat-123.sb",
		"--hazmat-metadata-json", `{"kind":"hazmat.session"}`,
		"/usr/bin/env", "-i",
		"HOME=/Users/agent", "PATH=/usr/bin",
		"GIT_SSH_COMMAND=helper",
		"/bin/zsh", "-lc", `exec "$@"`, "zsh",
		"claude", "-p", "hi",
	}
	if !reflect.DeepEqual(cmd.Args(), wantArgs) {
		t.Fatalf("Args = %#v, want %#v", cmd.Args(), wantArgs)
	}
}

func TestHelperCommandDefensivelyCopies(t *testing.T) {
	cmd, err := NewHelperCommand(validChildPlan(t), HelperCommandConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
	})
	if err != nil {
		t.Fatal(err)
	}

	args := cmd.Args()
	args[0] = "/bin/false"
	if got := cmd.Args()[0]; got != "/usr/local/libexec/hazmat-launch" {
		t.Fatalf("Args returned mutable internal slice: %q", got)
	}

	env := cmd.Env()
	env[0] = "SUDO_UID=0"
	if got := cmd.Env()[0]; got != "SUDO_UID=501" {
		t.Fatalf("Env returned mutable internal slice: %q", got)
	}
}

func TestHelperCommandContext(t *testing.T) {
	helper, err := NewHelperCommand(validChildPlan(t), HelperCommandConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
	})
	if err != nil {
		t.Fatal(err)
	}

	cmd, err := helper.CommandContext(context.Background())
	if err != nil {
		t.Fatalf("CommandContext: %v", err)
	}
	if cmd.Path != "/usr/local/libexec/hazmat-launch" {
		t.Fatalf("Path = %q", cmd.Path)
	}
	if !reflect.DeepEqual(cmd.Args, helper.Args()) {
		t.Fatalf("exec args = %#v, want %#v", cmd.Args, helper.Args())
	}
	if !reflect.DeepEqual(cmd.Env, []string{"SUDO_UID=501"}) {
		t.Fatalf("exec env = %#v", cmd.Env)
	}
}

func TestNewHelperCommandRejectsInvalidInputs(t *testing.T) {
	if _, err := NewHelperCommand(ChildPlan{}, HelperCommandConfig{LaunchHelperPath: "/usr/local/libexec/hazmat-launch"}); err == nil {
		t.Fatal("NewHelperCommand accepted zero child plan")
	}
	if _, err := NewHelperCommand(validChildPlan(t), HelperCommandConfig{LaunchHelperPath: "hazmat-launch"}); err == nil {
		t.Fatal("NewHelperCommand accepted relative helper path")
	}
}

func validChildPlan(t *testing.T) ChildPlan {
	t.Helper()
	peer, err := AuthenticatePeer(501, 501)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyLaunchRequest(peer, validDirectRequest())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewChildPlan(verified, ChildFDPolicyCloseInherited)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

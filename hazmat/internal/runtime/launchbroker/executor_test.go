package launchbroker

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestHelperExecutorRunsPlannedCommand(t *testing.T) {
	plan := validChildPlanWithMetadata(t, `{"kind":"hazmat.session"}`)
	runner := &recordingHelperRunner{
		result: HelperRunResult{
			ExitCode: 0,
			Stdout:   []byte("ok\n"),
			Stderr:   []byte("helper detail\n{\"kind\":\"hazmat.session\"}\nagent stderr\n"),
		},
	}
	executor, err := NewHelperExecutor(HelperExecutorConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
		Runner:           runner,
	})
	if err != nil {
		t.Fatalf("NewHelperExecutor: %v", err)
	}

	resp, err := executor.HandleLaunch(context.Background(), plan)
	if err != nil {
		t.Fatalf("HandleLaunch: %v", err)
	}
	if !resp.OK || resp.ExitCode != 0 {
		t.Fatalf("response = %+v", resp)
	}
	if resp.MetadataJSON != `{"kind":"hazmat.session"}` {
		t.Fatalf("MetadataJSON = %q", resp.MetadataJSON)
	}
	if resp.Stdout != "ok\n" {
		t.Fatalf("Stdout = %q", resp.Stdout)
	}
	if resp.Stderr != "helper detail\nagent stderr\n" {
		t.Fatalf("Stderr = %q", resp.Stderr)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("runner saw %d commands, want 1", len(runner.commands))
	}
	if got := runner.commands[0].Env(); !reflect.DeepEqual(got, []string{"SUDO_UID=501"}) {
		t.Fatalf("command env = %#v", got)
	}
}

func TestHelperExecutorReturnsCommandExitCode(t *testing.T) {
	plan := validChildPlanWithMetadata(t, `{"kind":"hazmat.session"}`)
	executor, err := NewHelperExecutor(HelperExecutorConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
		Runner: &recordingHelperRunner{result: HelperRunResult{
			ExitCode: 42,
			Stderr:   []byte("{\"kind\":\"hazmat.session\"}\n"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := executor.HandleLaunch(context.Background(), plan)
	if err != nil {
		t.Fatalf("HandleLaunch: %v", err)
	}
	if !resp.OK || resp.ExitCode != 42 {
		t.Fatalf("response = %+v, want OK exit 42", resp)
	}
}

func TestHelperExecutorFailsClosedWithoutConfirmedMetadata(t *testing.T) {
	plan := validChildPlanWithMetadata(t, `{"kind":"hazmat.session"}`)
	executor, err := NewHelperExecutor(HelperExecutorConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
		Runner: &recordingHelperRunner{result: HelperRunResult{
			ExitCode: 0,
			Stderr:   []byte("helper failed before sandbox\n"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := executor.HandleLaunch(context.Background(), plan)
	if err != nil {
		t.Fatalf("HandleLaunch: %v", err)
	}
	if resp.OK {
		t.Fatalf("response OK without metadata: %+v", resp)
	}
	if resp.Error != "launch helper did not confirm containment metadata" {
		t.Fatalf("Error = %q", resp.Error)
	}
	if resp.MetadataJSON != "" {
		t.Fatalf("MetadataJSON = %q", resp.MetadataJSON)
	}
}

func TestHelperExecutorAllowsLaunchWithoutMetadataRequest(t *testing.T) {
	executor, err := NewHelperExecutor(HelperExecutorConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
		Runner: &recordingHelperRunner{result: HelperRunResult{
			ExitCode: 0,
			Stderr:   []byte("plain stderr\n"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := executor.HandleLaunch(context.Background(), validChildPlan(t))
	if err != nil {
		t.Fatalf("HandleLaunch: %v", err)
	}
	if !resp.OK || resp.MetadataJSON != "" || resp.Stderr != "plain stderr\n" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestHelperLaunchHandlerFactory(t *testing.T) {
	runner := &recordingHelperRunner{}
	handler, err := NewHelperLaunchHandler(HelperExecutorConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
		Runner:           runner,
	})
	if err != nil {
		t.Fatalf("NewHelperLaunchHandler: %v", err)
	}
	if handler == nil {
		t.Fatal("handler is nil")
	}
}

func TestHelperExecutorPropagatesRunnerError(t *testing.T) {
	wantErr := errors.New("runner failed")
	executor, err := NewHelperExecutor(HelperExecutorConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
		Runner:           &recordingHelperRunner{err: wantErr},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := executor.HandleLaunch(context.Background(), validChildPlan(t)); !errors.Is(err, wantErr) {
		t.Fatalf("HandleLaunch error = %v, want %v", err, wantErr)
	}
}

func TestSplitConfirmedMetadata(t *testing.T) {
	metadata, stderr := splitConfirmedMetadata([]byte("before\n{}\nafter\n"), "{}")
	if metadata != "{}" || string(stderr) != "before\nafter\n" {
		t.Fatalf("metadata=%q stderr=%q", metadata, stderr)
	}

	metadata, stderr = splitConfirmedMetadata([]byte("before\n"), "{}")
	if metadata != "" || string(stderr) != "before\n" {
		t.Fatalf("metadata=%q stderr=%q", metadata, stderr)
	}
}

type recordingHelperRunner struct {
	commands []HelperCommand
	result   HelperRunResult
	err      error
}

func (r *recordingHelperRunner) Run(ctx context.Context, command HelperCommand) (HelperRunResult, error) {
	r.commands = append(r.commands, command)
	return r.result, r.err
}

func validChildPlanWithMetadata(t *testing.T, metadata string) ChildPlan {
	t.Helper()
	req := validDirectRequest()
	req.MetadataJSON = metadata
	peer, err := AuthenticatePeer(501, 501)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyLaunchRequest(peer, req)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewChildPlan(verified, ChildFDPolicyCloseInherited)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

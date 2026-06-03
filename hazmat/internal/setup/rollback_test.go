package setup

import (
	"reflect"
	"strings"
	"testing"
)

func TestRunRollbackStepsExecutesCallbacksInModelOrder(t *testing.T) {
	var got []string
	callback := func(name string) func() error {
		return func() error {
			got = append(got, name)
			return nil
		}
	}

	err := RunRollbackSteps(RollbackCallbacks{
		Sudoers:         callback("sudoers"),
		LaunchDaemon:    callback("launchDaemon"),
		PfFirewall:      callback("pfFirewall"),
		DNSBlocklist:    callback("dnsBlocklist"),
		Seatbelt:        callback("seatbelt"),
		Wrappers:        callback("wrappers"),
		HomeDirTraverse: callback("homeDirTraverse"),
		Umask:           callback("umask"),
		LocalRepo:       callback("localRepo"),
		AgentUser:       callback("agentUser"),
		DevGroup:        callback("devGroup"),
	}, RollbackOptions{
		DeleteUser:  true,
		DeleteGroup: true,
	})
	if err != nil {
		t.Fatalf("RunRollbackSteps: %v", err)
	}

	want := []string{
		"sudoers",
		"launchDaemon",
		"pfFirewall",
		"dnsBlocklist",
		"seatbelt",
		"wrappers",
		"homeDirTraverse",
		"umask",
		"localRepo",
		"agentUser",
		"devGroup",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callback order = %v, want %v", got, want)
	}
}

func TestRunRollbackStepsPreservesDestructiveResourcesByDefault(t *testing.T) {
	var got []string
	var warnings []string
	callback := func(name string) func() error {
		return func() error {
			got = append(got, name)
			return nil
		}
	}

	err := RunRollbackSteps(RollbackCallbacks{
		Sudoers:         callback("sudoers"),
		LaunchDaemon:    callback("launchDaemon"),
		PfFirewall:      callback("pfFirewall"),
		DNSBlocklist:    callback("dnsBlocklist"),
		Seatbelt:        callback("seatbelt"),
		Wrappers:        callback("wrappers"),
		HomeDirTraverse: callback("homeDirTraverse"),
		Umask:           callback("umask"),
		LocalRepo:       callback("localRepo"),
		AgentUser:       callback("agentUser"),
		DevGroup:        callback("devGroup"),
	}, RollbackOptions{
		AgentUserName: "agent",
		AgentHome:     "/Users/agent",
		GroupName:     "dev",
		Warn: func(message string) {
			warnings = append(warnings, message)
		},
	})
	if err != nil {
		t.Fatalf("RunRollbackSteps: %v", err)
	}

	if len(got) != 9 {
		t.Fatalf("executed callbacks = %v, want only core rollback callbacks", got)
	}
	if strings.Contains(strings.Join(got, ","), "agentUser") || strings.Contains(strings.Join(got, ","), "devGroup") {
		t.Fatalf("destructive callbacks ran without delete flags: %v", got)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want preserve warnings for user and group", warnings)
	}
}

func TestRunRollbackStepsRequiresConfiguredCallback(t *testing.T) {
	err := RunRollbackSteps(RollbackCallbacks{}, RollbackOptions{})
	if err == nil || !strings.Contains(err.Error(), "rollbackSudoers") {
		t.Fatalf("RunRollbackSteps error = %v, want missing rollbackSudoers", err)
	}
}

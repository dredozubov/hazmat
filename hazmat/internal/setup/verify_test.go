package setup

import (
	"reflect"
	"strings"
	"testing"
)

func TestRunVerificationStepsExecutesCallbacksInModelOrder(t *testing.T) {
	var got []string
	callback := func(name string) func() {
		return func() {
			got = append(got, name)
		}
	}

	err := RunVerificationSteps(VerificationCallbacks{
		AgentUser:       callback("agentUser"),
		AgentHome:       callback("agentHome"),
		HomeDirTraverse: callback("homeDirTraverse"),
		PfAnchorLoaded:  callback("pfAnchorLoaded"),
		PfEnabled:       callback("pfEnabled"),
		Sudoers:         callback("sudoers"),
		DNSBlocklist:    callback("dnsBlocklist"),
		SeatbeltWrapper: callback("seatbeltWrapper"),
		AgentEnv:        callback("agentEnv"),
		HostWrappers:    callback("hostWrappers"),
	})
	if err != nil {
		t.Fatalf("RunVerificationSteps: %v", err)
	}

	want := []string{
		"agentUser",
		"agentHome",
		"homeDirTraverse",
		"pfAnchorLoaded",
		"pfEnabled",
		"sudoers",
		"dnsBlocklist",
		"seatbeltWrapper",
		"agentEnv",
		"hostWrappers",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callback order = %v, want %v", got, want)
	}
}

func TestRunVerificationStepsRequiresConfiguredCallback(t *testing.T) {
	err := RunVerificationSteps(VerificationCallbacks{})
	if err == nil || !strings.Contains(err.Error(), "verifyAgentUser") {
		t.Fatalf("RunVerificationSteps error = %v, want missing verifyAgentUser", err)
	}
}

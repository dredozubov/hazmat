package setup

import (
	"reflect"
	"strings"
	"testing"
)

func TestRunInitSetupStepsExecutesCallbacksInModelOrder(t *testing.T) {
	var got []string
	callback := func(name string) func() error {
		return func() error {
			got = append(got, name)
			return nil
		}
	}

	err := RunInitSetupSteps(InitCallbacks{
		AgentUser:                  callback("agentUser"),
		DevGroup:                   callback("devGroup"),
		HomeDirTraverse:            callback("homeDirTraverse"),
		LocalRepo:                  callback("localRepo"),
		HardeningGaps:              callback("hardeningGaps"),
		Seatbelt:                   callback("seatbelt"),
		Wrappers:                   callback("wrappers"),
		PfFirewall:                 callback("pfFirewall"),
		DNSBlocklist:               callback("dnsBlocklist"),
		LaunchDaemon:               callback("launchDaemon"),
		LaunchHelper:               callback("launchHelper"),
		Sudoers:                    callback("sudoers"),
		OptionalMaintenanceSudoers: callback("maintenanceSudoers"),
		SelectedHarness:            callback("selectedHarness"),
		AgentCredentials:           callback("agentCredentials"),
	})
	if err != nil {
		t.Fatalf("RunInitSetupSteps: %v", err)
	}

	want := []string{
		"agentUser",
		"devGroup",
		"homeDirTraverse",
		"localRepo",
		"hardeningGaps",
		"seatbelt",
		"wrappers",
		"pfFirewall",
		"dnsBlocklist",
		"launchDaemon",
		"launchHelper",
		"sudoers",
		"maintenanceSudoers",
		"selectedHarness",
		"agentCredentials",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callback order = %v, want %v", got, want)
	}
}

func TestRunInitSetupStepsRequiresConfiguredCallback(t *testing.T) {
	err := RunInitSetupSteps(InitCallbacks{})
	if err == nil || !strings.Contains(err.Error(), "setupAgentUser") {
		t.Fatalf("RunInitSetupSteps error = %v, want missing setupAgentUser", err)
	}
}

package linux

import (
	"reflect"
	"strings"
	"testing"

	"hazmat/internal/setup"
)

func TestSetupStepsFollowModeledResourceGraph(t *testing.T) {
	got := resources(SetupSteps(Callbacks{}))
	want := []setup.Resource{
		setup.ResourceLinuxDistroProfile,
		setup.ResourceLinuxAgentUser,
		setup.ResourceLinuxSharedGroup,
		setup.ResourceLinuxAgentHome,
		setup.ResourceLinuxWorkspaceAccess,
		setup.ResourceLinuxToolHome,
		setup.ResourceLinuxFirewallPolicy,
		setup.ResourceLinuxResolverPolicy,
		setup.ResourceLinuxCgroupRoot,
		setup.ResourceLinuxLaunchHelper,
		setup.ResourceLinuxSudoers,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("setup resources = %#v, want %#v", got, want)
	}
}

func TestRollbackRemovesPrivilegeBeforeIdentity(t *testing.T) {
	got := resources(RollbackSteps(Callbacks{}, RollbackOptions{}))
	want := []setup.Resource{
		setup.ResourceLinuxSudoers,
		setup.ResourceLinuxLaunchHelper,
		setup.ResourceLinuxCgroupRoot,
		setup.ResourceLinuxFirewallPolicy,
		setup.ResourceLinuxResolverPolicy,
		setup.ResourceLinuxWorkspaceAccess,
		setup.ResourceLinuxToolHome,
		setup.ResourceLinuxAgentHome,
		setup.ResourceLinuxAgentUser,
		setup.ResourceLinuxSharedGroup,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback resources = %#v, want %#v", got, want)
	}
}

func TestRollbackPreservesDestructiveResourcesByDefault(t *testing.T) {
	var ran []setup.Resource
	var warnings []string
	callbacks := callbacksThatRecord(&ran)
	err := RunRollbackSteps(callbacks, RollbackOptions{
		Warn: func(message string) {
			warnings = append(warnings, message)
		},
	})
	if err != nil {
		t.Fatalf("RunRollbackSteps: %v", err)
	}
	if containsAny(ran,
		setup.ResourceLinuxToolHome,
		setup.ResourceLinuxAgentHome,
		setup.ResourceLinuxAgentUser,
		setup.ResourceLinuxSharedGroup,
	) {
		t.Fatalf("destructive callbacks ran without flags: %v", ran)
	}
	if len(warnings) != 4 {
		t.Fatalf("warnings = %v, want four preservation warnings", warnings)
	}
}

func TestRollbackDestructiveFlagsAreIndependent(t *testing.T) {
	var ran []setup.Resource
	err := RunRollbackSteps(callbacksThatRecord(&ran), RollbackOptions{
		DeleteToolHome:  true,
		DeleteAgentUser: true,
	})
	if err != nil {
		t.Fatalf("RunRollbackSteps: %v", err)
	}
	if !containsAll(ran, setup.ResourceLinuxToolHome, setup.ResourceLinuxAgentUser) {
		t.Fatalf("expected selected destructive callbacks to run: %v", ran)
	}
	if containsAny(ran, setup.ResourceLinuxAgentHome, setup.ResourceLinuxSharedGroup) {
		t.Fatalf("unselected destructive callbacks ran: %v", ran)
	}
}

func TestDryRunDoesNotExecuteCallbacks(t *testing.T) {
	callbacks := Callbacks{
		DistroProfile: func() error { t.Fatal("dry-run executed callback"); return nil },
	}
	records := DryRunSetup(callbacks)
	if len(records) != len(SetupSteps(Callbacks{})) {
		t.Fatalf("dry-run records = %d, want setup step count", len(records))
	}
	for _, record := range records {
		if !record.Skipped {
			t.Fatalf("record = %+v, want skipped", record)
		}
	}
}

func TestRunSetupStepsRequiresConfiguredCallbacks(t *testing.T) {
	err := RunSetupSteps(Callbacks{})
	if err == nil || !strings.Contains(err.Error(), "linuxSetupDistroProfile") {
		t.Fatalf("RunSetupSteps error = %v, want missing first callback", err)
	}
}

func callbacksThatRecord(ran *[]setup.Resource) Callbacks {
	callback := func(resource setup.Resource) Callback {
		return func() error {
			*ran = append(*ran, resource)
			return nil
		}
	}
	return Callbacks{
		DistroProfile:   callback(setup.ResourceLinuxDistroProfile),
		AgentUser:       callback(setup.ResourceLinuxAgentUser),
		SharedGroup:     callback(setup.ResourceLinuxSharedGroup),
		AgentHome:       callback(setup.ResourceLinuxAgentHome),
		WorkspaceAccess: callback(setup.ResourceLinuxWorkspaceAccess),
		ToolHome:        callback(setup.ResourceLinuxToolHome),
		FirewallPolicy:  callback(setup.ResourceLinuxFirewallPolicy),
		ResolverPolicy:  callback(setup.ResourceLinuxResolverPolicy),
		CgroupRoot:      callback(setup.ResourceLinuxCgroupRoot),
		LaunchHelper:    callback(setup.ResourceLinuxLaunchHelper),
		Sudoers:         callback(setup.ResourceLinuxSudoers),
	}
}

func resources(steps []Step) []setup.Resource {
	out := make([]setup.Resource, len(steps))
	for i, step := range steps {
		out[i] = step.Resource
	}
	return out
}

func containsAny(values []setup.Resource, needles ...setup.Resource) bool {
	seen := map[setup.Resource]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, needle := range needles {
		if seen[needle] {
			return true
		}
	}
	return false
}

func containsAll(values []setup.Resource, needles ...setup.Resource) bool {
	seen := map[setup.Resource]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, needle := range needles {
		if !seen[needle] {
			return false
		}
	}
	return true
}

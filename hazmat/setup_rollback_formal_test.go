package main

import (
	"reflect"
	"testing"
)

func TestInitSetupStepsMatchMCSetupRollbackResources(t *testing.T) {
	got := make([]setupRollbackTLAResource, 0, len(initSetupSteps()))
	gotNames := make([]string, 0, len(initSetupSteps()))
	for _, step := range initSetupSteps() {
		got = append(got, step.tlaResource)
		gotNames = append(gotNames, step.name)
	}

	want := []setupRollbackTLAResource{
		tlaResourceAgentUser,
		tlaResourceDevGroup,
		tlaResourceHomeDirTraverse,
		tlaResourceLocalRepo,
		tlaResourceUmask,
		tlaResourceSeatbelt,
		tlaResourceWrappers,
		tlaResourcePfAnchor,
		tlaResourceDNSBlocklist,
		tlaResourceLaunchDaemon,
		tlaResourceLaunchHelper,
		tlaResourceSudoers,
		tlaResourceMaintenanceSudoers,
		tlaResourceClaudeCode,
		tlaResourceCredentials,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("init setup TLA resources = %v, want %v", got, want)
	}
	if len(gotNames) != len(want) {
		t.Fatalf("init setup names = %v, want %d entries", gotNames, len(want))
	}
}

func TestRollbackStepsMatchMCSetupRollbackResources(t *testing.T) {
	gotCore := make([]setupRollbackTLAResource, 0, len(coreRollbackSteps()))
	gotCoreNames := make([]string, 0, len(coreRollbackSteps()))
	for _, step := range coreRollbackSteps() {
		gotCore = append(gotCore, step.tlaResource)
		gotCoreNames = append(gotCoreNames, step.name)
	}
	wantCore := []setupRollbackTLAResource{
		tlaResourceSudoers,
		tlaResourceLaunchDaemon,
		tlaResourcePfAnchor,
		tlaResourceDNSBlocklist,
		tlaResourceSeatbelt,
		tlaResourceWrappers,
		tlaResourceHomeDirTraverse,
		tlaResourceUmask,
		tlaResourceLocalRepo,
	}
	if !reflect.DeepEqual(gotCore, wantCore) {
		t.Fatalf("core rollback TLA resources = %v, want %v", gotCore, wantCore)
	}
	if len(gotCoreNames) != len(wantCore) {
		t.Fatalf("core rollback names = %v, want %d entries", gotCoreNames, len(wantCore))
	}

	gotDestructive := make([]setupRollbackTLAResource, 0, len(destructiveRollbackSteps()))
	gotDestructiveNames := make([]string, 0, len(destructiveRollbackSteps()))
	for _, step := range destructiveRollbackSteps() {
		gotDestructive = append(gotDestructive, step.tlaResource)
		gotDestructiveNames = append(gotDestructiveNames, step.name)
	}
	wantDestructive := []setupRollbackTLAResource{
		tlaResourceAgentUser,
		tlaResourceDevGroup,
	}
	if !reflect.DeepEqual(gotDestructive, wantDestructive) {
		t.Fatalf("destructive rollback TLA resources = %v, want %v", gotDestructive, wantDestructive)
	}
	if len(gotDestructiveNames) != len(wantDestructive) {
		t.Fatalf("destructive rollback names = %v, want %d entries", gotDestructiveNames, len(wantDestructive))
	}
}

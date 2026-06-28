package hazmat

import (
	"reflect"
	"strings"
	"testing"

	"hazmat/internal/setup"
	linuxsetup "hazmat/internal/setup/linux"
)

func TestLinuxSetupDiagnosticsRegisteredForGraph(t *testing.T) {
	for _, step := range linuxsetup.SetupRepairSteps(linuxsetup.Callbacks{}) {
		info, ok := linuxSetupDiagnosticInfos[step.Spec.Resource]
		if !ok {
			t.Fatalf("%s missing diagnostic metadata", step.Spec.Resource)
		}
		finding, ok := diagnosticFindingDefinitions[info.FindingID]
		if !ok {
			t.Fatalf("%s missing diagnostic finding", info.FindingID)
		}
		if finding.Resource != info.ResourceID {
			t.Fatalf("%s resource = %s, want %s", finding.ID, finding.Resource, info.ResourceID)
		}
		action, ok := diagnosticRepairAction(diagnosticRepairActionID(step.Spec.ActionID))
		if !ok {
			t.Fatalf("%s missing diagnostic repair action", step.Spec.ActionID)
		}
		if action.Receipt != diagnosticRepairReceiptID(step.Spec.ReceiptID) ||
			action.Verification != diagnosticVerificationID(step.Spec.VerificationID) ||
			action.RollbackBoundary != step.Spec.RollbackBoundary {
			t.Fatalf("%s action contract = %+v, want spec %+v", step.Spec.ActionID, action, step.Spec)
		}
		if !diagnosticHostRepairBackendSupportsAction(action.ID) {
			t.Fatalf("%s has no host apply handler", action.ID)
		}
		if !diagnosticHostRepairBackendSupportsVerification(action.Verification) {
			t.Fatalf("%s has no host verify handler", action.Verification)
		}
		if !diagnosticRepairActionHasProofLane(action, diagnosticRepairProofTLASetupRollback) {
			t.Fatalf("%s proof lanes = %v, want setup/rollback TLA lane", action.ID, action.ProofLanes)
		}
	}
}

func TestLinuxSetupDiagnosticBackendDispatchesThroughGraph(t *testing.T) {
	restoreRuntimeOS := linuxDiagnosticRuntimeOS
	restoreCallbacks := linuxDiagnosticSetupCallbacks
	defer func() {
		linuxDiagnosticRuntimeOS = restoreRuntimeOS
		linuxDiagnosticSetupCallbacks = restoreCallbacks
	}()

	var got []string
	linuxDiagnosticRuntimeOS = func() string { return "linux" }
	linuxDiagnosticSetupCallbacks = func(_ *diagnosticHostRepairBackend, operation linuxDiagnosticSetupOperation) linuxsetup.Callbacks {
		return linuxSetupCallbacksThatRecord(func(resource setup.Resource) {
			got = append(got, string(operation)+":"+string(resource))
		})
	}

	action, ok := diagnosticRepairAction("repair.linux-setup.launch-helper")
	if !ok {
		t.Fatal("missing linux launch-helper repair action")
	}
	backend := &diagnosticHostRepairBackend{}
	if result := backend.ApplyDiagnosticRepair(action, diagnosticRepairPlanItem{}); result.Err != nil {
		t.Fatalf("ApplyDiagnosticRepair: %v", result.Err)
	}
	if result := backend.VerifyDiagnosticRepair(action, diagnosticRepairPlanItem{}); result.Err != nil {
		t.Fatalf("VerifyDiagnosticRepair: %v", result.Err)
	}

	want := []string{
		"apply:linuxLaunchHelper",
		"verify:linuxLaunchHelper",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callbacks = %#v, want %#v", got, want)
	}
}

func TestLinuxSetupDiagnosticBackendFailsClosedOffLinux(t *testing.T) {
	restoreRuntimeOS := linuxDiagnosticRuntimeOS
	restoreCallbacks := linuxDiagnosticSetupCallbacks
	defer func() {
		linuxDiagnosticRuntimeOS = restoreRuntimeOS
		linuxDiagnosticSetupCallbacks = restoreCallbacks
	}()

	linuxDiagnosticRuntimeOS = func() string { return "darwin" }
	linuxDiagnosticSetupCallbacks = func(_ *diagnosticHostRepairBackend, _ linuxDiagnosticSetupOperation) linuxsetup.Callbacks {
		t.Fatal("off-Linux diagnostic repair must fail before callbacks")
		return linuxsetup.Callbacks{}
	}

	action, ok := diagnosticRepairAction("repair.linux-setup.agent-user")
	if !ok {
		t.Fatal("missing linux agent-user repair action")
	}
	result := (&diagnosticHostRepairBackend{}).ApplyDiagnosticRepair(action, diagnosticRepairPlanItem{})
	if result.Err == nil || !strings.Contains(result.Err.Error(), "requires Linux host diagnostics") {
		t.Fatalf("ApplyDiagnosticRepair error = %v, want Linux host refusal", result.Err)
	}
}

func linuxSetupCallbacksThatRecord(record func(setup.Resource)) linuxsetup.Callbacks {
	callback := func(resource setup.Resource) linuxsetup.Callback {
		return func() error {
			record(resource)
			return nil
		}
	}
	return linuxsetup.Callbacks{
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

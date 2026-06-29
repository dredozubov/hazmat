package hazmat

import (
	"errors"
	"path/filepath"
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

func TestLinuxSetupBackendCreatesAgentUserWithLockedLogin(t *testing.T) {
	runner := newFakeLinuxSetupRunner()
	runner.outputs[linuxSetupCommandKey("id", "-u", linuxAgentUserName)] = fakeLinuxSetupOutput{err: errors.New("missing")}
	backend := linuxDiagnosticSetupBackend{
		runner:      runner,
		operation:   linuxDiagnosticSetupApply,
		currentUser: "dr",
		projectDir:  "/work/project",
	}

	if err := backend.applyAgentUser(); err != nil {
		t.Fatalf("applyAgentUser: %v", err)
	}

	runner.assertSudo(t, []string{"useradd", "--create-home", "--home-dir", linuxAgentHomePath, "--shell", linuxAgentShell, "--uid", agentUID, linuxAgentUserName})
	runner.assertSudo(t, []string{"passwd", "-l", linuxAgentUserName})
}

func TestLinuxSetupBackendRefusesSudoersBeforePrerequisites(t *testing.T) {
	runner := newFakeLinuxSetupRunner()
	runner.outputs[linuxSetupCommandKey("id", "-u", linuxAgentUserName)] = fakeLinuxSetupOutput{out: agentUID + "\n"}
	runner.outputs[linuxSetupCommandKey("getent", "passwd", linuxAgentUserName)] = fakeLinuxSetupOutput{out: "agent:x:" + agentUID + ":" + sharedGID + "::" + linuxAgentHomePath + ":" + linuxAgentShell + "\n"}
	runner.outputs[linuxSetupCommandKey("cat", linuxDistroProfilePath)] = fakeLinuxSetupOutput{out: `{"runtime_os":"linux"}`}
	runner.sudoErrors[linuxSetupCommandKey("test", "-f", "/sys/fs/cgroup/cgroup.controllers")] = errors.New("no cgroup v2")
	backend := linuxDiagnosticSetupBackend{
		runner:      runner,
		operation:   linuxDiagnosticSetupApply,
		currentUser: "dr",
		projectDir:  "/work/project",
	}

	err := backend.applySudoers()
	if err == nil || !strings.Contains(err.Error(), "refuse Linux sudoers before prerequisites verify") {
		t.Fatalf("applySudoers err = %v, want prerequisite refusal", err)
	}
	if len(runner.writes) != 0 {
		t.Fatalf("sudoers writes = %+v, want none before prerequisites", runner.writes)
	}
}

func TestLinuxSetupBackendWritesRootHelperSudoersAfterPrerequisites(t *testing.T) {
	runner := newFakeLinuxSetupRunner()
	runner.outputs[linuxSetupCommandKey("id", "-u", linuxAgentUserName)] = fakeLinuxSetupOutput{out: agentUID + "\n"}
	runner.outputs[linuxSetupCommandKey("getent", "passwd", linuxAgentUserName)] = fakeLinuxSetupOutput{out: "agent:x:" + agentUID + ":" + sharedGID + "::" + linuxAgentHomePath + ":" + linuxAgentShell + "\n"}
	runner.outputs[linuxSetupCommandKey("cat", linuxDistroProfilePath)] = fakeLinuxSetupOutput{out: `{"runtime_os":"linux"}`}
	runner.outputs[linuxSetupCommandKey(linuxRootHelperPath, "run-agent", "--help")] = fakeLinuxSetupOutput{out: "usage: run-agent\n"}
	runner.outputs[linuxSetupCommandKey("cat", linuxSudoersFile)] = fakeLinuxSetupOutput{err: errors.New("missing")}
	backend := linuxDiagnosticSetupBackend{
		runner:      runner,
		operation:   linuxDiagnosticSetupApply,
		currentUser: "dr",
		projectDir:  "/work/project",
	}

	if err := backend.applySudoers(); err != nil {
		t.Fatalf("applySudoers: %v", err)
	}

	write, ok := runner.writes[linuxSudoersFile]
	if !ok {
		t.Fatalf("missing sudoers write, writes = %+v", runner.writes)
	}
	want := "dr ALL=(root) NOPASSWD: " + linuxRootHelperPath + " run-agent *\n"
	if write != want {
		t.Fatalf("sudoers write = %q, want %q", write, want)
	}
	runner.assertSudo(t, []string{"chmod", "440", linuxSudoersFile})
	runner.assertSudo(t, []string{"visudo", "-c", "-f", linuxSudoersFile})
}

func TestLinuxSetupBackendRejectsLaunchHelperWithoutRunAgent(t *testing.T) {
	runner := newFakeLinuxSetupRunner()
	runner.outputs[linuxSetupCommandKey(linuxRootHelperPath, "run-agent", "--help")] = fakeLinuxSetupOutput{err: errors.New("usage mismatch")}
	backend := linuxDiagnosticSetupBackend{
		runner:      runner,
		operation:   linuxDiagnosticSetupVerify,
		currentUser: "dr",
		projectDir:  "/work/project",
	}

	err := backend.verifyLaunchHelper()
	if err == nil || !strings.Contains(err.Error(), "does not support run-agent") {
		t.Fatalf("verifyLaunchHelper err = %v, want run-agent refusal", err)
	}
}

func TestLinuxSetupBackendVerifiesWorkspaceAccessWithSeparateModeChecks(t *testing.T) {
	runner := newFakeLinuxSetupRunner()
	backend := linuxDiagnosticSetupBackend{
		runner:     runner,
		operation:  linuxDiagnosticSetupVerify,
		projectDir: "/work/project",
	}

	if err := backend.verifyWorkspaceAccess(); err != nil {
		t.Fatalf("verifyWorkspaceAccess: %v", err)
	}

	for _, flag := range []string{"-r", "-w", "-x"} {
		runner.assertSudo(t, []string{"-u", linuxAgentUserName, "test", flag, "/work/project"})
	}
}

func TestLinuxSetupBackendRollbackRemovesPrivilegeBeforeIdentity(t *testing.T) {
	runner := newFakeLinuxSetupRunner()
	runner.outputs[linuxSetupCommandKey("getfacl", "-cp", "/work/project")] = fakeLinuxSetupOutput{
		out: "user::rwx\nuser:agent:rwx\ngroup:dev:rwx\ndefault:user:agent:rwx\ndefault:group:dev:rwx\n",
	}
	runner.outputs[linuxSetupCommandKey("id", "-u", linuxAgentUserName)] = fakeLinuxSetupOutput{out: agentUID + "\n"}
	runner.outputs[linuxSetupCommandKey("getent", "group", linuxSharedGroupName)] = fakeLinuxSetupOutput{out: "dev:x:" + sharedGID + ":dr,agent\n"}
	backend := linuxDiagnosticSetupBackend{
		runner:      runner,
		operation:   linuxDiagnosticSetupRollback,
		currentUser: "dr",
		projectDir:  "/work/project",
	}

	err := linuxsetup.RunRollbackSteps(backend.callbacks(), linuxsetup.RollbackOptions{
		DeleteToolHome:    true,
		DeleteAgentHome:   true,
		DeleteAgentUser:   true,
		DeleteSharedGroup: true,
	})
	if err != nil {
		t.Fatalf("RunRollbackSteps: %v", err)
	}

	runner.assertSudoBefore(t,
		[]string{"rm", "-f", linuxSudoersFile},
		[]string{"rm", "-f", linuxRootHelperPath},
	)
	runner.assertSudoBefore(t,
		[]string{"rm", "-f", linuxRootHelperPath},
		[]string{"rmdir", linuxCgroupRootPath},
	)
	runner.assertSudoBefore(t,
		[]string{"setfacl", "-x", "u:" + linuxAgentUserName, "-x", "g:" + linuxSharedGroupName, "/work/project"},
		[]string{"rm", "-rf", filepath.Join(linuxAgentHomePath, ".cache")},
	)
	runner.assertSudoBefore(t,
		[]string{"rm", "-rf", linuxAgentHomePath},
		[]string{"userdel", linuxAgentUserName},
	)
	runner.assertSudoBefore(t,
		[]string{"userdel", linuxAgentUserName},
		[]string{"groupdel", linuxSharedGroupName},
	)
}

func TestLinuxRollbackDryRunDoesNotCreateCallbacks(t *testing.T) {
	restoreRuntimeOS := linuxDiagnosticRuntimeOS
	restoreDryRun := flagDryRun
	restoreCallbacks := linuxDiagnosticSetupCallbacks
	defer func() {
		linuxDiagnosticRuntimeOS = restoreRuntimeOS
		flagDryRun = restoreDryRun
		linuxDiagnosticSetupCallbacks = restoreCallbacks
	}()

	linuxDiagnosticRuntimeOS = func() string { return "linux" }
	flagDryRun = true
	linuxDiagnosticSetupCallbacks = func(_ *diagnosticHostRepairBackend, _ linuxDiagnosticSetupOperation) linuxsetup.Callbacks {
		t.Fatal("Linux rollback dry-run must use DryRunRollback records, not callbacks")
		return linuxsetup.Callbacks{}
	}
	ui := &UI{DryRun: true, YesAll: true}
	if err := runLinuxRollback(ui, NewRunner(ui, false, true), false, false); err != nil {
		t.Fatalf("runLinuxRollback dry-run: %v", err)
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

type fakeLinuxSetupOutput struct {
	out string
	err error
}

type fakeLinuxSetupRunner struct {
	sudo       [][]string
	sudoErrors map[string]error
	outputs    map[string]fakeLinuxSetupOutput
	writes     map[string]string
}

func newFakeLinuxSetupRunner() *fakeLinuxSetupRunner {
	return &fakeLinuxSetupRunner{
		sudoErrors: make(map[string]error),
		outputs:    make(map[string]fakeLinuxSetupOutput),
		writes:     make(map[string]string),
	}
}

func (r *fakeLinuxSetupRunner) Sudo(_ string, args ...string) error {
	copied := append([]string{}, args...)
	r.sudo = append(r.sudo, copied)
	if err := r.sudoErrors[linuxSetupCommandKey(args...)]; err != nil {
		return err
	}
	return nil
}

func (r *fakeLinuxSetupRunner) SudoOutput(args ...string) (string, error) {
	if output, ok := r.outputs[linuxSetupCommandKey(args...)]; ok {
		return output.out, output.err
	}
	return "", errors.New("fake output not configured: " + strings.Join(args, " "))
}

func (r *fakeLinuxSetupRunner) SudoWriteFile(_ string, path, content string) error {
	r.writes[path] = content
	return nil
}

func (r *fakeLinuxSetupRunner) assertSudo(t *testing.T, want []string) {
	t.Helper()
	for _, got := range r.sudo {
		if reflect.DeepEqual(got, want) {
			return
		}
	}
	t.Fatalf("sudo calls missing %v; got %+v", want, r.sudo)
}

func (r *fakeLinuxSetupRunner) assertSudoBefore(t *testing.T, before, after []string) {
	t.Helper()
	beforeIndex := r.sudoIndex(before)
	afterIndex := r.sudoIndex(after)
	if beforeIndex == -1 || afterIndex == -1 {
		t.Fatalf("sudo calls missing before=%v after=%v; got %+v", before, after, r.sudo)
	}
	if beforeIndex >= afterIndex {
		t.Fatalf("sudo call %v index %d must precede %v index %d; got %+v", before, beforeIndex, after, afterIndex, r.sudo)
	}
}

func (r *fakeLinuxSetupRunner) sudoIndex(want []string) int {
	for i, got := range r.sudo {
		if reflect.DeepEqual(got, want) {
			return i
		}
	}
	return -1
}

func linuxSetupCommandKey(args ...string) string {
	return strings.Join(args, "\x00")
}

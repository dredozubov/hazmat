package hazmat

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const diagnosticRealHostRepairSmokeEnv = "HAZMAT_REAL_HOST_REPAIR_SMOKE"

func TestGuardedRealHostRepairSmokeDisabledByDefault(t *testing.T) {
	t.Setenv(diagnosticRealHostRepairSmokeEnv, "")
	if diagnosticRealHostRepairSmokeEnabled() {
		t.Fatalf("%s unexpectedly enabled", diagnosticRealHostRepairSmokeEnv)
	}
}

func TestGuardedRealHostRepairSmokeProducesReceipt(t *testing.T) {
	if !diagnosticRealHostRepairSmokeEnabled() {
		t.Skipf("set %s=1 to run guarded real-host repair smoke", diagnosticRealHostRepairSmokeEnv)
	}
	dir := t.TempDir()
	backend := diagnosticTempFileRepairSmokeBackend{
		path: filepath.Join(dir, "agent-umask-smoke"),
	}
	plan := planForSingleFinding(findingAgentUmask)

	executed := executeDiagnosticRepairPlan(plan, backend)
	if len(executed.AppliedReceipts) != 1 {
		t.Fatalf("receipts = %+v, want one verified receipt", executed.AppliedReceipts)
	}
	receipt := executed.AppliedReceipts[0]
	if !receipt.Verified || receipt.Action != "repair.agent-shell.umask" || receipt.RollbackBoundary != "setup.agent-shell" {
		t.Fatalf("receipt = %+v, want verified agent shell rollback receipt", receipt)
	}
	if len(executed.FailedVerifications) != 0 {
		t.Fatalf("failed verifications = %+v, want none", executed.FailedVerifications)
	}
}

func diagnosticRealHostRepairSmokeEnabled() bool {
	return os.Getenv(diagnosticRealHostRepairSmokeEnv) == "1"
}

type diagnosticTempFileRepairSmokeBackend struct {
	path string
}

func (b diagnosticTempFileRepairSmokeBackend) ApplyDiagnosticRepair(_ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) diagnosticRepairStepResult {
	if err := os.WriteFile(b.path, []byte("umask 007\n"), 0o600); err != nil {
		return diagnosticRepairStepResult{Err: err}
	}
	return diagnosticRepairStepResult{Evidence: []string{fmt.Sprintf("wrote smoke repair marker %s", b.path)}}
}

func (b diagnosticTempFileRepairSmokeBackend) VerifyDiagnosticRepair(_ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) diagnosticRepairStepResult {
	data, err := os.ReadFile(b.path)
	if err != nil {
		return diagnosticRepairStepResult{Err: err}
	}
	if string(data) != "umask 007\n" {
		return diagnosticRepairStepResult{Err: fmt.Errorf("unexpected smoke marker %q", string(data))}
	}
	return diagnosticRepairStepResult{Evidence: []string{fmt.Sprintf("verified smoke repair marker %s", b.path)}}
}

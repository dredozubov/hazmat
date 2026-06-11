package hazmat

import "fmt"

const (
	diagnosticRepairStatusRepaired     = "repaired"
	diagnosticRepairStatusStillFailing = "still-failing"
	diagnosticRepairStatusSkipped      = "skipped"
)

type diagnosticRepairBackend interface {
	ApplyDiagnosticRepair(action diagnosticRepairActionDefinition, item diagnosticRepairPlanItem) diagnosticRepairStepResult
	VerifyDiagnosticRepair(action diagnosticRepairActionDefinition, item diagnosticRepairPlanItem) diagnosticRepairStepResult
}

type diagnosticRepairStepResult struct {
	Evidence []string
	Err      error
}

func executeDiagnosticRepairPlan(plan diagnosticRepairPlan, backend diagnosticRepairBackend) diagnosticRepairPlan {
	if !plan.Execution.MutationAllowed {
		plan.Mutating = false
		return plan
	}

	plan.Mode = "executed"
	plan.Mutating = true
	plan.AppliedReceipts = []diagnosticRepairReceipt{}
	plan.FailedVerifications = []diagnosticVerificationFailure{}

	for i := range plan.Items {
		item := &plan.Items[i]
		action, ok := diagnosticRepairAction(diagnosticRepairActionID(item.RepairAction))
		if !ok {
			markDiagnosticRepairFailed(item, &plan, item.Verification, []string{fmt.Sprintf("unknown repair action %q", item.RepairAction)})
			continue
		}
		if !item.ExecutableByHazmat {
			item.Status = diagnosticRepairStatusSkipped
			item.BlockedReason = "repair action is not executable by Hazmat"
			continue
		}
		if backend == nil {
			markDiagnosticRepairFailed(item, &plan, string(action.Verification), []string{"no diagnostic repair backend configured"})
			continue
		}

		apply := backend.ApplyDiagnosticRepair(action, *item)
		item.Details = appendUnique(item.Details, apply.Evidence...)
		if apply.Err != nil {
			markDiagnosticRepairFailed(item, &plan, string(action.Verification), append(apply.Evidence, "apply failed: "+apply.Err.Error()))
			continue
		}

		verify := backend.VerifyDiagnosticRepair(action, *item)
		item.Details = appendUnique(item.Details, verify.Evidence...)
		if verify.Err != nil {
			markDiagnosticRepairFailed(item, &plan, string(action.Verification), append(verify.Evidence, "verification failed: "+verify.Err.Error()))
			continue
		}

		item.Status = diagnosticRepairStatusRepaired
		item.BlockedReason = ""
		item.Reason = "verified after action"
		plan.AppliedReceipts = append(plan.AppliedReceipts, diagnosticRepairReceipt{
			ID:       item.RepairReceipt,
			Action:   item.RepairAction,
			Verified: true,
		})
	}

	return plan
}

func markDiagnosticRepairFailed(item *diagnosticRepairPlanItem, plan *diagnosticRepairPlan, verification string, details []string) {
	item.Status = diagnosticRepairStatusStillFailing
	item.BlockedReason = "verification failed after repair attempt"
	item.Reason = "repair attempted but desired state is still not verified"
	item.Details = appendUnique(item.Details, details...)
	if verification == "" {
		verification = item.Verification
	}
	plan.FailedVerifications = append(plan.FailedVerifications, diagnosticVerificationFailure{
		Verification: verification,
		Details:      append([]string(nil), details...),
	})
}

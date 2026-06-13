package hazmat

import "strings"

type diagnosticRepairPlan struct {
	Mode                string                          `json:"mode"`
	Mutating            bool                            `json:"mutating"`
	Execution           diagnosticRepairExecutionPolicy `json:"execution"`
	Summary             diagnosticRepairPlanSummary     `json:"summary"`
	NextSteps           []diagnosticRepairNextStep      `json:"next_steps"`
	TrustBoundaries     []diagnosticRepairTrustBoundary `json:"trust_boundaries"`
	Items               []diagnosticRepairPlanItem      `json:"items"`
	ManualItems         []diagnosticRepairPlanItem      `json:"manual_items"`
	SkippedItems        []diagnosticRepairPlanItem      `json:"skipped_items"`
	AppliedReceipts     []diagnosticRepairReceipt       `json:"applied_receipts"`
	FailedVerifications []diagnosticVerificationFailure `json:"failed_verifications"`
}

type diagnosticRepairPlanSummary struct {
	Executable          int `json:"executable"`
	Manual              int `json:"manual"`
	Skipped             int `json:"skipped"`
	Applied             int `json:"applied"`
	FailedVerifications int `json:"failed_verifications"`
	RemainingExecutable int `json:"remaining_executable"`
	Remaining           int `json:"remaining"`
}

type diagnosticRepairNextStep struct {
	ID               string `json:"id"`
	Command          string `json:"command,omitempty"`
	Mutating         bool   `json:"mutating"`
	RequiresApproval bool   `json:"requires_approval,omitempty"`
	Reason           string `json:"reason"`
}

type diagnosticRepairPlanItem struct {
	Key                string   `json:"key"`
	Status             string   `json:"status"`
	Severity           string   `json:"severity"`
	FindingID          string   `json:"finding_id,omitempty"`
	ResourceID         string   `json:"resource_id,omitempty"`
	Repairability      string   `json:"repairability,omitempty"`
	Title              string   `json:"title"`
	Action             string   `json:"action"`
	SourceTrust        string   `json:"source_trust,omitempty"`
	Guardrails         []string `json:"guardrails,omitempty"`
	Authority          string   `json:"authority,omitempty"`
	Approval           string   `json:"approval,omitempty"`
	ExecutableByHazmat bool     `json:"executable_by_hazmat"`
	Privileged         bool     `json:"privileged"`
	Reversibility      string   `json:"reversibility,omitempty"`
	Preconditions      []string `json:"preconditions,omitempty"`
	TestObligations    []string `json:"test_obligations,omitempty"`
	ProofLanes         []string `json:"proof_lanes,omitempty"`
	ProofNotes         string   `json:"proof_notes,omitempty"`
	RepairAction       string   `json:"repair_action,omitempty"`
	RepairReceipt      string   `json:"repair_receipt,omitempty"`
	Verification       string   `json:"verification,omitempty"`
	RollbackBoundary   string   `json:"rollback_boundary,omitempty"`
	RollbackModel      string   `json:"rollback_model,omitempty"`
	ConsentPrompt      string   `json:"consent_prompt,omitempty"`
	SafetyRationale    string   `json:"safety_rationale,omitempty"`
	BlockedReason      string   `json:"blocked_reason,omitempty"`
	Details            []string `json:"details,omitempty"`
	Reason             string   `json:"reason,omitempty"`
}

type diagnosticRepairReceipt struct {
	ID               string   `json:"id"`
	Action           string   `json:"action"`
	ResourceID       string   `json:"resource_id,omitempty"`
	ResourceOwner    string   `json:"resource_owner,omitempty"`
	Authority        string   `json:"authority,omitempty"`
	Reversibility    string   `json:"reversibility,omitempty"`
	Verification     string   `json:"verification,omitempty"`
	RollbackBoundary string   `json:"rollback_boundary,omitempty"`
	RollbackModel    string   `json:"rollback_model,omitempty"`
	ProofLanes       []string `json:"proof_lanes,omitempty"`
	Details          []string `json:"details,omitempty"`
	Verified         bool     `json:"verified"`
	CreatedAt        string   `json:"created_at,omitempty"`
}

type diagnosticVerificationFailure struct {
	Verification string   `json:"verification"`
	Details      []string `json:"details,omitempty"`
}

type diagnosticRepairTrustBoundary struct {
	ID                string `json:"id"`
	Surface           string `json:"surface"`
	ControlledBy      string `json:"controlled_by"`
	Policy            string `json:"policy"`
	PlannerConstraint string `json:"planner_constraint"`
}

func planDiagnosticRepairs(findings []uiFinding, recommendations []uiRecommendation, execution diagnosticRepairExecutionRequest) diagnosticRepairPlan {
	plan := diagnosticRepairPlan{
		Mode:                "preview",
		Mutating:            false,
		Execution:           decideDiagnosticRepairExecution(execution),
		TrustBoundaries:     diagnosticRepairTrustBoundaries(),
		Items:               []diagnosticRepairPlanItem{},
		ManualItems:         []diagnosticRepairPlanItem{},
		SkippedItems:        []diagnosticRepairPlanItem{},
		AppliedReceipts:     []diagnosticRepairReceipt{},
		FailedVerifications: []diagnosticVerificationFailure{},
	}
	for _, rec := range recommendations {
		item := diagnosticRepairPlanItemForRecommendation(rec)
		switch rec.Definition.Repairability {
		case diagnosticRepairAuto, diagnosticRepairConsent:
			plan.Items = append(plan.Items, item)
		case diagnosticRepairManualExternal, diagnosticRepairUnsupported:
			item.Status = string(rec.Definition.Repairability)
			item.Reason = "not executable by Hazmat"
			item.BlockedReason = item.Reason
			plan.ManualItems = append(plan.ManualItems, item)
		case diagnosticRepairOptional, diagnosticRepairInformational:
			item.Status = string(rec.Definition.Repairability)
			item.Reason = "not required for containment repair"
			item.BlockedReason = item.Reason
			plan.SkippedItems = append(plan.SkippedItems, item)
		}
	}
	for _, finding := range findings {
		if finding.Typed {
			continue
		}
		reason := "missing typed diagnostic metadata"
		plan.SkippedItems = append(plan.SkippedItems, diagnosticRepairPlanItem{
			Key:           "untyped:" + finding.Message,
			Status:        "untyped",
			Severity:      finding.Severity.Label(),
			Title:         finding.Message,
			Action:        "No repair plan is available until this finding is migrated to typed diagnostic metadata.",
			SourceTrust:   "observed-untyped-text",
			Guardrails:    []string{"generated-repair-plan"},
			Authority:     string(diagnosticRepairAuthorityNone),
			Approval:      string(diagnosticRepairApprovalUnsupported),
			Details:       []string{finding.Message},
			Reason:        reason,
			BlockedReason: reason,
		})
	}
	return plan.withSummary()
}

func diagnosticRepairPlanItemForRecommendation(rec uiRecommendation) diagnosticRepairPlanItem {
	def := rec.Definition
	policy, _ := diagnosticRepairClassPolicyFor(def.Repairability)
	item := diagnosticRepairPlanItem{
		Key:                rec.Key,
		Status:             "planned",
		Severity:           rec.Severity.Label(),
		FindingID:          string(def.ID),
		ResourceID:         string(def.Resource),
		Repairability:      string(def.Repairability),
		Title:              rec.Title,
		Action:             rec.Action,
		SourceTrust:        diagnosticFindingSourceTrust(def),
		Guardrails:         diagnosticFindingGuardrailRefs(def),
		Authority:          string(policy.Authority),
		Approval:           string(policy.Approval),
		ExecutableByHazmat: policy.ExecutableByHazmat,
		Preconditions:      append([]string(nil), policy.Preconditions...),
		TestObligations:    append([]string(nil), policy.TestObligations...),
		ProofLanes:         diagnosticRepairProofLaneStrings(policy.ProofLanes),
		ProofNotes:         policy.ProofNotes,
		RepairAction:       string(def.RepairAction),
		RepairReceipt:      string(def.RepairReceipt),
		Verification:       string(def.Verification),
		RollbackBoundary:   def.RollbackBoundary,
		RollbackModel:      policy.RollbackModel,
		Details:            append([]string(nil), rec.Details...),
		SafetyRationale:    diagnosticRepairSafetyRationale(def),
	}
	if action, ok := diagnosticRepairAction(def.RepairAction); ok {
		item.Authority = string(action.Authority)
		item.Privileged = action.Privileged
		item.Reversibility = string(action.Reversibility)
		item.Preconditions = append([]string(nil), action.Preconditions...)
		item.TestObligations = append([]string(nil), action.TestObligations...)
		item.ProofLanes = diagnosticRepairProofLaneStrings(action.ProofLanes)
		item.ProofNotes = action.ProofNotes
	}
	if def.Repairability == diagnosticRepairConsent {
		item.ConsentPrompt = "Apply repair: " + def.Title + "?"
	}
	return item
}

func diagnosticRepairTrustBoundaries() []diagnosticRepairTrustBoundary {
	return []diagnosticRepairTrustBoundary{
		{
			ID:                "repo-integration-metadata",
			Surface:           ".hazmat/integrations.yaml and integration detector output",
			ControlledBy:      "repository",
			Policy:            "repo files may contribute toolchain evidence but cannot approve host mutations, credential access, or privileged repairs",
			PlannerConstraint: "repo-controlled metadata can only select typed informational or predeclared repair surfaces",
		},
		{
			ID:                "host-config-pins",
			Surface:           "host-user Hazmat config pins",
			ControlledBy:      "host user",
			Policy:            "host config may pin integration choices but does not bypass repair consent",
			PlannerConstraint: "privileged or credential-affecting repairs still require explicit repair approval",
		},
		{
			ID:                "project-path-canonicalization",
			Surface:           "project paths and generated affected-path evidence",
			ControlledBy:      "host observation with repo-influenced paths",
			Policy:            "paths are evidence only until canonicalized and checked against the repair action boundary",
			PlannerConstraint: "repair actions must use typed path boundaries, never shell fragments from findings",
		},
		{
			ID:                "generated-repair-plan",
			Surface:           "diagnostic repair plan",
			ControlledBy:      "Hazmat typed registry",
			Policy:            "repair action IDs, authority, receipts, rollback, and verification come from Hazmat code, not repo text",
			PlannerConstraint: "untyped findings are skipped and cannot synthesize executable actions",
		},
	}
}

func diagnosticFindingSourceTrust(def diagnosticFindingDefinition) string {
	switch {
	case strings.HasPrefix(string(def.Resource), "project-toolchain."):
		return "repo-observed-metadata"
	case strings.HasPrefix(string(def.Resource), "workspace."), def.Resource == "claude.project-permissions":
		return "host-observed-path"
	default:
		return "hazmat-typed-registry"
	}
}

func diagnosticFindingGuardrailRefs(def diagnosticFindingDefinition) []string {
	refs := []string{"generated-repair-plan"}
	switch {
	case strings.HasPrefix(string(def.Resource), "project-toolchain."):
		refs = append(refs, "repo-integration-metadata")
	case strings.HasPrefix(string(def.Resource), "workspace."), def.Resource == "claude.project-permissions":
		refs = append(refs, "project-path-canonicalization")
	case strings.HasPrefix(string(def.Resource), "credential."):
		refs = append(refs, "host-config-pins")
	}
	return refs
}

func diagnosticRepairSafetyRationale(def diagnosticFindingDefinition) string {
	switch def.Repairability {
	case diagnosticRepairAuto:
		return "Hazmat may apply this only because the typed governance policy allows non-privileged idempotent repair with receipt and verification."
	case diagnosticRepairConsent:
		return "Hazmat can plan this repair because the finding names a typed action, receipt, verification target, rollback boundary, and explicit consent requirement."
	case diagnosticRepairManualExternal:
		return "Hazmat cannot execute this repair because the required authority belongs to an external system owner."
	case diagnosticRepairUnsupported:
		return "Hazmat cannot execute this repair until a supported backend adapter or repair action exists."
	case diagnosticRepairOptional:
		return "Hazmat skips this during containment repair because it is an optional workflow capability."
	case diagnosticRepairInformational:
		return "Hazmat skips this because it is informational and requires no mutation."
	default:
		return "Hazmat cannot plan this repair because the repairability class is unknown."
	}
}

func (plan diagnosticRepairPlan) withSummary() diagnosticRepairPlan {
	plan.Summary = diagnosticRepairPlanSummaryFor(plan)
	plan.Execution.Examples = diagnosticRepairExecutionExamplesForPlan(plan)
	plan.NextSteps = diagnosticRepairNextStepsFor(plan)
	return plan
}

func diagnosticRepairPlanSummaryFor(plan diagnosticRepairPlan) diagnosticRepairPlanSummary {
	summary := diagnosticRepairPlanSummary{
		Executable:          len(plan.Items),
		Manual:              len(plan.ManualItems),
		Skipped:             len(plan.SkippedItems),
		Applied:             len(plan.AppliedReceipts),
		FailedVerifications: len(plan.FailedVerifications),
	}
	for _, item := range plan.Items {
		if item.Status != diagnosticRepairStatusRepaired {
			summary.RemainingExecutable++
		}
	}
	summary.Remaining = summary.RemainingExecutable + summary.Manual + summary.Skipped
	return summary
}

func diagnosticRepairExecutionExamplesForPlan(plan diagnosticRepairPlan) []string {
	examples := append([]string(nil), plan.Execution.Examples...)
	if plan.Summary.FailedVerifications > 0 {
		return filterMutatingDiagnosticExamples(examples)
	}
	if plan.Summary.RemainingExecutable > 0 || plan.Summary.Remaining == 0 {
		return examples
	}
	return filterMutatingDiagnosticExamples(examples)
}

func filterMutatingDiagnosticExamples(examples []string) []string {
	var filtered []string
	for _, example := range examples {
		if strings.HasPrefix(example, "hazmat doctor --fix") {
			continue
		}
		filtered = append(filtered, example)
	}
	return filtered
}

func diagnosticRepairNextStepsFor(plan diagnosticRepairPlan) []diagnosticRepairNextStep {
	var steps []diagnosticRepairNextStep
	addApplyStep := func(command, reason string) {
		steps = append(steps, diagnosticRepairNextStep{
			ID:               "apply-approved-repairs",
			Command:          command,
			Mutating:         true,
			RequiresApproval: true,
			Reason:           reason,
		})
	}
	addDryRunStep := func() {
		steps = append(steps, diagnosticRepairNextStep{
			ID:       "preview-repair-plan",
			Command:  "hazmat doctor --dry-run",
			Mutating: false,
			Reason:   "Preview the typed repair plan without applying host mutations.",
		})
	}
	addInspectRemainingStep := func() {
		steps = append(steps, diagnosticRepairNextStep{
			ID:       "inspect-remaining-items",
			Mutating: false,
			Reason:   "Review manual, optional, unsupported, or informational findings; no executable Hazmat repair is available for them.",
		})
	}

	switch plan.Execution.Mode {
	case "read-only":
		if plan.Summary.RemainingExecutable > 0 {
			addApplyStep("hazmat doctor --fix", "Apply executable Hazmat repairs after approval.")
			addDryRunStep()
		} else if plan.Summary.Remaining > 0 {
			addInspectRemainingStep()
		}
	case "post-init-verify":
		if plan.Summary.RemainingExecutable > 0 {
			addApplyStep("hazmat doctor --fix", "Repair post-init verification findings without rerunning init.")
			addDryRunStep()
		} else if plan.Summary.Remaining > 0 {
			addInspectRemainingStep()
		}
	case "plan-only", "dry-run":
		if plan.Summary.RemainingExecutable > 0 {
			addApplyStep("hazmat doctor --fix", "Apply the reviewed repair plan after approval.")
		} else if plan.Summary.Remaining > 0 {
			addInspectRemainingStep()
		}
	case "blocked-noninteractive":
		if plan.Summary.RemainingExecutable > 0 {
			addApplyStep("hazmat doctor --fix --yes", "Allow non-interactive repair execution for the approved plan.")
		} else if plan.Summary.Remaining > 0 {
			addInspectRemainingStep()
		}
	case "declined":
		if plan.Summary.RemainingExecutable > 0 {
			addApplyStep("hazmat doctor --fix", "Rerun the repair plan and approve execution.")
		} else if plan.Summary.Remaining > 0 {
			addInspectRemainingStep()
		}
	case "fix-yes", "fix-interactive":
		if plan.Summary.FailedVerifications > 0 {
			steps = append(steps, diagnosticRepairNextStep{
				ID:       "inspect-failed-verifications",
				Mutating: false,
				Reason:   "Inspect failed verification evidence before retrying the same repair.",
			})
			return steps
		}
		if plan.Summary.Remaining > 0 {
			steps = append(steps, diagnosticRepairNextStep{
				ID:       "inspect-remaining-items",
				Mutating: false,
				Reason:   "Review manual, optional, or informational findings that remain outside executable repair.",
			})
			return steps
		}
		steps = append(steps, diagnosticRepairNextStep{
			ID:       "verify-host",
			Command:  "hazmat check --full",
			Mutating: false,
			Reason:   "Verify the host after approved repairs.",
		})
	}

	return steps
}

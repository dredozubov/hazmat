package hazmat

type diagnosticRepairPlan struct {
	Mode                string                          `json:"mode"`
	Mutating            bool                            `json:"mutating"`
	Items               []diagnosticRepairPlanItem      `json:"items"`
	ManualItems         []diagnosticRepairPlanItem      `json:"manual_items"`
	SkippedItems        []diagnosticRepairPlanItem      `json:"skipped_items"`
	AppliedReceipts     []diagnosticRepairReceipt       `json:"applied_receipts"`
	FailedVerifications []diagnosticVerificationFailure `json:"failed_verifications"`
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
	Authority          string   `json:"authority,omitempty"`
	Approval           string   `json:"approval,omitempty"`
	ExecutableByHazmat bool     `json:"executable_by_hazmat"`
	Privileged         bool     `json:"privileged"`
	Reversibility      string   `json:"reversibility,omitempty"`
	Preconditions      []string `json:"preconditions,omitempty"`
	TestObligations    []string `json:"test_obligations,omitempty"`
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
	ID        string `json:"id"`
	Action    string `json:"action"`
	Verified  bool   `json:"verified"`
	CreatedAt string `json:"created_at,omitempty"`
}

type diagnosticVerificationFailure struct {
	Verification string   `json:"verification"`
	Details      []string `json:"details,omitempty"`
}

func planDiagnosticRepairs(findings []uiFinding, recommendations []uiRecommendation) diagnosticRepairPlan {
	plan := diagnosticRepairPlan{
		Mode:                "preview",
		Mutating:            false,
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
			Authority:     string(diagnosticRepairAuthorityNone),
			Approval:      string(diagnosticRepairApprovalUnsupported),
			Details:       []string{finding.Message},
			Reason:        reason,
			BlockedReason: reason,
		})
	}
	return plan
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
		Authority:          string(policy.Authority),
		Approval:           string(policy.Approval),
		ExecutableByHazmat: policy.ExecutableByHazmat,
		Preconditions:      append([]string(nil), policy.Preconditions...),
		TestObligations:    append([]string(nil), policy.TestObligations...),
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
	}
	if def.Repairability == diagnosticRepairConsent {
		item.ConsentPrompt = "Apply repair: " + def.Title + "?"
	}
	return item
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

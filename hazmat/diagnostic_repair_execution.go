package hazmat

type diagnosticRepairExecutionRequest struct {
	Command     string
	Fix         bool
	YesAll      bool
	Interactive bool
	DryRun      bool
}

type diagnosticRepairExecutionPolicy struct {
	Command                    string   `json:"command"`
	Mode                       string   `json:"mode"`
	MutationAllowed            bool     `json:"mutation_allowed"`
	RequiresFix                bool     `json:"requires_fix"`
	RequiresYes                bool     `json:"requires_yes"`
	RequiresInteractiveConsent bool     `json:"requires_interactive_consent"`
	Reason                     string   `json:"reason"`
	Examples                   []string `json:"examples"`
}

func decideDiagnosticRepairExecution(req diagnosticRepairExecutionRequest) diagnosticRepairExecutionPolicy {
	command := req.Command
	if command == "" {
		command = "check"
	}

	switch {
	case command == "check":
		return diagnosticRepairExecutionPolicy{
			Command:         command,
			Mode:            "read-only",
			MutationAllowed: false,
			RequiresFix:     false,
			RequiresYes:     false,
			Reason:          "hazmat check is a read-only health and repairability report",
			Examples:        []string{"hazmat check", "hazmat doctor --fix", "hazmat doctor --dry-run"},
		}
	case command == "init":
		return diagnosticRepairExecutionPolicy{
			Command:         command,
			Mode:            "post-init-verify",
			MutationAllowed: false,
			RequiresFix:     false,
			RequiresYes:     false,
			Reason:          "hazmat init has already run setup; remaining findings are post-init verification blockers, not advice to rerun init",
			Examples:        []string{"hazmat doctor --dry-run", "hazmat doctor --fix --yes", "hazmat check --full"},
		}
	case req.DryRun:
		return diagnosticRepairExecutionPolicy{
			Command:         command,
			Mode:            "dry-run",
			MutationAllowed: false,
			RequiresFix:     false,
			RequiresYes:     false,
			Reason:          "global --dry-run previews the typed repair plan and never executes repairs, even when --fix is also supplied",
			Examples:        []string{"hazmat doctor --dry-run", "hazmat doctor --fix"},
		}
	case !req.Fix:
		return diagnosticRepairExecutionPolicy{
			Command:         command,
			Mode:            "plan-only",
			MutationAllowed: false,
			RequiresFix:     true,
			RequiresYes:     false,
			Reason:          "doctor without --fix diagnoses drift and shows the repair plan without applying it",
			Examples:        []string{"hazmat doctor --dry-run", "hazmat doctor --dry-run --json", "hazmat doctor --fix"},
		}
	case req.YesAll:
		return diagnosticRepairExecutionPolicy{
			Command:         command,
			Mode:            "fix-yes",
			MutationAllowed: true,
			RequiresFix:     true,
			RequiresYes:     true,
			Reason:          "non-interactive mutation is allowed only when the caller explicitly supplied both --fix and --yes",
			Examples:        []string{"hazmat doctor --fix --yes", "hazmat doctor --fix --yes --json"},
		}
	case req.Interactive:
		return diagnosticRepairExecutionPolicy{
			Command:                    command,
			Mode:                       "fix-interactive",
			MutationAllowed:            true,
			RequiresFix:                true,
			RequiresInteractiveConsent: true,
			Reason:                     "interactive mutation requires --fix and per-plan consent before execution",
			Examples:                   []string{"hazmat doctor --fix"},
		}
	default:
		return diagnosticRepairExecutionPolicy{
			Command:         command,
			Mode:            "blocked-noninteractive",
			MutationAllowed: false,
			RequiresFix:     true,
			RequiresYes:     true,
			Reason:          "non-interactive mutation is blocked unless --yes is supplied with --fix",
			Examples:        []string{"hazmat doctor --fix --yes"},
		}
	}
}

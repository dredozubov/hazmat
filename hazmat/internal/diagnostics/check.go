package diagnostics

import "github.com/spf13/cobra"

type CheckOptions struct {
	Command string
	Quick   bool
	JSON    bool
	Fix     bool
}

type CheckRunner func(CheckOptions) error

func NewCheckCommand(run CheckRunner) *cobra.Command {
	return newCheckCommand("check", "Verify setup and show a read-only repairability report", `Runs the verification suite to check that containment is correctly configured.

By default runs quick checks: no sudo-adjacent launch-helper validation, no
helper-backed probes, no backup smoke tests, and no external traffic. Use --full
to include launch-helper validation plus helper-backed, backup, and cloud live
probes that can send external traffic and are sudo-adjacent in agent workflows.
Failures and warnings are summarized as a read-only health and repairability
report.

When executable typed repairs are planned, run hazmat doctor --fix. To preview
the typed repair plan explicitly, run hazmat doctor --dry-run.`, `  hazmat check
  hazmat doctor --fix
  hazmat doctor --dry-run
  hazmat check --full`, false, run)
}

func NewDoctorCommand(run CheckRunner) *cobra.Command {
	return newCheckCommand("doctor", "Diagnose setup and show the typed repair plan", `Runs the same diagnostic suite as hazmat check and ends with the typed
repair plan for any failures or warnings.

By default runs quick checks: no sudo-adjacent launch-helper validation, no
helper-backed probes, no backup smoke tests, and no external traffic. Use --full
to include launch-helper validation plus helper-backed, backup, and cloud live
probes that can send external traffic and are sudo-adjacent in agent workflows.

Use hazmat doctor --dry-run when you want to spell out non-mutating preview
behavior. Plain doctor remains compatible and plan-only. Mutation requires
--fix; non-interactive mutation requires both --fix and --yes.`, `  hazmat doctor --dry-run
  hazmat doctor --dry-run --json
  hazmat doctor --fix
  hazmat doctor --fix --yes`, true, run)
}

func newCheckCommand(use, short, long, example string, allowFix bool, run CheckRunner) *cobra.Command {
	var full bool
	var jsonOutput bool
	var fix bool
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		Example: example,
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if run == nil {
				return nil
			}
			return run(CheckOptions{Command: use, Quick: !full, JSON: jsonOutput, Fix: fix})
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "Include launch-helper validation plus helper-backed, backup, and cloud live probes (can send external traffic; sudo-adjacent in agent workflows)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable diagnostic report JSON")
	if allowFix {
		cmd.Flags().BoolVar(&fix, "fix", false, "Apply the typed repair plan after diagnosis")
	}
	return cmd
}

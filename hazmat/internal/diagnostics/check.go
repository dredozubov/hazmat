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

By default runs quick checks (no network traffic). Use --full to include
live network probes that verify firewall rules are active. Failures and warnings
are summarized as a read-only health and repairability report.`, false, run)
}

func NewDoctorCommand(run CheckRunner) *cobra.Command {
	return newCheckCommand("doctor", "Diagnose setup and show the typed repair plan", `Runs the same diagnostic suite as hazmat check and ends with the typed
repair plan for any failures or warnings.

By default runs quick checks (no network traffic). Use --full to include
live network probes that verify firewall rules are active.

Plain doctor is plan-only. Mutation requires --fix; non-interactive mutation
requires both --fix and --yes.`, true, run)
}

func newCheckCommand(use, short, long string, allowFix bool, run CheckRunner) *cobra.Command {
	var full bool
	var jsonOutput bool
	var fix bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if run == nil {
				return nil
			}
			return run(CheckOptions{Command: use, Quick: !full, JSON: jsonOutput, Fix: fix})
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "Include live network probes (sends external traffic)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable diagnostic report JSON")
	if allowFix {
		cmd.Flags().BoolVar(&fix, "fix", false, "Apply the typed repair plan after diagnosis")
	}
	return cmd
}

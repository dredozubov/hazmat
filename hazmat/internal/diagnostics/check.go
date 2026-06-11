package diagnostics

import "github.com/spf13/cobra"

type CheckOptions struct {
	Quick bool
	JSON  bool
}

type CheckRunner func(CheckOptions) error

func NewCheckCommand(run CheckRunner) *cobra.Command {
	return newCheckCommand("check", "Verify setup and show remediation guidance", `Runs the verification suite to check that containment is correctly configured.

By default runs quick checks (no network traffic). Use --full to include
live network probes that verify firewall rules are active. Failures and warnings
are summarized as recommended next actions at the end.`, run)
}

func NewDoctorCommand(run CheckRunner) *cobra.Command {
	return newCheckCommand("doctor", "Diagnose setup and recommend repairs", `Runs the same diagnostic suite as hazmat check and ends with a focused
remediation list for any failures or warnings.

By default runs quick checks (no network traffic). Use --full to include
live network probes that verify firewall rules are active.`, run)
}

func newCheckCommand(use, short, long string, run CheckRunner) *cobra.Command {
	var full bool
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if run == nil {
				return nil
			}
			return run(CheckOptions{Quick: !full, JSON: jsonOutput})
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "Include live network probes (sends external traffic)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable diagnostic report JSON")
	return cmd
}

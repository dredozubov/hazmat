package diagnostics

import "github.com/spf13/cobra"

type CheckRunner func(quick bool) error

func NewCheckCommand(run CheckRunner) *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Verify the setup is working",
		Long: `Runs the verification suite to check that containment is correctly configured.

By default runs quick checks (no network traffic). Use --full to include
live network probes that verify firewall rules are active.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if run == nil {
				return nil
			}
			return run(!full)
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "Include live network probes (sends external traffic)")
	return cmd
}

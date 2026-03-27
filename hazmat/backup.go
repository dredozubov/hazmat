package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// backupBuiltinExcludes are universal build artifacts always excluded from
// snapshots. These are reproducible from source and safe to omit.
var backupBuiltinExcludes = []string{
	"node_modules/",
	".venv/",
	"venv/",
	"__pycache__/",
	".next/",
	"dist/",
	"build/",
	"target/",
	".nix-*",
	".DS_Store",
	"*.pyc",
}

func newBackupCmd() *cobra.Command {
	var cloudMode bool
	cmd := &cobra.Command{
		Use:   "backup [--cloud]",
		Short: "Back up the workspace to cloud storage (Kopia)",
		Long: `Backs up the canonical workspace root (` + sharedWorkspace + `) to S3-compatible
cloud storage using Kopia. Snapshots are encrypted, deduplicated, and
incremental.

Configure cloud credentials first:
  hazmat init cloud

Local project snapshots happen automatically before every session
(hazmat claude/exec/shell). Use 'hazmat snapshots' to manage them.

Examples:
  hazmat backup --cloud      Encrypted snapshot to S3`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if !cloudMode {
				return fmt.Errorf("specify --cloud for S3 backup\n\n" +
					"Local project snapshots happen automatically before each session.\n" +
					"Use 'hazmat snapshots' to list them, 'hazmat restore' to roll back.")
			}
			return runCloudBackup()
		},
	}
	cmd.Flags().BoolVar(&cloudMode, "cloud", false,
		"Perform incremental encrypted backup to cloud (requires 'hazmat init cloud')")
	return cmd
}

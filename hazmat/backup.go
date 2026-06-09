package hazmat

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
	var project string
	cmd := &cobra.Command{
		Use:   "backup [--cloud]",
		Short: "Back up the current project to cloud storage (Kopia)",
		Long: `Backs up the selected project directory to S3-compatible cloud storage using
Kopia. Snapshots are encrypted, deduplicated, and incremental.

Configure cloud credentials first:
  hazmat init cloud

Cloud configuration alone does not write S3 objects. The bucket remains empty
until this command successfully creates the first encrypted snapshot.

Local project snapshots happen automatically before every session
(hazmat claude/exec/shell). Cloud snapshots are explicit and project-scoped.
Use 'hazmat snapshots' to manage local snapshots.

Examples:
  hazmat backup --cloud
  hazmat backup --cloud -C ~/workspace/my-app`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runBackupCommand(cloudMode, project, runCloudBackup)
		},
	}
	cmd.Flags().BoolVar(&cloudMode, "cloud", false,
		"Perform incremental encrypted backup to cloud (requires 'hazmat init cloud')")
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory to back up (default: current directory)")
	return cmd
}

func runBackupCommand(cloudMode bool, project string, cloudBackup func(string) error) error {
	if !cloudMode {
		return fmt.Errorf("specify --cloud for S3 backup\n\n" +
			"Local project snapshots happen automatically before each session.\n" +
			"Use 'hazmat snapshots' to list them, 'hazmat restore' to roll back.")
	}
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}
	return cloudBackup(projectDir)
}

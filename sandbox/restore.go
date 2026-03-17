package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	var syncMode bool
	cmd := &cobra.Command{
		Use:   "restore <source>",
		Short: "Restore the workspace root from a backup",
		Long: `Restores files from a backup into the canonical workspace root (` + sharedWorkspace + `).

Uses rsync in reverse (backup → workspace). By default, restore is additive:
no workspace files are deleted. Use --sync for a full mirror that removes
workspace-only files.

The backup source must contain a ` + backupTargetMarker + ` marker file to prevent
accidental restores from wrong paths.

Examples:
  sandbox restore /Volumes/BACKUP/workspace
  sandbox restore --sync /Volumes/BACKUP/workspace
  sandbox restore --dry-run /Volumes/BACKUP/workspace
  sandbox restore user@nas:/backup/workspace`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runRestore(syncMode, args[0])
		},
	}
	cmd.Flags().BoolVar(&syncMode, "sync", false,
		"Mirror mode: delete workspace-only files (full restore)")
	return cmd
}

func runRestore(syncMode bool, src string) error {
	if _, err := os.Stat(sharedWorkspace); err != nil {
		return fmt.Errorf("workspace root %q not found: %w\nRun 'sandbox setup' first.", sharedWorkspace, err)
	}

	if err := validateRestoreSrc(src); err != nil {
		return err
	}

	// Ensure trailing slash on src so rsync copies contents, not the directory itself.
	if !strings.HasSuffix(src, "/") && !strings.Contains(src, ":") {
		src += "/"
	}

	dest := sharedWorkspace + "/"

	fmt.Printf("Source:      %s\n", src)
	fmt.Printf("Destination: %s\n", dest)
	if syncMode {
		fmt.Println("Mode:        SYNC — workspace-only files will be deleted")
	} else {
		fmt.Println("Mode:        safe (additive, no deletions)")
	}
	fmt.Println()

	rsyncArgs := []string{"-aHAX", "--progress"}
	if syncMode {
		rsyncArgs = append(rsyncArgs, "--delete")
	}
	rsyncArgs = append(rsyncArgs, src, dest)

	ui := &UI{DryRun: flagDryRun}
	r := NewRunner(ui, flagVerbose, flagDryRun)
	return r.Interactive("rsync", rsyncArgs...)
}

// validateRestoreSrc checks that src is a valid, initialized backup.
// For local paths: src must exist and contain a backupTargetMarker.
// For remote paths (containing ":"): local checks are skipped.
func validateRestoreSrc(src string) error {
	if strings.Contains(src, ":") {
		return nil
	}

	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf(
			"restore source %q does not exist or is not mounted\n"+
				"Mount the volume or check the path.",
			src,
		)
	}

	marker := filepath.Join(src, backupTargetMarker)
	if _, err := os.Stat(marker); err != nil {
		return fmt.Errorf(
			"restore source %q is not an initialized backup target\n"+
				"Missing sentinel file: %s\n"+
				"This check prevents restoring from wrong paths.\n"+
				"If this is a valid backup, initialize it:\n"+
				"  touch %s",
			src, marker, marker,
		)
	}

	return nil
}

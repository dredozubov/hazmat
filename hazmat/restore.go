package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	var syncMode, cloudMode bool
	cmd := &cobra.Command{
		Use:   "restore [--cloud] [<source>]",
		Short: "Restore the workspace root from a backup",
		Long: `Restores files from a backup into the canonical workspace root (` + sharedWorkspace + `).

Local restores use rsync (backup → workspace). By default, restore is additive:
no workspace files are deleted. Use --sync for a full mirror that removes
workspace-only files.

Local backup source must contain a ` + backupTargetMarker + ` marker file to prevent
accidental restores from wrong paths.

Cloud restores (--cloud) use Kopia to restore from the latest snapshot in S3.

Examples:
  hazmat restore /Volumes/BACKUP/workspace
  hazmat restore --sync /Volumes/BACKUP/workspace
  hazmat restore --cloud`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(_ *cobra.Command, args []string) error {
			if cloudMode {
				return runCloudRestore()
			}
			if len(args) != 1 {
				return fmt.Errorf("source required (or use --cloud for S3 restore)")
			}
			return runRestore(syncMode, args[0])
		},
	}
	cmd.Flags().BoolVar(&syncMode, "sync", false,
		"Mirror mode (local only): delete workspace-only files (full restore)")
	cmd.Flags().BoolVar(&cloudMode, "cloud", false,
		"Restore latest snapshot from cloud storage")
	return cmd
}

func runRestore(syncMode bool, src string) error {
	if _, err := os.Stat(sharedWorkspace); err != nil {
		return fmt.Errorf("workspace root %q not found: %w\nRun 'hazmat setup' first.", sharedWorkspace, err)
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

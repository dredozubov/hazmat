package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	var cloudMode bool
	var sessionIdx int
	cmd := &cobra.Command{
		Use:   "restore [--cloud | --session=N]",
		Short: "Restore project from snapshot or workspace from cloud",
		Long: `Without flags, restores the current project directory from the most recent
local snapshot (taken automatically before each session).

Current state is snapshotted first ("pre-restore") so the restore is
reversible.

Use --session=N to restore to N sessions ago (default: 1, the most recent).
Use --cloud to restore the entire workspace from the latest cloud snapshot.

Examples:
  hazmat restore              Restore project to pre-last-session state
  hazmat restore --session=3  Restore project to 3 sessions ago
  hazmat restore --cloud      Restore workspace from S3`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if cloudMode {
				return runCloudRestore()
			}
			return runProjectRestore(sessionIdx)
		},
	}
	cmd.Flags().BoolVar(&cloudMode, "cloud", false,
		"Restore entire workspace from latest cloud snapshot")
	cmd.Flags().IntVar(&sessionIdx, "session", 1,
		"Which snapshot to restore (1 = most recent, 2 = second most recent, ...)")
	return cmd
}

func runProjectRestore(sessionIdx int) error {
	ctx := context.Background()

	projectDir, err := resolveDir("", true)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}

	r, err := openLocalRepo(ctx)
	if err != nil {
		return fmt.Errorf("open snapshot repository: %w", err)
	}
	defer r.Close(ctx)

	snaps, err := listSnapshots(ctx, r, projectDir)
	if err != nil || len(snaps) == 0 {
		return fmt.Errorf("no snapshots found for %s\nRun a session first (hazmat claude/exec/shell) to create one.", projectDir)
	}

	if sessionIdx < 1 || sessionIdx > len(snaps) {
		return fmt.Errorf("--session=%d out of range (have %d snapshots)\nUse 'hazmat snapshots' to see available snapshots.", sessionIdx, len(snaps))
	}

	// Snapshots are newest-last; --session=1 means the last element.
	target := snaps[len(snaps)-sessionIdx]

	fmt.Printf("Restore %s to snapshot from %v?\n", projectDir, target.StartTime.ToTime().Local().Format("2006-01-02 15:04:05"))
	fmt.Printf("  Description: %s\n", target.Description)
	fmt.Println("  Current state will be snapshotted first (recoverable).")
	fmt.Println()

	if !flagYesAll {
		ui := &UI{}
		if !ui.Ask("Proceed with restore?") {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	// Snapshot current state before restoring so "undo the undo" is possible.
	fmt.Print("  Snapshotting current state... ")
	if err := snapshotProject(projectDir, "pre-restore"); err != nil {
		fmt.Fprintf(os.Stderr, "\n  Warning: could not snapshot current state: %v\n", err)
		fmt.Fprintln(os.Stderr, "  Proceeding with restore — current state may not be recoverable.")
	} else {
		fmt.Println("done")
	}

	fmt.Print("  Restoring... ")
	stats, err := restoreSnapshotTo(ctx, r, target, projectDir)
	if err != nil {
		return err
	}

	fmt.Printf("done (%d files, %s)\n",
		stats.RestoredFileCount,
		formatBytes(stats.RestoredTotalFileSize))
	return nil
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return strconv.FormatFloat(float64(b)/float64(1<<30), 'f', 1, 64) + " GB"
	case b >= 1<<20:
		return strconv.FormatFloat(float64(b)/float64(1<<20), 'f', 1, 64) + " MB"
	case b >= 1<<10:
		return strconv.FormatFloat(float64(b)/float64(1<<10), 'f', 1, 64) + " KB"
	default:
		return strconv.FormatInt(b, 10) + " B"
	}
}

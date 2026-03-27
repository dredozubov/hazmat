package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kopia/kopia/snapshot/restore"
	"github.com/spf13/cobra"
)

func newSnapshotsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "snapshots",
		Short: "List local snapshots for the current project",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runListSnapshots()
		},
	}
}

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Show changes since the last snapshot",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDiffSnapshot()
		},
	}
}

func runListSnapshots() error {
	ctx := context.Background()

	projectDir, err := resolveDir("", true)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}

	r, err := openLocalRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close(ctx)

	snaps, err := listSnapshots(ctx, r, projectDir)
	if err != nil || len(snaps) == 0 {
		fmt.Printf("No snapshots for %s\n", projectDir)
		fmt.Println("Snapshots are created automatically before each session (hazmat claude/exec/shell).")
		return nil
	}

	fmt.Printf("Project: %s\n\n", projectDir)
	fmt.Printf("  %-4s  %-20s  %s\n", "#", "Timestamp", "Description")
	for i := len(snaps) - 1; i >= 0; i-- {
		idx := len(snaps) - i
		s := snaps[i]
		ts := s.StartTime.ToTime().Local().Format("2006-01-02 15:04:05")
		fmt.Printf("  %-4d  %-20s  %s\n", idx, ts, s.Description)
	}
	fmt.Printf("\nUse 'hazmat restore --session=N' to restore to a snapshot.\n")
	return nil
}

func runDiffSnapshot() error {
	ctx := context.Background()

	projectDir, err := resolveDir("", true)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}

	r, err := openLocalRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close(ctx)

	snaps, err := listSnapshots(ctx, r, projectDir)
	if err != nil || len(snaps) == 0 {
		return fmt.Errorf("no snapshots found for %s", projectDir)
	}

	latest := snaps[len(snaps)-1]
	fmt.Printf("Comparing %s against snapshot from %v\n\n",
		projectDir, latest.StartTime.ToTime().Local().Format("2006-01-02 15:04:05"))

	// Restore snapshot to a temp directory, then diff.
	tmpDir, err := os.MkdirTemp("", "hazmat-diff-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	restoreDir := filepath.Join(tmpDir, "snapshot")
	if err := os.MkdirAll(restoreDir, 0o700); err != nil {
		return fmt.Errorf("create restore dir: %w", err)
	}

	output := &restore.FilesystemOutput{
		TargetPath:           restoreDir,
		OverwriteFiles:       true,
		OverwriteDirectories: true,
	}
	if err := output.Init(ctx); err != nil {
		return fmt.Errorf("init restore: %w", err)
	}

	_, err = restoreSnapshotTo(ctx, r, latest, restoreDir)
	if err != nil {
		return fmt.Errorf("restore for diff: %w", err)
	}

	// Use system diff for readable output.
	cmd := exec.Command("diff", "-rq",
		"--exclude=.git",
		"--exclude=node_modules",
		"--exclude=.venv",
		"--exclude=.DS_Store",
		restoreDir, projectDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()

	// diff exits 1 when files differ — that's expected, not an error.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil
		}
		return err
	}

	fmt.Println("No changes since last snapshot.")
	return nil
}

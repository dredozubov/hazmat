package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// backupTargetMarker is the sentinel file that must exist inside a local
// backup destination before --sync (destructive mode) is allowed.
// Create it once: touch /Volumes/BACKUP/workspace/.backup-target
const backupTargetMarker = ".backup-target"

// backupExcludesFile is the user-editable file listing repos and paths to
// omit from backup.  Edit this file to change backup scope; the effective
// scope is always printed before rsync runs, and can be inspected with
// sandbox backup --show-scope.
const backupExcludesFile = sharedWorkspace + "/.backup-excludes"

// backupBuiltinExcludes are universal build artifacts always excluded.
// These are not user-configurable because they are safe to omit from any
// workspace backup (they are reproducible from source).
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

// defaultBackupExcludesContent is the template written to backupExcludesFile
// during setup.  Users should edit this file to list large or re-cloneable
// repos they do not want backed up.
const defaultBackupExcludesContent = `# Backup exclude patterns for ` + sharedWorkspace + `
# Format: one rsync pattern per line (same as rsync --exclude).
# Lines beginning with # are comments; blank lines are ignored.
#
# Edit this file to control what is excluded from backup.
# To verify the effective scope before running: sandbox backup --show-scope
#
# Add top-level directories you do not need backed up, for example:
# /nixpkgs/
# /bitcoin/
# /my-big-archive/
`

func newBackupCmd() *cobra.Command {
	var syncMode, showScope bool
	cmd := &cobra.Command{
		Use:   "backup [--show-scope | <destination>]",
		Short: "Back up the shared workspace to destination using rsync",
		Long: `Backs up the canonical shared workspace (` + sharedWorkspace + `) to the given
destination using rsync.  Always uses the shared workspace path regardless of
which user invokes the command.

Exclude rules come from two sources (printed before each run):
  1. Built-in excludes: universal build artifacts (node_modules/, .venv/, etc.)
  2. User excludes:     ` + backupExcludesFile + `
     Edit this file to add or remove repos from backup scope.

By default, backup is additive: no files are deleted from the destination.
Use --sync for a full mirror that removes destination-only files; the
destination must first be initialized with a ` + backupTargetMarker + ` marker file.

Use --show-scope to inspect effective includes/excludes without running rsync.

Examples:
  sandbox backup --show-scope
  sandbox backup /Volumes/BACKUP/workspace
  sandbox backup user@nas:/backup/workspace

  # One-time: initialize a local destination for --sync
  touch /Volumes/BACKUP/workspace/` + backupTargetMarker + `
  sandbox backup --sync /Volumes/BACKUP/workspace`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(_ *cobra.Command, args []string) error {
			if showScope {
				return printBackupScope()
			}
			if len(args) != 1 {
				return fmt.Errorf("destination required (or use --show-scope to inspect scope without running)")
			}
			return runBackup(syncMode, args)
		},
	}
	cmd.Flags().BoolVar(&syncMode, "sync", false,
		"Mirror mode: delete destination-only files (requires "+backupTargetMarker+" marker in destination)")
	cmd.Flags().BoolVar(&showScope, "show-scope", false,
		"Print effective backup scope (built-in and user excludes) then exit without running rsync")
	return cmd
}

// printBackupScope prints effective includes and excludes to stdout so the
// user can verify backup scope before committing to a run or restore.
func printBackupScope() error {
	fmt.Printf("Backup source: %s\n\n", sharedWorkspace)

	fmt.Println("Built-in excludes (always applied — universal build artifacts):")
	for _, e := range backupBuiltinExcludes {
		fmt.Printf("  --exclude=%s\n", e)
	}

	fmt.Printf("\nUser excludes file: %s\n", backupExcludesFile)
	userExcludes, err := loadUserExcludes()
	if err != nil {
		fmt.Println("  (file not found — no user-specific excludes)")
		fmt.Printf("  To create it, run: sandbox setup\n")
		fmt.Printf("  Or manually: cp /dev/null %s\n", backupExcludesFile)
	} else if len(userExcludes) == 0 {
		fmt.Println("  (file exists but contains no active exclude patterns)")
	} else {
		for _, e := range userExcludes {
			fmt.Printf("  --exclude=%s\n", e)
		}
	}
	return nil
}

// loadUserExcludes reads non-comment, non-empty lines from backupExcludesFile.
func loadUserExcludes() ([]string, error) {
	f, err := os.Open(backupExcludesFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var result []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		result = append(result, line)
	}
	return result, sc.Err()
}

func runBackup(syncMode bool, args []string) error {
	dest := args[0]
	src := sharedWorkspace + "/"

	if _, err := os.Stat(sharedWorkspace); err != nil {
		return fmt.Errorf("shared workspace %q not found: %w", sharedWorkspace, err)
	}

	if syncMode {
		if err := validateSyncDest(dest); err != nil {
			return err
		}
	}

	fmt.Printf("Source:      %s\n", src)
	fmt.Printf("Destination: %s\n", dest)
	if syncMode {
		fmt.Println("Mode:        SYNC — destination-only files will be deleted")
	} else {
		fmt.Println("Mode:        safe (additive, no deletions)")
	}

	// Print effective scope so omissions are visible before restore.
	fmt.Printf("Scope file:  %s", backupExcludesFile)
	if _, err := os.Stat(backupExcludesFile); err != nil {
		fmt.Print(" (not found — only built-in excludes applied)")
	}
	fmt.Println()
	fmt.Println()

	rsyncArgs := []string{"-aHAX", "--progress"}

	for _, e := range backupBuiltinExcludes {
		rsyncArgs = append(rsyncArgs, "--exclude="+e)
	}

	if _, err := os.Stat(backupExcludesFile); err == nil {
		rsyncArgs = append(rsyncArgs, "--exclude-from="+backupExcludesFile)
	}

	if syncMode {
		rsyncArgs = append(rsyncArgs, "--delete")
	}

	rsyncArgs = append(rsyncArgs, src, dest)

	ui := &UI{DryRun: flagDryRun}
	r := NewRunner(ui, flagVerbose, flagDryRun)
	return r.Interactive("rsync", rsyncArgs...)
}

// validateSyncDest ensures that dest is an initialized backup target before
// allowing destructive --sync mode.
//
// For local paths: dest must exist (catches missing mounts) and must contain
// a backupTargetMarker file (catches accidental wrong-path mistakes).
//
// For remote paths (containing ":"): local validation is skipped because the
// path lives on a remote host. The caller bears responsibility for remote setup.
func validateSyncDest(dest string) error {
	// Remote destination — skip local filesystem checks
	if strings.Contains(dest, ":") {
		return nil
	}

	// Local destination must be present (catches wrong path / unmounted volume)
	if _, err := os.Stat(dest); err != nil {
		return fmt.Errorf(
			"--sync destination %q does not exist or is not mounted\n"+
				"Mount the volume or create the directory, then initialize it:\n"+
				"  mkdir -p %s && touch %s",
			dest, dest, filepath.Join(dest, backupTargetMarker),
		)
	}

	// Must contain the sentinel marker (proves this is an intentional backup target)
	marker := filepath.Join(dest, backupTargetMarker)
	if _, err := os.Stat(marker); err != nil {
		return fmt.Errorf(
			"--sync destination %q is not an initialized backup target\n"+
				"Missing sentinel file: %s\n"+
				"To initialize this destination:\n"+
				"  touch %s",
			dest, marker, marker,
		)
	}

	return nil
}

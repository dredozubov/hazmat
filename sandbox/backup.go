package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newBackupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup <destination>",
		Short: "Back up ~/workspace to destination using rsync",
		Long: `Backs up ~/workspace to the given destination using rsync.

Examples:
  sandbox backup /Volumes/BACKUP/workspace
  sandbox backup user@nas:/backup/workspace`,
		Args: cobra.ExactArgs(1),
		RunE: runBackup,
	}
}

func runBackup(_ *cobra.Command, args []string) error {
	dest := args[0]
	src := os.Getenv("HOME") + "/workspace/"

	if _, err := os.Stat(strings.TrimSuffix(src, "/")); err != nil {
		return fmt.Errorf("source directory %q not found: %w", src, err)
	}

	fmt.Printf("Source:      %s\n", src)
	fmt.Printf("Destination: %s\n", dest)
	fmt.Println("Note: --delete is active — files in destination not present in source will be removed.")
	fmt.Println()

	ui := &UI{DryRun: flagDryRun}
	r := NewRunner(ui, flagVerbose, flagDryRun)
	return r.Interactive("rsync",
		"-aHAX",
		"--progress",
		"--delete",
		"--exclude=node_modules/",
		"--exclude=.venv/",
		"--exclude=venv/",
		"--exclude=__pycache__/",
		"--exclude=.next/",
		"--exclude=dist/",
		"--exclude=build/",
		"--exclude=target/",
		"--exclude=.nix-*",
		"--exclude=.DS_Store",
		"--exclude=*.pyc",
		"--exclude=/gurufocus-data/",
		"--exclude=/flux2.c/",
		"--exclude=/iTerm2/",
		"--exclude=/nixpkgs/",
		"--exclude=/postiz-app/",
		"--exclude=/dsss17/",
		"--exclude=/dsss17-nix/",
		"--exclude=/SillyTavern/",
		"--exclude=/emacs/",
		"--exclude=/bitcoin/",
		"--exclude=/MatchingCompressor/",
		"--exclude=/zcash/",
		"--exclude=/transmission/",
		"--exclude=/moltbot/",
		"--exclude=/agent-browser/",
		"--exclude=/darkfi/",
		"--exclude=/24slash6/",
		"--exclude=/urweb/",
		"--exclude=/bitcoinbook/",
		"--exclude=/nix-config/",
		"--exclude=/dotfiles/",
		src, dest,
	)
}

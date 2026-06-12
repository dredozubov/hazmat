package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRootPersistentDryRunParsesAfterSubcommand(t *testing.T) {
	var dryRun bool
	var called bool
	doctor := &cobra.Command{
		Use: "doctor",
		RunE: func(*cobra.Command, []string) error {
			called = true
			if !dryRun {
				t.Fatal("dry-run flag = false, want true for `hazmat doctor --dry-run`")
			}
			return nil
		},
	}

	root := NewRootCommand(RootConfig{
		Flags: PersistentFlags{DryRun: &dryRun},
		Setup: []*cobra.Command{doctor},
	})
	root.SetArgs([]string{"doctor", "--dry-run"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if !called {
		t.Fatal("doctor command did not run")
	}
}

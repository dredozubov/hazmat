package hazmat

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func newRepairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Run explicit Hazmat repair commands",
		Long: `Run explicit Hazmat repair commands.

These commands are separate from session startup. They are operator-invoked
repair paths for work that would be too expensive or too broad to hide inside a
normal agent launch.`,
	}
	cmd.AddCommand(newRepairProjectACLBackfillCmd())
	return cmd
}

func newRepairProjectACLBackfillCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "project-acl-backfill",
		Short: "Recursively apply collaborative ACLs to a project",
		Long: `Recursively apply Hazmat's collaborative dev-group ACL to a project.

Normal Hazmat launches use a bounded startup ACL repair so launching an agent is
not proportional to repository size. This command is the explicit full
historical backfill path for existing project files that predate Hazmat's ACLs.

The command does not use sudo. It skips symlinks, touches regular files and
directories under the selected project, and may be slow on large repositories.

Examples:
  hazmat repair project-acl-backfill --dry-run -C ~/workspace/my-app
  hazmat repair project-acl-backfill --yes -C ~/workspace/my-app`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProjectACLBackfillCommand(projectACLBackfillOptions{
				Project: project,
				DryRun:  flagDryRun,
				YesAll:  flagYesAll,
				Out:     cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory to repair (default: current directory)")
	return cmd
}

type projectACLBackfillOptions struct {
	Project string
	DryRun  bool
	YesAll  bool
	Out     io.Writer
	Confirm func() bool
}

func runProjectACLBackfillCommand(opts projectACLBackfillOptions) error {
	w := opts.Out
	if w == nil {
		w = os.Stdout
	}

	projectDir, err := resolveDir(opts.Project, true)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}
	targets := collectProjectACLBackfillTargets(projectDir)
	renderProjectACLBackfillPlan(w, projectDir, targets)
	if len(targets.Failures) > 0 {
		renderProjectACLBackfillFailures(w, "Plan failures", targets.Failures)
		return fmt.Errorf("cannot complete project ACL backfill plan: %s", targets.Failures[0])
	}
	if opts.DryRun {
		fmt.Fprintln(w, "  Dry run: no ACLs changed.")
		fmt.Fprintf(w, "  Apply with: hazmat repair project-acl-backfill --yes -C %s\n", projectDir)
		return nil
	}

	confirmed, err := confirmProjectACLBackfill(opts)
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Fprintln(w, "  Aborted.")
		return nil
	}

	fmt.Fprintln(w, "Applying project ACL backfill...")
	result := applyDevACLBackfillRepairResult(projectDir, targets)
	if len(result.Failures) > 0 {
		renderProjectACLBackfillFailures(w, "Apply failures", result.Failures)
		return fmt.Errorf("project ACL backfill incomplete: %s", result.Failures[0])
	}
	fmt.Fprintf(w, "  Applied ACLs to %d target(s) in %d batch(es).\n", result.Targets, result.Batches)
	fmt.Fprintln(w, "  Done.")
	return nil
}

func confirmProjectACLBackfill(opts projectACLBackfillOptions) (bool, error) {
	if opts.YesAll {
		return true, nil
	}
	if opts.Confirm != nil {
		return opts.Confirm(), nil
	}
	ui := &UI{DryRun: opts.DryRun, YesAll: opts.YesAll}
	if !ui.IsInteractive() {
		return false, fmt.Errorf("project ACL backfill requires confirmation; rerun with --yes to apply or --dry-run to preview")
	}
	return ui.Ask("Apply recursive project ACL backfill?"), nil
}

func renderProjectACLBackfillPlan(w io.Writer, projectDir string, targets projectACLTargetCollection) {
	fmt.Fprintln(w, "Project ACL backfill plan")
	fmt.Fprintf(w, "  Project: %s\n", projectDir)
	fmt.Fprintln(w, "  Scope: recursive non-symlink directories and regular files")
	fmt.Fprintf(w, "  Targets: %d directories, %d files, plus project root\n", len(targets.Dirs), len(targets.Files))
	fmt.Fprintf(w, "  Entries scanned: %d\n", targets.EntriesScanned)
	fmt.Fprintln(w, "  Authority: current user ACL update; no sudo")
	fmt.Fprintln(w, "  Warning: this can run many chmod batches on large repos.")
	fmt.Fprintln(w, "  Launches still use bounded startup repair; this backfill is never automatic.")
}

func renderProjectACLBackfillFailures(w io.Writer, title string, failures []string) {
	if len(failures) == 0 {
		return
	}
	fmt.Fprintf(w, "  %s:\n", title)
	const maxFailures = 5
	for i, failure := range failures {
		if i == maxFailures {
			fmt.Fprintf(w, "    ... %d more\n", len(failures)-maxFailures)
			return
		}
		fmt.Fprintf(w, "    - %s\n", failure)
	}
}

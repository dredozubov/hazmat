package hazmat

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// errImportPromptCancelled is the harness-neutral sentinel returned by
// promptImportConflictPolicy when the user cancels the interactive conflict
// prompt. runBasicsImport translates it into the harness-specific cancelled
// error carried by the spec.
var errImportPromptCancelled = errors.New("import conflict prompt cancelled")

// This file holds the shared curated-import engine. The per-harness importers
// (config_import.go for Claude, config_import_codex.go, config_import_opencode.go)
// share one set of types (importItem/Plan/ApplyResult/Options/Status/Kind
// and importConflictPolicy) and drive this engine through a harnessImportSpec.
// Only the genuinely-varying pieces stay per-harness: the env path layout, the
// scan set (which artifacts a harness imports), the per-item apply dispatch, and
// the display wording. The orchestration, conflict handling, apply loop, and the
// cobra command skeleton live here once.

// harnessImportSpec carries the per-harness behavior runBasicsImport needs. The
// scan and applyItem closures capture the harness-specific typed env, so the
// engine never sees harness-specific types.
type harnessImportSpec struct {
	label        string
	cancelledErr error
	scan         func(r *Runner) (importPlan, error)
	applyItem    func(item importItem, r *Runner) error
	printPlan    func(plan importPlan)
	printResult  func(result importApplyResult)
}

// runBasicsImport is the single orchestrator shared by every importable harness.
// All user-facing messages are derived from spec.label so the output matches the
// previous per-harness orchestrators verbatim.
func runBasicsImport(ui *UI, r *Runner, opts importOptions, spec harnessImportSpec) error {
	ui.Step(fmt.Sprintf("Import %s basics", spec.label))

	plan, err := spec.scan(r)
	if err != nil {
		return err
	}

	if !plan.hasFoundBasics() {
		ui.SkipDone(fmt.Sprintf("No %s basics found to import", spec.label))
		return nil
	}

	if opts.AllowNoopMessage && !plan.hasActionableChanges() && len(plan.Skips) == 0 {
		ui.SkipDone(fmt.Sprintf("%s basics already match the current import scope", spec.label))
		return nil
	}

	if opts.PromptBeforeImport && !plan.hasActionableChanges() && len(plan.Skips) == 0 {
		ui.SkipDone(fmt.Sprintf("%s basics already imported", spec.label))
		return nil
	}

	if opts.PromptBeforeImport && !ui.Ask(fmt.Sprintf("Import %s basics from your host setup?", spec.label)) {
		ui.SkipDone(fmt.Sprintf("%s basics import skipped", spec.label))
		return nil
	}

	spec.printPlan(plan)

	if !plan.hasActionableChanges() {
		ui.SkipDone("Nothing to import")
		return nil
	}

	policy := opts.ConflictPolicy
	if flagDryRun && policy == importConflictPrompt {
		policy = importConflictFail
	}
	if plan.conflictCount() > 0 && policy == importConflictPrompt {
		selected, err := promptImportConflictPolicy()
		if err != nil {
			if errors.Is(err, errImportPromptCancelled) {
				return spec.cancelledErr
			}
			return err
		}
		policy = selected
	}

	if flagDryRun {
		if plan.conflictCount() > 0 && (policy == importConflictPrompt || policy == importConflictFail) {
			cDim.Println("  Re-run with --overwrite or --skip-existing to choose a conflict policy.")
			fmt.Println()
			return nil
		}
		if err := plan.resolveConflicts(policy); err != nil {
			return err
		}
		return nil
	}

	if err := plan.resolveConflicts(policy); err != nil {
		return err
	}

	result, err := applyImportPlan(plan, r, spec.applyItem)
	if err != nil {
		return err
	}
	spec.printResult(result)
	return nil
}

// applyImportPlan is the shared apply loop. It classifies each planned item by
// status and delegates the actual write to the per-harness applyItem closure.
func applyImportPlan(plan importPlan, r *Runner, applyItem func(item importItem, r *Runner) error) (importApplyResult, error) {
	var result importApplyResult

	for _, item := range plan.Items {
		switch item.Status {
		case importUnchanged:
			result.Unchanged = append(result.Unchanged, item)
			continue
		case importSkip:
			result.Skipped = append(result.Skipped, item)
			continue
		case importNew, importConflict, importOverwrite:
			// New, conflict, and overwrite statuses all flow to apply below.
		default:
			return result, fmt.Errorf("unsupported import status %q", item.Status)
		}

		if err := applyItem(item, r); err != nil {
			return result, err
		}

		if item.Status == importOverwrite {
			result.Overwritten = append(result.Overwritten, item)
		} else {
			result.Imported = append(result.Imported, item)
		}
	}

	return result, nil
}

// newConfigImportHarnessCmd builds the shared cobra command for an importable
// harness. The use/short/long strings and the run closure (which materializes
// the typed env and calls the harness import function) are the only per-harness
// inputs; flag parsing and conflict-policy derivation are shared.
func newConfigImportHarnessCmd(use, short, long string, run func(ui *UI, r *Runner, policy importConflictPolicy) error) *cobra.Command {
	var overwrite bool
	var skipExisting bool

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if overwrite && skipExisting {
				return fmt.Errorf("choose either --overwrite or --skip-existing, not both")
			}

			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)

			policy := importConflictPrompt
			switch {
			case overwrite:
				policy = importConflictOverwrite
			case skipExisting:
				policy = importConflictSkip
			case !ui.IsInteractive():
				policy = importConflictFail
			}

			return run(ui, r, policy)
		},
	}

	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite conflicting imported items")
	cmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "Skip conflicting imported items")

	return cmd
}

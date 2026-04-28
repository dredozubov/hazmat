package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

func newHooksCmd() *cobra.Command {
	var project string

	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Inspect and manage repo-local git hooks",
		Long: `Repo-local git hooks are declared in .hazmat/hooks/hooks.yaml and only run
after host approval. Hazmat supports the common low-friction cases:
pre-commit, commit-msg, and pre-push.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runHooksStatus(project)
		},
	}
	cmd.PersistentFlags().StringVarP(&project, "project", "C", "", "Project directory (defaults to cwd)")

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show hook declaration, approval, and install status",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runHooksStatus(project)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "review",
		Short: "Review the current hook bundle and any drift from approval",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runHooksReview(project)
		},
	})

	var replace bool
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Approve and install repo-local git hooks",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runHooksInstall(project, replace)
		},
	}
	installCmd.Flags().BoolVar(&replace, "replace", false, "Replace an existing local core.hooksPath owner explicitly")
	cmd.AddCommand(installCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Remove Hazmat-managed hook runtime and approval state",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runHooksUninstall(project)
		},
	})

	return cmd
}

func runHooksStatus(project string) error {
	projectDir, status, err := inspectProjectHooks(project)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  Project: %s\n", projectDir)
	if status.Bundle == nil {
		fmt.Println("  Hooks:   none declared")
		fmt.Println()
		return nil
	}

	fmt.Printf("  Bundle hash: %s\n", status.Bundle.BundleHash)
	switch {
	case status.Approval == nil:
		fmt.Println("  Approval:    not approved")
	case status.Approval.BundleHash == status.Bundle.BundleHash:
		fmt.Println("  Approval:    approved")
	default:
		fmt.Printf("  Approval:    drifted (approved %s)\n", status.Approval.BundleHash)
	}

	switch {
	case status.RuntimeValid:
		fmt.Println("  Runtime:     installed")
	case status.RuntimeErr != "":
		fmt.Printf("  Runtime:     %s\n", status.RuntimeErr)
	default:
		fmt.Println("  Runtime:     not installed")
	}

	fmt.Println()
	printProjectHookSummary(summarizeProjectHookBundle(status.Bundle, approvedHashForStatus(status)))
	fmt.Println()
	return nil
}

func runHooksReview(project string) error {
	_, status, err := inspectProjectHooks(project)
	if err != nil {
		return err
	}
	if status.Bundle == nil {
		return fmt.Errorf("repo does not declare managed hooks")
	}

	fmt.Println()
	printProjectHookSummary(summarizeProjectHookBundle(status.Bundle, approvedHashForStatus(status)))
	changes := summarizeProjectHookChanges(status.Bundle, status.Approval)
	if len(changes) > 0 {
		fmt.Println()
		fmt.Println("  Review:")
		for _, change := range changes {
			fmt.Printf("    - %s\n", change)
		}
	}
	fmt.Println()
	return nil
}

func runHooksInstall(project string, replace bool) error {
	projectDir, status, err := inspectProjectHooks(project)
	if err != nil {
		return err
	}
	if status.Bundle == nil {
		return fmt.Errorf("repo does not declare managed hooks")
	}

	if status.Approval == nil || status.Approval.BundleHash != status.Bundle.BundleHash {
		if err := promptProjectHookApproval(status.Bundle, status.Approval); err != nil {
			return err
		}
		if _, err := recordProjectHookApproval(status.Bundle); err != nil {
			return err
		}
	}

	hazmatBinPath, err := os.Executable()
	if err != nil {
		return err
	}
	runtime, err := installProjectHookRuntimeWithOptions(projectDir, hazmatBinPath, replace)
	if err != nil {
		if !replace && strings.Contains(err.Error(), "refusing to replace it silently") {
			return fmt.Errorf("%w\nre-run with: hazmat hooks install --replace -C %s", err, projectDir)
		}
		return err
	}

	fmt.Println()
	fmt.Printf("  Installed Hazmat-managed hooks for %s\n", runtime.ProjectDir)
	fmt.Printf("  Wrapper: %s\n", runtime.WrapperPath)
	fmt.Printf("  Managed hooksPath: %s\n", runtime.ManagedDir)
	fmt.Println()
	return nil
}

func runHooksUninstall(project string) error {
	projectDir, _, err := inspectProjectHooks(project)
	if err != nil {
		return err
	}
	if err := uninstallProjectHookRuntime(projectDir); err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  Removed Hazmat-managed hooks for %s\n", projectDir)
	fmt.Println()
	return nil
}

func maybePromptProjectHooks(projectDir string) {
	_, status, err := inspectProjectHooks(projectDir)
	if err != nil || status.Bundle == nil {
		return
	}
	if status.RuntimeValid {
		return
	}

	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	approvedNow := false
	if status.Approval == nil || status.Approval.BundleHash != status.Bundle.BundleHash {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "hazmat: this repo declares repo-local git hooks")
		printProjectHookSummaryTo(os.Stderr, summarizeProjectHookBundle(status.Bundle, approvedHashForStatus(status)))
		for _, change := range summarizeProjectHookChanges(status.Bundle, status.Approval) {
			fmt.Fprintf(os.Stderr, "  - %s\n", change)
		}
		if !ui.Ask("Approve and install these repo-local git hooks?") {
			return
		}
		if _, err := recordProjectHookApproval(status.Bundle); err != nil {
			fmt.Fprintf(os.Stderr, "hazmat: warning: could not record hook approval: %v\n", err)
			return
		}
		approvedNow = true
	}

	if status.RuntimeHooksPathDrift != nil && status.RuntimeHooksPathDrift.ConfiguredHooksPath != "" && !status.RuntimeHasManagedArtifacts {
		return
	}
	if status.RuntimeErr != "" {
		fmt.Fprintf(os.Stderr, "hazmat: repo hooks need install/repair: %s\n", status.RuntimeErr)
	}
	if !approvedNow && !ui.Ask("Install or repair Hazmat-managed git hooks for this repo?") {
		return
	}

	hazmatBinPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hazmat: warning: locate hazmat binary: %v\n", err)
		return
	}
	if _, err := installProjectHookRuntime(projectDir, hazmatBinPath); err != nil {
		fmt.Fprintf(os.Stderr, "hazmat: warning: could not install repo hooks: %v\n", err)
		if strings.Contains(err.Error(), "refusing to replace it silently") {
			fmt.Fprintf(os.Stderr, "hazmat: repair manually with: hazmat hooks install --replace -C %s\n", projectDir)
		}
	}
}

type inspectedProjectHooks struct {
	Bundle                     *loadedProjectHookBundle
	Approval                   *projectHookApprovalRecord
	RuntimeErr                 string
	RuntimeValid               bool
	RuntimeHooksPathDrift      *projectHookHooksPathDriftError
	RuntimeHasManagedArtifacts bool
}

func inspectProjectHooks(project string) (string, inspectedProjectHooks, error) {
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return "", inspectedProjectHooks{}, err
	}

	bundle, err := loadProjectHookBundle(projectDir)
	if err != nil {
		return "", inspectedProjectHooks{}, err
	}
	approval, err := loadProjectHookApproval(projectDir)
	if err != nil {
		return "", inspectedProjectHooks{}, err
	}

	status := inspectedProjectHooks{
		Bundle:   bundle,
		Approval: approval,
	}
	if bundle != nil && approval != nil && approval.BundleHash == bundle.BundleHash {
		if _, err := validateProjectHookRuntime(projectDir); err != nil {
			status.RuntimeErr = err.Error()
			var hooksPathDrift *projectHookHooksPathDriftError
			if errors.As(err, &hooksPathDrift) {
				status.RuntimeHooksPathDrift = hooksPathDrift
				if runtime, buildErr := buildProjectHookRuntime(projectDir); buildErr == nil {
					status.RuntimeHasManagedArtifacts = projectHookManagedRuntimeArtifactsExist(runtime)
				}
			}
		} else {
			status.RuntimeValid = true
		}
	}

	return projectDir, status, nil
}

func promptProjectHookApproval(bundle *loadedProjectHookBundle, approval *projectHookApprovalRecord) error {
	if bundle == nil {
		return fmt.Errorf("project hook bundle is required")
	}

	fmt.Println()
	printProjectHookSummary(summarizeProjectHookBundle(bundle, approvedHashForSummary(approval)))
	changes := summarizeProjectHookChanges(bundle, approval)
	if len(changes) > 0 {
		fmt.Println()
		fmt.Println("  Review:")
		for _, change := range changes {
			fmt.Printf("    - %s\n", change)
		}
	}
	fmt.Println()

	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	if !ui.Ask("Approve this repo-local hook bundle?") {
		return fmt.Errorf("repo-local hook approval declined")
	}
	return nil
}

func summarizeProjectHookChanges(bundle *loadedProjectHookBundle, approval *projectHookApprovalRecord) []string {
	if bundle == nil {
		return nil
	}
	current := make(map[hookType]projectHookSummaryEntry, len(bundle.Hooks))
	for _, hook := range summarizeProjectHookBundle(bundle, "").Hooks {
		current[hook.Type] = hook
	}

	if approval == nil {
		var changes []string
		for _, hook := range summarizeProjectHookBundle(bundle, "").Hooks {
			changes = append(changes, fmt.Sprintf("%s added: %s", hook.Type, hook.Purpose))
		}
		return changes
	}

	prior := make(map[hookType]projectHookSummaryEntry, len(approval.Summary.Hooks))
	for _, hook := range approval.Summary.Hooks {
		prior[hook.Type] = hook
	}

	types := make([]hookType, 0, len(current)+len(prior))
	seen := make(map[hookType]struct{}, len(current)+len(prior))
	for hookType := range prior {
		seen[hookType] = struct{}{}
		types = append(types, hookType)
	}
	for hookType := range current {
		if _, ok := seen[hookType]; ok {
			continue
		}
		types = append(types, hookType)
	}
	slices.SortFunc(types, func(a, b hookType) int {
		switch {
		case a < b:
			return -1
		case a > b:
			return 1
		default:
			return 0
		}
	})

	var changes []string
	for _, hookType := range types {
		oldHook, hadOld := prior[hookType]
		newHook, hasNew := current[hookType]
		switch {
		case !hadOld && hasNew:
			changes = append(changes, fmt.Sprintf("%s added: %s", hookType, newHook.Purpose))
		case hadOld && !hasNew:
			changes = append(changes, fmt.Sprintf("%s removed", hookType))
		default:
			if oldHook.ScriptPath != newHook.ScriptPath {
				changes = append(changes, fmt.Sprintf("%s script: %s -> %s", hookType, oldHook.ScriptPath, newHook.ScriptPath))
			}
			if oldHook.Purpose != newHook.Purpose {
				changes = append(changes, fmt.Sprintf("%s purpose changed", hookType))
			}
			if oldHook.Interpreter != newHook.Interpreter {
				changes = append(changes, fmt.Sprintf("%s interpreter: %s -> %s", hookType, oldHook.Interpreter, newHook.Interpreter))
			}
			if strings.Join(oldHook.Requires, ",") != strings.Join(newHook.Requires, ",") {
				changes = append(changes, fmt.Sprintf("%s required binaries changed", hookType))
			}
		}
	}
	return changes
}

func printProjectHookSummary(summary projectHookReviewSummary) {
	printProjectHookSummaryTo(os.Stdout, summary)
}

func printProjectHookSummaryTo(file *os.File, summary projectHookReviewSummary) {
	kindLabel := "Install"
	if summary.Kind == hookReviewDrift {
		kindLabel = "Drift review"
	}
	fmt.Fprintf(file, "  %s\n", kindLabel)
	fmt.Fprintf(file, "  Bundle hash: %s\n", summary.BundleHash)
	for _, hook := range summary.Hooks {
		fmt.Fprintf(file, "  %s: %s\n", hook.Type, hook.Purpose)
		fmt.Fprintf(file, "    script: %s\n", hook.ScriptPath)
		fmt.Fprintf(file, "    interpreter: %s\n", hook.Interpreter)
		if len(hook.Requires) > 0 {
			fmt.Fprintf(file, "    requires: %s\n", strings.Join(hook.Requires, ", "))
		}
	}
}

func approvedHashForStatus(status inspectedProjectHooks) string {
	return approvedHashForSummary(status.Approval)
}

func approvedHashForSummary(approval *projectHookApprovalRecord) string {
	if approval == nil {
		return ""
	}
	return approval.BundleHash
}

package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type harnessProbe struct {
	Installed     bool
	BinaryPath    string
	Version       string
	VersionErr    string
	MissingReason string
}

type harnessManagedArtifactKind string

const (
	harnessArtifactFile harnessManagedArtifactKind = "file"
	harnessArtifactDir  harnessManagedArtifactKind = "dir"
)

type harnessManagedArtifact struct {
	Path        string
	Kind        harnessManagedArtifactKind
	Description string
}

type harnessArtifactStatus struct {
	Artifact harnessManagedArtifact
	Exists   bool
	Drift    string
}

type harnessStatus struct {
	Harness           ManagedHarness
	Probe             harnessProbe
	StateSummary      string
	ImportSummary     string
	CredentialSummary string
	NextAction        string
	StateErr          error
}

type harnessUninstallPlan struct {
	Harness         ManagedHarness
	Artifacts       []harnessArtifactStatus
	Preserved       []string
	MetadataPresent bool
	StateErr        error
}

var summarizeHarnessCredentialsForStatus = summarizeHarnessCredentials

func newHarnessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "harness",
		Short: "Inspect and manage agent-user harnesses",
		Long: `Inspect and manage built-in code harnesses installed for the agent user.

Use this command to see per-harness status, refresh harness code through the
same paths as bootstrap, or uninstall Hazmat-owned code artifacts without
removing auth, profile, session, or provider data by default.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runHarnessStatus("")
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "status [harness]",
		Short: "Show per-harness install, state, and credential status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			return runHarnessStatus(id)
		},
	})

	var updateAll bool
	updateCmd := &cobra.Command{
		Use:   "update <harness> | --all",
		Short: "Install or update harness code for the agent user",
		Args: func(_ *cobra.Command, args []string) error {
			if updateAll {
				if len(args) != 0 {
					return fmt.Errorf("--all cannot be combined with a harness argument")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("specify a harness or --all")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if updateAll {
				return runHarnessUpdateAll()
			}
			return runHarnessUpdate(args[0])
		},
	}
	updateCmd.Flags().BoolVar(&updateAll, "all", false, "Update every managed harness")
	cmd.AddCommand(updateCmd)

	var force bool
	uninstallCmd := &cobra.Command{
		Use:   "uninstall <harness>",
		Short: "Remove Hazmat-owned harness code artifacts",
		Long: `Remove Hazmat-owned harness code artifacts and the selected harness metadata.

By default this preserves auth files, imported profile basics, provider state,
sessions, and other user data in the agent home. If a planned artifact has an
unexpected type, uninstall refuses unless --force is passed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runHarnessUninstall(args[0], force)
		},
	}
	uninstallCmd.Flags().BoolVarP(&force, "force", "f", false, "Remove exact planned paths even when their type has drifted")
	cmd.AddCommand(uninstallCmd)

	return cmd
}

func normalizeManagedHarnessID(input string) (HarnessID, error) {
	id := HarnessID(strings.ToLower(strings.TrimSpace(input)))
	if _, ok := managedHarnessByID(id); ok {
		return id, nil
	}
	valid := make([]string, 0, len(managedHarnessRegistry))
	for _, harness := range managedHarnessRegistry {
		valid = append(valid, string(harness.Spec.ID))
	}
	sort.Strings(valid)
	return "", fmt.Errorf("unknown harness %q (valid: %s)", input, strings.Join(valid, ", "))
}

func runHarnessStatus(input string) error {
	state, stateErr := loadState()
	if strings.TrimSpace(input) != "" {
		id, err := normalizeManagedHarnessID(input)
		if err != nil {
			return err
		}
		harness, _ := managedHarnessByID(id)
		status := inspectManagedHarnessStatus(harness, state, stateErr, asAgentOutput)
		printHarnessDetailStatus(status)
		return nil
	}

	fmt.Println()
	cBold.Println("  Agent harnesses")
	fmt.Println()
	fmt.Printf("  %-12s %-12s %-18s %-18s %s\n", "Harness", "Installed", "State", "Import", "Next")
	for _, harness := range managedHarnesses() {
		status := inspectManagedHarnessStatus(harness, state, stateErr, asAgentOutput)
		fmt.Printf("  %-12s %-12s %-18s %-18s %s\n",
			status.Harness.Spec.ID,
			formatHarnessInstalledStatus(status.Probe),
			truncateStatusField(status.StateSummary, 18),
			truncateStatusField(status.ImportSummary, 18),
			status.NextAction,
		)
	}
	fmt.Println()
	fmt.Println("  Details: hazmat harness status <harness>")
	fmt.Println()
	return nil
}

func inspectManagedHarnessStatus(harness ManagedHarness, state HazmatState, stateErr error, read func(args ...string) (string, error)) harnessStatus {
	probe := harnessProbe{MissingReason: "probe unavailable"}
	if harness.Probe != nil {
		probe = harness.Probe(read)
	} else if harness.Installed != nil && harness.Installed() {
		probe = harnessProbe{Installed: true}
	}

	return harnessStatus{
		Harness:           harness,
		Probe:             probe,
		StateSummary:      formatHarnessStateSummary(harness, state, stateErr),
		ImportSummary:     formatHarnessImportSummary(harness, state, stateErr),
		CredentialSummary: summarizeHarnessCredentialsForStatus(harness.Spec.ID),
		NextAction:        nextHarnessAction(harness, probe, state, stateErr),
		StateErr:          stateErr,
	}
}

func formatHarnessInstalledStatus(probe harnessProbe) string {
	if probe.Installed {
		return "yes"
	}
	return "no"
}

func formatHarnessStateSummary(harness ManagedHarness, state HazmatState, stateErr error) string {
	if stateErr != nil {
		return "state unreadable"
	}
	recorded, ok := state.Harnesses[harness.Spec.ID]
	if !ok || recorded.StateVersion == "" {
		return "not recorded"
	}
	if !harnessStateCurrent(recorded, harness.Spec) {
		return fmt.Sprintf("outdated v%s", recorded.StateVersion)
	}
	return "recorded v" + recorded.StateVersion
}

func formatHarnessImportSummary(harness ManagedHarness, state HazmatState, stateErr error) string {
	if !harnessSupportsBasicsImport(harness.Spec.ID) {
		return "not supported"
	}
	if stateErr != nil {
		return "state unreadable"
	}
	recorded, ok := state.Harnesses[harness.Spec.ID]
	if !ok || recorded.LastImportRunAt == "" {
		return "not imported"
	}
	return recorded.LastImportRunAt
}

func harnessSupportsBasicsImport(id HarnessID) bool {
	switch id {
	case HarnessClaude, HarnessCodex, HarnessOpenCode, HarnessGemini:
		return true
	default:
		return false
	}
}

func nextHarnessAction(harness ManagedHarness, probe harnessProbe, state HazmatState, stateErr error) string {
	if !probe.Installed {
		return harness.BootstrapCommand
	}
	if stateErr != nil {
		return "repair state before update"
	}
	recorded, ok := state.Harnesses[harness.Spec.ID]
	if !ok || !harnessStateCurrent(recorded, harness.Spec) {
		return "hazmat harness update " + string(harness.Spec.ID)
	}
	return "hazmat harness update " + string(harness.Spec.ID)
}

func printHarnessDetailStatus(status harnessStatus) {
	harness := status.Harness
	fmt.Println()
	cBold.Printf("  %s (%s)\n", harness.Spec.DisplayName, harness.Spec.ID)
	fmt.Println()
	fmt.Printf("  Installed:   %s\n", formatHarnessInstalledStatus(status.Probe))
	if status.Probe.BinaryPath != "" {
		fmt.Printf("  Binary:      %s\n", status.Probe.BinaryPath)
	} else if status.Probe.MissingReason != "" {
		fmt.Printf("  Binary:      %s\n", status.Probe.MissingReason)
	}
	if status.Probe.Version != "" {
		fmt.Printf("  Version:     %s\n", status.Probe.Version)
	} else if status.Probe.VersionErr != "" {
		fmt.Printf("  Version:     unavailable (%s)\n", status.Probe.VersionErr)
	}
	fmt.Printf("  State:       %s\n", status.StateSummary)
	if status.StateErr != nil {
		fmt.Printf("  State error: %v\n", status.StateErr)
	}
	fmt.Printf("  Import:      %s\n", status.ImportSummary)
	fmt.Printf("  Credentials: %s\n", status.CredentialSummary)
	fmt.Printf("  Update:      hazmat harness update %s\n", harness.Spec.ID)
	fmt.Printf("  Uninstall:   hazmat harness uninstall %s\n", harness.Spec.ID)
	fmt.Println()
	printHarnessArtifactBoundaries(harness)
	fmt.Println()
}

func printHarnessArtifactBoundaries(harness ManagedHarness) {
	fmt.Println("  Hazmat-owned code artifacts:")
	if harness.ManagedCodeArtifacts == nil {
		fmt.Println("    - none")
	} else if owned := harness.ManagedCodeArtifacts(); len(owned) > 0 {
		for _, artifact := range owned {
			fmt.Printf("    - %s (%s)\n", artifact.Path, artifact.Description)
		}
	} else {
		fmt.Println("    - none")
	}

	fmt.Println("  Preserved by default:")
	if len(harness.PreservedArtifacts) == 0 {
		fmt.Println("    - auth, profile, sessions, and provider state")
		return
	}
	for _, item := range harness.PreservedArtifacts {
		fmt.Printf("    - %s\n", item)
	}
}

func runHarnessUpdate(input string) error {
	id, err := normalizeManagedHarnessID(input)
	if err != nil {
		return err
	}
	harness, _ := managedHarnessByID(id)
	return runManagedHarnessUpdate(harness)
}

func runHarnessUpdateAll() error {
	for _, harness := range managedHarnesses() {
		if harness.Spec.ID == HarnessHermes && harness.Probe != nil {
			probe := harness.Probe(asAgentOutput)
			if !probe.Installed {
				cYellow.Printf("  Skipping Hermes: %s; install %s manually, then run hazmat harness update hermes\n", probe.MissingReason, agentHome+hermesBinRel)
				continue
			}
		}
		if err := runManagedHarnessUpdate(harness); err != nil {
			return err
		}
	}
	return nil
}

func runManagedHarnessUpdate(harness ManagedHarness) error {
	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	r := NewRunner(ui, flagVerbose, flagDryRun)
	return harness.Bootstrap(ui, r)
}

func runHarnessUninstall(input string, force bool) error {
	id, err := normalizeManagedHarnessID(input)
	if err != nil {
		return err
	}
	harness, _ := managedHarnessByID(id)
	if _, err := requireAgentUser(); err != nil {
		return err
	}

	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll || force}
	r := NewRunner(ui, flagVerbose, flagDryRun)
	plan := buildHarnessUninstallPlan(harness, r.AgentOutput)
	if plan.StateErr != nil {
		return plan.StateErr
	}

	printHarnessUninstallPlan(plan)
	if !plan.HasWork() {
		fmt.Println("  Nothing to remove.")
		fmt.Println()
		return nil
	}
	if drift := plan.Drift(); len(drift) > 0 && !force {
		return fmt.Errorf("refusing to uninstall %s because planned artifacts drifted: %s\nre-run with --force to remove the exact planned paths", harness.Spec.ID, strings.Join(drift, "; "))
	}
	if !ui.Ask("Remove these Hazmat-owned harness artifacts?") {
		fmt.Println()
		return nil
	}

	for _, artifact := range plan.Artifacts {
		if !artifact.Exists {
			continue
		}
		if artifact.Drift != "" && !force {
			continue
		}
		if err := removeHarnessArtifact(r, artifact.Artifact); err != nil {
			return err
		}
	}
	if plan.MetadataPresent {
		if r.DryRun {
			cYellow.Printf("  Dry-run: would remove %s harness metadata\n", harness.Spec.ID)
		} else if err := removeHarnessState(harness.Spec.ID); err != nil {
			return fmt.Errorf("remove %s harness metadata: %w", harness.Spec.ID, err)
		} else {
			ui.Ok(fmt.Sprintf("Removed %s harness metadata", harness.Spec.ID))
		}
	}
	fmt.Println()
	return nil
}

func buildHarnessUninstallPlan(harness ManagedHarness, read func(args ...string) (string, error)) harnessUninstallPlan {
	artifacts := []harnessManagedArtifact(nil)
	if harness.ManagedCodeArtifacts != nil {
		artifacts = harness.ManagedCodeArtifacts()
	}
	statuses := make([]harnessArtifactStatus, 0, len(artifacts))
	for _, artifact := range artifacts {
		statuses = append(statuses, inspectHarnessArtifact(read, artifact))
	}

	state, stateErr := loadState()
	metadataPresent := false
	if stateErr == nil && state.Harnesses != nil {
		_, metadataPresent = state.Harnesses[harness.Spec.ID]
	}
	preserved := append([]string(nil), harness.PreservedArtifacts...)
	return harnessUninstallPlan{
		Harness:         harness,
		Artifacts:       statuses,
		Preserved:       preserved,
		MetadataPresent: metadataPresent,
		StateErr:        stateErr,
	}
}

func (p harnessUninstallPlan) HasWork() bool {
	if p.MetadataPresent {
		return true
	}
	for _, artifact := range p.Artifacts {
		if artifact.Exists {
			return true
		}
	}
	return false
}

func (p harnessUninstallPlan) Drift() []string {
	var drift []string
	for _, artifact := range p.Artifacts {
		if artifact.Drift != "" {
			drift = append(drift, fmt.Sprintf("%s: %s", artifact.Artifact.Path, artifact.Drift))
		}
	}
	return drift
}

func printHarnessUninstallPlan(plan harnessUninstallPlan) {
	fmt.Println()
	cBold.Printf("  Harness uninstall: %s (%s)\n", plan.Harness.Spec.DisplayName, plan.Harness.Spec.ID)
	fmt.Println()
	fmt.Println("  Remove:")
	if len(plan.Artifacts) == 0 {
		fmt.Println("    - no Hazmat-owned code artifacts declared")
	} else {
		for _, artifact := range plan.Artifacts {
			state := "missing"
			if artifact.Exists {
				state = "present"
			}
			if artifact.Drift != "" {
				state = "drifted: " + artifact.Drift
			}
			fmt.Printf("    - %s (%s, %s)\n", artifact.Artifact.Path, artifact.Artifact.Description, state)
		}
	}
	if plan.MetadataPresent {
		fmt.Printf("    - %s entry in %s\n", plan.Harness.Spec.ID, stateFilePath)
	} else {
		fmt.Printf("    - %s metadata: not recorded\n", plan.Harness.Spec.ID)
	}

	fmt.Println("  Preserve:")
	if len(plan.Preserved) == 0 {
		fmt.Println("    - auth, profile, sessions, and provider state")
	} else {
		for _, item := range plan.Preserved {
			fmt.Printf("    - %s\n", item)
		}
	}
	fmt.Println()
}

func inspectHarnessArtifact(read func(args ...string) (string, error), artifact harnessManagedArtifact) harnessArtifactStatus {
	status := harnessArtifactStatus{Artifact: artifact}
	if err := validateHarnessArtifactPath(artifact.Path); err != nil {
		status.Drift = err.Error()
		return status
	}

	exists := harnessArtifactExists(read, artifact.Path)
	status.Exists = exists
	if !exists {
		return status
	}

	switch artifact.Kind {
	case harnessArtifactFile:
		if !harnessArtifactIsFile(read, artifact.Path) {
			status.Drift = "expected file or symlink"
		}
	case harnessArtifactDir:
		if !harnessArtifactIsDir(read, artifact.Path) {
			status.Drift = "expected directory"
		}
	default:
		status.Drift = "unknown artifact kind " + string(artifact.Kind)
	}
	return status
}

func validateHarnessArtifactPath(path string) error {
	clean := filepath.Clean(path)
	if clean == filepath.Clean(agentHome) || !usesManagedAgentPath(clean) {
		return fmt.Errorf("path is outside the managed agent home")
	}
	return nil
}

func harnessArtifactExists(read func(args ...string) (string, error), path string) bool {
	if _, err := read("/usr/bin/test", "-e", path); err == nil {
		return true
	}
	if _, err := read("/usr/bin/test", "-L", path); err == nil {
		return true
	}
	return false
}

func harnessArtifactIsFile(read func(args ...string) (string, error), path string) bool {
	if _, err := read("/usr/bin/test", "-f", path); err == nil {
		return true
	}
	if _, err := read("/usr/bin/test", "-L", path); err == nil {
		return true
	}
	return false
}

func harnessArtifactIsDir(read func(args ...string) (string, error), path string) bool {
	_, err := read("/usr/bin/test", "-d", path)
	return err == nil
}

func removeHarnessArtifact(r *Runner, artifact harnessManagedArtifact) error {
	if err := validateHarnessArtifactPath(artifact.Path); err != nil {
		return err
	}
	flag := "-f"
	if artifact.Kind == harnessArtifactDir {
		flag = "-rf"
	}
	if err := r.AsAgent("remove "+artifact.Description, "/bin/rm", flag, "--", artifact.Path); err != nil {
		return fmt.Errorf("remove %s: %w", artifact.Path, err)
	}
	if r.ui != nil {
		r.ui.Ok("Removed " + artifact.Path)
	}
	return nil
}

func probeHarnessBinary(read func(args ...string) (string, error), find func(func(args ...string) (string, error)) (string, bool), versionArgs ...string) harnessProbe {
	path, ok := find(read)
	if !ok {
		return harnessProbe{MissingReason: "binary not found for agent user"}
	}
	probe := harnessProbe{
		Installed:  true,
		BinaryPath: path,
	}
	args := append([]string{path}, versionArgs...)
	out, err := read(args...)
	if err != nil {
		probe.VersionErr = strings.TrimSpace(err.Error())
		return probe
	}
	probe.Version = firstStatusLine(out)
	return probe
}

func firstStatusLine(value string) string {
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func truncateStatusField(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "."
}

func summarizeHarnessCredentials(id HarnessID) string {
	descriptors := credentialDescriptorsForHarnessLifecycle(id)
	if len(descriptors) == 0 {
		return "none"
	}

	entries, err := inspectCredentialInventory("")
	if err != nil {
		return "inventory unavailable; run hazmat config agent"
	}
	relevant := make(map[credentialID]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		relevant[descriptor.ID] = struct{}{}
	}

	var configured, unset, external, adapter, repair, errors int
	for _, entry := range entries {
		if _, ok := relevant[entry.ID]; !ok {
			continue
		}
		switch entry.Status() {
		case credentialInventoryConfigured:
			configured++
		case credentialInventoryNotConfigured:
			unset++
		case credentialInventoryExternal:
			external++
		case credentialInventoryAdapterRequired:
			adapter++
		case credentialInventoryNeedsRepair:
			repair++
		case credentialInventoryError:
			errors++
		}
	}
	parts := []string{fmt.Sprintf("%d configured", configured)}
	if unset > 0 {
		parts = append(parts, fmt.Sprintf("%d unset", unset))
	}
	if external > 0 {
		parts = append(parts, fmt.Sprintf("%d external", external))
	}
	if adapter > 0 {
		parts = append(parts, fmt.Sprintf("%d adapter-required", adapter))
	}
	if repair > 0 {
		parts = append(parts, fmt.Sprintf("%d need repair", repair))
	}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", errors))
	}
	return strings.Join(parts, ", ") + "; hazmat config agent"
}

func credentialDescriptorsForHarnessLifecycle(id HarnessID) []credentialDescriptor {
	var descriptors []credentialDescriptor
	for _, descriptor := range builtinCredentialDescriptors() {
		if descriptor.Harness == id || descriptor.CanDeliverTo(id) {
			descriptors = append(descriptors, descriptor)
		}
	}
	return descriptors
}

func harnessFileArtifact(path, description string) harnessManagedArtifact {
	return harnessManagedArtifact{
		Path:        path,
		Kind:        harnessArtifactFile,
		Description: description,
	}
}

func harnessDirArtifact(path, description string) harnessManagedArtifact {
	return harnessManagedArtifact{
		Path:        path,
		Kind:        harnessArtifactDir,
		Description: description,
	}
}

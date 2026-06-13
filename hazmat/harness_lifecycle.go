package hazmat

import (
	"encoding/json"
	"fmt"
	"hazmat/internal/harnessruntime"
	"os"
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

type harnessManagedArtifactKind = harnessruntime.ArtifactKind

const (
	harnessArtifactFile harnessManagedArtifactKind = harnessruntime.ArtifactFile
	harnessArtifactDir  harnessManagedArtifactKind = harnessruntime.ArtifactDir
)

type harnessArtifactOwnership = harnessruntime.ArtifactOwnership

type harnessArtifactSymlinkPolicy = harnessruntime.ArtifactSymlinkPolicy

type harnessManagedArtifact = harnessruntime.Artifact

type harnessArtifactStatus = harnessruntime.ArtifactStatus

type harnessStateStatus struct {
	Status       string `json:"status"`
	Summary      string `json:"summary"`
	Recorded     bool   `json:"recorded"`
	StateVersion string `json:"state_version,omitempty"`
	Current      bool   `json:"current"`
	Error        string `json:"error,omitempty"`
}

type harnessImportStatus struct {
	Supported   bool   `json:"supported"`
	Status      string `json:"status"`
	Summary     string `json:"summary"`
	LastRunAt   string `json:"last_run_at,omitempty"`
	Boundary    string `json:"boundary,omitempty"`
	StateError  string `json:"state_error,omitempty"`
	NextCommand string `json:"next_command,omitempty"`
}

type harnessCredentialEntryStatus struct {
	ID          credentialID              `json:"id"`
	DisplayName string                    `json:"display_name"`
	Status      credentialInventoryStatus `json:"status"`
	Kind        credentialKind            `json:"kind"`
	Backend     credentialStorageBackend  `json:"backend"`
	Delivery    credentialDeliveryMode    `json:"delivery"`
	Support     credentialSupportStatus   `json:"support"`
}

type harnessCredentialStatus struct {
	Summary         string                         `json:"summary"`
	DescriptorCount int                            `json:"descriptor_count"`
	Configured      int                            `json:"configured"`
	NotConfigured   int                            `json:"not_configured"`
	External        int                            `json:"external"`
	AdapterRequired int                            `json:"adapter_required"`
	NeedsRepair     int                            `json:"needs_repair"`
	Errors          int                            `json:"errors"`
	Error           string                         `json:"error,omitempty"`
	Entries         []harnessCredentialEntryStatus `json:"entries,omitempty"`
}

type harnessLifecycleStatus string

const (
	harnessLifecycleOK                     harnessLifecycleStatus = "ok"
	harnessLifecycleNotInstalled           harnessLifecycleStatus = "not_installed"
	harnessLifecycleInstalledUnrecorded    harnessLifecycleStatus = "installed_unrecorded"
	harnessLifecycleRecordedMissingBinary  harnessLifecycleStatus = "recorded_missing_binary"
	harnessLifecycleStateVersionStale      harnessLifecycleStatus = "state_version_stale"
	harnessLifecycleProbeFailed            harnessLifecycleStatus = "probe_failed"
	harnessLifecycleCredentialRepairNeeded harnessLifecycleStatus = "credential_repair_needed"
	harnessLifecycleStateUnreadable        harnessLifecycleStatus = "state_unreadable"
)

type harnessStatus struct {
	Harness          ManagedHarness
	Probe            harnessProbe
	LifecycleStatus  harnessLifecycleStatus
	StateStatus      harnessStateStatus
	ImportStatus     harnessImportStatus
	CredentialStatus harnessCredentialStatus
	Artifacts        []harnessArtifactStatus
	Preserved        []string
	NextAction       string
	StateErr         error
}

type harnessUninstallPlan = harnessruntime.UninstallPlan

const harnessLifecycleStatusFormatVersion = "hazmat.harness.status.v1"

var inspectHarnessCredentialStatusForStatus = inspectHarnessCredentialStatus

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
			return runHarnessStatus("", false)
		},
	}

	var statusJSON bool
	statusCmd := &cobra.Command{
		Use:   "status [harness]",
		Short: "Show per-harness install, state, and credential status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			return runHarnessStatus(id, statusJSON)
		},
	}
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "Emit machine-readable harness status JSON")
	cmd.AddCommand(statusCmd)

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

func runHarnessStatus(input string, outputJSON bool) error {
	state, stateErr := loadState()
	if strings.TrimSpace(input) != "" {
		id, err := normalizeManagedHarnessID(input)
		if err != nil {
			return err
		}
		harness, _ := managedHarnessByID(id)
		status := inspectManagedHarnessStatus(harness, state, stateErr, asAgentOutput)
		if outputJSON {
			return writeHarnessStatusJSON([]harnessStatus{status})
		}
		printHarnessDetailStatus(status)
		return nil
	}

	statuses := make([]harnessStatus, 0, len(managedHarnessRegistry))
	for _, harness := range managedHarnesses() {
		statuses = append(statuses, inspectManagedHarnessStatus(harness, state, stateErr, asAgentOutput))
	}
	if outputJSON {
		return writeHarnessStatusJSON(statuses)
	}

	fmt.Println()
	cBold.Println("  Agent harnesses")
	fmt.Println()
	fmt.Printf("  %-12s %-24s %-18s %-18s %s\n", "Harness", "Lifecycle", "State", "Import", "Next")
	for _, status := range statuses {
		fmt.Printf("  %-12s %-24s %-18s %-18s %s\n",
			status.Harness.Spec.ID,
			truncateStatusField(string(status.LifecycleStatus), 24),
			truncateStatusField(status.StateStatus.Summary, 18),
			truncateStatusField(status.ImportStatus.Summary, 18),
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

	stateStatus := inspectHarnessStateStatus(harness, state, stateErr)
	importStatus := inspectHarnessImportStatus(harness, state, stateErr)
	credentialStatus := inspectHarnessCredentialStatusForStatus(harness.Spec.ID)
	artifacts := inspectHarnessArtifactStatuses(harness, read)

	return harnessStatus{
		Harness:          harness,
		Probe:            probe,
		LifecycleStatus:  classifyHarnessLifecycleStatus(probe, stateStatus, credentialStatus),
		StateStatus:      stateStatus,
		ImportStatus:     importStatus,
		CredentialStatus: credentialStatus,
		Artifacts:        artifacts,
		Preserved:        append([]string(nil), harness.PreservedArtifacts...),
		NextAction:       nextHarnessAction(harness, probe, state, stateErr),
		StateErr:         stateErr,
	}
}

func formatHarnessInstalledStatus(probe harnessProbe) string {
	if probe.Installed {
		return "yes"
	}
	return "no"
}

func inspectHarnessStateStatus(harness ManagedHarness, state HazmatState, stateErr error) harnessStateStatus {
	if stateErr != nil {
		return harnessStateStatus{
			Status:  "unreadable",
			Summary: "state unreadable",
			Error:   stateErr.Error(),
		}
	}
	recorded, ok := state.Harnesses[harness.Spec.ID]
	if !ok || recorded.StateVersion == "" {
		return harnessStateStatus{
			Status:  "not_recorded",
			Summary: "not recorded",
		}
	}
	if !harnessruntime.StateCurrent(recorded, harness.Spec) {
		return harnessStateStatus{
			Status:       "stale",
			Summary:      fmt.Sprintf("outdated v%s", recorded.StateVersion),
			Recorded:     true,
			StateVersion: recorded.StateVersion,
		}
	}
	return harnessStateStatus{
		Status:       "current",
		Summary:      "recorded v" + recorded.StateVersion,
		Recorded:     true,
		StateVersion: recorded.StateVersion,
		Current:      true,
	}
}

func inspectHarnessImportStatus(harness ManagedHarness, state HazmatState, stateErr error) harnessImportStatus {
	policy := harness.ImportPolicy
	if !policy.Supported {
		summary := "not supported"
		if policy.Boundary != "" {
			summary += " (" + policy.Boundary + ")"
		}
		return harnessImportStatus{
			Supported: false,
			Status:    "not_supported",
			Summary:   summary,
			Boundary:  policy.Boundary,
		}
	}
	if stateErr != nil {
		return harnessImportStatus{
			Supported:  true,
			Status:     "state_unreadable",
			Summary:    "state unreadable",
			Boundary:   policy.Boundary,
			StateError: stateErr.Error(),
		}
	}
	recorded, ok := state.Harnesses[harness.Spec.ID]
	if !ok || recorded.LastImportRunAt == "" {
		return harnessImportStatus{
			Supported:   true,
			Status:      "not_imported",
			Summary:     "not imported",
			Boundary:    policy.Boundary,
			NextCommand: "hazmat config import " + string(harness.Spec.ID),
		}
	}
	return harnessImportStatus{
		Supported: true,
		Status:    "imported",
		Summary:   recorded.LastImportRunAt,
		LastRunAt: recorded.LastImportRunAt,
		Boundary:  policy.Boundary,
	}
}

func inspectHarnessArtifactStatuses(harness ManagedHarness, read func(args ...string) (string, error)) []harnessArtifactStatus {
	if harness.ManagedCodeArtifacts == nil {
		return nil
	}
	artifacts := harness.ManagedCodeArtifacts()
	statuses := make([]harnessArtifactStatus, 0, len(artifacts))
	for _, artifact := range artifacts {
		statuses = append(statuses, inspectHarnessArtifact(read, artifact))
	}
	return statuses
}

func classifyHarnessLifecycleStatus(probe harnessProbe, state harnessStateStatus, credentials harnessCredentialStatus) harnessLifecycleStatus {
	switch {
	case state.Status == "unreadable":
		return harnessLifecycleStateUnreadable
	case !probe.Installed && state.Recorded:
		return harnessLifecycleRecordedMissingBinary
	case !probe.Installed:
		return harnessLifecycleNotInstalled
	case probe.VersionErr != "":
		return harnessLifecycleProbeFailed
	case state.Status == "stale":
		return harnessLifecycleStateVersionStale
	case state.Status == "not_recorded":
		return harnessLifecycleInstalledUnrecorded
	case credentials.NeedsRepair > 0:
		return harnessLifecycleCredentialRepairNeeded
	default:
		return harnessLifecycleOK
	}
}

func nextHarnessAction(harness ManagedHarness, probe harnessProbe, state HazmatState, stateErr error) string {
	if !probe.Installed {
		return "hazmat harness update " + string(harness.Spec.ID)
	}
	if stateErr != nil {
		return "repair state before update"
	}
	recorded, ok := state.Harnesses[harness.Spec.ID]
	if !ok || !harnessruntime.StateCurrent(recorded, harness.Spec) {
		return "hazmat harness update " + string(harness.Spec.ID)
	}
	return "hazmat harness update " + string(harness.Spec.ID)
}

func printHarnessDetailStatus(status harnessStatus) {
	harness := status.Harness
	fmt.Println()
	cBold.Printf("  %s (%s)\n", harness.Spec.DisplayName, harness.Spec.ID)
	fmt.Println()
	fmt.Printf("  Lifecycle:   %s\n", status.LifecycleStatus)
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
	fmt.Printf("  State:       %s\n", status.StateStatus.Summary)
	if status.StateErr != nil {
		fmt.Printf("  State error: %v\n", status.StateErr)
	}
	fmt.Printf("  Import:      %s\n", status.ImportStatus.Summary)
	fmt.Printf("  Credentials: %s\n", status.CredentialStatus.Summary)
	fmt.Printf("  Update:      hazmat harness update %s\n", harness.Spec.ID)
	fmt.Printf("  Uninstall:   hazmat harness uninstall %s\n", harness.Spec.ID)
	fmt.Println()
	printHarnessArtifactBoundaries(status)
	fmt.Println()
}

func printHarnessArtifactBoundaries(status harnessStatus) {
	fmt.Println("  Hazmat-owned code artifacts:")
	if len(status.Artifacts) == 0 {
		fmt.Println("    - none")
	} else {
		for _, artifact := range status.Artifacts {
			fmt.Printf("    - %s (%s)\n", artifact.Artifact.Path, formatHarnessArtifactStatus(artifact))
		}
	}

	fmt.Println("  Preserved by default:")
	if len(status.Preserved) == 0 {
		fmt.Println("    - auth, profile, sessions, and provider state")
		return
	}
	for _, item := range status.Preserved {
		fmt.Printf("    - %s\n", item)
	}
}

type harnessStatusJSONEnvelope struct {
	FormatVersion string              `json:"format_version"`
	Harnesses     []harnessStatusJSON `json:"harnesses"`
}

type harnessStatusJSON struct {
	ID                       HarnessID                   `json:"id"`
	DisplayName              string                      `json:"display_name"`
	LifecycleStatus          harnessLifecycleStatus      `json:"lifecycle_status"`
	Installed                bool                        `json:"installed"`
	BinaryPath               string                      `json:"binary_path,omitempty"`
	Version                  string                      `json:"version,omitempty"`
	VersionUnavailableReason string                      `json:"version_unavailable_reason,omitempty"`
	MissingReason            string                      `json:"missing_reason,omitempty"`
	LaunchCommand            string                      `json:"launch_command"`
	UpdateCommand            string                      `json:"update_command"`
	UninstallCommand         string                      `json:"uninstall_command"`
	NextAction               string                      `json:"next_action"`
	State                    harnessStateStatus          `json:"state"`
	Import                   harnessImportStatus         `json:"import"`
	Credentials              harnessCredentialStatus     `json:"credentials"`
	OwnedArtifacts           []harnessArtifactStatusJSON `json:"owned_artifacts"`
	PreservedByDefault       []string                    `json:"preserved_by_default"`
}

type harnessArtifactStatusJSON struct {
	Path            string                       `json:"path"`
	Kind            harnessManagedArtifactKind   `json:"kind"`
	Description     string                       `json:"description"`
	Ownership       harnessArtifactOwnership     `json:"ownership"`
	SymlinkPolicy   harnessArtifactSymlinkPolicy `json:"symlink_policy"`
	PackageManager  string                       `json:"package_manager,omitempty"`
	PackageName     string                       `json:"package_name,omitempty"`
	DetectedPackage string                       `json:"detected_package,omitempty"`
	CreatedByUpdate bool                         `json:"created_by_update"`
	Exists          bool                         `json:"exists"`
	Symlink         bool                         `json:"symlink"`
	Drift           string                       `json:"drift,omitempty"`
}

func writeHarnessStatusJSON(statuses []harnessStatus) error {
	envelope := harnessStatusJSONEnvelope{
		FormatVersion: harnessLifecycleStatusFormatVersion,
		Harnesses:     make([]harnessStatusJSON, 0, len(statuses)),
	}
	for _, status := range statuses {
		envelope.Harnesses = append(envelope.Harnesses, harnessStatusForJSON(status))
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

func harnessStatusForJSON(status harnessStatus) harnessStatusJSON {
	harness := status.Harness
	return harnessStatusJSON{
		ID:                       harness.Spec.ID,
		DisplayName:              harness.Spec.DisplayName,
		LifecycleStatus:          status.LifecycleStatus,
		Installed:                status.Probe.Installed,
		BinaryPath:               status.Probe.BinaryPath,
		Version:                  status.Probe.Version,
		VersionUnavailableReason: status.Probe.VersionErr,
		MissingReason:            status.Probe.MissingReason,
		LaunchCommand:            harness.LaunchCommand,
		UpdateCommand:            "hazmat harness update " + string(harness.Spec.ID),
		UninstallCommand:         "hazmat harness uninstall " + string(harness.Spec.ID),
		NextAction:               status.NextAction,
		State:                    status.StateStatus,
		Import:                   status.ImportStatus,
		Credentials:              status.CredentialStatus,
		OwnedArtifacts:           harnessArtifactStatusesForJSON(status.Artifacts),
		PreservedByDefault:       append([]string(nil), status.Preserved...),
	}
}

func harnessArtifactStatusesForJSON(statuses []harnessArtifactStatus) []harnessArtifactStatusJSON {
	out := make([]harnessArtifactStatusJSON, 0, len(statuses))
	for _, status := range statuses {
		artifact := harnessruntime.NormalizeArtifact(status.Artifact)
		out = append(out, harnessArtifactStatusJSON{
			Path:            artifact.Path,
			Kind:            artifact.Kind,
			Description:     artifact.Description,
			Ownership:       artifact.Ownership,
			SymlinkPolicy:   artifact.SymlinkPolicy,
			PackageManager:  artifact.PackageManager,
			PackageName:     artifact.PackageName,
			DetectedPackage: status.PackageName,
			CreatedByUpdate: artifact.CreatedByUpdate,
			Exists:          status.Exists,
			Symlink:         status.Symlink,
			Drift:           status.Drift,
		})
	}
	return out
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
	if !flagDryRun {
		if _, err := requireAgentUser(); err != nil {
			return err
		}
	}

	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll || force}
	r := NewRunner(ui, flagVerbose, flagDryRun)
	if r.DryRun {
		ui.Step(fmt.Sprintf("Verify agent user %q", agentUser))
		ui.Ok(fmt.Sprintf("Would verify agent user %s exists", agentUser))
	}

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
		return fmt.Errorf("refusing to uninstall %s because planned artifacts drifted: %s\nre-run with --force to remove the exact planned paths", plan.Spec.ID, strings.Join(drift, "; "))
	}
	if !ui.Ask("Remove these Hazmat-owned harness artifacts?") {
		fmt.Println()
		return nil
	}

	remove := func(reason string, args ...string) error {
		if err := r.AsAgent(reason, args...); err != nil {
			return err
		}
		if r.ui != nil && len(args) > 0 {
			r.ui.Ok("Removed " + args[len(args)-1])
		}
		return nil
	}
	if err := harnessruntime.ExecuteUninstallPlan(plan, harnessruntime.UninstallOptions{
		Store:     stateStore(),
		Remove:    remove,
		AgentHome: agentHome,
		Force:     force,
		DryRun:    r.DryRun,
	}); err != nil {
		return err
	}
	if plan.MetadataPresent {
		if r.DryRun {
			cYellow.Printf("  Dry-run: would remove %s harness metadata\n", harness.Spec.ID)
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
	return harnessruntime.BuildUninstallPlan(stateStore(), read, agentHome, harness.Spec, artifacts, harness.PreservedArtifacts)
}

func printHarnessUninstallPlan(plan harnessUninstallPlan) {
	fmt.Println()
	cBold.Printf("  Harness uninstall: %s (%s)\n", plan.Spec.DisplayName, plan.Spec.ID)
	fmt.Println()
	fmt.Println("  Remove:")
	if len(plan.Artifacts) == 0 {
		fmt.Println("    - no Hazmat-owned code artifacts declared")
	} else {
		for _, artifact := range plan.Artifacts {
			fmt.Printf("    - %s (%s)\n", artifact.Artifact.Path, formatHarnessArtifactStatus(artifact))
		}
	}
	if plan.MetadataPresent {
		fmt.Printf("    - %s entry in %s\n", plan.Spec.ID, stateFilePath)
	} else {
		fmt.Printf("    - %s metadata: not recorded\n", plan.Spec.ID)
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
	return harnessruntime.InspectArtifact(read, agentHome, artifact)
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

func inspectHarnessCredentialStatus(id HarnessID) harnessCredentialStatus {
	descriptors := credentialDescriptorsForHarnessLifecycle(id)
	status := harnessCredentialStatus{DescriptorCount: len(descriptors)}
	if len(descriptors) == 0 {
		status.Summary = "none"
		return status
	}

	entries, err := inspectCredentialInventory("")
	if err != nil {
		status.Summary = "inventory unavailable; run hazmat config agent"
		status.Error = err.Error()
		return status
	}
	relevant := make(map[credentialID]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		relevant[descriptor.ID] = struct{}{}
	}

	for _, entry := range entries {
		if _, ok := relevant[entry.ID]; !ok {
			continue
		}
		entryStatus := entry.Status()
		status.Entries = append(status.Entries, harnessCredentialEntryStatus{
			ID:          entry.ID,
			DisplayName: entry.DisplayName,
			Status:      entryStatus,
			Kind:        entry.Kind,
			Backend:     entry.Backend,
			Delivery:    entry.Delivery,
			Support:     entry.Support,
		})
		switch entryStatus {
		case credentialInventoryConfigured:
			status.Configured++
		case credentialInventoryNotConfigured:
			status.NotConfigured++
		case credentialInventoryExternal:
			status.External++
		case credentialInventoryAdapterRequired:
			status.AdapterRequired++
		case credentialInventoryNeedsRepair:
			status.NeedsRepair++
		case credentialInventoryError:
			status.Errors++
		}
	}
	parts := []string{fmt.Sprintf("%d configured", status.Configured)}
	if status.NotConfigured > 0 {
		parts = append(parts, fmt.Sprintf("%d unset", status.NotConfigured))
	}
	if status.External > 0 {
		parts = append(parts, fmt.Sprintf("%d external", status.External))
	}
	if status.AdapterRequired > 0 {
		parts = append(parts, fmt.Sprintf("%d adapter-required", status.AdapterRequired))
	}
	if status.NeedsRepair > 0 {
		parts = append(parts, fmt.Sprintf("%d need repair", status.NeedsRepair))
	}
	if status.Errors > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", status.Errors))
	}
	status.Summary = strings.Join(parts, ", ") + "; hazmat config agent"
	return status
}

func harnessFileArtifact(path, description string) harnessManagedArtifact {
	return harnessruntime.FileArtifact(path, description)
}

func harnessDirArtifact(path, description string) harnessManagedArtifact {
	return harnessruntime.DirArtifact(path, description)
}

func harnessNpmPackageDirArtifact(path, packageName, description string) harnessManagedArtifact {
	return harnessruntime.NpmPackageDirArtifact(path, packageName, description)
}

func harnessSymlinkArtifact(path, description string) harnessManagedArtifact {
	return harnessruntime.SymlinkArtifact(path, description)
}

func harnessRegularFileArtifact(path, description string) harnessManagedArtifact {
	return harnessruntime.RegularFileArtifact(path, description)
}

func formatHarnessArtifactStatus(status harnessArtifactStatus) string {
	return harnessruntime.FormatArtifactStatus(status)
}

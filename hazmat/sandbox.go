package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	sandboxBackendDockerSandboxes = "docker-sandboxes"
	sandboxPolicyProfileBaseline  = "baseline"
)

var (
	minDockerDesktopVersion = semver{major: 4, minor: 58, patch: 0}
	minComposeVersion       = semver{major: 2, minor: 40, patch: 2}
	minShellSandboxVersion  = semver{major: 4, minor: 61, patch: 0}
	minExtraWorkspaceVer    = semver{major: 4, minor: 61, patch: 0}
	semverPattern           = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)
	sandboxHostPattern      = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9][A-Za-z0-9.-]*$`)
	sandboxNamePattern      = regexp.MustCompile(`[^a-z0-9]+`)
	sandboxNow              = func() time.Time { return time.Now().UTC() }
	sandboxProbeFactory     = func() sandboxProbe { return hostSandboxProbe{} }
)

var sandboxApprovalsFilePath = filepath.Join(os.Getenv("HOME"), ".hazmat/sandbox-approvals.yaml")

type semver struct {
	major int
	minor int
	patch int
}

type sandboxPolicyProfile struct {
	Name       string
	Policy     string
	AllowHosts []string
}

type sandboxApprovalRecord struct {
	ProjectDir    string `yaml:"project"`
	BackendType   string `yaml:"backend"`
	PolicyProfile string `yaml:"policy_profile"`
	ApprovedAt    string `yaml:"approved_at,omitempty"`
}

type sandboxApprovalsFile struct {
	Approvals []sandboxApprovalRecord `yaml:"approvals"`
}

type sandboxDoctorCheck struct {
	Name   string
	Passed bool
	Detail string
}

type sandboxDoctorReport struct {
	Backend        string
	DesktopVersion string
	ComposeVersion string
	PolicyProfile  sandboxPolicyProfile
	Checks         []sandboxDoctorCheck
}

type sandboxListResponse struct {
	VMs []sandboxVM `json:"vms"`
}

type sandboxVM struct {
	Name string `json:"name"`
}

type sandboxProbe interface {
	LookPath(name string) (string, error)
	Output(name string, args ...string) (string, error)
	Run(name string, args ...string) error
}

type sandboxLaunchSpec struct {
	Name    string
	Agent   string
	Config  sessionConfig
	Profile sandboxPolicyProfile
}

type sandboxBackendAdapter interface {
	Type() string
	ValidateLaunchCompatibility(spec sandboxLaunchSpec, backend *SandboxBackendConfig, version semver) error
	PrepareLaunch(probe sandboxProbe, spec sandboxLaunchSpec) error
	RunClaudeSession(probe sandboxProbe, sandboxName string, forwarded []string) error
	RunShellSession(probe sandboxProbe, sandboxName, projectDir string) error
	RunExecSession(probe sandboxProbe, sandboxName, projectDir string, commandArgs []string) error
	RemoveManagedSandboxes(probe sandboxProbe, sandboxes []ManagedSandboxConfig) error
}

type hostSandboxProbe struct{}
type dockerSandboxesBackend struct{}

func (hostSandboxProbe) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (hostSandboxProbe) Output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (hostSandboxProbe) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func sandboxBackendAdapterForType(kind string) (sandboxBackendAdapter, error) {
	switch kind {
	case sandboxBackendDockerSandboxes:
		return dockerSandboxesBackend{}, nil
	case "":
		return nil, fmt.Errorf("sandbox backend type is empty")
	default:
		return nil, fmt.Errorf("sandbox backend %q is not supported", kind)
	}
}

func (dockerSandboxesBackend) Type() string {
	return sandboxBackendDockerSandboxes
}

func (dockerSandboxesBackend) ValidateLaunchCompatibility(spec sandboxLaunchSpec, _ *SandboxBackendConfig, version semver) error {
	if spec.Agent == "shell" && !version.AtLeast(minShellSandboxVersion) {
		return fmt.Errorf("Docker Desktop %s is too old for shell sandboxes; hazmat shell --sandbox and hazmat exec --sandbox require >= %s",
			version.String(), minShellSandboxVersion.String())
	}
	if len(spec.Config.ReadDirs) > 0 && !version.AtLeast(minExtraWorkspaceVer) {
		return fmt.Errorf("Docker Desktop %s is too old for additional read-only workspaces; --sandbox with -R or auto-added read dirs requires >= %s",
			version.String(), minExtraWorkspaceVer.String())
	}
	return nil
}

func (dockerSandboxesBackend) PrepareLaunch(probe sandboxProbe, spec sandboxLaunchSpec) error {
	args := []string{"sandbox", "create", "--name", spec.Name, spec.Agent, spec.Config.ProjectDir}
	for _, dir := range spec.Config.ReadDirs {
		args = append(args, dir+":ro")
	}
	out, err := probe.Output("docker", args...)
	if err != nil && !sandboxAlreadyExists(out) {
		return fmt.Errorf("create Docker Sandbox %s: %s", spec.Name, oneLine(out))
	}

	policyArgs := []string{"sandbox", "network", "proxy", spec.Name, "--policy", spec.Profile.Policy}
	for _, host := range spec.Profile.AllowHosts {
		policyArgs = append(policyArgs, "--allow-host", host)
	}
	out, err = probe.Output("docker", policyArgs...)
	if err != nil {
		return fmt.Errorf("apply Docker Sandbox policy to %s: %s", spec.Name, oneLine(out))
	}
	return nil
}

func (dockerSandboxesBackend) RunClaudeSession(probe sandboxProbe, sandboxName string, forwarded []string) error {
	args := []string{"sandbox", "run", sandboxName}
	if len(forwarded) > 0 {
		args = append(args, "--")
		args = append(args, forwarded...)
	}
	return probe.Run("docker", args...)
}

func (dockerSandboxesBackend) RunShellSession(probe sandboxProbe, sandboxName, projectDir string) error {
	return probe.Run("docker", "sandbox", "run", sandboxName, "--",
		"-lc", `cd "$1" && exec /bin/bash -il`, "bash", projectDir)
}

func (dockerSandboxesBackend) RunExecSession(probe sandboxProbe, sandboxName, projectDir string, commandArgs []string) error {
	args := []string{"sandbox", "run", sandboxName, "--",
		"-lc", `cd "$1" && shift && exec "$@"`, "bash", projectDir}
	args = append(args, commandArgs...)
	return probe.Run("docker", args...)
}

func (dockerSandboxesBackend) RemoveManagedSandboxes(probe sandboxProbe, sandboxes []ManagedSandboxConfig) error {
	for _, sandbox := range sandboxes {
		out, err := probe.Output("docker", "sandbox", "rm", sandbox.Name)
		if err != nil {
			if sandboxMissing(out) {
				continue
			}
			return fmt.Errorf("remove Docker Sandbox %s: %s", sandbox.Name, oneLine(out))
		}
	}
	return nil
}

func newSandboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage Tier 3 backend setup and diagnostics",
		Long: `Manage Hazmat's Tier 3 backend state.

Phase 1 adds backend setup and diagnostics only. Session routing remains
unchanged until a later phase.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Verify the Tier 3 backend is healthy",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSandboxDoctor()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "setup",
		Short: "Validate and record the supported Tier 3 backend",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSandboxSetup()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "reset",
		Short: "Forget the configured Tier 3 backend",
		Long: `Forget Hazmat's recorded Tier 3 backend configuration.

Phase 1 only clears Hazmat's local backend state. It does not remove any
Docker Sandboxes because Hazmat is not managing session sandboxes yet.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSandboxReset()
		},
	})

	return cmd
}

func runSandboxDoctor() error {
	report := collectSandboxDoctorReport(sandboxProbeFactory())
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	printSandboxDoctorReport(report, cfg.SandboxBackend())
	if !report.Healthy() {
		return fmt.Errorf("sandbox backend is not healthy")
	}
	return nil
}

func runSandboxSetup() error {
	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	report := collectSandboxDoctorReport(sandboxProbeFactory())
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	printSandboxDoctorReport(report, cfg.SandboxBackend())
	if !report.Healthy() {
		return fmt.Errorf("sandbox backend is not healthy")
	}

	backend := &SandboxBackendConfig{
		Type:           sandboxBackendDockerSandboxes,
		PolicyProfile:  report.PolicyProfile.Name,
		DesktopVersion: report.DesktopVersion,
		ComposeVersion: report.ComposeVersion,
		ConfiguredAt:   sandboxNow().Format(time.RFC3339),
	}

	fmt.Println()
	cBold.Println("  Sandbox setup")
	fmt.Println()
	fmt.Printf("    Backend:         %s\n", formatSandboxBackendLabel(backend.Type))
	fmt.Printf("    Policy profile:  %s\n", backend.PolicyProfile)
	fmt.Printf("    Desktop version: %s\n", backend.DesktopVersion)
	fmt.Printf("    Compose version: %s\n", backend.ComposeVersion)
	fmt.Println()

	if flagDryRun {
		cYellow.Println("  Dry-run: would save sandbox backend configuration")
		fmt.Println()
		return nil
	}

	cfg.Sandbox.Backend = backend
	if err := saveConfig(cfg); err != nil {
		return err
	}

	ui.Ok("Saved Tier 3 backend configuration")
	cDim.Println("  Phase 1 complete: backend checks are recorded, but session routing is unchanged.")
	fmt.Println()
	return nil
}

func runSandboxReset() error {
	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	backend := cfg.SandboxBackend()
	managed := cfg.ManagedSandboxes()
	fmt.Println()
	cBold.Println("  Sandbox reset")
	fmt.Println()
	if backend == nil && len(managed) == 0 {
		cDim.Println("  No Tier 3 backend is currently configured.")
		fmt.Println()
		return nil
	}

	if backend != nil {
		fmt.Printf("    Backend:         %s\n", formatSandboxBackendLabel(backend.Type))
		fmt.Printf("    Policy profile:  %s\n", backend.PolicyProfile)
	} else {
		fmt.Printf("    Backend:         (not configured)\n")
	}
	if len(managed) > 0 {
		fmt.Printf("    Managed:         %d sandbox(es)\n", len(managed))
		for _, sandbox := range managed {
			fmt.Printf("      - %s (%s, %s)\n", sandbox.Name, sandbox.Agent, sandbox.ProjectDir)
		}
	} else {
		fmt.Printf("    Managed:         (none)\n")
	}
	fmt.Println()

	if !ui.Ask("Forget the configured Tier 3 backend and remove managed sandboxes?") {
		fmt.Println()
		return nil
	}

	if flagDryRun {
		cYellow.Println("  Dry-run: would clear sandbox backend configuration")
		for _, sandbox := range managed {
			cYellow.Printf("  Dry-run: would remove Docker Sandbox %s\n", sandbox.Name)
		}
		fmt.Println()
		return nil
	}

	if err := removeManagedSandboxes(sandboxProbeFactory(), managed); err != nil {
		return err
	}

	cfg.Sandbox.Backend = nil
	cfg.Sandbox.Managed = nil
	if err := saveConfig(cfg); err != nil {
		return err
	}

	ui.Ok("Cleared Tier 3 backend configuration")
	if len(managed) > 0 {
		cDim.Println("  Removed Hazmat-managed Docker Sandboxes.")
	}
	fmt.Println()
	return nil
}

func collectSandboxDoctorReport(probe sandboxProbe) sandboxDoctorReport {
	report := sandboxDoctorReport{
		Backend:       sandboxBackendDockerSandboxes,
		PolicyProfile: defaultSandboxPolicyProfile(),
	}

	if _, err := probe.LookPath("docker"); err != nil {
		report.addCheck("Docker CLI", false, "docker command not found in PATH")
	} else {
		report.addCheck("Docker CLI", true, "docker command found")
	}

	desktopOut, desktopErr := probe.Output("docker", "desktop", "version", "--short")
	if desktopErr != nil {
		report.addCheck("Docker Desktop version", false,
			fmt.Sprintf("docker desktop version --short failed: %s", oneLine(desktopOut)))
	} else if version, err := extractToolSemver(desktopOut); err != nil {
		report.addCheck("Docker Desktop version", false, err.Error())
	} else if !version.AtLeast(minDockerDesktopVersion) {
		report.addCheck("Docker Desktop version", false,
			fmt.Sprintf("found %s, need >= %s", version.String(), minDockerDesktopVersion.String()))
	} else {
		report.DesktopVersion = version.String()
		report.addCheck("Docker Desktop version", true,
			fmt.Sprintf("found %s (requires >= %s)", version.String(), minDockerDesktopVersion.String()))
	}

	composeOut, composeErr := probe.Output("docker", "compose", "version")
	if composeErr != nil {
		report.addCheck("Docker Compose version", false,
			fmt.Sprintf("docker compose version failed: %s", oneLine(composeOut)))
	} else if version, err := extractToolSemver(composeOut); err != nil {
		report.addCheck("Docker Compose version", false, err.Error())
	} else if !version.AtLeast(minComposeVersion) {
		report.addCheck("Docker Compose version", false,
			fmt.Sprintf("found %s, need >= %s", version.String(), minComposeVersion.String()))
	} else {
		report.ComposeVersion = version.String()
		report.addCheck("Docker Compose version", true,
			fmt.Sprintf("found %s (requires >= %s)", version.String(), minComposeVersion.String()))
	}

	sandboxLSOut, sandboxLSErr := probe.Output("docker", "sandbox", "ls", "--json")
	if sandboxLSErr != nil {
		report.addCheck("Docker Sandboxes control plane", false,
			fmt.Sprintf("docker sandbox ls --json failed: %s", oneLine(sandboxLSOut)))
	} else if vmCount, err := validateSandboxListJSON(sandboxLSOut); err != nil {
		report.addCheck("Docker Sandboxes control plane", false, err.Error())
	} else {
		report.addCheck("Docker Sandboxes control plane", true,
			fmt.Sprintf("docker sandbox ls --json returned %d VM(s)", vmCount))
	}

	policyHelpOut, policyHelpErr := probe.Output("docker", "sandbox", "network", "proxy", "--help")
	if policyHelpErr != nil {
		report.addCheck("Docker Sandboxes policy command", false,
			fmt.Sprintf("docker sandbox network proxy --help failed: %s", oneLine(policyHelpOut)))
	} else if err := validateSandboxProxyHelp(policyHelpOut); err != nil {
		report.addCheck("Docker Sandboxes policy command", false, err.Error())
	} else {
		report.addCheck("Docker Sandboxes policy command", true,
			"--policy and --allow-host flags detected")
	}

	if err := validateSandboxPolicyProfile(report.PolicyProfile); err != nil {
		report.addCheck("Hazmat policy profile", false, err.Error())
	} else {
		report.addCheck("Hazmat policy profile", true,
			fmt.Sprintf("%s profile uses %s mode with %d allow-host entries",
				report.PolicyProfile.Name, report.PolicyProfile.Policy, len(report.PolicyProfile.AllowHosts)))
	}

	return report
}

func (r *sandboxDoctorReport) addCheck(name string, passed bool, detail string) {
	r.Checks = append(r.Checks, sandboxDoctorCheck{
		Name:   name,
		Passed: passed,
		Detail: detail,
	})
}

func (r sandboxDoctorReport) Healthy() bool {
	for _, check := range r.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func printSandboxDoctorReport(report sandboxDoctorReport, configured *SandboxBackendConfig) {
	fmt.Println()
	cBold.Println("  Hazmat sandbox doctor")
	fmt.Println()
	fmt.Printf("    Target backend:   %s\n", formatSandboxBackendLabel(report.Backend))
	fmt.Printf("    Policy profile:   %s\n", report.PolicyProfile.Name)
	if configured != nil {
		fmt.Printf("    Config status:    configured (%s)\n", configured.PolicyProfile)
	} else {
		fmt.Printf("    Config status:    not configured\n")
	}
	fmt.Println()

	for _, check := range report.Checks {
		if check.Passed {
			cGreen.Print("  ✓ ")
		} else {
			cRed.Print("  ✗ ")
		}
		fmt.Printf("%s", check.Name)
		if check.Detail != "" {
			fmt.Printf(": %s", check.Detail)
		}
		fmt.Println()
	}

	fmt.Println()
	if report.Healthy() {
		if configured == nil {
			cYellow.Println("  Backend checks passed. Run 'hazmat sandbox setup' to record this backend.")
		} else {
			cGreen.Println("  Backend checks passed.")
		}
	} else {
		cRed.Println("  Backend checks failed.")
	}
	fmt.Println()
}

func defaultSandboxPolicyProfile() sandboxPolicyProfile {
	return sandboxPolicyProfile{
		Name:   sandboxPolicyProfileBaseline,
		Policy: "deny",
		AllowHosts: []string{
			"api.anthropic.com",
			"github.com",
			"registry.npmjs.org",
		},
	}
}

func validateSandboxPolicyProfile(profile sandboxPolicyProfile) error {
	if profile.Name == "" {
		return fmt.Errorf("policy profile name is required")
	}
	if profile.Policy != "deny" {
		return fmt.Errorf("policy profile %q must use deny mode", profile.Name)
	}
	if len(profile.AllowHosts) == 0 {
		return fmt.Errorf("policy profile %q must allow at least one host", profile.Name)
	}
	seen := make(map[string]struct{}, len(profile.AllowHosts))
	for _, host := range profile.AllowHosts {
		if !sandboxHostPattern.MatchString(host) {
			return fmt.Errorf("policy profile %q has invalid allow-host %q", profile.Name, host)
		}
		if _, dup := seen[host]; dup {
			return fmt.Errorf("policy profile %q duplicates allow-host %q", profile.Name, host)
		}
		seen[host] = struct{}{}
	}
	return nil
}

func validateSandboxProxyHelp(helpText string) error {
	if !strings.Contains(helpText, "--policy") {
		return fmt.Errorf("docker sandbox network proxy help did not mention --policy")
	}
	if !strings.Contains(helpText, "--allow-host") {
		return fmt.Errorf("docker sandbox network proxy help did not mention --allow-host")
	}
	return nil
}

func validateSandboxListJSON(data string) (int, error) {
	var parsed sandboxListResponse
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		return 0, fmt.Errorf("docker sandbox ls --json did not return valid JSON: %w", err)
	}
	if !strings.Contains(data, `"vms"`) {
		return 0, fmt.Errorf(`docker sandbox ls --json did not include a "vms" field`)
	}
	return len(parsed.VMs), nil
}

func extractToolSemver(raw string) (semver, error) {
	m := semverPattern.FindStringSubmatch(raw)
	if len(m) != 4 {
		return semver{}, fmt.Errorf("could not find x.y.z version in %q", oneLine(raw))
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return semver{}, err
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return semver{}, err
	}
	patch, err := strconv.Atoi(m[3])
	if err != nil {
		return semver{}, err
	}
	return semver{major: major, minor: minor, patch: patch}, nil
}

func (v semver) AtLeast(min semver) bool {
	if v.major != min.major {
		return v.major > min.major
	}
	if v.minor != min.minor {
		return v.minor > min.minor
	}
	return v.patch >= min.patch
}

func (v semver) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func formatSandboxBackendLabel(kind string) string {
	switch kind {
	case sandboxBackendDockerSandboxes:
		return "Docker Sandboxes"
	default:
		if kind == "" {
			return "(unknown)"
		}
		return kind
	}
}

func loadSandboxApprovals() sandboxApprovalsFile {
	data, err := os.ReadFile(sandboxApprovalsFilePath)
	if err != nil {
		return sandboxApprovalsFile{}
	}
	var af sandboxApprovalsFile
	_ = yaml.Unmarshal(data, &af)
	return af
}

func saveSandboxApprovals(af sandboxApprovalsFile) error {
	dir := filepath.Dir(sandboxApprovalsFilePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(&af)
	if err != nil {
		return err
	}
	return os.WriteFile(sandboxApprovalsFilePath, data, 0o600)
}

func sandboxApprovalGranted(projectDir, backendType, policyProfile string) bool {
	af := loadSandboxApprovals()
	for _, rec := range af.Approvals {
		if rec.ProjectDir == projectDir &&
			rec.BackendType == backendType &&
			rec.PolicyProfile == policyProfile {
			return true
		}
	}
	return false
}

func recordManagedSandbox(sandbox ManagedSandboxConfig) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	found := false
	for i, existing := range cfg.Sandbox.Managed {
		if existing.Name == sandbox.Name {
			cfg.Sandbox.Managed[i] = sandbox
			found = true
			break
		}
	}
	if !found {
		cfg.Sandbox.Managed = append(cfg.Sandbox.Managed, sandbox)
	}

	return saveConfig(cfg)
}

func removeManagedSandboxes(probe sandboxProbe, sandboxes []ManagedSandboxConfig) error {
	grouped := make(map[string][]ManagedSandboxConfig)
	for _, sandbox := range sandboxes {
		kind := sandbox.BackendType
		if kind == "" {
			kind = sandboxBackendDockerSandboxes
		}
		grouped[kind] = append(grouped[kind], sandbox)
	}

	for kind, managed := range grouped {
		adapter, err := sandboxBackendAdapterForType(kind)
		if err != nil {
			return err
		}
		if err := adapter.RemoveManagedSandboxes(probe, managed); err != nil {
			return err
		}
	}
	return nil
}

func sandboxMissing(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "not found") ||
		strings.Contains(lower, "no such")
}

func recordSandboxApproval(projectDir, backendType, policyProfile string) error {
	af := loadSandboxApprovals()

	filtered := af.Approvals[:0]
	for _, rec := range af.Approvals {
		if rec.ProjectDir != projectDir {
			filtered = append(filtered, rec)
		}
	}
	filtered = append(filtered, sandboxApprovalRecord{
		ProjectDir:    projectDir,
		BackendType:   backendType,
		PolicyProfile: policyProfile,
		ApprovedAt:    sandboxNow().Format(time.RFC3339),
	})
	af.Approvals = filtered

	return saveSandboxApprovals(af)
}

func ensureSandboxApproval(projectDir, backendType string, profile sandboxPolicyProfile) error {
	if sandboxApprovalGranted(projectDir, backendType, profile.Name) {
		return nil
	}

	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	if !ui.IsInteractive() && !flagDryRun && !flagYesAll {
		return fmt.Errorf("Tier 3 approval required for %s. Re-run interactively or with --yes to record approval, or use --ignore-docker for code-only work", projectDir)
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "hazmat: Tier 3 approval required for %s\n", projectDir)
	fmt.Fprintf(os.Stderr, "hazmat: backend: %s\n", formatSandboxBackendLabel(backendType))
	fmt.Fprintf(os.Stderr, "hazmat: policy profile: %s\n", profile.Name)
	fmt.Fprintln(os.Stderr)

	if !ui.Ask("Approve Tier 3 Docker Sandbox use for this project?") {
		return fmt.Errorf("Tier 3 approval declined for %s", projectDir)
	}

	if flagDryRun {
		fmt.Fprintln(os.Stderr, "hazmat: dry-run: would record Tier 3 approval")
		return nil
	}

	if err := recordSandboxApproval(projectDir, backendType, profile.Name); err != nil {
		return fmt.Errorf("save Tier 3 approval: %w", err)
	}

	fmt.Fprintln(os.Stderr, "hazmat: Tier 3 approval recorded.")
	return nil
}

func runSandboxClaudeSession(cfg sessionConfig, forwarded []string) error {
	if wantsResume, _, wantsContinue := detectResumeFlags(forwarded); wantsResume || wantsContinue {
		fmt.Fprintln(os.Stderr, "hazmat: note: --resume/--continue uses Docker Sandbox-local Claude history; host transcript sync is not applied in --sandbox mode")
	}

	if hcfg, _ := loadConfig(); hcfg.SkipPermissions() {
		forwarded = append([]string{"--dangerously-skip-permissions"}, forwarded...)
	}

	adapter, probe, name, err := prepareSandboxLaunch(cfg, "claude")
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "hazmat: using Docker Sandbox %s for Claude\n", name)
	return adapter.RunClaudeSession(probe, name, forwarded)
}

func runSandboxShellSession(cfg sessionConfig) error {
	adapter, probe, name, err := prepareSandboxLaunch(cfg, "shell")
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "hazmat: using Docker Sandbox %s for shell\n", name)
	return adapter.RunShellSession(probe, name, cfg.ProjectDir)
}

func runSandboxExecSession(cfg sessionConfig, commandArgs []string) error {
	adapter, probe, name, err := prepareSandboxLaunch(cfg, "shell")
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "hazmat: using Docker Sandbox %s for exec\n", name)
	return adapter.RunExecSession(probe, name, cfg.ProjectDir, commandArgs)
}

func prepareSandboxLaunch(cfg sessionConfig, agent string) (sandboxBackendAdapter, sandboxProbe, string, error) {
	if len(cfg.PackEnv) > 0 {
		return nil, nil, "", fmt.Errorf("stack pack env passthrough is not supported with --sandbox yet")
	}

	probe := sandboxProbeFactory()
	backend, profile, version, err := loadHealthySandboxLaunchBackend(probe)
	if err != nil {
		return nil, nil, "", err
	}
	adapter, err := sandboxBackendAdapterForType(backend.Type)
	if err != nil {
		return nil, nil, "", err
	}
	spec := sandboxLaunchSpec{
		Name:    sandboxName(agent, cfg, profile.Name),
		Agent:   agent,
		Config:  cfg,
		Profile: profile,
	}
	if err := adapter.ValidateLaunchCompatibility(spec, backend, version); err != nil {
		return nil, nil, "", err
	}
	if err := ensureSandboxApproval(cfg.ProjectDir, backend.Type, profile); err != nil {
		return nil, nil, "", err
	}
	if err := recordManagedSandbox(ManagedSandboxConfig{
		Name:          spec.Name,
		BackendType:   backend.Type,
		Agent:         agent,
		ProjectDir:    cfg.ProjectDir,
		PolicyProfile: profile.Name,
		LastUsedAt:    sandboxNow().Format(time.RFC3339),
	}); err != nil {
		return nil, nil, "", fmt.Errorf("record managed sandbox: %w", err)
	}
	if err := adapter.PrepareLaunch(probe, spec); err != nil {
		return nil, nil, "", err
	}

	return adapter, probe, spec.Name, nil
}

func loadHealthySandboxLaunchBackend(probe sandboxProbe) (*SandboxBackendConfig, sandboxPolicyProfile, semver, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, sandboxPolicyProfile{}, semver{}, err
	}
	backend := cfg.SandboxBackend()
	if backend == nil {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("Tier 3 backend is not configured. Run: hazmat sandbox setup")
	}
	if _, err := sandboxBackendAdapterForType(backend.Type); err != nil {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("configured Tier 3 backend %q is not supported for session launch", backend.Type)
	}

	report := collectSandboxDoctorReport(probe)
	if !report.Healthy() {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("Tier 3 backend is not healthy. Run: hazmat sandbox doctor")
	}
	if report.Backend != backend.Type {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("configured backend %q does not match detected backend %q. Run: hazmat sandbox setup", backend.Type, report.Backend)
	}

	profile, err := sandboxPolicyProfileByName(backend.PolicyProfile)
	if err != nil {
		return nil, sandboxPolicyProfile{}, semver{}, err
	}
	if report.PolicyProfile.Name != profile.Name {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("configured policy profile %q does not match detected profile %q. Run: hazmat sandbox setup", profile.Name, report.PolicyProfile.Name)
	}

	version, err := extractToolSemver(report.DesktopVersion)
	if err != nil {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("parse Docker Desktop version: %w", err)
	}
	return backend, profile, version, nil
}

func sandboxPolicyProfileByName(name string) (sandboxPolicyProfile, error) {
	switch name {
	case sandboxPolicyProfileBaseline:
		return defaultSandboxPolicyProfile(), nil
	case "":
		return sandboxPolicyProfile{}, fmt.Errorf("configured sandbox policy profile is empty")
	default:
		return sandboxPolicyProfile{}, fmt.Errorf("unsupported sandbox policy profile %q", name)
	}
}

func sandboxName(agent string, cfg sessionConfig, profileName string) string {
	base := strings.ToLower(filepath.Base(cfg.ProjectDir))
	base = sandboxNamePattern.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "workspace"
	}

	h := sha256.New()
	_, _ = h.Write([]byte(agent))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(cfg.ProjectDir))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(profileName))
	for _, dir := range cfg.ReadDirs {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(dir))
	}
	sum := hex.EncodeToString(h.Sum(nil)[:6])
	return fmt.Sprintf("hazmat-%s-%s-%s", agent, base, sum)
}

func sandboxAlreadyExists(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "already exists") ||
		strings.Contains(lower, "already in use") ||
		strings.Contains(lower, "name is already")
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no output"
	}
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[0])
}

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	errSandboxNotFound      = errors.New("sandbox not found")
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
	VMs       []sandboxVM `json:"vms"`
	Sandboxes []sandboxVM `json:"sandboxes"`
}

type sandboxVM struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

type dockerServerVersionResponse struct {
	Platform struct {
		Name string `json:"Name"`
	} `json:"Platform"`
}

type sandboxProbe interface {
	LookPath(name string) (string, error)
	Output(name string, args ...string) (string, error)
	Run(name string, args ...string) (string, error)
}

type sandboxLaunchSpec struct {
	Name          string
	Agent         string
	Config        sessionConfig
	Profile       sandboxPolicyProfile
	MountReadDirs []string
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

func (hostSandboxProbe) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &out)
	cmd.Stderr = io.MultiWriter(os.Stderr, &out)
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
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
	if len(spec.MountReadDirs) > 0 && !version.AtLeast(minExtraWorkspaceVer) {
		return fmt.Errorf("Docker Desktop %s is too old for additional read-only workspaces; --sandbox with -R or auto-added read dirs requires >= %s",
			version.String(), minExtraWorkspaceVer.String())
	}
	return nil
}

func (dockerSandboxesBackend) PrepareLaunch(probe sandboxProbe, spec sandboxLaunchSpec) error {
	status, exists, err := sandboxStatus(probe, spec.Name)
	if err != nil {
		return err
	}
	if exists {
		if status != "" && status != "running" {
			fmt.Fprintf(os.Stderr, "hazmat: removing stopped Docker Sandbox %s\n", spec.Name)
			out, rmErr := probe.Output("docker", "sandbox", "rm", spec.Name)
			if rmErr != nil && !sandboxMissing(out) {
				return fmt.Errorf("remove stopped Docker Sandbox %s: %s", spec.Name, oneLine(out))
			}
			exists = false
		}
	}
	if exists {
		fmt.Fprintf(os.Stderr, "hazmat: reusing Docker Sandbox %s\n", spec.Name)
	} else {
		fmt.Fprintf(os.Stderr, "hazmat: creating Docker Sandbox %s (first launch may take a few minutes)\n", spec.Name)
		args := []string{"sandbox", "create", "--name", spec.Name, spec.Agent, spec.Config.ProjectDir}
		for _, dir := range spec.MountReadDirs {
			args = append(args, dir+":ro")
		}
		if out, err := probe.Run("docker", args...); err != nil {
			return sandboxActionError(probe, out, err, "create Docker Sandbox %s", spec.Name)
		}
	}

	fmt.Fprintf(os.Stderr, "hazmat: applying Docker network policy to %s\n", spec.Name)
	policyArgs := []string{"sandbox", "network", "proxy", spec.Name, "--policy", spec.Profile.Policy}
	for _, host := range spec.Profile.AllowHosts {
		policyArgs = append(policyArgs, "--allow-host", host)
	}
	if out, err := probe.Run("docker", policyArgs...); err != nil {
		return sandboxActionError(probe, out, err, "apply Docker network policy to %s", spec.Name)
	}
	return nil
}

func (dockerSandboxesBackend) RunClaudeSession(probe sandboxProbe, sandboxName string, forwarded []string) error {
	args := []string{"sandbox", "run", sandboxName}
	if len(forwarded) > 0 {
		args = append(args, "--")
		args = append(args, forwarded...)
	}
	if out, err := probe.Run("docker", args...); err != nil {
		return sandboxActionError(probe, out, err, "run Claude in Docker Sandbox %s", sandboxName)
	}
	return nil
}

func (dockerSandboxesBackend) RunShellSession(probe sandboxProbe, sandboxName, projectDir string) error {
	if out, err := probe.Run("docker", "sandbox", "run", sandboxName, "--",
		"-lc", `cd "$1" && exec /bin/bash -il`, "bash", projectDir); err != nil {
		return sandboxActionError(probe, out, err, "run shell in Docker Sandbox %s", sandboxName)
	}
	return nil
}

func (dockerSandboxesBackend) RunExecSession(probe sandboxProbe, sandboxName, projectDir string, commandArgs []string) error {
	args := []string{"sandbox", "run", sandboxName, "--",
		"-lc", `cd "$1" && shift && exec "$@"`, "bash", projectDir}
	args = append(args, commandArgs...)
	if out, err := probe.Run("docker", args...); err != nil {
		return sandboxActionError(probe, out, err, "run exec session in Docker Sandbox %s", sandboxName)
	}
	return nil
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
		Short: "Manage Docker Sandbox support and diagnostics",
		Long:  `Manage Hazmat's Docker Sandbox backend state and launch prerequisites.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Verify Docker Sandbox support is healthy",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSandboxDoctor()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "setup",
		Short: "Validate and record Docker Sandbox support",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSandboxSetup()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "reset",
		Short: "Forget recorded Docker Sandbox support",
		Long:  `Forget Hazmat's recorded Docker Sandbox backend configuration and remove Hazmat-managed sandboxes.`,
		Args:  cobra.NoArgs,
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
	backend, _, _, err := healthySandboxBackendFromReport(report)
	if err != nil {
		return err
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

	ui.Ok("Saved Docker Sandbox configuration")
	cDim.Println("  Docker-capable sessions can now use the recorded backend, or auto-detect it later if needed.")
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
		cDim.Println("  No Docker Sandbox backend is currently configured.")
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

	if !ui.Ask("Forget the configured Docker Sandbox backend and remove managed sandboxes?") {
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

	ui.Ok("Cleared Docker Sandbox configuration")
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

	desktopVersion, source, err := detectDockerDesktopVersion(probe)
	if err != nil {
		report.addCheck("Docker Desktop version", false, err.Error())
	} else if !desktopVersion.AtLeast(minDockerDesktopVersion) {
		report.addCheck("Docker Desktop version", false,
			fmt.Sprintf("found %s, need >= %s", desktopVersion.String(), minDockerDesktopVersion.String()))
	} else {
		report.DesktopVersion = desktopVersion.String()
		report.addCheck("Docker Desktop version", true,
			fmt.Sprintf("found %s via %s (requires >= %s)", desktopVersion.String(), source, minDockerDesktopVersion.String()))
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
			fmt.Sprintf("docker sandbox ls --json returned %d sandbox(es)", vmCount))
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
			cYellow.Println("  Backend checks passed. Docker support can auto-detect this backend; run 'hazmat sandbox setup' to record it in advance.")
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
			"claude.ai",
			"platform.claude.com",
			"statsig.anthropic.com",
			"*.sentry.io",
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

func healthySandboxBackendFromReport(report sandboxDoctorReport) (*SandboxBackendConfig, sandboxPolicyProfile, semver, error) {
	if !report.Healthy() {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("sandbox backend is not healthy")
	}
	if _, err := sandboxBackendAdapterForType(report.Backend); err != nil {
		return nil, sandboxPolicyProfile{}, semver{}, err
	}
	profile, err := sandboxPolicyProfileByName(report.PolicyProfile.Name)
	if err != nil {
		return nil, sandboxPolicyProfile{}, semver{}, err
	}
	version, err := extractToolSemver(report.DesktopVersion)
	if err != nil {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("parse Docker Desktop version: %w", err)
	}
	backend := &SandboxBackendConfig{
		Type:           report.Backend,
		PolicyProfile:  profile.Name,
		DesktopVersion: report.DesktopVersion,
		ComposeVersion: report.ComposeVersion,
		ConfiguredAt:   sandboxNow().Format(time.RFC3339),
	}
	return backend, profile, version, nil
}

func detectHealthySandboxBackend(probe sandboxProbe) (*SandboxBackendConfig, sandboxPolicyProfile, semver, error) {
	return healthySandboxBackendFromReport(collectSandboxDoctorReport(probe))
}

func recordSandboxBackendConfig(backend *SandboxBackendConfig) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Sandbox.Backend = backend
	return saveConfig(cfg)
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
	names, err := sandboxListNames(data)
	if err != nil {
		return 0, err
	}
	return len(names), nil
}

func sandboxListNames(data string) ([]string, error) {
	var parsed sandboxListResponse
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		return nil, fmt.Errorf("docker sandbox ls --json did not return valid JSON: %w", err)
	}
	if strings.Contains(data, `"sandboxes"`) {
		names := make([]string, 0, len(parsed.Sandboxes))
		for _, sandbox := range parsed.Sandboxes {
			names = append(names, sandbox.Name)
		}
		return names, nil
	}
	if strings.Contains(data, `"vms"`) {
		names := make([]string, 0, len(parsed.VMs))
		for _, sandbox := range parsed.VMs {
			names = append(names, sandbox.Name)
		}
		return names, nil
	}
	return nil, fmt.Errorf(`docker sandbox ls --json did not include a "sandboxes" or "vms" field`)
}

func sandboxStatus(probe sandboxProbe, name string) (string, bool, error) {
	out, err := probe.Output("docker", "sandbox", "ls", "--json")
	if err != nil {
		return "", false, fmt.Errorf("list Docker Sandboxes: %s", oneLine(out))
	}
	var parsed sandboxListResponse
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return "", false, fmt.Errorf("docker sandbox ls --json did not return valid JSON: %w", err)
	}

	var sandboxes []sandboxVM
	switch {
	case strings.Contains(out, `"sandboxes"`):
		sandboxes = parsed.Sandboxes
	case strings.Contains(out, `"vms"`):
		sandboxes = parsed.VMs
	default:
		return "", false, fmt.Errorf(`docker sandbox ls --json did not include a "sandboxes" or "vms" field`)
	}

	for _, sandbox := range sandboxes {
		if sandbox.Name == name {
			return strings.ToLower(sandbox.Status), true, nil
		}
	}
	return "", false, nil
}

func extractDockerDesktopSemver(raw string) (semver, error) {
	var parsed dockerServerVersionResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return semver{}, fmt.Errorf("docker version --format '{{json .Server}}' did not return valid JSON: %w", err)
	}
	if parsed.Platform.Name == "" {
		return semver{}, fmt.Errorf("docker version --format '{{json .Server}}' did not include Server.Platform.Name")
	}
	version, err := extractToolSemver(parsed.Platform.Name)
	if err != nil {
		return semver{}, fmt.Errorf("parse Docker Desktop version from %q: %w", parsed.Platform.Name, err)
	}
	return version, nil
}

func dockerDesktopAppSemver(probe sandboxProbe) (semver, error) {
	raw, err := probe.Output("plutil", "-extract", "CFBundleShortVersionString", "raw", "/Applications/Docker.app/Contents/Info.plist")
	if err != nil {
		return semver{}, fmt.Errorf("plutil Docker.app version failed: %s", oneLine(raw))
	}
	version, err := extractToolSemver(raw)
	if err != nil {
		return semver{}, fmt.Errorf("parse Docker.app version from %q: %w", oneLine(raw), err)
	}
	return version, nil
}

func detectDockerDesktopVersion(probe sandboxProbe) (semver, string, error) {
	raw, err := probe.Output("docker", "version", "--format", "{{json .Server}}")
	if err == nil {
		version, parseErr := extractDockerDesktopSemver(raw)
		if parseErr == nil {
			return version, "docker version", nil
		}
	}

	appVersion, appErr := dockerDesktopAppSemver(probe)
	if appErr == nil {
		return appVersion, "Docker.app bundle", nil
	}

	if err != nil {
		return semver{}, "", fmt.Errorf("docker version --format '{{json .Server}}' failed: %s", oneLine(raw))
	}
	return semver{}, "", appErr
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

func dockerDesktopStatus(probe sandboxProbe) (string, error) {
	out, err := probe.Output("docker", "desktop", "status")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && fields[0] == "Status" {
			return strings.ToLower(fields[1]), nil
		}
	}
	return "", fmt.Errorf("status line not found")
}

func sandboxActionError(probe sandboxProbe, output string, err error, format string, args ...any) error {
	base := fmt.Sprintf(format, args...)
	if output != "" {
		if sandboxNotFoundError(output) {
			return fmt.Errorf("%s: %w", base, errSandboxNotFound)
		}
		if claudeSandboxAuthError(output) {
			return fmt.Errorf("%s: Claude is not authenticated in Docker Sandboxes; run 'hazmat claude' interactively and type /login, or configure ANTHROPIC_API_KEY in your shell startup files and restart Docker Desktop", base)
		}
	}
	if dockerDesktopClosedPipeError(output, err) {
		return fmt.Errorf("%s: Docker Desktop failed unexpectedly; if macOS showed a Docker data-access prompt, click Allow and retry; otherwise restart Docker Desktop and retry", base)
	}
	status, statusErr := dockerDesktopStatus(probe)
	if statusErr == nil && status == "stopped" {
		return fmt.Errorf("%s: Docker Desktop stopped unexpectedly; restart Docker Desktop and retry", base)
	}
	return fmt.Errorf("%s", base)
}

func sandboxNotFoundError(output string) bool {
	return strings.Contains(strings.ToLower(output), "no sandbox found")
}

func claudeSandboxAuthError(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "not logged in") && strings.Contains(lower, "/login")
}

func dockerDesktopClosedPipeError(output string, err error) bool {
	var text strings.Builder
	text.WriteString(strings.ToLower(output))
	if err != nil {
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(strings.ToLower(err.Error()))
	}
	return strings.Contains(text.String(), "io: read/write on closed pipe")
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
		return fmt.Errorf("Docker Sandbox approval required for %s. Re-run interactively or with --yes to record approval, or use --ignore-docker for code-only work", projectDir)
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "hazmat: Docker Sandbox approval required for %s\n", projectDir)
	fmt.Fprintf(os.Stderr, "hazmat: backend: %s\n", formatSandboxBackendLabel(backendType))
	fmt.Fprintf(os.Stderr, "hazmat: policy profile: %s\n", profile.Name)
	fmt.Fprintln(os.Stderr)

	if !ui.Ask("Approve Docker Sandbox support for this project?") {
		return fmt.Errorf("Docker Sandbox approval declined for %s", projectDir)
	}

	if flagDryRun {
		fmt.Fprintln(os.Stderr, "hazmat: dry-run: would record Docker Sandbox approval")
		return nil
	}

	if err := recordSandboxApproval(projectDir, backendType, profile.Name); err != nil {
		return fmt.Errorf("save Docker Sandbox approval: %w", err)
	}

	fmt.Fprintln(os.Stderr, "hazmat: Docker Sandbox approval recorded.")
	return nil
}

func runSandboxClaudeSession(cfg sessionConfig, forwarded []string) error {
	if wantsResume, _, wantsContinue := detectResumeFlags(forwarded); wantsResume || wantsContinue {
		fmt.Fprintln(os.Stderr, "hazmat: note: --resume/--continue uses Docker Sandbox-local Claude history; host transcript sync is not applied in --sandbox mode")
	}

	if hcfg, _ := loadConfig(); hcfg.SkipPermissions() {
		forwarded = append([]string{"--dangerously-skip-permissions"}, forwarded...)
	}

	return runPreparedSandboxSession(cfg, "claude", "Claude", func(adapter sandboxBackendAdapter, probe sandboxProbe, name string) error {
		return adapter.RunClaudeSession(probe, name, forwarded)
	})
}

func runSandboxShellSession(cfg sessionConfig) error {
	return runPreparedSandboxSession(cfg, "shell", "shell", func(adapter sandboxBackendAdapter, probe sandboxProbe, name string) error {
		return adapter.RunShellSession(probe, name, cfg.ProjectDir)
	})
}

func runSandboxExecSession(cfg sessionConfig, commandArgs []string) error {
	return runPreparedSandboxSession(cfg, "shell", "exec session", func(adapter sandboxBackendAdapter, probe sandboxProbe, name string) error {
		return adapter.RunExecSession(probe, name, cfg.ProjectDir, commandArgs)
	})
}

func runPreparedSandboxSession(cfg sessionConfig, agent, label string, run func(sandboxBackendAdapter, sandboxProbe, string) error) error {
	adapter, probe, name, err := prepareSandboxLaunch(cfg, agent)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "hazmat: starting %s in Docker Sandbox %s\n", label, name)
	err = run(adapter, probe, name)
	if !errors.Is(err, errSandboxNotFound) {
		return err
	}

	fmt.Fprintf(os.Stderr, "hazmat: Docker Sandbox %s is stale; removing and recreating once\n", name)
	if err := removeManagedSandboxes(probe, []ManagedSandboxConfig{{
		Name:        name,
		BackendType: adapter.Type(),
	}}); err != nil {
		return fmt.Errorf("remove stale Docker Sandbox %s: %w", name, err)
	}

	adapter, probe, name, err = prepareSandboxLaunch(cfg, agent)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "hazmat: retrying %s in Docker Sandbox %s\n", label, name)
	return run(adapter, probe, name)
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
	spec, err := buildSandboxLaunchSpec(agent, cfg, profile)
	if err != nil {
		return nil, nil, "", err
	}
	if err := adapter.ValidateLaunchCompatibility(spec, backend, version); err != nil {
		return nil, nil, "", err
	}
	if err := ensureSandboxApproval(cfg.ProjectDir, backend.Type, profile); err != nil {
		return nil, nil, "", err
	}
	if err := adapter.PrepareLaunch(probe, spec); err != nil {
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

	return adapter, probe, spec.Name, nil
}

func buildSandboxLaunchSpec(agent string, cfg sessionConfig, profile sandboxPolicyProfile) (sandboxLaunchSpec, error) {
	if isCredentialDenyPath(cfg.ProjectDir) {
		return sandboxLaunchSpec{}, fmt.Errorf("project dir %q resolves to credential deny zone", cfg.ProjectDir)
	}

	var mountReadDirs []string
	seen := make(map[string]struct{})
	for _, dir := range cfg.ReadDirs {
		if isCredentialDenyPath(dir) {
			return sandboxLaunchSpec{}, fmt.Errorf("read dir %q resolves to credential deny zone", dir)
		}
		// Skip dirs within the project — already accessible as the workspace.
		if isWithinDir(cfg.ProjectDir, dir) {
			continue
		}
		// Ancestor of project: can't mount the parent read-only because
		// Docker's sandbox template copies CLAUDE.md from parent dirs
		// during create, and an ancestor ro mount conflicts with the
		// writable workspace. Expand into sibling directories instead.
		if isWithinDir(dir, cfg.ProjectDir) {
			for _, s := range expandAncestorReadDir(dir, cfg.ProjectDir) {
				if isCredentialDenyPath(s) {
					continue
				}
				if _, dup := seen[s]; !dup {
					mountReadDirs = append(mountReadDirs, s)
					seen[s] = struct{}{}
				}
			}
			continue
		}
		covered := false
		for _, other := range cfg.ReadDirs {
			if other != dir && isWithinDir(other, dir) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		if _, dup := seen[dir]; !dup {
			mountReadDirs = append(mountReadDirs, dir)
			seen[dir] = struct{}{}
		}
	}

	return sandboxLaunchSpec{
		Name:          sandboxName(agent, cfg.ProjectDir, mountReadDirs, profile.Name),
		Agent:         agent,
		Config:        cfg,
		Profile:       profile,
		MountReadDirs: mountReadDirs,
	}, nil
}

// expandAncestorReadDir lists sibling directories at each path level
// between ancestor and projectDir. When a read_dir is a parent of the
// project, Docker sandboxes can't mount it read-only (it conflicts with
// the writable workspace). This function enumerates the directories at
// each level that don't lie on the path to the project, giving the
// sandbox access to siblings without the overlapping-mount conflict.
func expandAncestorReadDir(ancestor, projectDir string) []string {
	rel, err := filepath.Rel(ancestor, projectDir)
	if err != nil {
		return nil
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == ".") {
		return nil // ancestor IS the project
	}

	var result []string
	current := ancestor
	for i, part := range parts {
		entries, err := os.ReadDir(current)
		if err != nil {
			break
		}
		for _, e := range entries {
			if e.Name() == part {
				continue // on the path to project, skip
			}
			child := filepath.Join(current, e.Name())
			resolved, err := filepath.EvalSymlinks(child)
			if err != nil {
				continue
			}
			info, err := os.Stat(resolved)
			if err != nil || !info.IsDir() {
				continue
			}
			result = append(result, resolved)
		}
		if i < len(parts)-1 {
			current = filepath.Join(current, part)
		}
	}
	return result
}

func loadHealthySandboxLaunchBackend(probe sandboxProbe) (*SandboxBackendConfig, sandboxPolicyProfile, semver, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, sandboxPolicyProfile{}, semver{}, err
	}
	backend := cfg.SandboxBackend()
	detectedBackend, detectedProfile, version, err := detectHealthySandboxBackend(probe)
	if err != nil {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("Docker Sandbox support is not healthy. Run: hazmat sandbox doctor")
	}

	if backend == nil {
		if !flagDryRun {
			if err := recordSandboxBackendConfig(detectedBackend); err != nil {
				return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("record auto-detected Docker Sandbox backend: %w", err)
			}
			fmt.Fprintf(os.Stderr, "hazmat: detected Docker Sandbox support via %s\n", formatSandboxBackendLabel(detectedBackend.Type))
			fmt.Fprintln(os.Stderr, "hazmat: recorded backend configuration automatically.")
		}
		return detectedBackend, detectedProfile, version, nil
	}
	if _, err := sandboxBackendAdapterForType(backend.Type); err != nil {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("configured Docker Sandbox backend %q is not supported for session launch", backend.Type)
	}
	if detectedBackend.Type != backend.Type {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("configured backend %q does not match detected backend %q. Run: hazmat sandbox setup", backend.Type, detectedBackend.Type)
	}
	if detectedProfile.Name != backend.PolicyProfile {
		return nil, sandboxPolicyProfile{}, semver{}, fmt.Errorf("configured policy profile %q does not match detected profile %q. Run: hazmat sandbox setup", backend.PolicyProfile, detectedProfile.Name)
	}
	return backend, detectedProfile, version, nil
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

func sandboxName(agent, projectDir string, mountReadDirs []string, profileName string) string {
	base := strings.ToLower(filepath.Base(projectDir))
	base = sandboxNamePattern.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "workspace"
	}

	h := sha256.New()
	_, _ = h.Write([]byte(agent))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(projectDir))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(profileName))
	for _, dir := range mountReadDirs {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(dir))
	}
	sum := hex.EncodeToString(h.Sum(nil)[:6])
	return fmt.Sprintf("hazmat-%s-%s-%s", agent, base, sum)
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no output"
	}
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[0])
}

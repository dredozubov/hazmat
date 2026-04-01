package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	sandboxBackendDockerSandboxes = "docker-sandboxes"
	sandboxPolicyProfileBaseline  = "baseline"
)

var (
	minDockerDesktopVersion = semver{major: 4, minor: 58, patch: 0}
	minComposeVersion       = semver{major: 2, minor: 40, patch: 2}
	semverPattern           = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)
	sandboxHostPattern      = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9][A-Za-z0-9.-]*$`)
	sandboxNow              = func() time.Time { return time.Now().UTC() }
	sandboxProbeFactory     = func() sandboxProbe { return hostSandboxProbe{} }
)

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
}

type hostSandboxProbe struct{}

func (hostSandboxProbe) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (hostSandboxProbe) Output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
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
	fmt.Println()
	cBold.Println("  Sandbox reset")
	fmt.Println()
	if backend == nil {
		cDim.Println("  No Tier 3 backend is currently configured.")
		fmt.Println()
		return nil
	}

	fmt.Printf("    Backend:         %s\n", formatSandboxBackendLabel(backend.Type))
	fmt.Printf("    Policy profile:  %s\n", backend.PolicyProfile)
	fmt.Println()

	if !ui.Ask("Forget the configured Tier 3 backend?") {
		fmt.Println()
		return nil
	}

	if flagDryRun {
		cYellow.Println("  Dry-run: would clear sandbox backend configuration")
		fmt.Println()
		return nil
	}

	cfg.Sandbox.Backend = nil
	if err := saveConfig(cfg); err != nil {
		return err
	}

	ui.Ok("Cleared Tier 3 backend configuration")
	cDim.Println("  Existing Docker Sandboxes were left untouched.")
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

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no output"
	}
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[0])
}

// Package applecontainer is the experimental Apple Container runtime. It
// probes host admission facts and runs one named container per Hazmat
// session via the apple/container CLI, invoked as the invoking macOS user.
//
// The launch boundary is governed by tla/MC_AppleContainerLaunch: admission
// before launch, unsupported network policies fail closed (enforced at
// compile time), cleanup by exact session container name with recorded
// failures, and never any prune-style sweep.
//
// Identity model (revised 2026-06-10, spike sandboxing-ajmn finding F1): the
// container CLI requires a per-user-session apiserver, so it runs as the
// invoking user. Host account isolation is NOT provided by this backend;
// the boundary is the VM plus exact mount planning, and the session
// contract states that bluntly.
package applecontainer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	applecontainerspec "hazmat/containment/applecontainer"
)

const (
	PackagePath = "hazmat/internal/runtime/applecontainer"

	// EnvExperimentalGate must be set to "1" for the runtime to launch.
	EnvExperimentalGate = "HAZMAT_EXPERIMENTAL_APPLE_CONTAINER"

	// ApprovedCLIPath is the only CLI location the runtime accepts in the
	// MVP. Custom absolute paths are future work behind admission changes.
	ApprovedCLIPath = "/usr/local/bin/container"

	minSupportedMajor = 1
)

// Runner abstracts host command execution for admission probes so tests can
// inject transcripts.
type Runner interface {
	Output(name string, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// HostRunner returns the real host prober.
func HostRunner() Runner {
	return execRunner{}
}

// GateEnabled reports whether the experimental gate env var is set.
func GateEnabled() bool {
	return os.Getenv(EnvExperimentalGate) == "1"
}

// GateError explains how to enable the experimental runtime.
func GateError() error {
	return fmt.Errorf("the Apple Container backend is experimental and disabled by default.\n"+
		"It runs Linux VM-per-session execution with Hazmat-planned host mounts.\n"+
		"Host file IO occurs as the invoking macOS user. Host account isolation is\n"+
		"not provided by this backend; use native containment for that.\n"+
		"Set %s=1 to enable it for this invocation", EnvExperimentalGate)
}

// ProbeHost collects admission facts for the invoking user. Failures
// surface as zero values, which the compiler turns into capability gaps;
// the runtime never launches while gaps remain.
func ProbeHost(r Runner, goos, goarch string) applecontainerspec.HostReport {
	report := applecontainerspec.HostReport{
		GOOS:   goos,
		GOARCH: goarch,
	}
	if version, err := r.Output("/usr/bin/sw_vers", "-productVersion"); err == nil {
		report.MacOSMajorVersion = parseMajorVersion(version)
	}
	if info, err := os.Stat(ApprovedCLIPath); err == nil && !info.IsDir() {
		report.CLIPath = ApprovedCLIPath
	}
	if report.CLIPath == "" {
		return report
	}
	if raw, err := r.Output(report.CLIPath, "system", "version", "--format", "json"); err == nil {
		report.CLIVersion, report.CLIVersionSupported = parseSystemVersion(raw)
	}
	if raw, err := r.Output(report.CLIPath, "system", "status", "--format", "json"); err == nil {
		report.APIServerHealthy = parseSystemStatus(raw)
	}
	return report
}

// RunResult records the launch outcome plus the cleanup obligations the
// session metadata must reflect.
type RunResult struct {
	ExitCode        int
	ContainerName   string
	CleanupFailures []string
}

// RunOptions carries the stdio wiring for a session.
type RunOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run launches the compiled spec's container, streams stdio, and removes
// the session container by exact name afterwards. Cleanup failures are
// recorded in the result, never silently dropped — both required by
// tla/MC_AppleContainerLaunch (TerminalContainerHandled).
func Run(spec applecontainerspec.LaunchSpec, opts RunOptions) (RunResult, error) {
	result := RunResult{ContainerName: spec.ContainerName}
	if spec.Phase != applecontainerspec.PhaseExperimental {
		return result, fmt.Errorf("apple-container runtime refuses a %q spec; compile with the executable runtime option", spec.Phase)
	}
	if len(spec.CapabilityGaps) > 0 {
		return result, admissionError(spec.CapabilityGaps)
	}
	argv, err := applecontainerspec.Argv(spec)
	if err != nil {
		return result, err
	}

	cmd := exec.Command(ApprovedCLIPath, argv[1:]...)
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	runErr := cmd.Run()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	// Cleanup is by exact session container name only. Never prune.
	if out, err := exec.Command(ApprovedCLIPath, "rm", spec.ContainerName).CombinedOutput(); err != nil {
		result.CleanupFailures = append(result.CleanupFailures,
			fmt.Sprintf("container rm %s: %v: %s", spec.ContainerName, err, strings.TrimSpace(string(out))))
	}
	for _, path := range spec.Cleanup.RemoveGeneratedFiles {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			result.CleanupFailures = append(result.CleanupFailures,
				fmt.Sprintf("remove generated file %s: %v", path, err))
		}
	}

	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return result, fmt.Errorf("apple-container launch failed: %w", runErr)
	}
	return result, nil
}

func admissionError(gaps []applecontainerspec.CapabilityGap) error {
	lines := make([]string, 0, len(gaps)+1)
	lines = append(lines, "apple-container admission failed; refusing to launch:")
	for _, gap := range gaps {
		line := fmt.Sprintf("  - %s: %s", gap.Code, gap.Message)
		if gap.State != "" {
			line += fmt.Sprintf(" (%s)", gap.State)
		}
		lines = append(lines, line)
	}
	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}

func parseMajorVersion(raw string) int {
	fields := strings.SplitN(strings.TrimSpace(raw), ".", 2)
	major, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	return major
}

type systemVersionEntry struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
}

// parseSystemVersion reads `container system version --format json`. The
// payload is an array of component entries; the "container" entry carries a
// bare semver, while the apiserver entry's version is prose.
func parseSystemVersion(raw string) (string, bool) {
	var entries []systemVersionEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.AppName != "container" {
			continue
		}
		return entry.Version, versionSupported(entry.Version)
	}
	return "", false
}

func versionSupported(version string) bool {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	fields := strings.SplitN(trimmed, ".", 3)
	major, err := strconv.Atoi(fields[0])
	if err != nil {
		return false
	}
	return major >= minSupportedMajor
}

type systemStatus struct {
	Status string `json:"status"`
}

func parseSystemStatus(raw string) bool {
	var status systemStatus
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return false
	}
	return status.Status == "running"
}

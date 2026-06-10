// Package applecontainer compiles Hazmat's backend-neutral containment
// contract into a plan-only Apple Container launch spec. It does not run the
// container CLI, probe host state, write files, or materialize credentials.
//
// The host-side launch boundary for this backend is governed by
// tla/MC_AppleContainerLaunch: credential deny paths and parents are never
// mounted, forbidden launch features are rejected before any other work,
// unsupported network policies fail closed, and credential artifacts are
// session-scoped with cleanup accounting. The compiler enforces the
// compile-time half of that contract; the runtime half stays unimplemented
// until the model's ordering is followed by real launch code.
package applecontainer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"hazmat/containment"
	"hazmat/sessionmeta"
)

const (
	// LaunchSpecFormatVersion is the current Apple Container launch-spec
	// schema version.
	LaunchSpecFormatVersion = 1

	BackendAppleContainer = "apple-container"
	PhasePlanOnly         = "plan-only"

	// DefaultGuestUID and DefaultGuestGID are the provisional non-root guest
	// identity from the backend design spec. The VirtioFS ownership probe
	// (sandboxing-ajmn) must confirm them before the runtime ships.
	DefaultGuestUID = 502
	DefaultGuestGID = 20

	// LabelSessionID ties a container to its Hazmat session for exact-name
	// cleanup. The runtime must never prune; it removes only labeled,
	// session-owned containers by name.
	LabelSessionID = "dev.hazmat.session-id"
	LabelHarness   = "dev.hazmat.harness"
)

var containerNamePattern = regexp.MustCompile(`[^a-z0-9]+`)

// CompileOptions supplies session facts the contract does not carry.
type CompileOptions struct {
	// Harness is the session target (claude, codex, exec, shell, ...).
	Harness string
	// Image is the explicit Linux/arm64 image reference. Required; the
	// backend never guesses an image.
	Image string
	// SessionID scopes the container name and labels. Required.
	SessionID string
	// Command is the in-guest argv appended after the image.
	Command []string
	// GuestUID/GuestGID override the provisional non-root guest identity.
	// Zero values select the defaults; root (explicit "0") is rejected via
	// GuestRootRequested.
	GuestUID int
	GuestGID int
	// GuestRootRequested is true when the caller explicitly asked for uid 0
	// in the guest. Always rejected: images needing root must do that work
	// at image build time, not during the agent session.
	GuestRootRequested bool

	// IntegrationEnvKeys are integration env passthrough requests. Any entry
	// makes compilation fail: this backend rejects integration env.
	IntegrationEnvKeys []string
	// SSHForwardRequested is true when the session asked for SSH agent
	// forwarding (`container run --ssh`). Always rejected in this backend.
	SSHForwardRequested bool
	// PublishSockets are requested host socket exposures. Always rejected.
	PublishSockets []string

	// CredentialEnvFile optionally names the planned session-scoped env-file
	// (path under agent-owned temp state). The compiler records the plan and
	// the cleanup obligation; it never reads or writes the file.
	CredentialEnvFile string

	// Resources optionally bounds guest CPU/memory.
	Resources ResourceSpec

	// Host carries inspected admission facts. Unknown or failing facts
	// surface as capability gaps; they never silently pass.
	Host HostReport
}

// HostReport is the data-only admission report for the Apple Container
// backend. Probes fill it at frontend boundaries; the compiler only reads it.
type HostReport struct {
	// GOOS/GOARCH of the host ("darwin"/"arm64" required).
	GOOS   string `json:"goos,omitempty"`
	GOARCH string `json:"goarch,omitempty"`
	// MacOSMajorVersion is the inspected macOS major version (26 required).
	// Zero means uninspected.
	MacOSMajorVersion int `json:"macos_major_version,omitempty"`
	// CLIPath is the approved absolute `container` CLI path, empty when the
	// CLI was not found at an approved location.
	CLIPath string `json:"cli_path,omitempty"`
	// CLIVersion is the parsed `container system version` result.
	CLIVersion string `json:"cli_version,omitempty"`
	// CLIVersionSupported is true only when the version was inspected and is
	// >= the minimum supported release.
	CLIVersionSupported bool `json:"cli_version_supported,omitempty"`
	// APIServerHealthy is true only when `container system status` reported
	// a healthy API server.
	APIServerHealthy bool `json:"api_server_healthy,omitempty"`
	// RunnableAsAgent is true only when the CLI is confirmed runnable as the
	// dedicated macOS `agent` user.
	RunnableAsAgent bool `json:"runnable_as_agent,omitempty"`
}

// LaunchSpec is a JSON-friendly Apple Container launch plan. The argv is
// derived from the validated spec at the runtime boundary, never built
// directly from session inputs.
type LaunchSpec struct {
	FormatVersion  int               `json:"format_version"`
	Backend        string            `json:"backend"`
	Phase          string            `json:"phase"`
	ContainerName  string            `json:"container_name"`
	Image          string            `json:"image"`
	Workdir        string            `json:"workdir"`
	User           UserSpec          `json:"user"`
	Mounts         []MountSpec       `json:"mounts"`
	Tmpfs          []string          `json:"tmpfs,omitempty"`
	Rootfs         RootfsSpec        `json:"rootfs"`
	Network        NetworkSpec       `json:"network"`
	Resources      ResourceSpec      `json:"resources,omitempty"`
	Capabilities   CapabilitySpec    `json:"capabilities"`
	Environment    EnvironmentSpec   `json:"environment"`
	Labels         map[string]string `json:"labels,omitempty"`
	Cleanup        CleanupSpec       `json:"cleanup"`
	Command        []string          `json:"command,omitempty"`
	CapabilityGaps []CapabilityGap   `json:"capability_gaps,omitempty"`
}

// UserSpec is the non-root guest identity for the agent process.
type UserSpec struct {
	UID int `json:"uid"`
	GID int `json:"gid"`
}

// MountSpec describes one host bind mount. Targets preserve absolute host
// paths inside the Linux VM so tool output stays understandable.
type MountSpec struct {
	Source string                 `json:"source"`
	Target string                 `json:"target"`
	Access containment.PathAccess `json:"access"`
}

// RootfsSpec records the rootfs policy. The MVP default is a writable
// ephemeral rootfs removed during cleanup; the strict read-only option comes
// after harness smoke tests.
type RootfsSpec struct {
	ReadOnly  bool `json:"read_only"`
	Ephemeral bool `json:"ephemeral"`
}

// NetworkSpec is the honestly-reported effective network policy. Only the
// default outbound-allowed mode is supported; everything else fails closed
// at compile time.
type NetworkSpec struct {
	Mode        sessionmeta.NetworkMode `json:"mode"`
	Enforcement string                  `json:"enforcement"`
}

// ResourceSpec optionally bounds guest resources.
type ResourceSpec struct {
	CPUs   int    `json:"cpus,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// CapabilitySpec records Linux capability policy for the guest process.
type CapabilitySpec struct {
	DropAll bool     `json:"drop_all"`
	Add     []string `json:"add,omitempty"`
}

// EnvironmentSpec records env delivery policy. Host shell environment is
// never inherited; the only env source is an optional generated
// session-scoped credential env-file.
type EnvironmentSpec struct {
	InheritHostEnv    bool   `json:"inherit_host_env"`
	CredentialEnvFile string `json:"credential_env_file,omitempty"`
}

// CleanupSpec records the session cleanup obligations the runtime must meet
// or record as failures in session metadata.
type CleanupSpec struct {
	RemoveContainerByName bool     `json:"remove_container_by_name"`
	RemoveGeneratedFiles  []string `json:"remove_generated_files,omitempty"`
	NeverPrune            bool     `json:"never_prune"`
}

// CapabilityGap records why a compiled plan is not yet executable.
type CapabilityGap struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	State   string `json:"state,omitempty"`
}

// Compile converts a backend-neutral contract into an Apple Container launch
// spec without executing anything.
func Compile(contract containment.Contract, opts CompileOptions) (LaunchSpec, error) {
	// Forbidden launch features are rejected before any other work,
	// mirroring phase 0 of MC_AppleContainerLaunch.
	if len(opts.IntegrationEnvKeys) > 0 {
		return LaunchSpec{}, fmt.Errorf(
			"apple-container sessions reject integration env passthrough (requested: %s)",
			strings.Join(sortedCopy(opts.IntegrationEnvKeys), ", "))
	}
	if opts.SSHForwardRequested {
		return LaunchSpec{}, fmt.Errorf("apple-container sessions reject SSH agent forwarding (--ssh)")
	}
	if len(opts.PublishSockets) > 0 {
		return LaunchSpec{}, fmt.Errorf(
			"apple-container sessions reject host socket publishing (requested: %s)",
			strings.Join(sortedCopy(opts.PublishSockets), ", "))
	}

	// Mount inputs are validated by the contract: grants overlapping the
	// credential deny floor are rejected by Contract.Validate.
	if err := contract.Validate(); err != nil {
		return LaunchSpec{}, fmt.Errorf("invalid containment contract: %w", err)
	}

	if strings.TrimSpace(opts.Harness) == "" {
		return LaunchSpec{}, fmt.Errorf("apple-container compile requires an explicit harness target")
	}
	if strings.TrimSpace(opts.SessionID) == "" {
		return LaunchSpec{}, fmt.Errorf("apple-container compile requires a session ID")
	}
	if err := validateImage(opts.Image); err != nil {
		return LaunchSpec{}, err
	}

	if opts.GuestRootRequested {
		return LaunchSpec{}, fmt.Errorf(
			"apple-container sessions reject a root guest identity; build root-requiring steps into the image instead")
	}
	uid, gid := opts.GuestUID, opts.GuestGID
	if uid == 0 {
		uid = DefaultGuestUID
	}
	if gid == 0 {
		gid = DefaultGuestGID
	}
	if uid <= 0 || gid <= 0 {
		return LaunchSpec{}, fmt.Errorf("apple-container guest identity must be a positive non-root uid/gid, got %d:%d", uid, gid)
	}

	// Unsupported network policies fail closed before any plan is produced.
	networkMode := sessionmeta.NormalizeNetworkMode(contract.Network.Mode)
	if networkMode != sessionmeta.NetworkDefault {
		return LaunchSpec{}, fmt.Errorf(
			"--network %s is not implemented for the Apple Container backend yet.\n"+
				"Apple Container networking is VM-backed and Hazmat does not have a proved\n"+
				"deny-all egress mechanism for this backend.\n"+
				"Use native containment for network-none macOS sessions, Docker Sandbox for\n"+
				"Hazmat-managed deny-mode profiles, or omit the network restriction.",
			networkMode)
	}

	credEnvFile := strings.TrimSpace(opts.CredentialEnvFile)
	if credEnvFile != "" && !filepath.IsAbs(credEnvFile) {
		return LaunchSpec{}, fmt.Errorf("credential env-file path %q must be absolute", credEnvFile)
	}

	name := ContainerName(opts.Harness, contract.ProjectPath(), opts.SessionID)
	spec := LaunchSpec{
		FormatVersion: LaunchSpecFormatVersion,
		Backend:       BackendAppleContainer,
		Phase:         PhasePlanOnly,
		ContainerName: name,
		Image:         strings.TrimSpace(opts.Image),
		Workdir:       contract.ProjectPath(),
		User:          UserSpec{UID: uid, GID: gid},
		Mounts:        compileMounts(contract),
		Rootfs: RootfsSpec{
			ReadOnly:  false,
			Ephemeral: true,
		},
		Network: NetworkSpec{
			Mode:        networkMode,
			Enforcement: "apple-container-vm-network",
		},
		Resources: opts.Resources,
		Capabilities: CapabilitySpec{
			DropAll: true,
		},
		Environment: EnvironmentSpec{
			InheritHostEnv:    false,
			CredentialEnvFile: credEnvFile,
		},
		Labels: map[string]string{
			LabelSessionID: opts.SessionID,
			LabelHarness:   opts.Harness,
		},
		Cleanup: CleanupSpec{
			RemoveContainerByName: true,
			NeverPrune:            true,
		},
		Command:        append([]string(nil), opts.Command...),
		CapabilityGaps: capabilityGaps(opts.Host),
	}
	if credEnvFile != "" {
		spec.Cleanup.RemoveGeneratedFiles = []string{credEnvFile}
	}
	return spec, nil
}

// Argv derives the host `container run` command from a validated launch
// spec. It is plan-only output: the exact flag surface must be re-validated
// against the installed CLI during the host behavior spike before any
// runtime executes it.
func Argv(spec LaunchSpec) ([]string, error) {
	if spec.Backend != BackendAppleContainer {
		return nil, fmt.Errorf("argv derivation requires an apple-container spec, got backend %q", spec.Backend)
	}
	if spec.ContainerName == "" || spec.Image == "" {
		return nil, fmt.Errorf("argv derivation requires a compiled spec with container name and image")
	}
	if spec.Environment.InheritHostEnv {
		return nil, fmt.Errorf("argv derivation refuses host env inheritance")
	}

	argv := []string{
		"container", "run",
		"--name", spec.ContainerName,
		"--user", fmt.Sprintf("%d:%d", spec.User.UID, spec.User.GID),
		"--workdir", spec.Workdir,
		"--network", string(spec.Network.Mode),
	}
	for _, mount := range spec.Mounts {
		argv = append(argv, "--mount", mountFlag(mount))
	}
	for _, path := range spec.Tmpfs {
		argv = append(argv, "--tmpfs", path)
	}
	if spec.Rootfs.ReadOnly {
		argv = append(argv, "--read-only")
	}
	if spec.Resources.CPUs > 0 {
		argv = append(argv, "--cpus", fmt.Sprintf("%d", spec.Resources.CPUs))
	}
	if spec.Resources.Memory != "" {
		argv = append(argv, "--memory", spec.Resources.Memory)
	}
	if spec.Capabilities.DropAll {
		argv = append(argv, "--cap-drop", "all")
	}
	for _, cap := range spec.Capabilities.Add {
		argv = append(argv, "--cap-add", cap)
	}
	if spec.Environment.CredentialEnvFile != "" {
		argv = append(argv, "--env-file", spec.Environment.CredentialEnvFile)
	}
	for _, key := range sortedLabelKeys(spec.Labels) {
		argv = append(argv, "--label", key+"="+spec.Labels[key])
	}
	argv = append(argv, spec.Image)
	argv = append(argv, spec.Command...)
	return argv, nil
}

// MarshalJSON encodes a launch spec as indented JSON.
func MarshalJSON(spec LaunchSpec) ([]byte, error) {
	return json.MarshalIndent(spec, "", "  ")
}

// ContainerName builds the deterministic per-session container name:
// hazmat-<harness>-<project-base>-<hash>.
func ContainerName(harness, projectDir, sessionID string) string {
	base := strings.ToLower(filepath.Base(projectDir))
	base = containerNamePattern.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "workspace"
	}
	h := sha256.New()
	_, _ = h.Write([]byte(harness))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(projectDir))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(sessionID))
	sum := hex.EncodeToString(h.Sum(nil)[:6])
	return fmt.Sprintf("hazmat-%s-%s-%s", strings.ToLower(harness), base, sum)
}

func validateImage(image string) error {
	trimmed := strings.TrimSpace(image)
	if trimmed == "" {
		return fmt.Errorf("apple-container compile requires an explicit image; the backend never guesses one")
	}
	if strings.ContainsAny(trimmed, " \t\n") {
		return fmt.Errorf("apple-container image reference %q must not contain whitespace", image)
	}
	return nil
}

func compileMounts(contract containment.Contract) []MountSpec {
	mounts := []MountSpec{{
		Source: contract.ProjectPath(),
		Target: contract.ProjectPath(),
		Access: containment.PathReadWrite,
	}}
	for _, path := range contract.EffectiveReadOnlyDirs() {
		mounts = append(mounts, MountSpec{
			Source: path,
			Target: path,
			Access: containment.PathReadOnly,
		})
	}
	for _, path := range contract.EffectiveWritableDirs() {
		mounts = append(mounts, MountSpec{
			Source: path,
			Target: path,
			Access: containment.PathReadWrite,
		})
	}
	return mounts
}

func mountFlag(mount MountSpec) string {
	flag := fmt.Sprintf("type=bind,source=%s,target=%s", mount.Source, mount.Target)
	if mount.Access == containment.PathReadOnly {
		flag += ",readonly"
	}
	return flag
}

func capabilityGaps(host HostReport) []CapabilityGap {
	gaps := []CapabilityGap{{
		Code:    "apple-container-runtime-missing",
		Message: "Apple Container runtime is not implemented; spec is plan-only",
	}}
	if host.GOOS != "" && host.GOOS != "darwin" {
		gaps = append(gaps, CapabilityGap{
			Code:    "host-not-darwin",
			Message: "Apple Container requires macOS",
			State:   host.GOOS,
		})
	}
	if host.GOARCH != "" && host.GOARCH != "arm64" {
		gaps = append(gaps, CapabilityGap{
			Code:    "host-not-apple-silicon",
			Message: "Apple Container requires Apple silicon",
			State:   host.GOARCH,
		})
	}
	if host.MacOSMajorVersion == 0 {
		gaps = append(gaps, CapabilityGap{
			Code:    "macos-version-uninspected",
			Message: "macOS version has not been inspected; macOS 26 or newer is required",
		})
	} else if host.MacOSMajorVersion < 26 {
		gaps = append(gaps, CapabilityGap{
			Code:    "macos-version-unsupported",
			Message: "Apple Container requires macOS 26 or newer",
			State:   fmt.Sprintf("%d", host.MacOSMajorVersion),
		})
	}
	if host.CLIPath == "" {
		gaps = append(gaps, CapabilityGap{
			Code:    "container-cli-missing",
			Message: "container CLI was not found at an approved absolute path",
		})
	} else if !filepath.IsAbs(host.CLIPath) {
		gaps = append(gaps, CapabilityGap{
			Code:    "container-cli-path-not-absolute",
			Message: "container CLI path must be absolute",
			State:   host.CLIPath,
		})
	}
	if !host.CLIVersionSupported {
		gaps = append(gaps, CapabilityGap{
			Code:    "container-cli-version-unsupported",
			Message: "container CLI version is not positively supported (>= 1.0.0 required)",
			State:   host.CLIVersion,
		})
	}
	if !host.APIServerHealthy {
		gaps = append(gaps, CapabilityGap{
			Code:    "container-api-server-unhealthy",
			Message: "container system status has not positively reported a healthy API server",
		})
	}
	if !host.RunnableAsAgent {
		gaps = append(gaps, CapabilityGap{
			Code:    "agent-user-execution-unverified",
			Message: "container CLI execution as the dedicated agent user is not positively verified",
		})
	}
	return gaps
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func sortedLabelKeys(labels map[string]string) []string {
	if len(labels) == 0 {
		return nil
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

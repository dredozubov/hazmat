// Package sessionmeta describes the machine-readable launch metadata emitted
// by Hazmat sessions.
package sessionmeta

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// NetworkMode is the requested outbound network policy for a session.
type NetworkMode string

const (
	// NetworkDefault leaves outbound networking available for the session.
	NetworkDefault NetworkMode = "default"

	// NetworkNone denies outbound IPv4, IPv6, and DNS in native sessions.
	NetworkNone NetworkMode = "none"
)

// Mode is the effective session runtime backend.
type Mode string

const (
	// ModeNative runs through Hazmat's native macOS containment backend.
	ModeNative Mode = "native"
	// ModeDockerSandbox runs through Docker Sandboxes.
	ModeDockerSandbox Mode = "docker-sandbox"
	// ModeAppleContainer runs a Linux agent session in an Apple Container
	// microVM. Plan-only: no executable launch path exists yet.
	ModeAppleContainer Mode = "apple-container"
)

// LaunchMetadataFormatVersion is the current JSON schema version.
const LaunchMetadataFormatVersion = 1

// LaunchMetadataInput contains the session facts needed to build metadata.
type LaunchMetadataInput struct {
	Target      string
	Mode        Mode
	ProjectDir  string
	NetworkMode NetworkMode
}

// LaunchMetadata is the JSON payload emitted before session launch.
type LaunchMetadata struct {
	FormatVersion int                   `json:"format_version"`
	Kind          string                `json:"kind"`
	Target        string                `json:"target,omitempty"`
	Mode          string                `json:"mode"`
	ModeLabel     string                `json:"mode_label"`
	ProjectDir    string                `json:"project_dir"`
	NetworkPolicy NetworkPolicyMetadata `json:"network_policy"`
}

// NetworkPolicyMetadata describes the effective network policy in metadata.
type NetworkPolicyMetadata struct {
	Requested       string   `json:"requested"`
	Effective       string   `json:"effective"`
	Enforced        bool     `json:"enforced"`
	Enforcement     string   `json:"enforcement"`
	DenyAllEgress   bool     `json:"deny_all_egress"`
	Denied          []string `json:"denied,omitempty"`
	CleanupRequired bool     `json:"cleanup_required"`
}

// ParseNetworkMode parses a CLI/config network mode string.
func ParseNetworkMode(raw string) (NetworkMode, error) {
	mode := NetworkMode(strings.ToLower(strings.TrimSpace(raw)))
	if mode == "" {
		return NetworkDefault, nil
	}
	switch mode {
	case NetworkDefault, NetworkNone:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid network mode %q (want default or none)", raw)
	}
}

// NormalizeNetworkMode converts the empty mode to NetworkDefault.
func NormalizeNetworkMode(mode NetworkMode) NetworkMode {
	if mode == "" {
		return NetworkDefault
	}
	return mode
}

// String returns the normalized mode string.
func (m NetworkMode) String() string {
	return string(NormalizeNetworkMode(m))
}

// ContractLabel returns the human-readable session contract label.
func (m NetworkMode) ContractLabel() string {
	switch NormalizeNetworkMode(m) {
	case NetworkDefault:
		return "default (outbound allowed)"
	case NetworkNone:
		return "none (deny outbound IPv4, IPv6, and DNS)"
	default:
		return "default (outbound allowed)"
	}
}

// Label returns the human-readable runtime mode label.
func (m Mode) Label() string {
	switch m {
	case ModeNative:
		return "Native containment"
	case ModeDockerSandbox:
		return "Docker Sandbox"
	case ModeAppleContainer:
		return "Apple Container"
	default:
		return "Native containment"
	}
}

// NetworkContractLabel returns the user-facing network policy label.
func NetworkContractLabel(networkMode NetworkMode, mode Mode) string {
	if mode == ModeDockerSandbox {
		return "Docker Sandbox profile (deny by default)"
	}
	if mode == ModeAppleContainer {
		return "default (outbound allowed, Apple Container VM network)"
	}
	return NormalizeNetworkMode(networkMode).ContractLabel()
}

// BuildLaunchMetadata returns the machine-readable launch metadata.
func BuildLaunchMetadata(input LaunchMetadataInput) LaunchMetadata {
	return LaunchMetadata{
		FormatVersion: LaunchMetadataFormatVersion,
		Kind:          "hazmat.session",
		Target:        input.Target,
		Mode:          string(input.Mode),
		ModeLabel:     input.Mode.Label(),
		ProjectDir:    input.ProjectDir,
		NetworkPolicy: BuildNetworkPolicyMetadata(input.NetworkMode, input.Mode),
	}
}

// BuildNetworkPolicyMetadata returns the metadata for the effective policy.
func BuildNetworkPolicyMetadata(networkMode NetworkMode, mode Mode) NetworkPolicyMetadata {
	requested := NormalizeNetworkMode(networkMode)
	meta := NetworkPolicyMetadata{
		Requested:       requested.String(),
		Effective:       requested.String(),
		Enforced:        true,
		Enforcement:     "native-seatbelt",
		CleanupRequired: false,
	}
	if requested == NetworkNone {
		meta.DenyAllEgress = true
		meta.Denied = []string{"outbound-ipv4", "outbound-ipv6", "dns"}
	}
	if mode == ModeDockerSandbox {
		meta.Effective = "sandbox-profile"
		meta.Enforcement = "docker-sandbox-network-profile"
		meta.CleanupRequired = true
		meta.DenyAllEgress = false
		meta.Denied = nil
	}
	if mode == ModeAppleContainer {
		// Honest reporting: Apple Container networking is VM-backed and only
		// the default outbound-allowed policy is supported. Network none and
		// allowlists fail closed before launch instead of being claimed here.
		meta.Effective = NetworkDefault.String()
		meta.Enforcement = "apple-container-vm-network"
		meta.DenyAllEgress = false
		meta.Denied = nil
	}
	return meta
}

// EmitLaunchMetadataJSON writes one newline-terminated metadata JSON object.
func EmitLaunchMetadataJSON(w io.Writer, input LaunchMetadataInput) error {
	metadataJSON, err := MarshalLaunchMetadataJSON(input)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, metadataJSON)
	return err
}

// MarshalLaunchMetadataJSON encodes one metadata JSON object.
func MarshalLaunchMetadataJSON(input LaunchMetadataInput) (string, error) {
	data, err := json.Marshal(BuildLaunchMetadata(input))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

package sessionbackend

import (
	"runtime"
	"sort"

	"hazmat/sessionmeta"
)

type Kind string

const (
	KindDarwinNative       Kind   = "darwin-native"
	KindLinuxNative        Kind   = "linux-native"
	KindDockerSandbox      Kind   = "docker-sandbox"
	KindUnsupportedNative  Kind   = "unsupported-native"
	GapNativeLaunch        string = "native-launch"
	GapRemoteLaunch        string = "remote-launch"
	GapIntegrationEnv      string = "integration-env-passthrough"
	ArtifactSeatbeltPolicy string = "seatbelt-policy"
	ArtifactDockerSandbox  string = "docker-sandbox"
)

type CapabilityGap struct {
	Feature string `json:"feature"`
	Reason  string `json:"reason"`
}

type LifecycleArtifact struct {
	Kind            string `json:"kind"`
	Path            string `json:"path,omitempty"`
	CleanupRequired bool   `json:"cleanup_required,omitempty"`
}

type Input struct {
	Target             string
	Mode               sessionmeta.Mode
	ProjectDir         string
	ReadOnlyDirs       []string
	ReadWriteDirs      []string
	NetworkMode        sessionmeta.NetworkMode
	Integrations       []string
	IntegrationEnvKeys []string
	GOOS               string
}

type Plan struct {
	Target             string                  `json:"target"`
	Mode               sessionmeta.Mode        `json:"mode"`
	Backend            Kind                    `json:"backend"`
	ProjectDir         string                  `json:"project_dir"`
	ReadOnlyDirs       []string                `json:"read_only_dirs,omitempty"`
	ReadWriteDirs      []string                `json:"read_write_dirs,omitempty"`
	NetworkMode        sessionmeta.NetworkMode `json:"network_mode"`
	Integrations       []string                `json:"integrations,omitempty"`
	IntegrationEnvKeys []string                `json:"integration_env_keys,omitempty"`
	CapabilityGaps     []CapabilityGap         `json:"capability_gaps,omitempty"`
	LifecycleArtifacts []LifecycleArtifact     `json:"lifecycle_artifacts,omitempty"`
}

func BuildPlan(input Input) Plan {
	goos := input.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	backend := BackendFor(input.Mode, goos)
	plan := Plan{
		Target:             input.Target,
		Mode:               input.Mode,
		Backend:            backend,
		ProjectDir:         input.ProjectDir,
		ReadOnlyDirs:       copyStrings(input.ReadOnlyDirs),
		ReadWriteDirs:      copyStrings(input.ReadWriteDirs),
		NetworkMode:        sessionmeta.NormalizeNetworkMode(input.NetworkMode),
		Integrations:       copyStrings(input.Integrations),
		IntegrationEnvKeys: sortedStrings(input.IntegrationEnvKeys),
		CapabilityGaps:     capabilityGaps(input, backend),
		LifecycleArtifacts: lifecycleArtifacts(backend),
	}
	return plan
}

func BackendFor(mode sessionmeta.Mode, goos string) Kind {
	if mode == sessionmeta.ModeDockerSandbox {
		return KindDockerSandbox
	}
	if mode != sessionmeta.ModeNative {
		return KindUnsupportedNative
	}
	switch goos {
	case "darwin":
		return KindDarwinNative
	case "linux":
		return KindLinuxNative
	default:
		return KindUnsupportedNative
	}
}

func capabilityGaps(input Input, backend Kind) []CapabilityGap {
	var gaps []CapabilityGap
	switch backend {
	case KindDarwinNative, KindDockerSandbox:
	case KindLinuxNative:
		gaps = append(gaps, CapabilityGap{
			Feature: GapNativeLaunch,
			Reason:  "Linux native launch is currently plan-only; the guarded launch helper is not implemented yet.",
		})
	case KindUnsupportedNative:
		gaps = append(gaps, CapabilityGap{
			Feature: GapNativeLaunch,
			Reason:  "Native launch is not implemented for this platform.",
		})
	case KindRemoteEnvelope:
		gaps = append(gaps, CapabilityGap{
			Feature: GapRemoteLaunch,
			Reason:  "Remote launch envelopes are plan-only; worker admission and runner semantics are not implemented yet.",
		})
	}
	if backend == KindDockerSandbox && len(input.IntegrationEnvKeys) > 0 {
		gaps = append(gaps, CapabilityGap{
			Feature: GapIntegrationEnv,
			Reason:  "Docker Sandbox launch does not yet support integration env passthrough.",
		})
	}
	return gaps
}

func lifecycleArtifacts(backend Kind) []LifecycleArtifact {
	switch backend {
	case KindDarwinNative:
		return []LifecycleArtifact{{
			Kind:            ArtifactSeatbeltPolicy,
			CleanupRequired: true,
		}}
	case KindDockerSandbox:
		return []LifecycleArtifact{{
			Kind:            ArtifactDockerSandbox,
			CleanupRequired: false,
		}}
	default:
		return nil
	}
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func sortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := copyStrings(values)
	sort.Strings(out)
	return out
}

// Package sessioncontract defines backend-neutral session request and plan
// shapes for Hazmat. It is intentionally data-only: no Cobra commands, prompts,
// host mutation execution, SBPL rendering, Docker CLI calls, or process launch.
package sessioncontract

import (
	"sort"

	"hazmat/sessionmeta"
)

// PlanFormatVersion is the current JSON contract version for session plans.
const PlanFormatVersion = 1

// Request is the side-effect-free input shape for planning a Hazmat session.
type Request struct {
	Target              string                  `json:"target"`
	ProjectDir          string                  `json:"project_dir"`
	ReadOnlyDirs        []string                `json:"read_only_dirs,omitempty"`
	AutoReadOnlyDirs    []string                `json:"auto_read_only_dirs,omitempty"`
	UserReadOnlyDirs    []string                `json:"user_read_only_dirs,omitempty"`
	ReadWriteExtensions []string                `json:"read_write_extensions,omitempty"`
	NetworkMode         sessionmeta.NetworkMode `json:"network_mode"`
	Integrations        []string                `json:"integrations,omitempty"`
	HarnessID           string                  `json:"harness_id,omitempty"`
	MetadataJSON        bool                    `json:"metadata_json,omitempty"`
}

// Normalized returns a defensive-copy request with default network mode
// materialized.
func (r Request) Normalized() Request {
	r.ReadOnlyDirs = copyStrings(r.ReadOnlyDirs)
	r.AutoReadOnlyDirs = copyStrings(r.AutoReadOnlyDirs)
	r.UserReadOnlyDirs = copyStrings(r.UserReadOnlyDirs)
	r.ReadWriteExtensions = copyStrings(r.ReadWriteExtensions)
	r.Integrations = copyStrings(r.Integrations)
	r.NetworkMode = sessionmeta.NormalizeNetworkMode(r.NetworkMode)
	return r
}

// LaunchMetadataInput returns the corresponding launch metadata input for a
// selected backend mode.
func (r Request) LaunchMetadataInput(mode sessionmeta.Mode) sessionmeta.LaunchMetadataInput {
	normalized := r.Normalized()
	return sessionmeta.LaunchMetadataInput{
		Target:      normalized.Target,
		Mode:        mode,
		ProjectDir:  normalized.ProjectDir,
		NetworkMode: normalized.NetworkMode,
	}
}

// PlanInput is the side-effect-free material used to construct a session plan.
type PlanInput struct {
	Target                string
	Mode                  sessionmeta.Mode
	ProjectDir            string
	RoutingReason         string
	SuggestedIntegrations []string
	RepoSetupSummary      string
	RepoSetupApplied      []RepoSetupEffect
	RepoSetupPending      []RepoSetupEffect
	ActiveIntegrations    []string
	IntegrationSources    []string
	IntegrationDetails    []string
	IntegrationWarnings   []string
	IntegrationEnv        map[string]string
	RegistryEnvKeys       []string
	CredentialEnvGrants   []CredentialEnvGrant
	PlannedHostMutations  []HostMutation
	ReadOnlyDirs          []string
	AutoReadOnlyDirs      []string
	UserReadOnlyDirs      []string
	ReadWriteExtensions   []string
	NetworkMode           sessionmeta.NetworkMode
	ServiceAccess         []string
	GitSSHKey             string
	Snapshot              Snapshot
	SessionNotes          []string
}

// Plan is the backend-neutral session contract preview.
type Plan struct {
	FormatVersion         int                               `json:"format_version"`
	Target                string                            `json:"target"`
	Mode                  string                            `json:"mode"`
	ModeLabel             string                            `json:"mode_label"`
	ProjectDir            string                            `json:"project_dir"`
	RoutingReason         string                            `json:"routing_reason,omitempty"`
	SuggestedIntegrations []string                          `json:"suggested_integrations,omitempty"`
	RepoSetupSummary      string                            `json:"repo_setup_summary,omitempty"`
	RepoSetupApplied      []RepoSetupEffect                 `json:"repo_setup_applied,omitempty"`
	RepoSetupPending      []RepoSetupEffect                 `json:"repo_setup_pending,omitempty"`
	ActiveIntegrations    []string                          `json:"active_integrations,omitempty"`
	IntegrationSources    []string                          `json:"integration_sources,omitempty"`
	IntegrationDetails    []string                          `json:"integration_details,omitempty"`
	IntegrationWarnings   []string                          `json:"integration_warnings,omitempty"`
	IntegrationEnvKeys    []string                          `json:"integration_env_keys,omitempty"`
	RegistryEnvKeys       []string                          `json:"integration_registry_env_keys,omitempty"`
	CredentialEnvGrants   []CredentialEnvGrant              `json:"credential_env_grants,omitempty"`
	PlannedHostMutations  []HostMutation                    `json:"planned_host_mutations,omitempty"`
	ReadOnlyDirs          []string                          `json:"read_only_dirs,omitempty"`
	AutoReadOnlyDirs      []string                          `json:"auto_read_only_dirs,omitempty"`
	UserReadOnlyDirs      []string                          `json:"user_read_only_dirs,omitempty"`
	ReadWriteExtensions   []string                          `json:"read_write_extensions,omitempty"`
	NetworkPolicy         sessionmeta.NetworkPolicyMetadata `json:"network_policy"`
	ServiceAccess         []string                          `json:"service_access,omitempty"`
	GitSSHKey             string                            `json:"git_ssh_key,omitempty"`
	Snapshot              Snapshot                          `json:"snapshot"`
	SessionNotes          []string                          `json:"session_notes,omitempty"`
}

// RepoSetupEffect is a redaction-safe repo setup effect in a session plan.
type RepoSetupEffect struct {
	Class   string   `json:"class"`
	Kind    string   `json:"kind"`
	Value   string   `json:"value"`
	Sources []string `json:"sources,omitempty"`
}

// CredentialEnvGrant describes a redaction-safe env credential grant. For
// provider API keys, ConsumerHarness names the active harness allowed to consume
// the shared provider credential.
type CredentialEnvGrant struct {
	EnvVar          string `json:"env_var"`
	CredentialID    string `json:"credential_id,omitempty"`
	Source          string `json:"source,omitempty"`
	ConsumerHarness string `json:"consumer_harness,omitempty"`
	Redacted        bool   `json:"redacted"`
}

// HostMutation describes a planned host mutation without executable behavior.
type HostMutation struct {
	Summary     string `json:"summary"`
	Detail      string `json:"detail"`
	Persistence string `json:"persistence"`
	ProofScope  string `json:"proof_scope"`
}

// Snapshot describes pre-session snapshot behavior for a plan.
type Snapshot struct {
	Enabled  bool     `json:"enabled"`
	Excludes []string `json:"excludes,omitempty"`
}

// BuildPlan constructs a defensive-copy session plan from input values.
func BuildPlan(input PlanInput) Plan {
	mode := input.Mode
	return Plan{
		FormatVersion:         PlanFormatVersion,
		Target:                input.Target,
		Mode:                  string(mode),
		ModeLabel:             mode.Label(),
		ProjectDir:            input.ProjectDir,
		RoutingReason:         input.RoutingReason,
		SuggestedIntegrations: copyStrings(input.SuggestedIntegrations),
		RepoSetupSummary:      input.RepoSetupSummary,
		RepoSetupApplied:      copyRepoSetupEffects(input.RepoSetupApplied),
		RepoSetupPending:      copyRepoSetupEffects(input.RepoSetupPending),
		ActiveIntegrations:    copyStrings(input.ActiveIntegrations),
		IntegrationSources:    copyStrings(input.IntegrationSources),
		IntegrationDetails:    copyStrings(input.IntegrationDetails),
		IntegrationWarnings:   copyStrings(input.IntegrationWarnings),
		IntegrationEnvKeys:    integrationEnvKeys(input.IntegrationEnv),
		RegistryEnvKeys:       copyStrings(input.RegistryEnvKeys),
		CredentialEnvGrants:   copyCredentialEnvGrants(input.CredentialEnvGrants),
		PlannedHostMutations:  copyHostMutations(input.PlannedHostMutations),
		ReadOnlyDirs:          copyStrings(input.ReadOnlyDirs),
		AutoReadOnlyDirs:      copyStrings(input.AutoReadOnlyDirs),
		UserReadOnlyDirs:      copyStrings(input.UserReadOnlyDirs),
		ReadWriteExtensions:   copyStrings(input.ReadWriteExtensions),
		NetworkPolicy:         sessionmeta.BuildNetworkPolicyMetadata(input.NetworkMode, input.Mode),
		ServiceAccess:         copyStrings(input.ServiceAccess),
		GitSSHKey:             input.GitSSHKey,
		Snapshot: Snapshot{
			Enabled:  input.Snapshot.Enabled,
			Excludes: copyStrings(input.Snapshot.Excludes),
		},
		SessionNotes: copyStrings(input.SessionNotes),
	}
}

func integrationEnvKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func copyRepoSetupEffects(values []RepoSetupEffect) []RepoSetupEffect {
	if len(values) == 0 {
		return nil
	}
	out := make([]RepoSetupEffect, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Sources = copyStrings(value.Sources)
	}
	return out
}

func copyCredentialEnvGrants(values []CredentialEnvGrant) []CredentialEnvGrant {
	if len(values) == 0 {
		return nil
	}
	out := make([]CredentialEnvGrant, len(values))
	copy(out, values)
	return out
}

func copyHostMutations(values []HostMutation) []HostMutation {
	if len(values) == 0 {
		return nil
	}
	out := make([]HostMutation, len(values))
	copy(out, values)
	return out
}

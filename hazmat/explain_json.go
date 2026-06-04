package hazmat

import (
	"sort"
	"strings"

	"hazmat/hostfacts"
	linuxplatform "hazmat/platform/linux"
	"hazmat/sessioncontract"
)

const explainJSONFormatVersion = sessioncontract.PlanFormatVersion

type explainJSONPreview struct {
	FormatVersion         int                             `json:"format_version"`
	Target                string                          `json:"target"`
	Mode                  string                          `json:"mode"`
	ModeLabel             string                          `json:"mode_label"`
	ProjectDir            string                          `json:"project_dir"`
	RoutingReason         string                          `json:"routing_reason,omitempty"`
	SuggestedIntegrations []string                        `json:"suggested_integrations,omitempty"`
	RepoSetupSummary      string                          `json:"repo_setup_summary,omitempty"`
	RepoSetupApplied      []explainJSONRepoSetupEffect    `json:"repo_setup_applied,omitempty"`
	RepoSetupPending      []explainJSONRepoSetupEffect    `json:"repo_setup_pending,omitempty"`
	ActiveIntegrations    []string                        `json:"active_integrations,omitempty"`
	IntegrationSources    []string                        `json:"integration_sources,omitempty"`
	IntegrationDetails    []string                        `json:"integration_details,omitempty"`
	IntegrationWarnings   []string                        `json:"integration_warnings,omitempty"`
	IntegrationEnvKeys    []string                        `json:"integration_env_keys,omitempty"`
	RegistryEnvKeys       []string                        `json:"integration_registry_env_keys,omitempty"`
	CredentialEnvGrants   []explainJSONCredentialEnvGrant `json:"credential_env_grants,omitempty"`
	PlannedHostMutations  []sessionMutation               `json:"planned_host_mutations,omitempty"`
	ReadOnlyDirs          []string                        `json:"read_only_dirs,omitempty"`
	AutoReadOnlyDirs      []string                        `json:"auto_read_only_dirs,omitempty"`
	UserReadOnlyDirs      []string                        `json:"user_read_only_dirs,omitempty"`
	ReadWriteExtensions   []string                        `json:"read_write_extensions,omitempty"`
	NetworkPolicy         sessionNetworkPolicyMetadata    `json:"network_policy"`
	ServiceAccess         []string                        `json:"service_access,omitempty"`
	GitSSHKey             string                          `json:"git_ssh_key,omitempty"`
	Snapshot              explainJSONBackup               `json:"snapshot"`
	SessionNotes          []string                        `json:"session_notes,omitempty"`
	Platform              *linuxplatform.Report           `json:"platform,omitempty"`
}

type explainJSONRepoSetupEffect = sessioncontract.RepoSetupEffect
type explainJSONCredentialEnvGrant = sessioncontract.CredentialEnvGrant
type explainJSONBackup = sessioncontract.Snapshot

func buildExplainJSON(target string, cfg sessionConfig, mode sessionMode, skipSnapshot bool) explainJSONPreview {
	facts := currentHostFacts()
	plan := buildSessionPlanForHostFacts(target, cfg, mode, skipSnapshot, facts)
	return explainJSONPreviewFromPlan(plan.Contract, explainPlatformReport(facts))
}

func explainJSONPreviewFromPlan(plan sessioncontract.Plan, platform *linuxplatform.Report) explainJSONPreview {
	return explainJSONPreview{
		FormatVersion:         plan.FormatVersion,
		Target:                plan.Target,
		Mode:                  plan.Mode,
		ModeLabel:             plan.ModeLabel,
		ProjectDir:            plan.ProjectDir,
		RoutingReason:         plan.RoutingReason,
		SuggestedIntegrations: plan.SuggestedIntegrations,
		RepoSetupSummary:      plan.RepoSetupSummary,
		RepoSetupApplied:      plan.RepoSetupApplied,
		RepoSetupPending:      plan.RepoSetupPending,
		ActiveIntegrations:    plan.ActiveIntegrations,
		IntegrationSources:    plan.IntegrationSources,
		IntegrationDetails:    plan.IntegrationDetails,
		IntegrationWarnings:   plan.IntegrationWarnings,
		IntegrationEnvKeys:    plan.IntegrationEnvKeys,
		RegistryEnvKeys:       plan.RegistryEnvKeys,
		CredentialEnvGrants:   plan.CredentialEnvGrants,
		PlannedHostMutations:  plan.PlannedHostMutations,
		ReadOnlyDirs:          plan.ReadOnlyDirs,
		AutoReadOnlyDirs:      plan.AutoReadOnlyDirs,
		UserReadOnlyDirs:      plan.UserReadOnlyDirs,
		ReadWriteExtensions:   plan.ReadWriteExtensions,
		NetworkPolicy:         plan.NetworkPolicy,
		ServiceAccess:         plan.ServiceAccess,
		GitSSHKey:             plan.GitSSHKey,
		Snapshot:              plan.Snapshot,
		SessionNotes:          plan.SessionNotes,
		Platform:              platform,
	}
}

func buildSessionContractPlanInput(target string, cfg sessionConfig, mode sessionMode, skipSnapshot bool) sessioncontract.PlanInput {
	return sessioncontract.PlanInput{
		Target:                target,
		Mode:                  mode,
		ProjectDir:            cfg.ProjectDir,
		RoutingReason:         cfg.RoutingReason,
		SuggestedIntegrations: cfg.SuggestedIntegrations,
		RepoSetupSummary:      repoSetupSummary(cfg.RepoSetup),
		RepoSetupApplied:      explainJSONRepoSetupEffects(cfg.RepoSetup, true),
		RepoSetupPending:      explainJSONRepoSetupEffects(cfg.RepoSetup, false),
		ActiveIntegrations:    cfg.ActiveIntegrations,
		IntegrationSources:    cfg.IntegrationSources,
		IntegrationDetails:    cfg.IntegrationDetails,
		IntegrationWarnings:   cfg.IntegrationWarnings,
		IntegrationEnv:        cfg.IntegrationEnv,
		RegistryEnvKeys:       cfg.IntegrationRegistryKeys,
		CredentialEnvGrants:   explainJSONCredentialEnvGrants(cfg.CredentialEnvGrants),
		PlannedHostMutations:  cfg.PlannedHostMutations,
		ReadOnlyDirs:          cfg.ReadDirs,
		AutoReadOnlyDirs:      cfg.AutoReadDirs,
		UserReadOnlyDirs:      cfg.UserReadDirs,
		ReadWriteExtensions:   cfg.WriteDirs,
		NetworkMode:           cfg.NetworkMode,
		ServiceAccess:         cfg.ServiceAccess,
		GitSSHKey:             explainGitSSHKey(cfg.GitSSH),
		Snapshot: sessioncontract.Snapshot{
			Enabled:  !skipSnapshot,
			Excludes: cfg.IntegrationExcludes,
		},
		SessionNotes: cfg.SessionNotes,
	}
}

var explainPlatformReport = func(facts hostfacts.HostFacts) *linuxplatform.Report {
	if facts.TargetGOOS() != "linux" {
		return nil
	}
	report := linuxplatform.InspectHost()
	return &report
}

func explainJSONCredentialEnvGrants(grants []sessionCredentialEnvGrant) []explainJSONCredentialEnvGrant {
	normalized := normalizedSessionCredentialEnvGrants(grants)
	if len(normalized) == 0 {
		return nil
	}
	out := make([]explainJSONCredentialEnvGrant, 0, len(normalized))
	for _, grant := range normalized {
		out = append(out, explainJSONCredentialEnvGrant{
			EnvVar:          grant.EnvVar,
			CredentialID:    string(grant.CredentialID),
			Source:          grant.Source,
			ConsumerHarness: string(grant.ConsumerHarness),
			Redacted:        true,
		})
	}
	return out
}

func explainJSONRepoSetupEffects(state *repoSetupState, applied bool) []explainJSONRepoSetupEffect {
	if state == nil {
		return nil
	}
	var effects []repoSetupEffect
	if applied {
		effects = append(effects, state.AppliedSafe...)
		effects = append(effects, state.AppliedExplicit...)
	} else {
		effects = append(effects, state.PendingSafe...)
		effects = append(effects, state.PendingExplicit...)
	}
	if len(effects) == 0 {
		return nil
	}
	out := make([]explainJSONRepoSetupEffect, 0, len(effects))
	for _, effect := range effects {
		out = append(out, explainJSONRepoSetupEffect{
			Class:   string(effect.Class),
			Kind:    string(effect.Kind),
			Value:   effect.Value,
			Sources: append([]string(nil), effect.Sources...),
		})
	}
	return out
}

func explainGitSSHKey(cfg *sessionGitSSHConfig) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.DisplayName)
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

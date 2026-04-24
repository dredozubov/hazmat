package main

import (
	"sort"
	"strings"
)

const explainJSONFormatVersion = 1

type explainJSONPreview struct {
	FormatVersion         int               `json:"format_version"`
	Target                string            `json:"target"`
	Mode                  string            `json:"mode"`
	ModeLabel             string            `json:"mode_label"`
	ProjectDir            string            `json:"project_dir"`
	RoutingReason         string            `json:"routing_reason,omitempty"`
	SuggestedIntegrations []string          `json:"suggested_integrations,omitempty"`
	RepoSetupSummary      string            `json:"repo_setup_summary,omitempty"`
	RepoSetupApplied      []explainJSONRepoSetupEffect `json:"repo_setup_applied,omitempty"`
	RepoSetupPending      []explainJSONRepoSetupEffect `json:"repo_setup_pending,omitempty"`
	ActiveIntegrations    []string          `json:"active_integrations,omitempty"`
	IntegrationSources    []string          `json:"integration_sources,omitempty"`
	IntegrationDetails    []string          `json:"integration_details,omitempty"`
	IntegrationWarnings   []string          `json:"integration_warnings,omitempty"`
	IntegrationEnvKeys    []string          `json:"integration_env_keys,omitempty"`
	RegistryEnvKeys       []string          `json:"integration_registry_env_keys,omitempty"`
	PlannedHostMutations  []sessionMutation `json:"planned_host_mutations,omitempty"`
	ReadOnlyDirs          []string          `json:"read_only_dirs,omitempty"`
	AutoReadOnlyDirs      []string          `json:"auto_read_only_dirs,omitempty"`
	UserReadOnlyDirs      []string          `json:"user_read_only_dirs,omitempty"`
	ReadWriteExtensions   []string          `json:"read_write_extensions,omitempty"`
	ServiceAccess         []string          `json:"service_access,omitempty"`
	GitSSHKey             string            `json:"git_ssh_key,omitempty"`
	Snapshot              explainJSONBackup `json:"snapshot"`
	SessionNotes          []string          `json:"session_notes,omitempty"`
}

type explainJSONRepoSetupEffect struct {
	Class   string   `json:"class"`
	Kind    string   `json:"kind"`
	Value   string   `json:"value"`
	Sources []string `json:"sources,omitempty"`
}

type explainJSONBackup struct {
	Enabled  bool     `json:"enabled"`
	Excludes []string `json:"excludes,omitempty"`
}

func buildExplainJSON(target string, cfg sessionConfig, mode sessionMode, skipSnapshot bool) explainJSONPreview {
	return explainJSONPreview{
		FormatVersion:         explainJSONFormatVersion,
		Target:                target,
		Mode:                  string(mode),
		ModeLabel:             mode.label(),
		ProjectDir:            cfg.ProjectDir,
		RoutingReason:         cfg.RoutingReason,
		SuggestedIntegrations: append([]string(nil), cfg.SuggestedIntegrations...),
		RepoSetupSummary:      repoSetupSummary(cfg.RepoSetup),
		RepoSetupApplied:      explainJSONRepoSetupEffects(cfg.RepoSetup, true),
		RepoSetupPending:      explainJSONRepoSetupEffects(cfg.RepoSetup, false),
		ActiveIntegrations:    append([]string(nil), cfg.ActiveIntegrations...),
		IntegrationSources:    append([]string(nil), cfg.IntegrationSources...),
		IntegrationDetails:    append([]string(nil), cfg.IntegrationDetails...),
		IntegrationWarnings:   append([]string(nil), cfg.IntegrationWarnings...),
		IntegrationEnvKeys:    integrationEnvKeys(cfg.IntegrationEnv),
		RegistryEnvKeys:       append([]string(nil), cfg.IntegrationRegistryKeys...),
		PlannedHostMutations:  append([]sessionMutation(nil), cfg.PlannedHostMutations...),
		ReadOnlyDirs:          append([]string(nil), cfg.ReadDirs...),
		AutoReadOnlyDirs:      append([]string(nil), cfg.AutoReadDirs...),
		UserReadOnlyDirs:      append([]string(nil), cfg.UserReadDirs...),
		ReadWriteExtensions:   append([]string(nil), cfg.WriteDirs...),
		ServiceAccess:         append([]string(nil), cfg.ServiceAccess...),
		GitSSHKey:             explainGitSSHKey(cfg.GitSSH),
		Snapshot: explainJSONBackup{
			Enabled:  !skipSnapshot,
			Excludes: append([]string(nil), cfg.IntegrationExcludes...),
		},
		SessionNotes: append([]string(nil), cfg.SessionNotes...),
	}
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

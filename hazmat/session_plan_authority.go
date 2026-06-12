package hazmat

import (
	"sort"
	"strings"

	"hazmat/credentials"
	"hazmat/harnesses"
	"hazmat/hostfacts"
	"hazmat/integrations"
	"hazmat/sessionbackend"
	"hazmat/sessioncontract"
	"hazmat/sessionplanner"
)

type sessionPlanAuthority struct {
	request               sessioncontract.Request
	mode                  sessionMode
	skipSnapshot          bool
	routingReason         string
	suggestedIntegrations []integrations.Name
	activeIntegrations    []integrations.Name
	integrationSources    []string
	integrationDetails    []string
	integrationWarnings   []string
	integrationEnv        map[integrations.EnvKey]string
	registryEnvKeys       []integrations.EnvKey
	credentialEnvGrants   []sessionCredentialGrantAuthority
	plannedHostMutations  []sessionMutation
	serviceAccess         []string
	sessionNotes          []string
	snapshotExcludes      []string
	sessionHome           *sessionHomeRuntimePlan
	harnessID             harnesses.ID
	repoSetup             *repoSetupState
	gitSSHKey             string
}

type sessionCredentialGrantAuthority struct {
	envVar          integrations.EnvKey
	credentialID    credentials.ID
	source          string
	consumerHarness harnesses.ID
}

func newSessionPlanAuthority(target string, cfg sessionConfig, mode sessionMode, skipSnapshot bool) sessionPlanAuthority {
	request := sessioncontract.Request{
		Target:              target,
		ProjectDir:          cfg.ProjectDir,
		ReadOnlyDirs:        cfg.ReadDirs,
		AutoReadOnlyDirs:    cfg.AutoReadDirs,
		UserReadOnlyDirs:    cfg.UserReadDirs,
		ReadWriteExtensions: cfg.WriteDirs,
		NetworkMode:         cfg.NetworkMode,
		Integrations:        cfg.ActiveIntegrations,
		HarnessID:           string(cfg.HarnessID),
		MetadataJSON:        cfg.EmitSessionMetadataJSON,
	}.Normalized()
	return sessionPlanAuthority{
		request:               request,
		mode:                  mode,
		skipSnapshot:          skipSnapshot,
		routingReason:         cfg.RoutingReason,
		suggestedIntegrations: sessionIntegrationNames(cfg.SuggestedIntegrations),
		activeIntegrations:    sessionIntegrationNames(request.Integrations),
		integrationSources:    append([]string(nil), cfg.IntegrationSources...),
		integrationDetails:    append([]string(nil), cfg.IntegrationDetails...),
		integrationWarnings:   append([]string(nil), cfg.IntegrationWarnings...),
		integrationEnv:        sessionIntegrationEnv(cfg.IntegrationEnv),
		registryEnvKeys:       sessionEnvKeys(cfg.IntegrationRegistryKeys),
		credentialEnvGrants:   sessionCredentialGrantAuthorities(cfg.CredentialEnvGrants),
		plannedHostMutations:  append([]sessionMutation(nil), cfg.PlannedHostMutations...),
		serviceAccess:         append([]string(nil), cfg.ServiceAccess...),
		sessionNotes:          append([]string(nil), cfg.SessionNotes...),
		snapshotExcludes:      append([]string(nil), cfg.IntegrationExcludes...),
		sessionHome:           copySessionHomeRuntimePlan(cfg.SessionHome),
		harnessID:             harnesses.ID(cfg.HarnessID),
		repoSetup:             cfg.RepoSetup,
		gitSSHKey:             explainGitSSHKey(cfg.GitSSH),
	}
}

func (authority sessionPlanAuthority) ContractInput() sessioncontract.PlanInput {
	request := authority.request.Normalized()
	return sessioncontract.PlanInput{
		Target:                request.Target,
		Mode:                  authority.mode,
		ProjectDir:            request.ProjectDir,
		RoutingReason:         authority.routingReason,
		SuggestedIntegrations: sessionIntegrationNameStrings(authority.suggestedIntegrations),
		RepoSetupSummary:      repoSetupSummary(authority.repoSetup),
		RepoSetupApplied:      explainJSONRepoSetupEffects(authority.repoSetup, true),
		RepoSetupPending:      explainJSONRepoSetupEffects(authority.repoSetup, false),
		ActiveIntegrations:    sessionIntegrationNameStrings(authority.activeIntegrations),
		IntegrationSources:    append([]string(nil), authority.integrationSources...),
		IntegrationDetails:    append([]string(nil), authority.integrationDetails...),
		IntegrationWarnings:   append([]string(nil), authority.integrationWarnings...),
		IntegrationEnv:        authority.integrationEnvMap(),
		RegistryEnvKeys:       sessionEnvKeyStrings(authority.registryEnvKeys),
		CredentialEnvGrants:   authority.contractCredentialEnvGrants(),
		PlannedHostMutations:  append([]sessionMutation(nil), authority.plannedHostMutations...),
		ReadOnlyDirs:          request.ReadOnlyDirs,
		AutoReadOnlyDirs:      request.AutoReadOnlyDirs,
		UserReadOnlyDirs:      request.UserReadOnlyDirs,
		ReadWriteExtensions:   request.ReadWriteExtensions,
		NetworkMode:           request.NetworkMode,
		ServiceAccess:         append([]string(nil), authority.serviceAccess...),
		GitSSHKey:             authority.gitSSHKey,
		Snapshot: sessioncontract.Snapshot{
			Enabled:  !authority.skipSnapshot,
			Excludes: append([]string(nil), authority.snapshotExcludes...),
		},
		SessionHome:  authority.contractSessionHome(),
		SessionNotes: append([]string(nil), authority.sessionNotes...),
	}
}

func (authority sessionPlanAuthority) BackendInput(facts hostfacts.HostFacts) sessionbackend.Input {
	request := authority.request.Normalized()
	return sessionbackend.Input{
		Target:             request.Target,
		Mode:               authority.mode,
		ProjectDir:         request.ProjectDir,
		ReadOnlyDirs:       request.ReadOnlyDirs,
		ReadWriteDirs:      request.ReadWriteExtensions,
		NetworkMode:        request.NetworkMode,
		Integrations:       sessionIntegrationNameStrings(authority.activeIntegrations),
		IntegrationEnvKeys: authority.integrationEnvKeys(),
		HostFacts:          facts,
	}
}

func (authority sessionPlanAuthority) HarnessRequirements() []sessionplanner.HarnessRequirement {
	if authority.harnessID == "" {
		return nil
	}
	return []sessionplanner.HarnessRequirement{{
		ID:     string(authority.harnessID),
		Reason: "session target harness",
	}}
}

func (authority sessionPlanAuthority) integrationEnvMap() map[string]string {
	if len(authority.integrationEnv) == 0 {
		return nil
	}
	out := make(map[string]string, len(authority.integrationEnv))
	for key, value := range authority.integrationEnv {
		out[string(key)] = value
	}
	return out
}

func (authority sessionPlanAuthority) integrationEnvKeys() []string {
	if len(authority.integrationEnv) == 0 {
		return nil
	}
	keys := make([]string, 0, len(authority.integrationEnv))
	for key := range authority.integrationEnv {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	return keys
}

func (authority sessionPlanAuthority) contractCredentialEnvGrants() []sessioncontract.CredentialEnvGrant {
	if len(authority.credentialEnvGrants) == 0 {
		return nil
	}
	out := make([]sessioncontract.CredentialEnvGrant, len(authority.credentialEnvGrants))
	for i, grant := range authority.credentialEnvGrants {
		out[i] = sessioncontract.CredentialEnvGrant{
			EnvVar:          string(grant.envVar),
			CredentialID:    string(grant.credentialID),
			Source:          grant.source,
			ConsumerHarness: string(grant.consumerHarness),
			Redacted:        true,
		}
	}
	return out
}

func (authority sessionPlanAuthority) contractSessionHome() *sessioncontract.SessionHome {
	if authority.sessionHome == nil {
		return nil
	}
	launch := authority.sessionHome.Launch
	policy := authority.sessionHome.AgentHomePolicy
	phases := make([]string, len(launch.Phases))
	for i, phase := range launch.Phases {
		phases[i] = string(phase)
	}
	return &sessioncontract.SessionHome{
		Enabled:            true,
		Status:             "experimental-preview",
		Mode:               string(policy.Mode),
		Home:               launch.Layout.Home,
		PersistentHome:     policy.PersistentPath,
		CleanupRoot:        launch.Cleanup.Root,
		CleanupMaxAge:      launch.Cleanup.MaxAge.String(),
		Phases:             phases,
		ResumeRequested:    launch.ResumeRequested,
		DurableBridgeRoots: append([]string(nil), policy.DurableBridgeRoots...),
	}
}

func copySessionHomeRuntimePlan(value *sessionHomeRuntimePlan) *sessionHomeRuntimePlan {
	if value == nil {
		return nil
	}
	out := *value
	out.Launch.Assembly = append([]sessionHomeAssemblyEntry(nil), value.Launch.Assembly...)
	out.Launch.BridgeRequirements = append([]sessionHomeBridgeRequirement(nil), value.Launch.BridgeRequirements...)
	out.Launch.Phases = append([]sessionHomeLaunchPhase(nil), value.Launch.Phases...)
	out.Launch.Blockers = append([]sessionHomeLaunchBlocker(nil), value.Launch.Blockers...)
	out.AgentHomePolicy.DurableBridgeRoots = append([]string(nil), value.AgentHomePolicy.DurableBridgeRoots...)
	return &out
}

func sessionIntegrationNames(values []string) []integrations.Name {
	if len(values) == 0 {
		return nil
	}
	out := make([]integrations.Name, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		out = append(out, integrations.Name(name))
	}
	return out
}

func sessionIntegrationNameStrings(values []integrations.Name) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}

func sessionIntegrationEnv(values map[string]string) map[integrations.EnvKey]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[integrations.EnvKey]string, len(values))
	for key, value := range values {
		envKey := strings.ToUpper(strings.TrimSpace(key))
		if envKey == "" {
			continue
		}
		out[integrations.EnvKey(envKey)] = value
	}
	return out
}

func sessionEnvKeys(values []string) []integrations.EnvKey {
	if len(values) == 0 {
		return nil
	}
	out := make([]integrations.EnvKey, 0, len(values))
	for _, value := range values {
		key := strings.ToUpper(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		out = append(out, integrations.EnvKey(key))
	}
	return out
}

func sessionEnvKeyStrings(values []integrations.EnvKey) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}

func sessionCredentialGrantAuthorities(values []sessionCredentialEnvGrant) []sessionCredentialGrantAuthority {
	normalized := normalizedSessionCredentialEnvGrants(values)
	if len(normalized) == 0 {
		return nil
	}
	out := make([]sessionCredentialGrantAuthority, 0, len(normalized))
	for _, value := range normalized {
		envKey := strings.ToUpper(strings.TrimSpace(value.EnvVar))
		if envKey == "" {
			continue
		}
		out = append(out, sessionCredentialGrantAuthority{
			envVar:          integrations.EnvKey(envKey),
			credentialID:    credentials.ID(value.CredentialID),
			source:          value.Source,
			consumerHarness: harnesses.ID(value.ConsumerHarness),
		})
	}
	return out
}

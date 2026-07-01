// Package sessionlaunch defines Hazmat's reusable session preparation facade.
//
// The package is data-only: it does not know about Cobra commands, protocol
// frontends, harness config files, MCP, HTTP, OpenAI, or Muginn.
package sessionlaunch

import (
	"context"
	"sync"

	"hazmat/sessionbackend"
	"hazmat/sessioncontract"
	"hazmat/sessionmeta"
	"hazmat/sessionplanner"
)

// Launcher prepares contained Hazmat sessions for CLI commands and protocol
// frontends.
type Launcher interface {
	Prepare(context.Context, LaunchRequest) (PreparedSession, error)
}

// LaunchRequest is the protocol-neutral input for preparing a session.
type LaunchRequest struct {
	Target       string
	ProjectDir   string
	ReadOnly     []string
	ReadWrite    []string
	Integrations []string
	NetworkMode  sessionmeta.NetworkMode
	Options      LaunchOptions
}

// LaunchOptions holds transitional launch-preparation controls while the
// facade still wraps the existing root-package resolver.
type LaunchOptions struct {
	SupportsSandbox              bool
	PlanOnly                     bool
	SkipAutoIntegrations         bool
	SkipIntegrationHints         bool
	SkipRepoSetupDiscovery       bool
	SkipGitSafeDirectoryPlanning bool
	SkipAmbientAccessGrants      bool
	SkipGitHTTPSRuntime          bool
	SkipGoModCacheEnv            bool
	SkipProjectHooks             bool
	SkipDockerDetection          bool
	SkipHarnessAssetsSync        bool
	SkipSnapshot                 bool
	GitHub                       bool
	UseSandbox                   bool
	AllowDocker                  bool
	DockerMode                   string
	DockerModeExplicit           bool
	NetworkModeExplicit          bool
	MetadataJSON                 bool
	AuditInstall                 bool
	RuntimeProvider              string
	RuntimeProviderExplicit      bool
	InteractiveRepoSetup         bool
	PersistRepoSetup             bool
}

// Normalized returns a defensive-copy request with defaults materialized.
func (r LaunchRequest) Normalized() LaunchRequest {
	r.ReadOnly = copyStrings(r.ReadOnly)
	r.ReadWrite = copyStrings(r.ReadWrite)
	r.Integrations = copyStrings(r.Integrations)
	r.NetworkMode = sessionmeta.NormalizeNetworkMode(r.NetworkMode)
	if r.Options.DockerMode == "" {
		r.Options.DockerMode = "none"
	}
	return r
}

// ReadOnlyDirs returns a defensive copy of the requested read-only grants.
func (r LaunchRequest) ReadOnlyDirs() []string {
	return copyStrings(r.ReadOnly)
}

// ReadWriteDirs returns a defensive copy of the requested read-write grants.
func (r LaunchRequest) ReadWriteDirs() []string {
	return copyStrings(r.ReadWrite)
}

// IntegrationNames returns a defensive copy of explicit integration names.
func (r LaunchRequest) IntegrationNames() []string {
	return copyStrings(r.Integrations)
}

// PreparedSessionInput constructs a PreparedSession without exposing mutable
// backing slices to callers.
type PreparedSessionInput struct {
	Request     LaunchRequest
	Plan        sessionplanner.Plan
	BackendPlan sessionbackend.Plan
	Mode        sessionmeta.Mode
	RuntimeEnv  []string
	RuntimeDir  string
	Cleanup     func()
}

// PreparedSession is the redaction-safe planning result plus cleanup handle
// returned by Launcher.
type PreparedSession struct {
	request     LaunchRequest
	plan        sessionplanner.Plan
	backendPlan sessionbackend.Plan
	mode        sessionmeta.Mode
	runtimeEnv  []string
	runtimeDir  string
	cleanupOnce sync.Once
	cleanup     func()
}

// NewPreparedSession builds a defensive-copy prepared session value.
func NewPreparedSession(input PreparedSessionInput) PreparedSession {
	cleanup := input.Cleanup
	if cleanup == nil {
		cleanup = func() {}
	}
	return PreparedSession{
		request:     input.Request.Normalized(),
		plan:        copyPlannerPlan(input.Plan),
		backendPlan: copyBackendPlan(input.BackendPlan),
		mode:        input.Mode,
		runtimeEnv:  copyStrings(input.RuntimeEnv),
		runtimeDir:  input.RuntimeDir,
		cleanup:     cleanup,
	}
}

// Request returns the normalized launch request.
func (p *PreparedSession) Request() LaunchRequest {
	return p.request.Normalized()
}

// Plan returns the redaction-safe composed planner DTO.
func (p *PreparedSession) Plan() sessionplanner.Plan {
	return copyPlannerPlan(p.plan)
}

// BackendPlan returns the prepared backend plan.
func (p *PreparedSession) BackendPlan() sessionbackend.Plan {
	return copyBackendPlan(p.backendPlan)
}

// Mode returns the effective session mode.
func (p *PreparedSession) Mode() sessionmeta.Mode {
	return p.mode
}

// Backend returns the selected backend kind.
func (p *PreparedSession) Backend() sessionbackend.Kind {
	return p.backendPlan.Backend
}

// RuntimeEnv returns runtime env pairs prepared for process launch.
func (p *PreparedSession) RuntimeEnv() []string {
	return copyStrings(p.runtimeEnv)
}

// RuntimeDir returns the session runtime directory, when one has been prepared.
func (p *PreparedSession) RuntimeDir() string {
	return p.runtimeDir
}

// Cleanup releases prepared runtime artifacts. It is idempotent.
func (p *PreparedSession) Cleanup() {
	p.cleanupOnce.Do(p.cleanup)
}

func copyPlannerPlan(plan sessionplanner.Plan) sessionplanner.Plan {
	plan.Contract = copyContractPlan(plan.Contract)
	plan.Backend = copyBackendPlan(plan.Backend)
	plan.HostMutations = copyHostMutations(plan.HostMutations)
	plan.CredentialEnvGrants = copyCredentialEnvGrants(plan.CredentialEnvGrants)
	plan.HarnessRequirements = copyHarnessRequirements(plan.HarnessRequirements)
	plan.Warnings = copyWarnings(plan.Warnings)
	return plan
}

func copyContractPlan(plan sessioncontract.Plan) sessioncontract.Plan {
	plan.SuggestedIntegrations = copyStrings(plan.SuggestedIntegrations)
	plan.RepoSetupApplied = copyRepoSetupEffects(plan.RepoSetupApplied)
	plan.RepoSetupPending = copyRepoSetupEffects(plan.RepoSetupPending)
	plan.ActiveIntegrations = copyStrings(plan.ActiveIntegrations)
	plan.IntegrationSources = copyStrings(plan.IntegrationSources)
	plan.IntegrationDetails = copyStrings(plan.IntegrationDetails)
	plan.IntegrationWarnings = copyStrings(plan.IntegrationWarnings)
	plan.IntegrationEnvKeys = copyStrings(plan.IntegrationEnvKeys)
	plan.RegistryEnvKeys = copyStrings(plan.RegistryEnvKeys)
	plan.CredentialEnvGrants = copyCredentialEnvGrants(plan.CredentialEnvGrants)
	plan.PlannedHostMutations = copyHostMutations(plan.PlannedHostMutations)
	plan.ReadOnlyDirs = copyStrings(plan.ReadOnlyDirs)
	plan.AutoReadOnlyDirs = copyStrings(plan.AutoReadOnlyDirs)
	plan.UserReadOnlyDirs = copyStrings(plan.UserReadOnlyDirs)
	plan.ReadWriteExtensions = copyStrings(plan.ReadWriteExtensions)
	plan.NetworkPolicy.Denied = copyStrings(plan.NetworkPolicy.Denied)
	plan.ServiceAccess = copyStrings(plan.ServiceAccess)
	plan.Snapshot.Excludes = copyStrings(plan.Snapshot.Excludes)
	plan.SessionHome = copySessionHome(plan.SessionHome)
	plan.SessionNotes = copyStrings(plan.SessionNotes)
	return plan
}

func copyBackendPlan(plan sessionbackend.Plan) sessionbackend.Plan {
	plan.ReadOnlyDirs = copyStrings(plan.ReadOnlyDirs)
	plan.ReadWriteDirs = copyStrings(plan.ReadWriteDirs)
	plan.Integrations = copyStrings(plan.Integrations)
	plan.IntegrationEnvKeys = copyStrings(plan.IntegrationEnvKeys)
	plan.CapabilityGaps = copyCapabilityGaps(plan.CapabilityGaps)
	plan.LifecycleArtifacts = copyLifecycleArtifacts(plan.LifecycleArtifacts)
	return plan
}

func copyRepoSetupEffects(values []sessioncontract.RepoSetupEffect) []sessioncontract.RepoSetupEffect {
	if len(values) == 0 {
		return nil
	}
	out := make([]sessioncontract.RepoSetupEffect, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Sources = copyStrings(value.Sources)
	}
	return out
}

func copyCredentialEnvGrants(values []sessioncontract.CredentialEnvGrant) []sessioncontract.CredentialEnvGrant {
	if len(values) == 0 {
		return nil
	}
	out := make([]sessioncontract.CredentialEnvGrant, len(values))
	copy(out, values)
	return out
}

func copyHostMutations(values []sessioncontract.HostMutation) []sessioncontract.HostMutation {
	if len(values) == 0 {
		return nil
	}
	out := make([]sessioncontract.HostMutation, len(values))
	copy(out, values)
	return out
}

func copySessionHome(value *sessioncontract.SessionHome) *sessioncontract.SessionHome {
	if value == nil {
		return nil
	}
	out := *value
	out.ActivationBlockers = copySessionHomeBlockers(value.ActivationBlockers)
	out.Phases = copyStrings(value.Phases)
	out.DurableBridgeRoots = copyStrings(value.DurableBridgeRoots)
	return &out
}

func copySessionHomeBlockers(values []sessioncontract.SessionHomeBlocker) []sessioncontract.SessionHomeBlocker {
	if len(values) == 0 {
		return nil
	}
	out := make([]sessioncontract.SessionHomeBlocker, len(values))
	copy(out, values)
	return out
}

func copyHarnessRequirements(values []sessionplanner.HarnessRequirement) []sessionplanner.HarnessRequirement {
	if len(values) == 0 {
		return nil
	}
	out := make([]sessionplanner.HarnessRequirement, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Notes = copyStrings(value.Notes)
	}
	return out
}

func copyWarnings(values []sessionplanner.Warning) []sessionplanner.Warning {
	if len(values) == 0 {
		return nil
	}
	out := make([]sessionplanner.Warning, len(values))
	copy(out, values)
	return out
}

func copyCapabilityGaps(values []sessionbackend.CapabilityGap) []sessionbackend.CapabilityGap {
	if len(values) == 0 {
		return nil
	}
	out := make([]sessionbackend.CapabilityGap, len(values))
	copy(out, values)
	return out
}

func copyLifecycleArtifacts(values []sessionbackend.LifecycleArtifact) []sessionbackend.LifecycleArtifact {
	if len(values) == 0 {
		return nil
	}
	out := make([]sessionbackend.LifecycleArtifact, len(values))
	copy(out, values)
	return out
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

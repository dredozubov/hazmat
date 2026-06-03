package harnessruntime

import (
	"fmt"
	"strings"

	"hazmat/harnesses"
)

type UninstallPlan struct {
	Spec            harnesses.Spec
	Artifacts       []ArtifactStatus
	Preserved       []string
	MetadataPresent bool
	StateErr        error
}

type UninstallOptions struct {
	Store     StateStore
	Remove    ArtifactRemover
	AgentHome string
	Force     bool
	DryRun    bool
}

func BuildUninstallPlan(store StateStore, read CommandReader, agentHome string, spec harnesses.Spec, artifacts []Artifact, preserved []string) UninstallPlan {
	statuses := make([]ArtifactStatus, 0, len(artifacts))
	for _, artifact := range artifacts {
		statuses = append(statuses, InspectArtifact(read, agentHome, artifact))
	}

	state, stateErr := store.Load()
	metadataPresent := false
	if stateErr == nil && state.Harnesses != nil {
		_, metadataPresent = state.Harnesses[spec.ID]
	}
	return UninstallPlan{
		Spec:            spec,
		Artifacts:       statuses,
		Preserved:       append([]string(nil), preserved...),
		MetadataPresent: metadataPresent,
		StateErr:        stateErr,
	}
}

func (p UninstallPlan) HasWork() bool {
	if p.MetadataPresent {
		return true
	}
	for _, artifact := range p.Artifacts {
		if artifact.Exists {
			return true
		}
	}
	return false
}

func (p UninstallPlan) Drift() []string {
	var drift []string
	for _, artifact := range p.Artifacts {
		if artifact.Drift != "" {
			drift = append(drift, fmt.Sprintf("%s: %s", artifact.Artifact.Path, artifact.Drift))
		}
	}
	return drift
}

func ExecuteUninstallPlan(plan UninstallPlan, opts UninstallOptions) error {
	if plan.StateErr != nil {
		return plan.StateErr
	}
	if drift := plan.Drift(); len(drift) > 0 && !opts.Force {
		return fmt.Errorf("refusing to uninstall %s because planned artifacts drifted: %s\nre-run with --force to remove the exact planned paths", plan.Spec.ID, strings.Join(drift, "; "))
	}

	for _, artifact := range plan.Artifacts {
		if !artifact.Exists {
			continue
		}
		if artifact.Drift != "" && !opts.Force {
			continue
		}
		if err := RemoveArtifact(opts.Remove, opts.AgentHome, artifact.Artifact); err != nil {
			return err
		}
	}
	if plan.MetadataPresent && !opts.DryRun {
		if err := RemoveHarnessState(opts.Store, plan.Spec.ID); err != nil {
			return fmt.Errorf("remove %s harness metadata: %w", plan.Spec.ID, err)
		}
	}
	return nil
}

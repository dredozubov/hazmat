// Package sessionplanner composes Hazmat's side-effect-free session planning
// artifacts. It does not inspect the host, mutate state, render backend
// policies, or launch processes.
package sessionplanner

import (
	"hazmat/sessionbackend"
	"hazmat/sessioncontract"
)

// PlanFormatVersion is the current JSON contract version for composed planner
// DTOs.
const PlanFormatVersion = 1

// Input contains the already-resolved inputs for every pure session plan
// artifact produced before backend execution.
type Input struct {
	Contract            sessioncontract.PlanInput
	Backend             sessionbackend.Input
	HarnessRequirements []HarnessRequirement
	Warnings            []Warning
}

// Plan is the side-effect-free planning result shared by preview/explain and
// launch preparation paths.
type Plan struct {
	FormatVersion       int                                  `json:"format_version"`
	Contract            sessioncontract.Plan                 `json:"contract"`
	Backend             sessionbackend.Plan                  `json:"backend"`
	HostMutations       []sessioncontract.HostMutation       `json:"host_mutations,omitempty"`
	CredentialEnvGrants []sessioncontract.CredentialEnvGrant `json:"credential_env_grants,omitempty"`
	HarnessRequirements []HarnessRequirement                 `json:"harness_requirements,omitempty"`
	Warnings            []Warning                            `json:"warnings,omitempty"`
}

// HarnessRequirement is a redaction-safe harness lifecycle requirement that a
// runtime or frontend may need to satisfy before launch. It is data only:
// install/update/uninstall effects stay outside the planner.
type HarnessRequirement struct {
	ID     string   `json:"id"`
	Reason string   `json:"reason,omitempty"`
	Notes  []string `json:"notes,omitempty"`
}

// Warning is a canonical planner warning from integration descriptors, backend
// capability gaps, or explicit frontend-supplied planning context.
type Warning struct {
	Source  string `json:"source"`
	Feature string `json:"feature,omitempty"`
	Message string `json:"message"`
}

// Build constructs every pure planning artifact from resolved inputs.
func Build(input Input) Plan {
	contract := BuildContractPlan(input.Contract)
	backend := BuildBackendPlan(input.Backend)
	return Plan{
		FormatVersion:       PlanFormatVersion,
		Contract:            contract,
		Backend:             backend,
		HostMutations:       copyHostMutations(contract.PlannedHostMutations),
		CredentialEnvGrants: copyCredentialEnvGrants(contract.CredentialEnvGrants),
		HarnessRequirements: copyHarnessRequirements(input.HarnessRequirements),
		Warnings:            collectWarnings(contract, backend, input.Warnings),
	}
}

// BuildContractPlan constructs the preview/explain session contract plan.
func BuildContractPlan(input sessioncontract.PlanInput) sessioncontract.Plan {
	return sessioncontract.BuildPlan(input)
}

// BuildBackendPlan constructs the backend selection/capability plan.
func BuildBackendPlan(input sessionbackend.Input) sessionbackend.Plan {
	return sessionbackend.BuildPlan(input)
}

func collectWarnings(contract sessioncontract.Plan, backend sessionbackend.Plan, explicit []Warning) []Warning {
	total := len(contract.IntegrationWarnings) + len(backend.CapabilityGaps) + len(explicit)
	if total == 0 {
		return nil
	}
	warnings := make([]Warning, 0, total)
	for _, warning := range contract.IntegrationWarnings {
		warnings = append(warnings, Warning{Source: "integration", Message: warning})
	}
	for _, gap := range backend.CapabilityGaps {
		warnings = append(warnings, Warning{
			Source:  "backend",
			Feature: gap.Feature,
			Message: gap.Reason,
		})
	}
	warnings = append(warnings, copyWarnings(explicit)...)
	return warnings
}

func copyHostMutations(values []sessioncontract.HostMutation) []sessioncontract.HostMutation {
	if len(values) == 0 {
		return nil
	}
	out := make([]sessioncontract.HostMutation, len(values))
	copy(out, values)
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

func copyHarnessRequirements(values []HarnessRequirement) []HarnessRequirement {
	if len(values) == 0 {
		return nil
	}
	out := make([]HarnessRequirement, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Notes = append([]string(nil), value.Notes...)
	}
	return out
}

func copyWarnings(values []Warning) []Warning {
	if len(values) == 0 {
		return nil
	}
	out := make([]Warning, len(values))
	copy(out, values)
	return out
}

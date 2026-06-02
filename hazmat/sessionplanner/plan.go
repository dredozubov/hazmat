// Package sessionplanner composes Hazmat's side-effect-free session planning
// artifacts. It does not inspect the host, mutate state, render backend
// policies, or launch processes.
package sessionplanner

import (
	"hazmat/sessionbackend"
	"hazmat/sessioncontract"
)

// Input contains the already-resolved inputs for every pure session plan
// artifact produced before backend execution.
type Input struct {
	Contract sessioncontract.PlanInput
	Backend  sessionbackend.Input
}

// Plan is the side-effect-free planning result shared by preview/explain and
// launch preparation paths.
type Plan struct {
	Contract sessioncontract.Plan
	Backend  sessionbackend.Plan
}

// Build constructs every pure planning artifact from resolved inputs.
func Build(input Input) Plan {
	return Plan{
		Contract: BuildContractPlan(input.Contract),
		Backend:  BuildBackendPlan(input.Backend),
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

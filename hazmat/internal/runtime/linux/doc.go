// Package linux contains the non-effectful Linux native admission planner. It
// stays plan-only until MC_LinuxNativeLaunch is wired to a concrete helper.
package linux

const (
	PackagePath = "hazmat/internal/runtime/linux"
	PlanOnly    = true
)

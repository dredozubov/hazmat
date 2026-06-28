// Package linux contains Linux native admission planning plus the experimental
// current-user runner contract. The main session pipeline remains plan-only
// until VM smoke evidence promotes the current-user lane.
package linux

const (
	PackagePath          = "hazmat/internal/runtime/linux"
	SessionPipelineWired = false
)

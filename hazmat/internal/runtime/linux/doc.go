// Package linux contains Linux native admission planning plus the experimental
// current-user runner contract. The kernel enforcer remains fail-closed until
// MC_LinuxNativeLaunch is wired to concrete Linux syscalls.
package linux

const (
	PackagePath         = "hazmat/internal/runtime/linux"
	KernelEnforcerWired = false
)

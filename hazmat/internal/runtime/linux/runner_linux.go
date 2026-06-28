//go:build linux

package linux

import (
	"context"
	"fmt"

	linuxspec "hazmat/containment/linux"
)

func HostCurrentUserEnforcer() CurrentUserEnforcer {
	return hostCurrentUserEnforcer{}
}

type hostCurrentUserEnforcer struct{}

func (hostCurrentUserEnforcer) CloseInheritedFDs(context.Context, FDClosurePlan) error {
	return errKernelEnforcerPending()
}

func (hostCurrentUserEnforcer) CreateNamespaces(context.Context, NamespacePlan) error {
	return errKernelEnforcerPending()
}

func (hostCurrentUserEnforcer) ApplyMounts(context.Context, linuxspec.LaunchSpec) error {
	return errKernelEnforcerPending()
}

func (hostCurrentUserEnforcer) ConfigureNetwork(context.Context, NetworkAdmission) error {
	return errKernelEnforcerPending()
}

func (hostCurrentUserEnforcer) DropPrivileges(context.Context, linuxspec.ProcessSpec) error {
	return errKernelEnforcerPending()
}

func (hostCurrentUserEnforcer) ApplyLandlock(context.Context, PolicyPlan) error {
	return errKernelEnforcerPending()
}

func (hostCurrentUserEnforcer) ApplySeccomp(context.Context, PolicyPlan) error {
	return errKernelEnforcerPending()
}

func (hostCurrentUserEnforcer) Exec(context.Context, linuxspec.LaunchSpec, RunOptions) (ExecResult, error) {
	return ExecResult{}, errKernelEnforcerPending()
}

func errKernelEnforcerPending() error {
	return fmt.Errorf("linux current-user kernel enforcer is not wired")
}

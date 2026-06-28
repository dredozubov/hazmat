//go:build !linux

package linux

import (
	"context"
	"fmt"

	linuxspec "hazmat/containment/linux"
)

func HostCurrentUserEnforcer() CurrentUserEnforcer {
	return unsupportedCurrentUserEnforcer{}
}

func NewCommandAgentUserRootHelper(path string) (AgentUserRootHelper, error) {
	if err := validateAgentUserRootHelperPath(path); err != nil {
		return nil, err
	}
	return nil, errLinuxRunnerUnsupported()
}

type unsupportedCurrentUserEnforcer struct{}

func (unsupportedCurrentUserEnforcer) CloseInheritedFDs(context.Context, FDClosurePlan) error {
	return errLinuxRunnerUnsupported()
}

func (unsupportedCurrentUserEnforcer) CreateNamespaces(context.Context, NamespacePlan) error {
	return errLinuxRunnerUnsupported()
}

func (unsupportedCurrentUserEnforcer) ApplyMounts(context.Context, linuxspec.LaunchSpec) error {
	return errLinuxRunnerUnsupported()
}

func (unsupportedCurrentUserEnforcer) ConfigureNetwork(context.Context, NetworkAdmission) error {
	return errLinuxRunnerUnsupported()
}

func (unsupportedCurrentUserEnforcer) DropPrivileges(context.Context, linuxspec.ProcessSpec) error {
	return errLinuxRunnerUnsupported()
}

func (unsupportedCurrentUserEnforcer) ApplyLandlock(context.Context, PolicyPlan) error {
	return errLinuxRunnerUnsupported()
}

func (unsupportedCurrentUserEnforcer) ApplySeccomp(context.Context, PolicyPlan) error {
	return errLinuxRunnerUnsupported()
}

func (unsupportedCurrentUserEnforcer) Exec(context.Context, linuxspec.LaunchSpec, RunOptions) (ExecResult, error) {
	return ExecResult{}, errLinuxRunnerUnsupported()
}

func errLinuxRunnerUnsupported() error {
	return fmt.Errorf("linux current-user enforcer is only available on Linux")
}

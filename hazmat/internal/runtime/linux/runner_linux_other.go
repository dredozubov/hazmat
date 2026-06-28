//go:build linux && !amd64 && !arm64

package linux

import (
	"context"
	"fmt"

	linuxspec "hazmat/containment/linux"
)

func HostCurrentUserEnforcer() CurrentUserEnforcer {
	return unsupportedLinuxArchEnforcer{}
}

func NewCommandAgentUserRootHelper(path string) (AgentUserRootHelper, error) {
	if err := validateAgentUserRootHelperPath(path); err != nil {
		return nil, err
	}
	return nil, errUnsupportedLinuxArch()
}

type unsupportedLinuxArchEnforcer struct{}

func (unsupportedLinuxArchEnforcer) CloseInheritedFDs(context.Context, FDClosurePlan) error {
	return errUnsupportedLinuxArch()
}

func (unsupportedLinuxArchEnforcer) CreateNamespaces(context.Context, NamespacePlan) error {
	return errUnsupportedLinuxArch()
}

func (unsupportedLinuxArchEnforcer) ApplyMounts(context.Context, linuxspec.LaunchSpec) error {
	return errUnsupportedLinuxArch()
}

func (unsupportedLinuxArchEnforcer) ConfigureNetwork(context.Context, NetworkAdmission) error {
	return errUnsupportedLinuxArch()
}

func (unsupportedLinuxArchEnforcer) DropPrivileges(context.Context, linuxspec.ProcessSpec) error {
	return errUnsupportedLinuxArch()
}

func (unsupportedLinuxArchEnforcer) ApplyLandlock(context.Context, PolicyPlan) error {
	return errUnsupportedLinuxArch()
}

func (unsupportedLinuxArchEnforcer) ApplySeccomp(context.Context, PolicyPlan) error {
	return errUnsupportedLinuxArch()
}

func (unsupportedLinuxArchEnforcer) Exec(context.Context, linuxspec.LaunchSpec, RunOptions) (ExecResult, error) {
	return ExecResult{}, errUnsupportedLinuxArch()
}

func errUnsupportedLinuxArch() error {
	return fmt.Errorf("linux current-user kernel enforcer is wired only for linux/amd64 and linux/arm64")
}

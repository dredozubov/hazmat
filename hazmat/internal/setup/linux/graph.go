package linux

import (
	"fmt"

	"hazmat/internal/setup"
)

type Callback func() error

type Callbacks struct {
	DistroProfile   Callback
	AgentUser       Callback
	SharedGroup     Callback
	AgentHome       Callback
	WorkspaceAccess Callback
	ToolHome        Callback
	FirewallPolicy  Callback
	ResolverPolicy  Callback
	CgroupRoot      Callback
	LaunchHelper    Callback
	Sudoers         Callback
}

type RollbackOptions struct {
	DeleteToolHome    bool
	DeleteAgentHome   bool
	DeleteAgentUser   bool
	DeleteSharedGroup bool
	Warn              func(string)
}

type Step struct {
	Name        string
	Resource    setup.Resource
	Destructive bool
	Run         Callback
}

type DryRunRecord struct {
	Name        string
	Resource    setup.Resource
	Destructive bool
	Skipped     bool
}

func SetupSteps(callbacks Callbacks) []Step {
	return []Step{
		{Name: "linuxSetupDistroProfile", Resource: setup.ResourceLinuxDistroProfile, Run: callbacks.DistroProfile},
		{Name: "linuxSetupAgentUser", Resource: setup.ResourceLinuxAgentUser, Run: callbacks.AgentUser},
		{Name: "linuxSetupSharedGroup", Resource: setup.ResourceLinuxSharedGroup, Run: callbacks.SharedGroup},
		{Name: "linuxSetupAgentHome", Resource: setup.ResourceLinuxAgentHome, Run: callbacks.AgentHome},
		{Name: "linuxSetupWorkspaceAccess", Resource: setup.ResourceLinuxWorkspaceAccess, Run: callbacks.WorkspaceAccess},
		{Name: "linuxSetupToolHome", Resource: setup.ResourceLinuxToolHome, Run: callbacks.ToolHome},
		{Name: "linuxSetupFirewallPolicy", Resource: setup.ResourceLinuxFirewallPolicy, Run: callbacks.FirewallPolicy},
		{Name: "linuxSetupResolverPolicy", Resource: setup.ResourceLinuxResolverPolicy, Run: callbacks.ResolverPolicy},
		{Name: "linuxSetupCgroupRoot", Resource: setup.ResourceLinuxCgroupRoot, Run: callbacks.CgroupRoot},
		{Name: "linuxSetupLaunchHelper", Resource: setup.ResourceLinuxLaunchHelper, Run: callbacks.LaunchHelper},
		{Name: "linuxSetupSudoers", Resource: setup.ResourceLinuxSudoers, Run: callbacks.Sudoers},
	}
}

func RollbackSteps(callbacks Callbacks, options RollbackOptions) []Step {
	return []Step{
		{Name: "linuxRollbackSudoers", Resource: setup.ResourceLinuxSudoers, Run: callbacks.Sudoers},
		{Name: "linuxRollbackLaunchHelper", Resource: setup.ResourceLinuxLaunchHelper, Run: callbacks.LaunchHelper},
		{Name: "linuxRollbackCgroupRoot", Resource: setup.ResourceLinuxCgroupRoot, Run: callbacks.CgroupRoot},
		{Name: "linuxRollbackFirewallPolicy", Resource: setup.ResourceLinuxFirewallPolicy, Run: callbacks.FirewallPolicy},
		{Name: "linuxRollbackResolverPolicy", Resource: setup.ResourceLinuxResolverPolicy, Run: callbacks.ResolverPolicy},
		{Name: "linuxRollbackWorkspaceAccess", Resource: setup.ResourceLinuxWorkspaceAccess, Run: callbacks.WorkspaceAccess},
		destructiveStep("linuxRollbackToolHome", setup.ResourceLinuxToolHome, callbacks.ToolHome, options.DeleteToolHome, options.Warn),
		destructiveStep("linuxRollbackAgentHome", setup.ResourceLinuxAgentHome, callbacks.AgentHome, options.DeleteAgentHome, options.Warn),
		destructiveStep("linuxRollbackAgentUser", setup.ResourceLinuxAgentUser, callbacks.AgentUser, options.DeleteAgentUser, options.Warn),
		destructiveStep("linuxRollbackSharedGroup", setup.ResourceLinuxSharedGroup, callbacks.SharedGroup, options.DeleteSharedGroup, options.Warn),
	}
}

func RunSetupSteps(callbacks Callbacks) error {
	for _, step := range SetupSteps(callbacks) {
		if err := runStep(step); err != nil {
			return err
		}
	}
	return nil
}

func RunRollbackSteps(callbacks Callbacks, options RollbackOptions) error {
	for _, step := range RollbackSteps(callbacks, options) {
		if err := runStep(step); err != nil {
			return err
		}
	}
	return nil
}

func DryRunSetup(callbacks Callbacks) []DryRunRecord {
	return dryRunRecords(SetupSteps(callbacks))
}

func DryRunRollback(callbacks Callbacks, options RollbackOptions) []DryRunRecord {
	return dryRunRecords(RollbackSteps(callbacks, options))
}

func destructiveStep(name string, resource setup.Resource, callback Callback, enabled bool, warn func(string)) Step {
	return Step{
		Name:        name,
		Resource:    resource,
		Destructive: true,
		Run: func() error {
			if enabled {
				return runConfigured(name, callback)
			}
			if warn != nil {
				warn(fmt.Sprintf("%s preserved; pass the explicit destructive rollback flag to remove it", resource))
			}
			return nil
		},
	}
}

func runStep(step Step) error {
	return runConfigured(step.Name, step.Run)
}

func runConfigured(name string, callback Callback) error {
	if callback == nil {
		return fmt.Errorf("linux setup step %s is not configured", name)
	}
	return callback()
}

func dryRunRecords(steps []Step) []DryRunRecord {
	records := make([]DryRunRecord, 0, len(steps))
	for _, step := range steps {
		records = append(records, DryRunRecord{
			Name:        step.Name,
			Resource:    step.Resource,
			Destructive: step.Destructive,
			Skipped:     true,
		})
	}
	return records
}

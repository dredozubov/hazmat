package setup

import "fmt"

type HomeTraverseEnv struct {
	HomeDir                string
	AllowsAgentTraverse    func(string) bool
	HasAgentTraverseACL    func(string) bool
	EnsureAgentTraverseACL func(string) error
	RemoveAgentTraverseACL func(string) error
}

func SetupHomeDirTraverse(env HomeTraverseEnv, ui StepStatusUI) error {
	ui.Step("Allow agent to traverse home directory")

	homeDir := env.HomeDir
	if env.allowsAgentTraverse(homeDir) {
		if env.hasAgentTraverseACL(homeDir) {
			ui.SkipDone("Home directory ACL already allows agent traversal")
		} else {
			ui.SkipDone("Home directory permissions already allow agent traversal")
		}
		return nil
	}

	if err := env.ensureAgentTraverseACL(homeDir); err != nil {
		return fmt.Errorf("set home traversal ACL: %w", err)
	}
	ui.Ok("Home directory ACL set — agent can reach project directories")
	return nil
}

func RollbackHomeDirTraverse(env HomeTraverseEnv, ui StepStatusUI) {
	ui.Step("Remove home directory traverse ACL")

	homeDir := env.HomeDir
	if env.hasAgentTraverseACL(homeDir) {
		if err := env.removeAgentTraverseACL(homeDir); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove home traversal ACL: %v", err))
		} else {
			ui.Ok("Removed home traversal ACL")
		}
	} else {
		ui.SkipDone("Home traversal ACL not present")
	}
}

func (env HomeTraverseEnv) allowsAgentTraverse(path string) bool {
	return env.AllowsAgentTraverse != nil && env.AllowsAgentTraverse(path)
}

func (env HomeTraverseEnv) hasAgentTraverseACL(path string) bool {
	return env.HasAgentTraverseACL != nil && env.HasAgentTraverseACL(path)
}

func (env HomeTraverseEnv) ensureAgentTraverseACL(path string) error {
	if env.EnsureAgentTraverseACL == nil {
		return fmt.Errorf("home traversal ACL ensure callback is not configured")
	}
	return env.EnsureAgentTraverseACL(path)
}

func (env HomeTraverseEnv) removeAgentTraverseACL(path string) error {
	if env.RemoveAgentTraverseACL == nil {
		return fmt.Errorf("home traversal ACL remove callback is not configured")
	}
	return env.RemoveAgentTraverseACL(path)
}

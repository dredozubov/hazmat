package hazmat

import (
	"os"
	"path/filepath"

	"hazmat/internal/setup"
)

// seatbeltWrapperContent is the Claude launch wrapper installed at
// seatbeltWrapperPath. Hazmat prepares it during init so the Claude harness can
// be added later without rewriting the base shell environment.
//
// It is aliased to `claude` inside agent-shell sessions. The outer
// sandbox-exec confinement applied by `hazmat shell/exec/claude` already
// covers the session, so this wrapper simply execs the claude binary directly.
const seatbeltWrapperContent = `#!/bin/bash
# claude-sandboxed — launch Claude Code inside the agent sandbox.
# Installed by hazmat init — do not edit manually.
#
# This wrapper is aliased to "claude" in the agent shell. It runs inside a
# The session is already confined by sandbox-exec (started via "hazmat shell"
# or "hazmat claude"), so no additional seatbelt policy is applied here.
set -euo pipefail

CLAUDE_BIN=/Users/agent/.local/bin/claude

if [[ ! -x "$CLAUDE_BIN" ]]; then
    printf 'error: claude binary not found: %s\n' "$CLAUDE_BIN" >&2
    exit 1
fi

exec "$CLAUDE_BIN" "$@"
`

func setupToolingEnv() setup.ToolingEnv {
	return setup.ToolingEnv{
		AgentUser:             agentUser,
		AgentHome:             agentHome,
		SeatbeltProfileDir:    seatbeltProfileDir,
		SeatbeltWrapperPath:   seatbeltWrapperPath,
		SeatbeltWrapper:       seatbeltWrapperContent,
		AgentEnvPath:          agentEnvPath,
		DefaultAgentPath:      defaultAgentPath,
		DefaultAgentCacheHome: defaultAgentCacheHome,
		DefaultAgentDataHome:  defaultAgentDataHome,
		HostWrapperDir:        hostWrapperDir(),
		HostClaudeWrapperName: hostClaudeWrapperName,
		HostExecWrapperName:   hostExecWrapperName,
		HostShellWrapperName:  hostShellWrapperName,
		AgentShellBlockStart:  agentShellBlockStart,
		AgentShellBlockEnd:    agentShellBlockEnd,
		UserPathBlockStart:    userPathBlockStart,
		UserPathBlockEnd:      userPathBlockEnd,
		UmaskBlockStart:       umaskBlockStart,
		UmaskBlockEnd:         umaskBlockEnd,
		ShellName:             filepath.Base(os.Getenv("SHELL")),
		ShellProfiles:         setupShellProfiles(),
	}
}

func setupHardeningEnv() setup.HardeningEnv {
	return setup.HardeningEnv{
		AgentUser:       agentUser,
		AgentHome:       agentHome,
		HostHome:        os.Getenv("HOME"),
		UmaskBlockStart: umaskBlockStart,
		UmaskBlockEnd:   umaskBlockEnd,
	}
}

func setupShellProfiles() []setup.ShellProfile {
	profiles := supportedUserShellProfiles()
	out := make([]setup.ShellProfile, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, setup.ShellProfile{
			Name:           profile.name,
			RCPath:         profile.rcPath,
			PathBlockLines: append([]string(nil), profile.pathBlockLines...),
		})
	}
	return out
}

func setupHardeningGaps(ui *UI, r *Runner) error {
	return setup.SetupHardeningGaps(setupHardeningEnv(), ui, r)
}

func setupHomeDirTraverse(ui *UI, r *Runner) error {
	inv := sudoACLInvoker{runner: r, reason: "allow agent to traverse home directory"}
	return setup.SetupHomeDirTraverse(setup.HomeTraverseEnv{
		HomeDir:             os.Getenv("HOME"),
		AllowsAgentTraverse: homeAllowsAgentTraverse,
		HasAgentTraverseACL: homeHasAgentTraverseACL,
		EnsureAgentTraverseACL: func(path string) error {
			return ensureACL(inv, path, agentTraverseGrant)
		},
	}, ui)
}

func setupSeatbelt(ui *UI, r *Runner) error {
	return setup.SetupSeatbelt(setupToolingEnv(), ui, r)
}

func setupUserExperience(ui *UI, r *Runner) error {
	return setup.SetupUserExperience(setupToolingEnv(), ui, r)
}

func rollbackSeatbelt(ui *UI, r *Runner) {
	setup.RollbackSeatbelt(setupToolingEnv(), ui, r)
}

func rollbackUserExperience(ui *UI, r *Runner) {
	setup.RollbackUserExperience(setupToolingEnv(), ui, r)
}

func rollbackHomeDirTraverse(ui *UI, r *Runner) {
	inv := sudoACLInvoker{runner: r, reason: "remove home directory traverse ACL"}
	setup.RollbackHomeDirTraverse(setup.HomeTraverseEnv{
		HomeDir:             os.Getenv("HOME"),
		HasAgentTraverseACL: homeHasAgentTraverseACL,
		RemoveAgentTraverseACL: func(path string) error {
			return removeACL(inv, path, agentTraverseGrant)
		},
	}, ui)
}

func rollbackUmask(ui *UI, r *Runner) {
	setup.RollbackUmask(setupToolingEnv(), ui, r)
}

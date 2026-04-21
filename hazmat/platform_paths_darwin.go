//go:build darwin

package main

// Native platform paths for the current macOS backend. Linux support should
// replace these through platform-specific files rather than branching at call
// sites throughout setup, rollback, session launch, and tests.
const (
	agentUser                   = "agent"
	agentHome                   = "/Users/agent"
	systemLaunchHelper          = "/usr/local/libexec/hazmat-launch"
	sharedGroup                 = "dev"
	pfAnchorName                = "agent"
	pfAnchorFile                = "/etc/pf.anchors/agent"
	pfDaemonLabel               = "com.local.pf-agent"
	pfDaemonPlist               = "/Library/LaunchDaemons/com.local.pf-agent.plist"
	sudoersFile                 = "/etc/sudoers.d/agent"
	agentMaintenanceSudoersFile = "/etc/sudoers.d/agent-maintenance"
	hostsMarker                 = "# === AI Agent Blocklist ==="

	seatbeltProfileDir  = agentHome + "/.config/hazmat"
	seatbeltWrapperPath = agentHome + "/.local/bin/claude-sandboxed"
	agentEnvPath        = seatbeltProfileDir + "/agent-env.zsh"

	defaultAgentPath       = agentHome + "/.opencode/bin:" + agentHome + "/.local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	defaultAgentCacheHome  = agentHome + "/.cache"
	defaultAgentConfigHome = agentHome + "/.config"
	defaultAgentDataHome   = agentHome + "/.local/share"
	defaultAgentTmpDir     = "/private/tmp"
)

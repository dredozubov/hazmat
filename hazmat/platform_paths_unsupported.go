//go:build !darwin

package main

// Unsupported-platform path defaults keep the package buildable while Linux
// support is still being built. Platform entry points still fail fast via
// checkPlatform() until these values are backed by real setup/rollback code.
const (
	agentUser                   = "agent"
	agentHome                   = "/home/agent"
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

	defaultAgentPath       = agentHome + "/.opencode/bin:" + agentHome + "/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"
	defaultAgentCacheHome  = agentHome + "/.cache"
	defaultAgentConfigHome = agentHome + "/.config"
	defaultAgentDataHome   = agentHome + "/.local/share"
	defaultAgentTmpDir     = "/tmp"
)

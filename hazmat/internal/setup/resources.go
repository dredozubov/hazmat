package setup

type Resource string

const (
	ResourceAgentUser          Resource = "agentUser"
	ResourceDevGroup           Resource = "devGroup"
	ResourceHomeDirTraverse    Resource = "homeDirTraverse"
	ResourceLocalRepo          Resource = "localRepo"
	ResourceHardeningGaps      Resource = "umask+hostCredentialModes"
	ResourceUmask              Resource = "umask"
	ResourceSeatbelt           Resource = "seatbelt"
	ResourceWrappers           Resource = "wrappers"
	ResourcePfAnchor           Resource = "pfAnchor"
	ResourceDNSBlocklist       Resource = "dnsBlocklist"
	ResourceLaunchDaemon       Resource = "launchDaemon"
	ResourceLaunchHelper       Resource = "launchHelper"
	ResourceSudoers            Resource = "sudoers"
	ResourceMaintenanceSudoers Resource = "maintenanceSudoers"
	ResourceClaudeCode         Resource = "claudeCode"
	ResourceCredentials        Resource = "credentials"

	ResourceLinuxDistroProfile   Resource = "linuxDistroProfile"
	ResourceLinuxAgentUser       Resource = "linuxAgentUser"
	ResourceLinuxSharedGroup     Resource = "linuxSharedGroup"
	ResourceLinuxAgentHome       Resource = "linuxAgentHome"
	ResourceLinuxWorkspaceAccess Resource = "linuxWorkspaceAccess"
	ResourceLinuxToolHome        Resource = "linuxToolHome"
	ResourceLinuxFirewallPolicy  Resource = "linuxFirewallPolicy"
	ResourceLinuxResolverPolicy  Resource = "linuxResolverPolicy"
	ResourceLinuxCgroupRoot      Resource = "linuxCgroupRoot"
	ResourceLinuxLaunchHelper    Resource = "linuxLaunchHelper"
	ResourceLinuxSudoers         Resource = "linuxSudoers"
)

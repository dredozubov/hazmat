package main

type setupRollbackTLAResource string

const (
	tlaResourceAgentUser          setupRollbackTLAResource = "agentUser"
	tlaResourceDevGroup           setupRollbackTLAResource = "devGroup"
	tlaResourceHomeDirTraverse    setupRollbackTLAResource = "homeDirTraverse"
	tlaResourceLocalRepo          setupRollbackTLAResource = "localRepo"
	tlaResourceUmask              setupRollbackTLAResource = "umask"
	tlaResourceSeatbelt           setupRollbackTLAResource = "seatbelt"
	tlaResourceWrappers           setupRollbackTLAResource = "wrappers"
	tlaResourcePfAnchor           setupRollbackTLAResource = "pfAnchor"
	tlaResourceDNSBlocklist       setupRollbackTLAResource = "dnsBlocklist"
	tlaResourceLaunchDaemon       setupRollbackTLAResource = "launchDaemon"
	tlaResourceLaunchHelper       setupRollbackTLAResource = "launchHelper"
	tlaResourceSudoers            setupRollbackTLAResource = "sudoers"
	tlaResourceMaintenanceSudoers setupRollbackTLAResource = "maintenanceSudoers"
	tlaResourceClaudeCode         setupRollbackTLAResource = "claudeCode"
	tlaResourceCredentials        setupRollbackTLAResource = "credentials"
)

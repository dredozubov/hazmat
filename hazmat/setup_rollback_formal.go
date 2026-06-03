package hazmat

import "hazmat/internal/setup"

type setupRollbackTLAResource = setup.Resource

const (
	tlaResourceAgentUser          = setup.ResourceAgentUser
	tlaResourceDevGroup           = setup.ResourceDevGroup
	tlaResourceHomeDirTraverse    = setup.ResourceHomeDirTraverse
	tlaResourceLocalRepo          = setup.ResourceLocalRepo
	tlaResourceHardeningGaps      = setup.ResourceHardeningGaps
	tlaResourceUmask              = setup.ResourceUmask
	tlaResourceSeatbelt           = setup.ResourceSeatbelt
	tlaResourceWrappers           = setup.ResourceWrappers
	tlaResourcePfAnchor           = setup.ResourcePfAnchor
	tlaResourceDNSBlocklist       = setup.ResourceDNSBlocklist
	tlaResourceLaunchDaemon       = setup.ResourceLaunchDaemon
	tlaResourceLaunchHelper       = setup.ResourceLaunchHelper
	tlaResourceSudoers            = setup.ResourceSudoers
	tlaResourceMaintenanceSudoers = setup.ResourceMaintenanceSudoers
	tlaResourceClaudeCode         = setup.ResourceClaudeCode
	tlaResourceCredentials        = setup.ResourceCredentials
)

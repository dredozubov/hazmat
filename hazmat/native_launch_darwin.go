//go:build darwin

package hazmat

import (
	"os"

	darwinruntime "hazmat/internal/runtime/darwin"
)

type darwinNativeLaunchBackend struct{}

func newNativeLaunchBackend() nativeLaunchBackend {
	return darwinNativeLaunchBackend{}
}

func (darwinNativeLaunchBackend) PreparePolicy(req nativeLaunchPolicyRequest) (nativeLaunchPolicyArtifact, error) {
	policy, err := buildNativeSessionPolicy(req.Config)
	if err != nil {
		return nativeLaunchPolicyArtifact{}, err
	}
	artifact, err := darwinruntime.PreparePolicyArtifact(darwinruntime.PolicyArtifactRequest{
		Contract:                 policy.Contract,
		MacOSSecurityFramework:   policy.MacOSSecurityFramework,
		MacOSAgentKeychainAccess: policy.MacOSAgentKeychainAccess,
		RuntimeTempDirs:          policy.RuntimeTempDirs,
		PID:                      os.Getpid(),
	})
	if err != nil {
		return nativeLaunchPolicyArtifact{}, err
	}
	return nativeLaunchPolicyArtifact{
		Path:    artifact.Path,
		cleanup: artifact.Cleanup,
	}, nil
}

func (b darwinNativeLaunchBackend) CommandSudoArgs(req nativeLaunchCommandRequest) []string {
	// The NOPASSWD sudoers rule covers exactly:
	//   sudo -u agent /usr/local/libexec/hazmat-launch <policy-file> ...
	//
	// hazmat-launch validates the policy file path and SUDO_UID ownership
	// before applying the platform sandbox. It refuses inline policies.
	// Optional metadata is emitted by hazmat-launch only after sandbox_init()
	// succeeds, so callers never see "enforced": true for a failed native
	// sandbox application.
	// env -i runs *inside* the sandbox so the environment is set after the
	// privilege boundary is crossed.
	return darwinruntime.CommandSudoArgs(darwinruntime.CommandRequest{
		AgentUser:        agentUser,
		LaunchHelperPath: launchHelperPath(),
		PolicyPath:       req.Policy.Path,
		MetadataJSON:     req.MetadataJSON,
		Profile:          req.Profile,
		DirectExec:       req.DirectExec,
		WorkingDir:       req.WorkingDir,
		SessionTempDir:   req.SessionTempDir,
		EnvPairs:         b.AgentEnvPairs(nativeLaunchEnvRequest{Config: req.Config, Plan: req.Plan}),
		RuntimeEnvPairs:  req.RuntimeEnvPairs,
		Script:           req.Script,
		Args:             req.Args,
	})
}

func (darwinNativeLaunchBackend) AgentEnvPairs(req nativeLaunchEnvRequest) []string {
	return nativeLaunchBaseEnvPairs(req.Config, nativeLaunchEnvironment{
		Shell:         "/bin/zsh",
		Path:          defaultAgentPath,
		Home:          agentHome,
		TmpDir:        defaultAgentTmpDir,
		CacheHome:     defaultAgentCacheHome,
		ConfigHome:    defaultAgentConfigHome,
		DataHome:      defaultAgentDataHome,
		PlatformPairs: darwinruntime.PlatformEnvPairs(),
	})
}

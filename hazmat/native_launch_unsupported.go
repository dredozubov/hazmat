//go:build !darwin

package hazmat

import (
	"fmt"
	"runtime"
)

type unsupportedNativeLaunchBackend struct{}

func newNativeLaunchBackend() nativeLaunchBackend {
	return unsupportedNativeLaunchBackend{}
}

func (unsupportedNativeLaunchBackend) PreparePolicy(nativeLaunchPolicyRequest) (nativeLaunchPolicyArtifact, error) {
	return nativeLaunchPolicyArtifact{}, fmt.Errorf("hazmat does not implement native launch for %s yet; supported platform is macOS", runtime.GOOS)
}

func (unsupportedNativeLaunchBackend) CommandSudoArgs(nativeLaunchCommandRequest) []string {
	return nil
}

func (unsupportedNativeLaunchBackend) AgentEnvPairs(req nativeLaunchEnvRequest) []string {
	return nativeLaunchBaseEnvPairs(req.Config, nativeLaunchEnvironment{
		Shell:      "/bin/sh",
		Path:       defaultAgentPath,
		TmpDir:     defaultAgentTmpDir,
		CacheHome:  defaultAgentCacheHome,
		ConfigHome: defaultAgentConfigHome,
		DataHome:   defaultAgentDataHome,
	})
}

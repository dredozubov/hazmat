//go:build darwin

package hazmat

import (
	"fmt"
	"os"
)

type darwinNativeLaunchBackend struct{}

func newNativeLaunchBackend() nativeLaunchBackend {
	return darwinNativeLaunchBackend{}
}

func (darwinNativeLaunchBackend) PreparePolicy(req nativeLaunchPolicyRequest) (nativeLaunchPolicyArtifact, error) {
	policy, err := generateSBPLChecked(req.Config)
	if err != nil {
		return nativeLaunchPolicyArtifact{}, err
	}
	policyFile := fmt.Sprintf("/private/tmp/hazmat-%d.sb", os.Getpid())
	if err := os.WriteFile(policyFile, []byte(policy), 0o644); err != nil {
		return nativeLaunchPolicyArtifact{}, fmt.Errorf("write seatbelt policy: %w", err)
	}
	if err := os.Chmod(policyFile, 0o644); err != nil {
		_ = os.Remove(policyFile)
		return nativeLaunchPolicyArtifact{}, fmt.Errorf("set seatbelt policy mode: %w", err)
	}

	return nativeLaunchPolicyArtifact{
		Path: policyFile,
		cleanup: func() {
			_ = os.Remove(policyFile)
		},
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
	full := []string{
		"-u", agentUser,
		launchHelperPath(), req.Policy.Path,
	}
	if req.MetadataJSON != "" {
		full = append(full, "--hazmat-metadata-json", req.MetadataJSON)
	}
	full = append(full, "/usr/bin/env", "-i")
	full = append(full, b.AgentEnvPairs(nativeLaunchEnvRequest{Config: req.Config, Plan: req.Plan})...)
	full = append(full, req.RuntimeEnvPairs...)
	full = append(full, "/bin/zsh", "-lc", req.Script, "zsh")
	full = append(full, req.Args...)
	return full
}

func (darwinNativeLaunchBackend) AgentEnvPairs(req nativeLaunchEnvRequest) []string {
	return nativeLaunchBaseEnvPairs(req.Config, nativeLaunchEnvironment{
		Shell:      "/bin/zsh",
		Path:       defaultAgentPath,
		TmpDir:     defaultAgentTmpDir,
		CacheHome:  defaultAgentCacheHome,
		ConfigHome: defaultAgentConfigHome,
		DataHome:   defaultAgentDataHome,
		PlatformPairs: []string{
			"HOMEBREW_NO_AUTO_UPDATE=1",
			// CGO compilation: the /usr/bin/cc shim dispatches through
			// xcode-select which may resolve to Xcode.app (outside the
			// seatbelt). Set CC/CXX directly to CommandLineTools compilers
			// and SDKROOT so clang can find system headers without probing
			// restricted paths.
			"DEVELOPER_DIR=/Library/Developer/CommandLineTools",
			"SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk",
			"CC=/Library/Developer/CommandLineTools/usr/bin/cc",
			"CXX=/Library/Developer/CommandLineTools/usr/bin/c++",
		},
	})
}

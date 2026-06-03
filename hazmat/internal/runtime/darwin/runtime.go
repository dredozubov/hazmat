package darwin

import (
	"fmt"
	"os"
)

const PackagePath = "hazmat/internal/runtime/darwin"

type PolicyArtifact struct {
	Path    string
	Cleanup func()
}

func PreparePolicy(policy string, pid int) (PolicyArtifact, error) {
	policyFile := fmt.Sprintf("/private/tmp/hazmat-%d.sb", pid)
	if err := os.WriteFile(policyFile, []byte(policy), 0o644); err != nil {
		return PolicyArtifact{}, fmt.Errorf("write seatbelt policy: %w", err)
	}
	if err := os.Chmod(policyFile, 0o644); err != nil {
		_ = os.Remove(policyFile)
		return PolicyArtifact{}, fmt.Errorf("set seatbelt policy mode: %w", err)
	}

	return PolicyArtifact{
		Path: policyFile,
		Cleanup: func() {
			_ = os.Remove(policyFile)
		},
	}, nil
}

type CommandRequest struct {
	AgentUser        string
	LaunchHelperPath string
	PolicyPath       string
	MetadataJSON     string
	EnvPairs         []string
	RuntimeEnvPairs  []string
	Script           string
	Args             []string
}

func CommandSudoArgs(req CommandRequest) []string {
	full := []string{
		"-u", req.AgentUser,
		req.LaunchHelperPath, req.PolicyPath,
	}
	if req.MetadataJSON != "" {
		full = append(full, "--hazmat-metadata-json", req.MetadataJSON)
	}
	full = append(full, "/usr/bin/env", "-i")
	full = append(full, req.EnvPairs...)
	full = append(full, req.RuntimeEnvPairs...)
	full = append(full, "/bin/zsh", "-lc", req.Script, "zsh")
	full = append(full, req.Args...)
	return full
}

func PlatformEnvPairs() []string {
	return []string{
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
	}
}

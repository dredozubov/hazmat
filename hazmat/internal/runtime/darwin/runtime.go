package darwin

import (
	"fmt"
	"os"

	"hazmat/containment"
	darwincompiler "hazmat/containment/darwin"
)

const PackagePath = "hazmat/internal/runtime/darwin"

type PolicyArtifact struct {
	Path    string
	Cleanup func()
}

type PolicyArtifactRequest struct {
	Contract                 containment.Contract
	MacOSSecurityFramework   bool
	MacOSAgentKeychainAccess bool
	RuntimeTempDirs          []string
	PID                      int
}

func PreparePolicyArtifact(req PolicyArtifactRequest) (PolicyArtifact, error) {
	if req.PID <= 0 {
		return PolicyArtifact{}, fmt.Errorf("policy artifact pid must be positive")
	}
	policy, err := darwincompiler.Compile(req.Contract, darwincompiler.CompileOptions{
		MacOSSecurityFramework:   req.MacOSSecurityFramework,
		MacOSAgentKeychainAccess: req.MacOSAgentKeychainAccess,
		RuntimeTempDirs:          append([]string(nil), req.RuntimeTempDirs...),
	})
	if err != nil {
		return PolicyArtifact{}, fmt.Errorf("compile seatbelt policy: %w", err)
	}
	return PreparePolicy(policy, req.PID)
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
	Profile          bool
	DirectExec       bool
	WorkingDir       string
	SessionTempDir   string
	EnvPairs         []string
	RuntimeEnvPairs  []string
	Script           string
	Args             []string
}

func CommandSudoArgs(req CommandRequest) []string {
	full := []string{
		"-u", req.AgentUser,
	}
	full = append(full, CommandLaunchHelperArgs(req)...)
	return full
}

func CommandCurrentUserArgs(req CommandRequest) []string {
	full := []string{req.LaunchHelperPath}
	if req.Profile {
		full = append(full, "--hazmat-launch-profile")
	}
	full = append(full, "--hazmat-current-user")
	full = append(full, CommandLaunchHelperArgsWithoutBinary(req)...)
	return full
}

func CommandLaunchHelperArgs(req CommandRequest) []string {
	full := []string{req.LaunchHelperPath}
	if req.Profile {
		full = append(full, "--hazmat-launch-profile")
	}
	full = append(full, CommandLaunchHelperArgsWithoutBinary(req)...)
	return full
}

func CommandLaunchHelperArgsWithoutBinary(req CommandRequest) []string {
	var full []string
	full = append(full, req.PolicyPath)
	if req.MetadataJSON != "" {
		full = append(full, "--hazmat-metadata-json", req.MetadataJSON)
	}
	if req.SessionTempDir != "" {
		full = append(full, "--hazmat-session-temp", req.SessionTempDir)
	}
	if req.DirectExec {
		full = append(full, "--hazmat-direct-exec")
		if req.WorkingDir != "" {
			full = append(full, "--hazmat-working-dir", req.WorkingDir)
		}
		for _, pair := range req.EnvPairs {
			full = append(full, "--hazmat-env", pair)
		}
		for _, pair := range req.RuntimeEnvPairs {
			full = append(full, "--hazmat-env", pair)
		}
		full = append(full, "--")
		full = append(full, req.Args...)
		return full
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

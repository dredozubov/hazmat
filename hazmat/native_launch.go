package main

import (
	"encoding/json"
	"os"
)

type nativeLaunchBackend interface {
	PreparePolicy(sessionConfig) (nativeLaunchPolicyArtifact, error)
	CommandSudoArgs(nativeLaunchCommandRequest) []string
	AgentEnvPairs(sessionConfig) []string
}

type nativeLaunchCommandRequest struct {
	Config          sessionConfig
	Policy          nativeLaunchPolicyArtifact
	RuntimeEnvPairs []string
	Script          string
	Args            []string
}

type nativeLaunchEnvironment struct {
	Shell         string
	Path          string
	TmpDir        string
	CacheHome     string
	ConfigHome    string
	DataHome      string
	PlatformPairs []string
}

func nativeLaunchSudoArgs(cfg sessionConfig, policy nativeLaunchPolicyArtifact, runtimeEnvPairs []string, script string, args ...string) []string {
	return newNativeLaunchBackend().CommandSudoArgs(nativeLaunchCommandRequest{
		Config:          cfg,
		Policy:          policy,
		RuntimeEnvPairs: runtimeEnvPairs,
		Script:          script,
		Args:            args,
	})
}

func agentEnvPairs(cfg sessionConfig) []string {
	return newNativeLaunchBackend().AgentEnvPairs(cfg)
}

func nativeLaunchBaseEnvPairs(cfg sessionConfig, env nativeLaunchEnvironment) []string {
	readDirsJSON, _ := json.Marshal(cfg.ReadDirs)
	writeDirsJSON, _ := json.Marshal(cfg.WriteDirs)
	pairs := []string{
		"HOME=" + agentHome,
		"USER=" + agentUser,
		"LOGNAME=" + agentUser,
		"SHELL=" + env.Shell,
		"PATH=" + env.Path,
		"TMPDIR=" + env.TmpDir,
		"XDG_CACHE_HOME=" + env.CacheHome,
		"XDG_CONFIG_HOME=" + env.ConfigHome,
		"XDG_DATA_HOME=" + env.DataHome,
	}
	pairs = append(pairs, env.PlatformPairs...)
	pairs = append(pairs,
		"SANDBOX_ACTIVE=1",
		"SANDBOX_PROJECT_DIR="+cfg.ProjectDir,
		"SANDBOX_READ_DIRS_JSON="+string(readDirsJSON),
		"SANDBOX_WRITE_DIRS_JSON="+string(writeDirsJSON),
	)
	if home, err := os.UserHomeDir(); err == nil {
		terminalPairs, _ := terminalCapabilitySupport(home, os.Getenv)
		pairs = append(pairs, terminalPairs...)
	}

	// Go toolchain: share the invoking user's module cache read-only.
	// GOMODCACHE points to the invoker's cache so `go build` uses
	// pre-downloaded modules instead of re-fetching. The seatbelt enforces
	// read-only access — if a new dependency is needed, `go mod download`
	// must be run outside the sandbox first.
	if modCache := invokerGoModCache(); modCache != "" {
		pairs = append(pairs, "GOMODCACHE="+modCache)
	}

	// Integration env passthrough: passive path pointers and selectors resolved
	// from the invoker's environment. Only keys in safeEnvKeys are allowed;
	// validation happens at integration-manifest load time.
	for key, val := range cfg.IntegrationEnv {
		pairs = append(pairs, key+"="+val)
	}
	for key, val := range cfg.HarnessEnv {
		pairs = append(pairs, key+"="+val)
	}

	return pairs
}

package hazmat

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
)

type nativeLaunchBackend interface {
	PreparePolicy(nativeLaunchPolicyRequest) (nativeLaunchPolicyArtifact, error)
	CommandSudoArgs(nativeLaunchCommandRequest) []string
	AgentEnvPairs(nativeLaunchEnvRequest) []string
}

type nativeLaunchPolicyRequest struct {
	Config sessionConfig
	Plan   sessionBackendPlan
}

type nativeLaunchEnvRequest struct {
	Config sessionConfig
	Plan   sessionBackendPlan
}

type nativeLaunchCommandRequest struct {
	Config          sessionConfig
	Plan            sessionBackendPlan
	Policy          nativeLaunchPolicyArtifact
	RuntimeEnvPairs []string
	MetadataJSON    string
	Profile         bool
	DirectExec      bool
	WorkingDir      string
	Script          string
	Args            []string
}

type nativeLaunchEnvironment struct {
	Shell         string
	Path          string
	Home          string
	TmpDir        string
	CacheHome     string
	ConfigHome    string
	DataHome      string
	PlatformPairs []string
}

var launchHelperSupportsDirectExec = launchHelperSupportsDirectExecImpl

const launchHelperCapabilityScanLimit = 2 << 20

func launchHelperSupportsDirectExecImpl(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	return readerContainsWithin(file, []byte("--hazmat-direct-exec"), launchHelperCapabilityScanLimit)
}

func readerContainsWithin(r io.Reader, marker []byte, limit int64) bool {
	if len(marker) == 0 || limit <= 0 {
		return false
	}
	buf := make([]byte, 64*1024+len(marker)-1)
	carry := 0
	remaining := limit
	for remaining > 0 {
		readSize := len(buf) - carry
		if int64(readSize) > remaining {
			readSize = int(remaining)
		}
		n, err := r.Read(buf[carry : carry+readSize])
		if n > 0 {
			window := buf[:carry+n]
			if bytes.Contains(window, marker) {
				return true
			}
			carry = min(len(marker)-1, len(window))
			copy(buf[:carry], window[len(window)-carry:])
			remaining -= int64(n)
		}
		if n == 0 && err == nil {
			return false
		}
		if err != nil {
			return false
		}
	}
	return false
}

func nativeLaunchSudoArgs(cfg sessionConfig, policy nativeLaunchPolicyArtifact, runtimeEnvPairs []string, script string, args ...string) []string {
	return nativeLaunchSudoArgsWithMetadataAndPlan(cfg, nativeLaunchPlanForConfig(cfg), policy, runtimeEnvPairs, "", script, args...)
}

func nativeLaunchSudoArgsWithMetadata(cfg sessionConfig, policy nativeLaunchPolicyArtifact, runtimeEnvPairs []string, metadataJSON string, script string, args ...string) []string {
	return nativeLaunchSudoArgsWithMetadataAndPlan(cfg, nativeLaunchPlanForConfig(cfg), policy, runtimeEnvPairs, metadataJSON, script, args...)
}

func nativeLaunchSudoArgsWithMetadataAndPlan(cfg sessionConfig, plan sessionBackendPlan, policy nativeLaunchPolicyArtifact, runtimeEnvPairs []string, metadataJSON string, script string, args ...string) []string {
	directExec := script == nativeDirectProjectExecScript && launchHelperSupportsDirectExec(launchHelperPath())
	workingDir := ""
	if directExec {
		workingDir = cfg.ProjectDir
	}
	return newNativeLaunchBackend().CommandSudoArgs(nativeLaunchCommandRequest{
		Config:          cfg,
		Plan:            plan,
		Policy:          policy,
		RuntimeEnvPairs: runtimeEnvPairs,
		MetadataJSON:    metadataJSON,
		Profile:         sessionPreparationProfileEnabled(),
		DirectExec:      directExec,
		WorkingDir:      workingDir,
		Script:          script,
		Args:            args,
	})
}

func agentEnvPairs(cfg sessionConfig) []string {
	return agentEnvPairsWithPlan(cfg, nativeLaunchPlanForConfig(cfg))
}

func agentEnvPairsWithPlan(cfg sessionConfig, plan sessionBackendPlan) []string {
	return newNativeLaunchBackend().AgentEnvPairs(nativeLaunchEnvRequest{
		Config: cfg,
		Plan:   plan,
	})
}

func nativeLaunchPlanForConfig(cfg sessionConfig) sessionBackendPlan {
	return buildSessionPlanForHostFacts(cfg.Target, cfg, sessionModeNative, false, currentHostFacts()).Backend
}

func nativeLaunchBaseEnvPairs(cfg sessionConfig, env nativeLaunchEnvironment) []string {
	if cfg.SessionHome != nil {
		env = nativeLaunchEnvironmentWithSessionHome(env, cfg.SessionHome.Launch.Layout)
	}
	readDirsJSON := marshalStringSliceEnvValue(cfg.ReadDirs)
	writeDirsJSON := marshalStringSliceEnvValue(cfg.WriteDirs)
	home := env.Home
	if home == "" {
		home = agentHome
	}
	tmpDir := env.TmpDir
	if cfg.TempDir != "" {
		tmpDir = cfg.TempDir
	}
	pairs := []string{
		"HOME=" + home,
		"USER=" + agentUser,
		"LOGNAME=" + agentUser,
		"SHELL=" + env.Shell,
		"PATH=" + env.Path,
		"TMPDIR=" + tmpDir,
		"TMP=" + tmpDir,
		"TEMP=" + tmpDir,
		"BUN_TMPDIR=" + tmpDir,
		"XDG_CACHE_HOME=" + env.CacheHome,
		"XDG_CONFIG_HOME=" + env.ConfigHome,
		"XDG_DATA_HOME=" + env.DataHome,
	}
	pairs = append(pairs, env.PlatformPairs...)
	pairs = append(pairs,
		"SANDBOX_ACTIVE=1",
		"SANDBOX_PROJECT_DIR="+cfg.ProjectDir,
		"SANDBOX_NETWORK_MODE="+normalizeSessionNetworkMode(cfg.NetworkMode).String(),
		"SANDBOX_READ_DIRS_JSON="+readDirsJSON,
		"SANDBOX_WRITE_DIRS_JSON="+writeDirsJSON,
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

func nativeLaunchEnvironmentWithSessionHome(env nativeLaunchEnvironment, layout sessionHomeLayout) nativeLaunchEnvironment {
	env.Home = layout.Home
	env.CacheHome = layout.CacheHome
	env.ConfigHome = layout.ConfigHome
	env.DataHome = layout.DataHome
	return env
}

func marshalStringSliceEnvValue(value []string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

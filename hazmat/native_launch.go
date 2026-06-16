package hazmat

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
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
	SessionTempDir  string
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
var launchHelperSupportsSessionTemp = launchHelperSupportsSessionTempImpl
var launchHelperPathForBrokerChild = defaultLaunchHelperPathForBrokerChild

const launchHelperCapabilityScanLimit = 2 << 20
const brokerChildLaunchHelperEnv = "HAZMAT_BROKER_CHILD_LAUNCH_HELPER"

var launchHelperCapabilityCache sync.Map
var launchHelperCapabilityDiskCachePath = defaultLaunchHelperCapabilityDiskCachePath

type launchHelperCapabilities struct {
	DirectExec  bool `json:"direct_exec"`
	SessionTemp bool `json:"session_temp"`
}

type launchHelperFileFingerprint struct {
	Size            int64  `json:"size"`
	Mode            uint32 `json:"mode"`
	ModTimeUnixNano int64  `json:"mod_time_unix_nano"`
	Dev             uint64 `json:"dev"`
	Ino             uint64 `json:"ino"`
}

type launchHelperCapabilityDiskCache struct {
	Version int                                        `json:"version"`
	Entries map[string]launchHelperCapabilityDiskEntry `json:"entries"`
}

type launchHelperCapabilityDiskEntry struct {
	Fingerprint  launchHelperFileFingerprint `json:"fingerprint"`
	Capabilities launchHelperCapabilities    `json:"capabilities"`
}

func launchHelperSupportsDirectExecImpl(path string) bool {
	return launchHelperCapabilitiesFor(path).DirectExec
}

func launchHelperSupportsSessionTempImpl(path string) bool {
	return launchHelperCapabilitiesFor(path).SessionTemp
}

func defaultLaunchHelperPathForBrokerChild() string {
	if override := os.Getenv(brokerChildLaunchHelperEnv); override != "" {
		return override
	}
	if override := os.Getenv("HAZMAT_LAUNCH_HELPER"); override != "" {
		return override
	}
	helperPath := launchHelperPath()
	if exe, err := currentExecutablePath(); err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
			exe = resolved
		}
		candidate := filepath.Join(filepath.Dir(exe), "hazmat-launch")
		if candidate != helperPath && executableRegularFile(candidate) {
			return candidate
		}
	}
	return helperPath
}

func executableRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func launchHelperCapabilitiesFor(path string) launchHelperCapabilities {
	if cached, ok := launchHelperCapabilityCache.Load(path); ok {
		if caps, ok := cached.(launchHelperCapabilities); ok {
			return caps
		}
	}
	if fingerprint, ok := currentLaunchHelperFingerprint(path); ok {
		if caps, ok := readLaunchHelperCapabilityDiskCache(path, fingerprint); ok {
			launchHelperCapabilityCache.Store(path, caps)
			return caps
		}
	}
	caps, fingerprint, cacheable := readLaunchHelperCapabilitiesWithFingerprint(path)
	if cacheable {
		writeLaunchHelperCapabilityDiskCache(path, fingerprint, caps)
	}
	launchHelperCapabilityCache.Store(path, caps)
	return caps
}

func readLaunchHelperCapabilities(path string) launchHelperCapabilities {
	caps, _, _ := readLaunchHelperCapabilitiesWithFingerprint(path)
	return caps
}

func readLaunchHelperCapabilitiesWithFingerprint(path string) (launchHelperCapabilities, launchHelperFileFingerprint, bool) {
	file, err := os.Open(path)
	if err != nil {
		return launchHelperCapabilities{}, launchHelperFileFingerprint{}, false
	}
	defer file.Close()

	info, statErr := file.Stat()
	fingerprint, cacheable := launchHelperFingerprintFromFileInfo(info, statErr)
	markers := map[string][]byte{
		"direct_exec":  []byte("--hazmat-direct-exec"),
		"session_temp": []byte("--hazmat-session-temp"),
	}
	found := readerMarkersWithin(file, markers, launchHelperCapabilityScanLimit)
	return launchHelperCapabilities{
		DirectExec:  found["direct_exec"],
		SessionTemp: found["session_temp"],
	}, fingerprint, cacheable
}

func defaultLaunchHelperCapabilityDiskCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".hazmat", "launch-helper-capabilities.json")
}

func currentLaunchHelperFingerprint(path string) (launchHelperFileFingerprint, bool) {
	info, err := os.Stat(path)
	return launchHelperFingerprintFromFileInfo(info, err)
}

func launchHelperFingerprintFromFileInfo(info os.FileInfo, err error) (launchHelperFileFingerprint, bool) {
	if err != nil || info == nil || !info.Mode().IsRegular() {
		return launchHelperFileFingerprint{}, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return launchHelperFileFingerprint{}, false
	}
	return launchHelperFileFingerprint{
		Size:            info.Size(),
		Mode:            uint32(info.Mode().Perm()),
		ModTimeUnixNano: info.ModTime().UnixNano(),
		Dev:             uint64(stat.Dev),
		Ino:             uint64(stat.Ino),
	}, true
}

func readLaunchHelperCapabilityDiskCache(path string, fingerprint launchHelperFileFingerprint) (launchHelperCapabilities, bool) {
	cachePath := launchHelperCapabilityDiskCachePath()
	if cachePath == "" {
		return launchHelperCapabilities{}, false
	}
	cache, err := loadLaunchHelperCapabilityDiskCache(cachePath)
	if err != nil || cache.Version != 1 {
		return launchHelperCapabilities{}, false
	}
	entry, ok := cache.Entries[path]
	if !ok || entry.Fingerprint != fingerprint {
		return launchHelperCapabilities{}, false
	}
	return entry.Capabilities, true
}

func writeLaunchHelperCapabilityDiskCache(path string, fingerprint launchHelperFileFingerprint, caps launchHelperCapabilities) {
	cachePath := launchHelperCapabilityDiskCachePath()
	if cachePath == "" {
		return
	}
	cache, err := loadLaunchHelperCapabilityDiskCache(cachePath)
	if err != nil || cache.Version != 1 {
		cache = launchHelperCapabilityDiskCache{Version: 1}
	}
	if cache.Entries == nil || len(cache.Entries) > 32 {
		cache.Entries = make(map[string]launchHelperCapabilityDiskEntry)
	}
	cache.Entries[path] = launchHelperCapabilityDiskEntry{
		Fingerprint:  fingerprint,
		Capabilities: caps,
	}
	_ = saveLaunchHelperCapabilityDiskCache(cachePath, cache)
}

func loadLaunchHelperCapabilityDiskCache(path string) (launchHelperCapabilityDiskCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return launchHelperCapabilityDiskCache{}, err
	}
	var cache launchHelperCapabilityDiskCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return launchHelperCapabilityDiskCache{}, err
	}
	return cache, nil
}

func saveLaunchHelperCapabilityDiskCache(path string, cache launchHelperCapabilityDiskCache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".launch-helper-capabilities-*.json")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func readerContainsWithin(r io.Reader, marker []byte, limit int64) bool {
	return readerMarkersWithin(r, map[string][]byte{"marker": marker}, limit)["marker"]
}

func readerMarkersWithin(r io.Reader, markers map[string][]byte, limit int64) map[string]bool {
	found := make(map[string]bool, len(markers))
	maxMarkerLen := 0
	for name, marker := range markers {
		if len(marker) == 0 {
			continue
		}
		if len(marker) > maxMarkerLen {
			maxMarkerLen = len(marker)
		}
		found[name] = false
	}
	if len(found) == 0 || limit <= 0 {
		return found
	}
	allFound := func() bool {
		for _, ok := range found {
			if !ok {
				return false
			}
		}
		return true
	}
	if allFound() {
		return found
	}
	buf := make([]byte, 64*1024+maxMarkerLen-1)
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
			for name, marker := range markers {
				if !found[name] && bytes.Contains(window, marker) {
					found[name] = true
				}
			}
			if allFound() {
				return found
			}
			carry = min(maxMarkerLen-1, len(window))
			copy(buf[:carry], window[len(window)-carry:])
			remaining -= int64(n)
		}
		if n == 0 && err == nil {
			return found
		}
		if err != nil {
			return found
		}
	}
	return found
}

func nativeLaunchSudoArgs(cfg sessionConfig, policy nativeLaunchPolicyArtifact, runtimeEnvPairs []string, script string, args ...string) []string {
	return nativeLaunchSudoArgsWithMetadataAndPlan(cfg, nativeLaunchPlanForConfig(cfg), policy, runtimeEnvPairs, "", script, args...)
}

func nativeLaunchSudoArgsWithMetadata(cfg sessionConfig, policy nativeLaunchPolicyArtifact, runtimeEnvPairs []string, metadataJSON string, script string, args ...string) []string {
	return nativeLaunchSudoArgsWithMetadataAndPlan(cfg, nativeLaunchPlanForConfig(cfg), policy, runtimeEnvPairs, metadataJSON, script, args...)
}

func nativeLaunchSudoArgsWithMetadataAndPlan(cfg sessionConfig, plan sessionBackendPlan, policy nativeLaunchPolicyArtifact, runtimeEnvPairs []string, metadataJSON string, script string, args ...string) []string {
	return nativeLaunchSudoArgsWithMetadataPlanAndRuntime(cfg, plan, policy, runtimeEnvPairs, metadataJSON, "", script, args...)
}

func nativeLaunchSudoArgsWithMetadataPlanAndRuntime(cfg sessionConfig, plan sessionBackendPlan, policy nativeLaunchPolicyArtifact, runtimeEnvPairs []string, metadataJSON string, launchHelperTempDir string, script string, args ...string) []string {
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
		SessionTempDir:  launchHelperTempDir,
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

	if !cfg.SkipGoModCacheEnv {
		// Go toolchain: share the invoking user's module cache read-only.
		// GOMODCACHE points to the invoker's cache so `go build` uses
		// pre-downloaded modules instead of re-fetching. The seatbelt enforces
		// read-only access — if a new dependency is needed, `go mod download`
		// must be run outside the sandbox first.
		if modCache := invokerGoModCache(); modCache != "" {
			pairs = append(pairs, "GOMODCACHE="+modCache)
		}
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

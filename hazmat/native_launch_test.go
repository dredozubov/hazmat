package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReaderContainsWithinFindsMarkerAcrossChunkBoundary(t *testing.T) {
	marker := []byte("--hazmat-direct-exec")
	firstRead := 64*1024 + len(marker) - 1
	prefixLen := firstRead - 3
	input := strings.NewReader(strings.Repeat("x", prefixLen) + string(marker) + "suffix")

	if !readerContainsWithin(input, marker, int64(prefixLen+len(marker)+6)) {
		t.Fatal("readerContainsWithin() = false, want true for marker crossing chunk boundary")
	}
}

func TestReaderContainsWithinHonorsLimit(t *testing.T) {
	marker := []byte("--hazmat-direct-exec")
	limit := int64(128)
	input := strings.NewReader(strings.Repeat("x", int(limit)) + string(marker))

	if readerContainsWithin(input, marker, limit) {
		t.Fatal("readerContainsWithin() = true past limit, want false")
	}
}

func TestReaderContainsWithinRejectsMissingMarker(t *testing.T) {
	marker := []byte("--hazmat-direct-exec")
	input := strings.NewReader(strings.Repeat("x", 4096))

	if readerContainsWithin(input, marker, 4096) {
		t.Fatal("readerContainsWithin() = true for missing marker, want false")
	}
}

func TestReaderMarkersWithinFindsBothLaunchHelperCapabilities(t *testing.T) {
	markers := map[string][]byte{
		"direct_exec":  []byte("--hazmat-direct-exec"),
		"session_temp": []byte("--hazmat-session-temp"),
	}
	input := strings.NewReader(strings.Repeat("x", 127) + "--hazmat-session-temp" + strings.Repeat("y", 127) + "--hazmat-direct-exec")

	got := readerMarkersWithin(input, markers, 1024)
	for name := range markers {
		if !got[name] {
			t.Fatalf("readerMarkersWithin()[%s] = false, want true; got %#v", name, got)
		}
	}
}

func TestReadLaunchHelperCapabilitiesUsesBoundedMarkerScan(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "hazmat-launch")
	content := strings.Join([]string{
		"prefix",
		"--hazmat-session-temp",
		"--hazmat-direct-exec",
	}, "\x00")
	if err := os.WriteFile(helper, []byte(content), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	got := readLaunchHelperCapabilities(helper)
	if !got.DirectExec || !got.SessionTemp {
		t.Fatalf("readLaunchHelperCapabilities() = %+v, want both capabilities", got)
	}
}

func TestLaunchHelperCapabilitiesUsesMatchingDiskCache(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "hazmat-launch")
	if err := os.WriteFile(helper, []byte("helper without markers"), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	fingerprint, ok := currentLaunchHelperFingerprint(helper)
	if !ok {
		t.Fatal("helper fingerprint was not cacheable")
	}

	cachePath := filepath.Join(dir, "cache", "launch-helper-capabilities.json")
	restore := replaceLaunchHelperCapabilityCacheForTest(t, cachePath)
	defer restore()
	if err := saveLaunchHelperCapabilityDiskCache(cachePath, launchHelperCapabilityDiskCache{
		Version: 1,
		Entries: map[string]launchHelperCapabilityDiskEntry{
			helper: {
				Fingerprint: fingerprint,
				Capabilities: launchHelperCapabilities{
					DirectExec:  true,
					SessionTemp: true,
				},
			},
		},
	}); err != nil {
		t.Fatalf("save disk cache: %v", err)
	}

	got := launchHelperCapabilitiesFor(helper)
	if !got.DirectExec || !got.SessionTemp {
		t.Fatalf("launchHelperCapabilitiesFor() = %+v, want disk-cached capabilities", got)
	}
}

func TestLaunchHelperCapabilitiesInvalidatesStaleDiskCache(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "hazmat-launch")
	if err := os.WriteFile(helper, []byte("--hazmat-session-temp\x00--hazmat-direct-exec"), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	cachePath := filepath.Join(dir, "cache", "launch-helper-capabilities.json")
	restore := replaceLaunchHelperCapabilityCacheForTest(t, cachePath)
	defer restore()
	first := launchHelperCapabilitiesFor(helper)
	if !first.DirectExec || !first.SessionTemp {
		t.Fatalf("first capabilities = %+v, want both capabilities", first)
	}

	clearLaunchHelperCapabilityMemoryCacheForTest()
	if err := os.WriteFile(helper, []byte("replacement helper without capability markers and different size"), 0o755); err != nil {
		t.Fatalf("replace helper: %v", err)
	}

	got := launchHelperCapabilitiesFor(helper)
	if got.DirectExec || got.SessionTemp {
		t.Fatalf("stale disk cache was used after helper replacement: %+v", got)
	}
}

func TestNativeLaunchBaseEnvPairsCanSkipGoModCacheProbe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range terminalEnvPassthroughKeys {
		t.Setenv(key, "")
	}
	t.Setenv("TERMINFO", "")
	t.Setenv("TERMINFO_DIRS", "")

	called := false
	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		called = true
		return &fakeIntegrationProbe{}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	pairs := nativeLaunchBaseEnvPairs(sessionConfig{
		ProjectDir:        "/Users/dr/workspace/project",
		SkipGoModCacheEnv: true,
	}, nativeLaunchEnvironment{
		Shell:      "/bin/zsh",
		Path:       defaultAgentPath,
		Home:       agentHome,
		TmpDir:     defaultAgentTmpDir,
		CacheHome:  defaultAgentCacheHome,
		ConfigHome: defaultAgentConfigHome,
		DataHome:   defaultAgentDataHome,
	})

	if called {
		t.Fatal("integration probe factory was called despite SkipGoModCacheEnv")
	}
	for _, pair := range pairs {
		if strings.HasPrefix(pair, "GOMODCACHE=") {
			t.Fatalf("GOMODCACHE unexpectedly present: %v", pairs)
		}
	}
}

func TestNativeLaunchBaseEnvPairsIncludesGoModCacheByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range terminalEnvPassthroughKeys {
		t.Setenv(key, "")
	}
	t.Setenv("TERMINFO", "")
	t.Setenv("TERMINFO_DIRS", "")
	modCache := t.TempDir()
	resolvedModCache, err := filepath.EvalSymlinks(modCache)
	if err != nil {
		t.Fatalf("resolve mod cache: %v", err)
	}

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			outputs: map[string]string{
				"go env GOMODCACHE": modCache,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	pairs := nativeLaunchBaseEnvPairs(sessionConfig{
		ProjectDir: "/Users/dr/workspace/project",
	}, nativeLaunchEnvironment{
		Shell:      "/bin/zsh",
		Path:       defaultAgentPath,
		Home:       agentHome,
		TmpDir:     defaultAgentTmpDir,
		CacheHome:  defaultAgentCacheHome,
		ConfigHome: defaultAgentConfigHome,
		DataHome:   defaultAgentDataHome,
	})

	if !containsString(pairs, "GOMODCACHE="+resolvedModCache) {
		t.Fatalf("GOMODCACHE missing from env pairs: %v", pairs)
	}
}

func replaceLaunchHelperCapabilityCacheForTest(t *testing.T, path string) func() {
	t.Helper()
	clearLaunchHelperCapabilityMemoryCacheForTest()
	oldPath := launchHelperCapabilityDiskCachePath
	launchHelperCapabilityDiskCachePath = func() string { return path }
	return func() {
		clearLaunchHelperCapabilityMemoryCacheForTest()
		launchHelperCapabilityDiskCachePath = oldPath
	}
}

func clearLaunchHelperCapabilityMemoryCacheForTest() {
	launchHelperCapabilityCache.Range(func(key, _ any) bool {
		launchHelperCapabilityCache.Delete(key)
		return true
	})
}

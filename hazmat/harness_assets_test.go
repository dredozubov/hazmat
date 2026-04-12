package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type harnessAssetTestEnv struct {
	hostHome  string
	agentHome string
}

func isolateHarnessAssets(t *testing.T) harnessAssetTestEnv {
	t.Helper()

	savedPath := harnessAssetsFilePath
	savedHome := harnessAssetAgentHome
	savedSpecs := harnessAssetSpecs
	savedNow := harnessAssetsNow

	root := t.TempDir()
	env := harnessAssetTestEnv{
		hostHome:  filepath.Join(root, "host"),
		agentHome: filepath.Join(root, "agent"),
	}
	for _, dir := range []string{env.hostHome, env.agentHome} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	harnessAssetsFilePath = filepath.Join(root, ".hazmat", "harness-assets.json")
	harnessAssetAgentHome = env.agentHome
	harnessAssetSpecs = map[HarnessID][]harnessAssetSpec{}
	harnessAssetsNow = func() time.Time {
		return time.Date(2026, time.April, 12, 10, 0, 0, 0, time.UTC)
	}

	t.Cleanup(func() {
		harnessAssetsFilePath = savedPath
		harnessAssetAgentHome = savedHome
		harnessAssetSpecs = savedSpecs
		harnessAssetsNow = savedNow
	})

	return env
}

func writeHarnessAssetTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func hasHarnessAssetMutation(mutations []sessionMutation) bool {
	for _, mutation := range mutations {
		if strings.Contains(strings.ToLower(mutation.Summary), "asset sync") {
			return true
		}
	}
	return false
}

func TestParseHarnessArgsRecognizesSkipHarnessAssetsSync(t *testing.T) {
	opts, forwarded, err := parseHarnessArgs([]string{"--skip-harness-assets-sync", "--model", "sonnet"})
	if err != nil {
		t.Fatalf("parseHarnessArgs: %v", err)
	}
	if !opts.skipHarnessAssetsSync {
		t.Fatal("expected skipHarnessAssetsSync to be true")
	}
	if len(forwarded) != 2 || forwarded[0] != "--model" || forwarded[1] != "sonnet" {
		t.Fatalf("forwarded = %v, want [--model sonnet]", forwarded)
	}
}

func TestRunConfigSetSessionHarnessAssets(t *testing.T) {
	isolateConfig(t)

	if err := runConfigSet("session.harness_assets", "false"); err != nil {
		t.Fatalf("runConfigSet(false): %v", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.HarnessAssets() {
		t.Fatal("expected HarnessAssets to be disabled")
	}

	if err := runConfigSet("session.harness_assets", "true"); err != nil {
		t.Fatalf("runConfigSet(true): %v", err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !cfg.HarnessAssets() {
		t.Fatal("expected HarnessAssets to be enabled")
	}
}

func TestSyncHarnessAssetsCreatesUpdatesAndDeletesManagedEntries(t *testing.T) {
	env := isolateHarnessAssets(t)

	hostRoot := filepath.Join(env.hostHome, ".claude", "commands")
	destRoot := filepath.Join(env.agentHome, ".claude", "commands")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "commands", Kind: harnessAssetDirRoot, HostPath: hostRoot, AgentPath: destRoot},
	}

	sourceFile := filepath.Join(hostRoot, "create-prd.md")
	destFile := filepath.Join(destRoot, "create-prd.md")
	writeHarnessAssetTestFile(t, sourceFile, "# create prd\n")

	result, err := syncHarnessAssets(HarnessClaude)
	if err != nil {
		t.Fatalf("syncHarnessAssets(create): %v", err)
	}
	if result.Added != 1 || result.Updated != 0 || result.Deleted != 0 {
		t.Fatalf("create result = %+v, want 1 added", result)
	}
	raw, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("read dest file: %v", err)
	}
	if string(raw) != "# create prd\n" {
		t.Fatalf("dest content = %q", string(raw))
	}

	state, err := loadHarnessAssetsState()
	if err != nil {
		t.Fatalf("loadHarnessAssetsState: %v", err)
	}
	if _, ok := state.harnessEntries(HarnessClaude).Entries[destFile]; !ok {
		t.Fatalf("expected manifest entry for %s", destFile)
	}

	writeHarnessAssetTestFile(t, sourceFile, "# updated\n")
	result, err = syncHarnessAssets(HarnessClaude)
	if err != nil {
		t.Fatalf("syncHarnessAssets(update): %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("update result = %+v, want 1 updated", result)
	}
	raw, err = os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("read updated dest file: %v", err)
	}
	if string(raw) != "# updated\n" {
		t.Fatalf("updated dest content = %q", string(raw))
	}

	if err := os.Remove(sourceFile); err != nil {
		t.Fatalf("remove source file: %v", err)
	}
	result, err = syncHarnessAssets(HarnessClaude)
	if err != nil {
		t.Fatalf("syncHarnessAssets(delete): %v", err)
	}
	if result.Deleted != 1 {
		t.Fatalf("delete result = %+v, want 1 deleted", result)
	}
	if _, err := os.Stat(destFile); !os.IsNotExist(err) {
		t.Fatalf("dest file still exists after delete: %v", err)
	}
	state, err = loadHarnessAssetsState()
	if err != nil {
		t.Fatalf("loadHarnessAssetsState: %v", err)
	}
	if _, ok := state.Harnesses[HarnessClaude]; ok {
		t.Fatalf("expected no remaining Claude harness manifest entries, got %+v", state.Harnesses[HarnessClaude])
	}
}

func TestSyncHarnessAssetsAdoptsEqualUnmanagedEntry(t *testing.T) {
	env := isolateHarnessAssets(t)

	hostFile := filepath.Join(env.hostHome, ".claude", "CLAUDE.md")
	destFile := filepath.Join(env.agentHome, ".claude", "CLAUDE.md")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "claude-md", Kind: harnessAssetFileRoot, HostPath: hostFile, AgentPath: destFile},
	}

	writeHarnessAssetTestFile(t, hostFile, "host instructions\n")
	writeHarnessAssetTestFile(t, destFile, "host instructions\n")

	result, err := syncHarnessAssets(HarnessClaude)
	if err != nil {
		t.Fatalf("syncHarnessAssets(adopt): %v", err)
	}
	if result.Adopted != 1 || result.Added != 0 {
		t.Fatalf("adopt result = %+v, want 1 adopted and 0 added", result)
	}

	state, err := loadHarnessAssetsState()
	if err != nil {
		t.Fatalf("loadHarnessAssetsState: %v", err)
	}
	if _, ok := state.harnessEntries(HarnessClaude).Entries[destFile]; !ok {
		t.Fatalf("expected adopted manifest entry for %s", destFile)
	}
}

func TestSyncHarnessAssetsRejectsTopLevelSymlinkEscape(t *testing.T) {
	env := isolateHarnessAssets(t)

	hostRoot := filepath.Join(env.hostHome, ".claude", "commands")
	destRoot := filepath.Join(env.agentHome, ".claude", "commands")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "commands", Kind: harnessAssetDirRoot, HostPath: hostRoot, AgentPath: destRoot},
	}

	outside := filepath.Join(env.hostHome, "outside.md")
	writeHarnessAssetTestFile(t, outside, "outside\n")
	if err := os.MkdirAll(hostRoot, 0o755); err != nil {
		t.Fatalf("mkdir host root: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(hostRoot, "escape.md")); err != nil {
		t.Fatalf("symlink escape: %v", err)
	}

	result, err := syncHarnessAssets(HarnessClaude)
	if err != nil {
		t.Fatalf("syncHarnessAssets(escape): %v", err)
	}
	if result.Added != 0 {
		t.Fatalf("escape result = %+v, want 0 added", result)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "escapes the allowed root") {
		t.Fatalf("warnings = %v, want escape warning", result.Warnings)
	}
	if _, err := os.Stat(filepath.Join(destRoot, "escape.md")); !os.IsNotExist(err) {
		t.Fatalf("escape destination should not exist: %v", err)
	}
}

func TestSyncHarnessAssetsRejectsNestedSymlink(t *testing.T) {
	env := isolateHarnessAssets(t)

	hostRoot := filepath.Join(env.hostHome, ".claude", "commands")
	destRoot := filepath.Join(env.agentHome, ".claude", "commands")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "commands", Kind: harnessAssetDirRoot, HostPath: hostRoot, AgentPath: destRoot},
	}

	target := filepath.Join(hostRoot, "bundle", "real.md")
	writeHarnessAssetTestFile(t, target, "real\n")
	if err := os.Symlink(target, filepath.Join(hostRoot, "bundle", "link.md")); err != nil {
		t.Fatalf("nested symlink: %v", err)
	}

	result, err := syncHarnessAssets(HarnessClaude)
	if err != nil {
		t.Fatalf("syncHarnessAssets(nested symlink): %v", err)
	}
	if result.Added != 0 {
		t.Fatalf("nested symlink result = %+v, want 0 added", result)
	}
	found := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "nested symlink") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want nested symlink warning", result.Warnings)
	}
	if _, err := os.Stat(filepath.Join(destRoot, "bundle")); !os.IsNotExist(err) {
		t.Fatalf("nested symlink destination should not exist: %v", err)
	}
}

func TestResolvePreparedSessionPlansHarnessAssetSyncForHarnessCommands(t *testing.T) {
	env := isolateHarnessAssets(t)
	isolateConfig(t)
	skipInitCheck(t)

	hostFile := filepath.Join(env.hostHome, ".claude", "CLAUDE.md")
	destFile := filepath.Join(env.agentHome, ".claude", "CLAUDE.md")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "claude-md", Kind: harnessAssetFileRoot, HostPath: hostFile, AgentPath: destFile},
	}
	writeHarnessAssetTestFile(t, hostFile, "claude\n")

	projectDir := t.TempDir()
	prepared, err := resolvePreparedSession("claude", harnessSessionOpts{project: projectDir}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(claude): %v", err)
	}
	if !hasHarnessAssetMutation(prepared.Config.PlannedHostMutations) {
		t.Fatalf("PlannedHostMutations = %+v, want harness asset sync", prepared.Config.PlannedHostMutations)
	}

	shellPrepared, err := resolvePreparedSession("shell", harnessSessionOpts{project: projectDir}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(shell): %v", err)
	}
	if hasHarnessAssetMutation(shellPrepared.Config.PlannedHostMutations) {
		t.Fatalf("shell PlannedHostMutations = %+v, want no harness asset sync", shellPrepared.Config.PlannedHostMutations)
	}
}

func TestResolvePreparedSessionSkipsHarnessAssetSyncWhenDisabled(t *testing.T) {
	env := isolateHarnessAssets(t)
	isolateConfig(t)
	skipInitCheck(t)

	hostFile := filepath.Join(env.hostHome, ".claude", "CLAUDE.md")
	destFile := filepath.Join(env.agentHome, ".claude", "CLAUDE.md")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "claude-md", Kind: harnessAssetFileRoot, HostPath: hostFile, AgentPath: destFile},
	}
	writeHarnessAssetTestFile(t, hostFile, "claude\n")

	if err := runConfigSet("session.harness_assets", "false"); err != nil {
		t.Fatalf("runConfigSet(false): %v", err)
	}

	projectDir := t.TempDir()
	prepared, err := resolvePreparedSession("claude", harnessSessionOpts{project: projectDir}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(claude): %v", err)
	}
	if hasHarnessAssetMutation(prepared.Config.PlannedHostMutations) {
		t.Fatalf("PlannedHostMutations = %+v, want no harness asset sync when disabled", prepared.Config.PlannedHostMutations)
	}
}

func TestResolvePreparedSessionSkipsHarnessAssetSyncWhenFlagged(t *testing.T) {
	env := isolateHarnessAssets(t)
	isolateConfig(t)
	skipInitCheck(t)

	hostFile := filepath.Join(env.hostHome, ".claude", "CLAUDE.md")
	destFile := filepath.Join(env.agentHome, ".claude", "CLAUDE.md")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "claude-md", Kind: harnessAssetFileRoot, HostPath: hostFile, AgentPath: destFile},
	}
	writeHarnessAssetTestFile(t, hostFile, "claude\n")

	projectDir := t.TempDir()
	prepared, err := resolvePreparedSession("claude", harnessSessionOpts{
		project:               projectDir,
		skipHarnessAssetsSync: true,
	}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession(claude): %v", err)
	}
	if hasHarnessAssetMutation(prepared.Config.PlannedHostMutations) {
		t.Fatalf("PlannedHostMutations = %+v, want no harness asset sync when skip flag is set", prepared.Config.PlannedHostMutations)
	}
}

func TestResolveExplainSessionDoesNotApplyHarnessAssetSync(t *testing.T) {
	env := isolateHarnessAssets(t)
	isolateConfig(t)
	skipInitCheck(t)

	hostFile := filepath.Join(env.hostHome, ".claude", "CLAUDE.md")
	destFile := filepath.Join(env.agentHome, ".claude", "CLAUDE.md")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "claude-md", Kind: harnessAssetFileRoot, HostPath: hostFile, AgentPath: destFile},
	}
	writeHarnessAssetTestFile(t, hostFile, "claude\n")

	projectDir := t.TempDir()
	cfg, _, err := resolveExplainSession("claude", harnessSessionOpts{project: projectDir})
	if err != nil {
		t.Fatalf("resolveExplainSession: %v", err)
	}
	if !hasHarnessAssetMutation(cfg.PlannedHostMutations) {
		t.Fatalf("PlannedHostMutations = %+v, want harness asset sync", cfg.PlannedHostMutations)
	}
	if _, err := os.Stat(destFile); !os.IsNotExist(err) {
		t.Fatalf("explain should not materialize harness assets: %v", err)
	}
}

package hazmat

import (
	"errors"
	"os"
	"os/exec"
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
	savedUseAgentBackend := harnessAssetUseAgentBackend
	savedAgentEnsureDir := harnessAssetAgentEnsureDir
	savedAgentWriteFile := harnessAssetAgentWriteFile
	savedAgentPathExists := harnessAssetAgentPathExists
	savedAgentRename := harnessAssetAgentRename
	savedAgentRemoveAll := harnessAssetAgentRemoveAll
	savedAgentDirWritable := harnessAssetAgentDirWritable
	savedAgentRepairDir := harnessAssetAgentRepairDir
	savedPathForDirectIO := harnessAssetPathForDirectIO

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
		harnessAssetUseAgentBackend = savedUseAgentBackend
		harnessAssetAgentEnsureDir = savedAgentEnsureDir
		harnessAssetAgentWriteFile = savedAgentWriteFile
		harnessAssetAgentPathExists = savedAgentPathExists
		harnessAssetAgentRename = savedAgentRename
		harnessAssetAgentRemoveAll = savedAgentRemoveAll
		harnessAssetAgentDirWritable = savedAgentDirWritable
		harnessAssetAgentRepairDir = savedAgentRepairDir
		harnessAssetPathForDirectIO = savedPathForDirectIO
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

func TestSyncHarnessAssetsUsesAgentBackendForPersistentAgentHome(t *testing.T) {
	env := isolateHarnessAssets(t)

	hostRoot := filepath.Join(env.hostHome, ".claude", "commands")
	destRoot := filepath.Join(env.agentHome, ".claude", "commands")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "commands", Kind: harnessAssetDirRoot, HostPath: hostRoot, AgentPath: destRoot},
	}

	sourceFile := filepath.Join(hostRoot, "review.md")
	destFile := filepath.Join(destRoot, "review.md")
	writeHarnessAssetTestFile(t, sourceFile, "review\n")

	var ensureCalls, writeCalls, renameCalls, removeCalls int
	harnessAssetUseAgentBackend = func(path string) bool {
		clean := filepath.Clean(path)
		return clean == env.agentHome || strings.HasPrefix(clean, env.agentHome+string(os.PathSeparator))
	}
	harnessAssetAgentEnsureDir = func(path string, mode os.FileMode) error {
		ensureCalls++
		return os.MkdirAll(path, mode)
	}
	harnessAssetAgentWriteFile = func(path string, content []byte, mode os.FileMode) error {
		writeCalls++
		if err := os.WriteFile(path, content, mode); err != nil {
			return err
		}
		return os.Chmod(path, mode)
	}
	harnessAssetAgentPathExists = func(path string) (bool, error) {
		_, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return false, nil
		}
		return err == nil, err
	}
	harnessAssetAgentRename = func(src, dst string) error {
		renameCalls++
		return os.Rename(src, dst)
	}
	harnessAssetAgentRemoveAll = func(path string) error {
		removeCalls++
		return os.RemoveAll(path)
	}

	result, err := syncHarnessAssets(HarnessClaude)
	if err != nil {
		t.Fatalf("syncHarnessAssets(create): %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("create result = %+v, want 1 added", result)
	}
	if ensureCalls == 0 || writeCalls == 0 || renameCalls == 0 {
		t.Fatalf("agent backend calls ensure=%d write=%d rename=%d, want all non-zero", ensureCalls, writeCalls, renameCalls)
	}
	raw, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("read dest file: %v", err)
	}
	if string(raw) != "review\n" {
		t.Fatalf("dest content = %q", raw)
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
	if removeCalls == 0 {
		t.Fatal("agent remove backend was not called")
	}
	if _, err := os.Stat(destFile); !os.IsNotExist(err) {
		t.Fatalf("dest file still exists after agent-backed delete: %v", err)
	}
}

func TestSyncHarnessAssetsWarnsWhenAgentInstallFails(t *testing.T) {
	env := isolateHarnessAssets(t)

	hostRoot := filepath.Join(env.hostHome, ".claude", "skills")
	destRoot := filepath.Join(env.agentHome, ".claude", "skills")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "skills", Kind: harnessAssetDirRoot, HostPath: hostRoot, AgentPath: destRoot},
	}
	writeHarnessAssetTestFile(t, filepath.Join(hostRoot, "planning-with-files", "SKILL.md"), "skill\n")

	harnessAssetUseAgentBackend = func(path string) bool {
		clean := filepath.Clean(path)
		return clean == env.agentHome || strings.HasPrefix(clean, env.agentHome+string(os.PathSeparator))
	}
	harnessAssetAgentEnsureDir = func(string, os.FileMode) error {
		return errors.New("agent cannot write parent")
	}

	result, err := syncHarnessAssets(HarnessClaude)
	if err != nil {
		t.Fatalf("syncHarnessAssets: %v", err)
	}
	if result.Conflicts != 1 || len(result.Warnings) != 1 {
		t.Fatalf("result = %+v, want one skipped warning", result)
	}
	if !strings.Contains(result.Warnings[0], "agent cannot write parent") {
		t.Fatalf("warning = %q, want install failure", result.Warnings[0])
	}
	if _, err := os.Stat(filepath.Join(destRoot, "planning-with-files", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("failed asset should not be materialized, err=%v", err)
	}
}

func TestDefaultHarnessAssetAgentEnsureDirOnlyCreatesDirectory(t *testing.T) {
	var calls [][]string
	savedNewAgentCommand := newAgentCommand
	newAgentCommand = func(args ...string) *exec.Cmd {
		calls = append(calls, append([]string(nil), args...))
		return exec.Command("/usr/bin/true")
	}
	t.Cleanup(func() { newAgentCommand = savedNewAgentCommand })

	savedAgentDirWritable := harnessAssetAgentDirWritable
	harnessAssetAgentDirWritable = func(string) (bool, error) {
		return true, nil
	}
	t.Cleanup(func() { harnessAssetAgentDirWritable = savedAgentDirWritable })

	savedAgentRepairDir := harnessAssetAgentRepairDir
	harnessAssetAgentRepairDir = func(path string, mode os.FileMode) error {
		t.Fatalf("unexpected repair of %s with mode %s", path, mode)
		return nil
	}
	t.Cleanup(func() { harnessAssetAgentRepairDir = savedAgentRepairDir })

	path := agentHome + "/.claude/skills/.planning-with-files.hazmat-test"
	if err := defaultHarnessAssetAgentEnsureDir(path, 0o2770); err != nil {
		t.Fatalf("defaultHarnessAssetAgentEnsureDir: %v", err)
	}

	want := [][]string{
		{"/bin/mkdir", "-p", path},
	}
	if len(calls) != len(want) {
		t.Fatalf("agent calls = %v, want %v", calls, want)
	}
	for i := range want {
		if strings.Join(calls[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Fatalf("agent call %d = %v, want %v", i, calls[i], want[i])
		}
	}
}

func TestDefaultHarnessAssetAgentEnsureDirRepairsExistingUnwritableDirectory(t *testing.T) {
	savedNewAgentCommand := newAgentCommand
	var mkdirCalls int
	newAgentCommand = func(args ...string) *exec.Cmd {
		if len(args) == 0 || args[0] != "/bin/mkdir" {
			t.Fatalf("unexpected agent command: %v", args)
		}
		mkdirCalls++
		return exec.Command("/usr/bin/true")
	}
	t.Cleanup(func() { newAgentCommand = savedNewAgentCommand })

	savedAgentDirWritable := harnessAssetAgentDirWritable
	var writableCalls int
	harnessAssetAgentDirWritable = func(path string) (bool, error) {
		writableCalls++
		return writableCalls > 1, nil
	}
	t.Cleanup(func() { harnessAssetAgentDirWritable = savedAgentDirWritable })

	savedAgentRepairDir := harnessAssetAgentRepairDir
	var repairedPath string
	var repairedMode os.FileMode
	harnessAssetAgentRepairDir = func(path string, mode os.FileMode) error {
		repairedPath = path
		repairedMode = mode
		return nil
	}
	t.Cleanup(func() { harnessAssetAgentRepairDir = savedAgentRepairDir })

	path := agentHome + "/.claude/skills"
	if err := defaultHarnessAssetAgentEnsureDir(path, 0o2770); err != nil {
		t.Fatalf("defaultHarnessAssetAgentEnsureDir: %v", err)
	}
	if mkdirCalls != 1 {
		t.Fatalf("mkdirCalls = %d, want 1", mkdirCalls)
	}
	if writableCalls != 2 {
		t.Fatalf("writableCalls = %d, want 2", writableCalls)
	}
	if repairedPath != path || repairedMode != 0o2770 {
		t.Fatalf("repair = (%q, %s), want (%q, 2770)", repairedPath, repairedMode, path)
	}
}

func TestDefaultHarnessAssetAgentEnsureDirRepairsParentAfterMkdirFailure(t *testing.T) {
	savedNewAgentCommand := newAgentCommand
	var mkdirCalls int
	newAgentCommand = func(args ...string) *exec.Cmd {
		if len(args) == 0 || args[0] != "/bin/mkdir" {
			t.Fatalf("unexpected agent command: %v", args)
		}
		mkdirCalls++
		if mkdirCalls == 1 {
			return exec.Command("/usr/bin/false")
		}
		return exec.Command("/usr/bin/true")
	}
	t.Cleanup(func() { newAgentCommand = savedNewAgentCommand })

	savedAgentDirWritable := harnessAssetAgentDirWritable
	harnessAssetAgentDirWritable = func(string) (bool, error) {
		return true, nil
	}
	t.Cleanup(func() { harnessAssetAgentDirWritable = savedAgentDirWritable })

	savedAgentRepairDir := harnessAssetAgentRepairDir
	var repairedPath string
	var repairedMode os.FileMode
	harnessAssetAgentRepairDir = func(path string, mode os.FileMode) error {
		repairedPath = path
		repairedMode = mode
		return nil
	}
	t.Cleanup(func() { harnessAssetAgentRepairDir = savedAgentRepairDir })

	path := agentHome + "/.claude/skills/.planning-with-files.hazmat-test"
	if err := defaultHarnessAssetAgentEnsureDir(path, 0o2770); err != nil {
		t.Fatalf("defaultHarnessAssetAgentEnsureDir: %v", err)
	}
	if mkdirCalls != 2 {
		t.Fatalf("mkdirCalls = %d, want 2", mkdirCalls)
	}
	wantParent := filepath.Dir(path)
	if repairedPath != wantParent || repairedMode != 0o2770 {
		t.Fatalf("repair = (%q, %s), want (%q, 2770)", repairedPath, repairedMode, wantParent)
	}
}

func TestSyncHarnessAssetsWithPreviewReusesDesiredStateWhenSourcesUnchanged(t *testing.T) {
	env := isolateHarnessAssets(t)

	hostFile := filepath.Join(env.hostHome, ".claude", "CLAUDE.md")
	destFile := filepath.Join(env.agentHome, ".claude", "CLAUDE.md")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "claude-md", Kind: harnessAssetFileRoot, HostPath: hostFile, AgentPath: destFile},
	}
	writeHarnessAssetTestFile(t, hostFile, "preview content\n")

	realCollect := collectDesiredHarnessAssets
	oldCollect := collectDesiredHarnessAssetsForSync
	var collectCalls int
	collectDesiredHarnessAssetsForSync = func(harnessID HarnessID) (map[string]harnessAssetDesiredEntry, []string, error) {
		collectCalls++
		return realCollect(harnessID)
	}
	t.Cleanup(func() {
		collectDesiredHarnessAssetsForSync = oldCollect
	})

	preview, err := previewHarnessAssetSync(HarnessClaude)
	if err != nil {
		t.Fatalf("previewHarnessAssetSync: %v", err)
	}
	if collectCalls != 1 {
		t.Fatalf("collectDesiredHarnessAssetsForSync calls after preview = %d, want 1", collectCalls)
	}

	result, err := syncHarnessAssetsWithPreview(HarnessClaude, preview)
	if err != nil {
		t.Fatalf("syncHarnessAssetsWithPreview: %v", err)
	}
	if collectCalls != 1 {
		t.Fatalf("collectDesiredHarnessAssetsForSync calls after apply = %d, want preview reuse", collectCalls)
	}
	if result.Added != 1 || result.Updated != 0 {
		t.Fatalf("result = %+v, want 1 added", result)
	}

	raw, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("read dest file: %v", err)
	}
	if string(raw) != "preview content\n" {
		t.Fatalf("dest content = %q", string(raw))
	}
}

func TestSyncHarnessAssetsWithPreviewFallsBackWhenSourcesChange(t *testing.T) {
	env := isolateHarnessAssets(t)

	hostFile := filepath.Join(env.hostHome, ".claude", "CLAUDE.md")
	destFile := filepath.Join(env.agentHome, ".claude", "CLAUDE.md")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "claude-md", Kind: harnessAssetFileRoot, HostPath: hostFile, AgentPath: destFile},
	}
	writeHarnessAssetTestFile(t, hostFile, "initial preview\n")

	realCollect := collectDesiredHarnessAssets
	oldCollect := collectDesiredHarnessAssetsForSync
	var collectCalls int
	collectDesiredHarnessAssetsForSync = func(harnessID HarnessID) (map[string]harnessAssetDesiredEntry, []string, error) {
		collectCalls++
		return realCollect(harnessID)
	}
	t.Cleanup(func() {
		collectDesiredHarnessAssetsForSync = oldCollect
	})

	preview, err := previewHarnessAssetSync(HarnessClaude)
	if err != nil {
		t.Fatalf("previewHarnessAssetSync: %v", err)
	}
	if collectCalls != 1 {
		t.Fatalf("collectDesiredHarnessAssetsForSync calls after preview = %d, want 1", collectCalls)
	}

	time.Sleep(10 * time.Millisecond)
	writeHarnessAssetTestFile(t, hostFile, "updated before apply\n")

	result, err := syncHarnessAssetsWithPreview(HarnessClaude, preview)
	if err != nil {
		t.Fatalf("syncHarnessAssetsWithPreview: %v", err)
	}
	if collectCalls != 2 {
		t.Fatalf("collectDesiredHarnessAssetsForSync calls after changed source = %d, want 2", collectCalls)
	}
	if result.Added != 1 || result.Updated != 0 {
		t.Fatalf("result = %+v, want 1 added after fallback recompute", result)
	}

	raw, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("read dest file: %v", err)
	}
	if string(raw) != "updated before apply\n" {
		t.Fatalf("dest content = %q", string(raw))
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

func TestSyncHarnessAssetsSkipsCodexHostStateDenyRoot(t *testing.T) {
	env := isolateHarnessAssets(t)
	t.Setenv("HOME", env.hostHome)

	hostRoot := filepath.Join(env.hostHome, ".codex")
	destRoot := filepath.Join(env.agentHome, ".codex")
	harnessAssetSpecs[HarnessCodex] = []harnessAssetSpec{
		{Harness: HarnessCodex, Key: "codex-root", Kind: harnessAssetDirRoot, HostPath: hostRoot, AgentPath: destRoot},
	}
	writeHarnessAssetTestFile(t, filepath.Join(hostRoot, "sqlite", "codex-dev.db"), "private\n")

	result, err := syncHarnessAssets(HarnessCodex)
	if err != nil {
		t.Fatalf("syncHarnessAssets(host-state): %v", err)
	}
	if result.Added != 0 {
		t.Fatalf("host-state result = %+v, want 0 added", result)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "host-state deny zone") {
		t.Fatalf("warnings = %v, want host-state deny warning", result.Warnings)
	}
}

func TestSyncHarnessAssetsAllowsNarrowCodexPromptAssets(t *testing.T) {
	env := isolateHarnessAssets(t)
	t.Setenv("HOME", env.hostHome)

	hostRoot := filepath.Join(env.hostHome, ".codex", "prompts")
	destRoot := filepath.Join(env.agentHome, ".codex", "prompts")
	harnessAssetSpecs[HarnessCodex] = []harnessAssetSpec{
		{Harness: HarnessCodex, Key: "prompts", Kind: harnessAssetDirRoot, HostPath: hostRoot, AgentPath: destRoot},
	}
	writeHarnessAssetTestFile(t, filepath.Join(hostRoot, "review.md"), "review\n")

	result, err := syncHarnessAssets(HarnessCodex)
	if err != nil {
		t.Fatalf("syncHarnessAssets(prompts): %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("prompt result = %+v, want 1 added", result)
	}
	if _, err := os.Stat(filepath.Join(destRoot, "review.md")); err != nil {
		t.Fatalf("prompt destination should exist: %v", err)
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

func TestHarnessAssetSessionMutationTargetsSessionHome(t *testing.T) {
	env := isolateHarnessAssets(t)
	isolateConfig(t)

	hostFile := filepath.Join(env.hostHome, ".claude", "CLAUDE.md")
	persistentDest := filepath.Join(env.agentHome, ".claude", "CLAUDE.md")
	harnessAssetSpecs[HarnessClaude] = []harnessAssetSpec{
		{Harness: HarnessClaude, Key: "claude-md", Kind: harnessAssetFileRoot, HostPath: hostFile, AgentPath: persistentDest},
	}
	writeHarnessAssetTestFile(t, hostFile, "session-home asset\n")

	layout, err := newSessionHomeLayout(filepath.Join(t.TempDir(), "hazmat-home"), "session-123")
	if err != nil {
		t.Fatalf("newSessionHomeLayout: %v", err)
	}
	runtimePlan := sessionHomeRuntimePlan{
		Launch: sessionHomeLaunchPlan{
			Layout: layout,
		},
	}
	plan, err := buildHarnessAssetSessionMutationPlan(sessionConfig{
		SessionHome: &runtimePlan,
	}, "claude", sessionModeNative, harnessSessionOpts{})
	if err != nil {
		t.Fatalf("buildHarnessAssetSessionMutationPlan: %v", err)
	}
	if len(plan.Mutations) != 1 {
		t.Fatalf("mutations = %d, want 1", len(plan.Mutations))
	}
	mutation := plan.Mutations[0]
	if mutation.Metadata.Persistence != "session-local home only" {
		t.Fatalf("persistence = %q, want session-local", mutation.Metadata.Persistence)
	}
	if !strings.Contains(mutation.Metadata.Detail, layout.Home) {
		t.Fatalf("detail = %q, want session home path", mutation.Metadata.Detail)
	}

	exec, err := mutation.Apply()
	if err != nil {
		t.Fatalf("apply harness asset mutation: %v", err)
	}
	if exec.AppliedMessage == "" {
		t.Fatal("AppliedMessage is empty, want sync confirmation")
	}

	sessionDest := filepath.Join(layout.Home, ".claude", "CLAUDE.md")
	raw, err := os.ReadFile(sessionDest)
	if err != nil {
		t.Fatalf("read session-local asset: %v", err)
	}
	if string(raw) != "session-home asset\n" {
		t.Fatalf("session-local asset = %q", raw)
	}
	if _, err := os.Stat(persistentDest); !os.IsNotExist(err) {
		t.Fatalf("persistent agent asset should not be materialized, err=%v", err)
	}
	if _, err := os.Stat(harnessAssetsFilePath); !os.IsNotExist(err) {
		t.Fatalf("session-local asset sync should not write persistent manifest, err=%v", err)
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

func TestSummarizeHarnessAssetWarningsFormatsEscapesActionably(t *testing.T) {
	warnings := []string{
		"skipped /Users/dr/.claude/commands/a.md: resolved path /Users/dr/workspace/a.md escapes the allowed root /Users/dr/.claude/commands",
		"skipped /Users/dr/.claude/commands/b.md: resolved path /Users/dr/workspace/b.md escapes the allowed root /Users/dr/.claude/commands",
		"skipped /Users/dr/.claude/commands/c.md: resolved path /Users/dr/workspace/c.md escapes the allowed root /Users/dr/.claude/commands",
		"skipped /Users/dr/.claude/commands/d.md: resolved path /Users/dr/workspace/d.md escapes the allowed root /Users/dr/.claude/commands",
		"skipped /Users/dr/.claude/commands/e.md: resolved path /Users/dr/workspace/e.md escapes the allowed root /Users/dr/.claude/commands",
		"skipped /Users/dr/.claude/commands/f.md: resolved path /Users/dr/workspace/f.md escapes the allowed root /Users/dr/.claude/commands",
	}

	got := summarizeHarnessAssetWarnings(warnings)
	for _, want := range []string{
		"6 harness assets were not copied",
		"symlink targets leave the managed source root",
		"Showing 5",
		"- 1 more omitted",
		"--skip-harness-assets-sync",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "+4 more") {
		t.Fatalf("summary still uses collapsed warning format: %q", got)
	}
}

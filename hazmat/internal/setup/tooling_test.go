package setup

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestManagedBlockRoundTrip(t *testing.T) {
	original := "export FOO=1\nexport BAR=2\n"
	updated := UpsertManagedBlock(original, "start", "end", "export PATH=new")

	if got := RemoveManagedBlock(updated, "start", "end"); got != original {
		t.Fatalf("round-trip mismatch:\nwant %q\ngot  %q", original, got)
	}
}

func TestHostWrapperContentPinsExecutable(t *testing.T) {
	got := HostWrapperContent("/opt/hazmat/bin/hazmat", "shell")

	for _, want := range []string{
		`HAZMAT_BIN="/opt/hazmat/bin/hazmat"`,
		`exec "$HAZMAT_BIN" shell "$@"`,
		`Setup drift detected: refresh Hazmat-owned wrappers with "hazmat doctor --fix".`,
		`Preview the repair plan with "hazmat doctor --dry-run".`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("wrapper content missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "hazmat init") {
		t.Fatalf("wrapper content routes runtime drift back to init:\n%s", got)
	}
	if strings.Contains(got, "Preview first") {
		t.Fatalf("wrapper content makes dry-run the first step for setup drift:\n%s", got)
	}
}

func TestSetupUserExperienceWritesEnvWrappersAndPathBlock(t *testing.T) {
	tmp := t.TempDir()
	env := testToolingEnv(t, tmp)
	runner := newFakeToolingRunner(t)
	agentZshrc := filepath.Join(env.AgentHome, ".zshrc")
	runner.agentOutput[agentZshrc] = "export EXISTING=1\n"
	ui := &fakeToolingUI{}

	if err := SetupUserExperience(env, ui, runner); err != nil {
		t.Fatalf("SetupUserExperience: %v", err)
	}

	if got := runner.sudoWrites[env.AgentEnvPath]; !strings.Contains(got, `export PATH="/agent/bin:/usr/bin"`) {
		t.Fatalf("agent env content = %q, want default agent PATH", got)
	}

	if got := runner.sudoWrites[agentZshrc]; !strings.Contains(got, `[[ -f "$HOME/.config/hazmat/agent-env.zsh" ]] && source "$HOME/.config/hazmat/agent-env.zsh"`) ||
		!strings.Contains(got, "export EXISTING=1") {
		t.Fatalf("agent zshrc content missing shell bootstrap:\n%s", got)
	}
	assertNoSudoOutputForPath(t, runner, agentZshrc)

	for _, name := range []string{env.HostClaudeWrapperName, env.HostExecWrapperName, env.HostShellWrapperName} {
		path := filepath.Join(env.HostWrapperDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read wrapper %s: %v", path, err)
		}
		if !strings.Contains(string(data), `HAZMAT_BIN="/opt/hazmat/bin/hazmat"`) {
			t.Fatalf("wrapper %s did not pin resolved hazmat binary:\n%s", path, string(data))
		}
	}

	rcData, err := os.ReadFile(env.ShellProfiles[0].RCPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rcData), env.UserPathBlockStart) ||
		!strings.Contains(string(rcData), `export PATH="$HOME/.local/bin:$PATH"`) {
		t.Fatalf("rc file missing hazmat PATH block:\n%s", string(rcData))
	}
}

func TestEnsureAgentToolchainDirsRepairsParentsBeforeChildren(t *testing.T) {
	env := testToolingEnv(t, t.TempDir())
	runner := newFakeToolingRunner(t)

	if err := EnsureAgentToolchainDirs(env, runner); err != nil {
		t.Fatalf("EnsureAgentToolchainDirs: %v", err)
	}

	// All toolchain dirs must be created in a single privileged call so the user
	// is prompted for sudo at most once, not once per directory.
	if calls := installDirCalls(runner.sudoCalls); len(calls) != 1 {
		t.Fatalf("expected one batched install -d call, got %d", len(calls))
	}

	got := installDirPaths(runner.sudoCalls)
	want := []string{
		env.DefaultAgentCacheHome,
		env.DefaultAgentConfigHome,
		filepath.Dir(env.AgentEnvPath),
		filepath.Join(env.AgentHome, ".local"),
		filepath.Join(env.AgentHome, ".local", "bin"),
		filepath.Join(env.AgentHome, ".local", "lib"),
		env.DefaultAgentDataHome,
		env.DefaultAgentStateHome,
		filepath.Join(env.AgentHome, ".npm"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("agent toolchain dirs:\nwant %q\ngot  %q", want, got)
	}

	assertPathBefore(t, got, filepath.Join(env.AgentHome, ".local"), env.DefaultAgentStateHome)
	assertPathBefore(t, got, env.DefaultAgentConfigHome, filepath.Dir(env.AgentEnvPath))
}

func TestAgentToolchainDirsCoverAgentWritableSetupParents(t *testing.T) {
	for name, home := range map[string]string{
		"darwin": "/Users/agent",
		"linux":  "/home/agent",
	} {
		t.Run(name, func(t *testing.T) {
			env := testToolingEnvForHome(t, t.TempDir(), home)
			repairTargets := pathSet(agentToolchainDirs(env))
			for _, path := range requiredAgentWritableSetupParents(env) {
				if !repairTargets[filepath.Clean(path)] {
					t.Fatalf("agentToolchainDirs missing writable setup parent %q; targets=%v", path, sortedPathSet(repairTargets))
				}
			}
		})
	}
}

func TestRollbackUserExperienceRemovesManagedBlocksAndHostWrappers(t *testing.T) {
	tmp := t.TempDir()
	env := testToolingEnv(t, tmp)
	runner := newFakeToolingRunner(t)
	ui := &fakeToolingUI{}

	for _, name := range []string{env.HostClaudeWrapperName, env.HostExecWrapperName, env.HostShellWrapperName} {
		path := filepath.Join(env.HostWrapperDir, name)
		if err := os.WriteFile(path, []byte("wrapper\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	agentZshrc := filepath.Join(env.AgentHome, ".zshrc")
	runner.agentOutput[agentZshrc] = UpsertManagedBlock("export KEEP=1\n", env.AgentShellBlockStart, env.AgentShellBlockEnd, "source env")
	if err := os.WriteFile(env.ShellProfiles[0].RCPath, []byte(UpsertManagedBlock("export KEEP=1\n", env.UserPathBlockStart, env.UserPathBlockEnd, "export PATH=hazmat")), 0o644); err != nil {
		t.Fatal(err)
	}

	RollbackUserExperience(env, ui, runner)

	for _, name := range []string{env.HostClaudeWrapperName, env.HostExecWrapperName, env.HostShellWrapperName} {
		if _, err := os.Stat(filepath.Join(env.HostWrapperDir, name)); !os.IsNotExist(err) {
			t.Fatalf("host wrapper %s still exists or stat failed: %v", name, err)
		}
	}

	if got := runner.sudoWrites[agentZshrc]; strings.Contains(got, env.AgentShellBlockStart) || !strings.Contains(got, "export KEEP=1") {
		t.Fatalf("agent zshrc cleanup = %q, want managed block removed and surrounding content preserved", got)
	}
	rcData, err := os.ReadFile(env.ShellProfiles[0].RCPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rcData), env.UserPathBlockStart) || !strings.Contains(string(rcData), "export KEEP=1") {
		t.Fatalf("rc cleanup = %q, want managed block removed and surrounding content preserved", string(rcData))
	}
}

func testToolingEnv(t *testing.T, tmp string) ToolingEnv {
	t.Helper()
	return testToolingEnvForHome(t, tmp, "/Users/agent")
}

func testToolingEnvForHome(t *testing.T, tmp, agentHome string) ToolingEnv {
	t.Helper()
	hostWrapperDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(hostWrapperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rcPath := filepath.Join(tmp, ".zshrc")
	if err := os.WriteFile(rcPath, []byte("export KEEP=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ToolingEnv{
		AgentUser:              "agent",
		AgentHome:              agentHome,
		SeatbeltProfileDir:     filepath.Join(agentHome, ".config", "hazmat"),
		SeatbeltWrapperPath:    filepath.Join(agentHome, ".local", "bin", "claude-sandboxed"),
		SeatbeltWrapper:        "#!/bin/bash\n",
		AgentEnvPath:           filepath.Join(agentHome, ".config", "hazmat", "agent-env.zsh"),
		DefaultAgentPath:       "/agent/bin:/usr/bin",
		DefaultAgentCacheHome:  filepath.Join(agentHome, ".cache"),
		DefaultAgentConfigHome: filepath.Join(agentHome, ".config"),
		DefaultAgentDataHome:   filepath.Join(agentHome, ".local", "share"),
		DefaultAgentStateHome:  filepath.Join(agentHome, ".local", "state"),
		HostWrapperDir:         hostWrapperDir,
		HostClaudeWrapperName:  "claude-hazmat",
		HostExecWrapperName:    "agent-exec",
		HostShellWrapperName:   "agent-shell",
		AgentShellBlockStart:   "# >>> hazmat agent shell >>>",
		AgentShellBlockEnd:     "# <<< hazmat agent shell <<<",
		UserPathBlockStart:     "# >>> hazmat user path >>>",
		UserPathBlockEnd:       "# <<< hazmat user path <<<",
		UmaskBlockStart:        "# >>> hazmat umask >>>",
		UmaskBlockEnd:          "# <<< hazmat umask <<<",
		ShellName:              "zsh",
		ShellProfiles: []ShellProfile{
			{
				Name:           "zsh",
				RCPath:         rcPath,
				PathBlockLines: []string{`export PATH="$HOME/.local/bin:$PATH"`},
			},
		},
		Executable: func() (string, error) {
			return filepath.Join(tmp, "hazmat-link"), nil
		},
		EvalSymlinks: func(string) (string, error) {
			return "/opt/hazmat/bin/hazmat", nil
		},
	}
}

func requiredAgentWritableSetupParents(env ToolingEnv) []string {
	agentWrittenPaths := []string{
		env.AgentEnvPath,
		filepath.Join(env.AgentHome, ".zshrc"),
		filepath.Join(env.AgentHome, ".npmrc"),
		filepath.Join(env.AgentHome, ".config", "pip", "pip.conf"),
		filepath.Join(env.AgentHome, ".local", "bin", "claude"),
		filepath.Join(env.AgentHome, ".local", "bin", "claude-sandboxed"),
		filepath.Join(env.AgentHome, ".local", "bin", "codex"),
		filepath.Join(env.AgentHome, ".local", "bin", "agy"),
		filepath.Join(env.AgentHome, ".local", "bin", "hermes"),
		filepath.Join(env.AgentHome, ".local", "bin", "opencode"),
		filepath.Join(env.AgentHome, ".local", "bin", "qwen"),
		filepath.Join(env.AgentHome, ".local", "lib", "node_modules"),
		filepath.Join(env.AgentHome, ".local", "share", "opencode", "auth.json"),
		filepath.Join(env.AgentHome, ".npm", "_cacache"),
	}
	parents := []string{
		env.DefaultAgentCacheHome,
		env.DefaultAgentConfigHome,
		env.DefaultAgentDataHome,
		env.DefaultAgentStateHome,
		filepath.Join(env.AgentHome, ".local"),
	}
	for _, path := range agentWrittenPaths {
		parent := firstRepairTargetForAgentPath(env.AgentHome, path)
		if parent == filepath.Clean(env.AgentHome) {
			continue
		}
		parents = append(parents, parent)
	}
	return compactUniquePaths(parents)
}

func firstRepairTargetForAgentPath(home, path string) string {
	home = filepath.Clean(home)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(home, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Dir(path)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) == 0 || parts[0] == "." {
		return home
	}
	switch parts[0] {
	case ".cache", ".config", ".npm":
		return filepath.Join(home, parts[0])
	case ".local":
		if len(parts) >= 2 {
			return filepath.Join(home, ".local", parts[1])
		}
		return filepath.Join(home, ".local")
	default:
		return filepath.Dir(path)
	}
}

func pathSet(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, path := range paths {
		out[filepath.Clean(path)] = true
	}
	return out
}

func sortedPathSet(paths map[string]bool) []string {
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

type fakeToolingUI struct{}

func (fakeToolingUI) Step(string)     {}
func (fakeToolingUI) SkipDone(string) {}
func (fakeToolingUI) WarnMsg(string)  {}
func (fakeToolingUI) Ok(string)       {}

type fakeToolingRunner struct {
	t               *testing.T
	sudoOutput      map[string]string
	agentOutput     map[string]string
	sudoWrites      map[string]string
	sudoCalls       [][]string
	sudoOutputCalls [][]string
	agentCalls      [][]string
}

func newFakeToolingRunner(t *testing.T) *fakeToolingRunner {
	t.Helper()
	return &fakeToolingRunner{
		t:           t,
		sudoOutput:  make(map[string]string),
		agentOutput: make(map[string]string),
		sudoWrites:  make(map[string]string),
	}
}

func (r *fakeToolingRunner) Sudo(_ string, args ...string) error {
	r.sudoCalls = append(r.sudoCalls, append([]string(nil), args...))
	return nil
}

func (r *fakeToolingRunner) SudoOutput(args ...string) (string, error) {
	r.sudoOutputCalls = append(r.sudoOutputCalls, append([]string(nil), args...))
	if len(args) == 2 && args[0] == "cat" {
		return r.sudoOutput[args[1]], nil
	}
	return "", nil
}

func (r *fakeToolingRunner) SudoWriteFile(_ string, path, content string) error {
	r.sudoWrites[path] = content
	return nil
}

func (r *fakeToolingRunner) AgentOutput(args ...string) (string, error) {
	r.agentCalls = append(r.agentCalls, append([]string(nil), args...))
	if len(args) == 2 && args[0] == "cat" {
		return r.agentOutput[args[1]], nil
	}
	return "", nil
}

func (r *fakeToolingRunner) UserWriteFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func (r *fakeToolingRunner) MkdirAll(path string, mode os.FileMode) error {
	return os.MkdirAll(path, mode)
}

func (r *fakeToolingRunner) Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}

func assertNoSudoOutputForPath(t *testing.T, runner *fakeToolingRunner, path string) {
	t.Helper()
	for _, call := range runner.sudoOutputCalls {
		if len(call) == 2 && call[0] == "cat" && call[1] == path {
			t.Fatalf("sudo output read %s; agent-owned files must be read through AgentOutput", path)
		}
	}
}

func installDirPaths(calls [][]string) []string {
	var paths []string
	for _, call := range installDirCalls(calls) {
		paths = append(paths, call[8:]...)
	}
	return paths
}

func installDirCalls(calls [][]string) [][]string {
	var matched [][]string
	for _, call := range calls {
		if len(call) >= 8 &&
			call[0] == "install" &&
			call[1] == "-d" &&
			call[2] == "-o" &&
			call[4] == "-g" &&
			call[6] == "-m" {
			matched = append(matched, call)
		}
	}
	return matched
}

func assertPathBefore(t *testing.T, paths []string, before, after string) {
	t.Helper()
	beforeIndex, afterIndex := -1, -1
	for i, path := range paths {
		switch path {
		case before:
			beforeIndex = i
		case after:
			afterIndex = i
		}
	}
	if beforeIndex < 0 || afterIndex < 0 || beforeIndex >= afterIndex {
		t.Fatalf("path order = %v, want %s before %s", paths, before, after)
	}
}

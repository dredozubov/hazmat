package setup

import (
	"os"
	"path/filepath"
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
		`Re-run "hazmat init" to refresh the wrappers.`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("wrapper content missing %q:\n%s", want, got)
		}
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
	hostWrapperDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(hostWrapperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rcPath := filepath.Join(tmp, ".zshrc")
	if err := os.WriteFile(rcPath, []byte("export KEEP=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ToolingEnv{
		AgentUser:             "agent",
		AgentHome:             "/Users/agent",
		SeatbeltProfileDir:    "/Users/agent/.config/hazmat",
		SeatbeltWrapperPath:   "/Users/agent/.local/bin/claude-sandboxed",
		SeatbeltWrapper:       "#!/bin/bash\n",
		AgentEnvPath:          "/Users/agent/.config/hazmat/agent-env.zsh",
		DefaultAgentPath:      "/agent/bin:/usr/bin",
		DefaultAgentCacheHome: "/Users/agent/.cache",
		DefaultAgentDataHome:  "/Users/agent/.local/share",
		HostWrapperDir:        hostWrapperDir,
		HostClaudeWrapperName: "claude-hazmat",
		HostExecWrapperName:   "agent-exec",
		HostShellWrapperName:  "agent-shell",
		AgentShellBlockStart:  "# >>> hazmat agent shell >>>",
		AgentShellBlockEnd:    "# <<< hazmat agent shell <<<",
		UserPathBlockStart:    "# >>> hazmat user path >>>",
		UserPathBlockEnd:      "# <<< hazmat user path <<<",
		UmaskBlockStart:       "# >>> hazmat umask >>>",
		UmaskBlockEnd:         "# <<< hazmat umask <<<",
		ShellName:             "zsh",
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

func (r *fakeToolingRunner) Sudo(string, ...string) error {
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

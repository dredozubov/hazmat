//go:build hazmat_smoke_fixture

package hazmat

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var hermeticSmokeHarnesses = []HarnessID{
	HarnessClaude,
	HarnessCodex,
	HarnessOpenCode,
	HarnessGemini,
	HarnessHermes,
	HarnessQwen,
	HarnessCursorAgent,
}

type hermeticHarnessSmoke struct {
	t            *testing.T
	root         string
	hostHome     string
	agentHome    string
	project      string
	forbiddenBin string
	sudoMarker   string
	outputs      map[HarnessID]string
}

func TestHermeticHarnessSmoke(t *testing.T) {
	smoke := newHermeticHarnessSmoke(t)
	smoke.installTestSeams()
	smoke.assertManagedHarnessCoverage()
	smoke.seedProviderSecrets()

	smoke.runHermes()
	smoke.runClaude()
	smoke.runCodex()
	smoke.runOpenCode()
	smoke.runGemini()
	smoke.runQwen()
	smoke.runCursorAgent()

	smoke.assertNoSudo()
}

func newHermeticHarnessSmoke(t *testing.T) *hermeticHarnessSmoke {
	t.Helper()

	root := t.TempDir()
	smoke := &hermeticHarnessSmoke{
		t:            t,
		root:         root,
		hostHome:     filepath.Join(root, "host-home"),
		agentHome:    filepath.Join(root, "agent-home"),
		project:      filepath.Join(root, "project"),
		forbiddenBin: filepath.Join(root, "forbidden-bin"),
		sudoMarker:   filepath.Join(root, "sudo-invoked"),
		outputs:      make(map[HarnessID]string),
	}
	for _, dir := range []string{
		smoke.hostHome,
		smoke.agentHome,
		smoke.project,
		filepath.Join(root, "tmp"),
		filepath.Join(root, "cache"),
		smoke.forbiddenBin,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create fixture dir %s: %v", dir, err)
		}
	}
	if externalForbiddenBin := os.Getenv("HAZMAT_SMOKE_FORBIDDEN_BIN"); externalForbiddenBin != "" {
		smoke.forbiddenBin = externalForbiddenBin
	}
	smoke.writeExecutable(filepath.Join(smoke.forbiddenBin, "sudo"), fmt.Sprintf(`#!/bin/sh
echo "sudo $*" >> %q
exit 97
`, smoke.sudoMarker))

	t.Setenv("HOME", smoke.hostHome)
	t.Setenv("TMPDIR", filepath.Join(root, "tmp"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("PATH", smoke.agentPath())
	return smoke
}

func (s *hermeticHarnessSmoke) installTestSeams() {
	t := s.t

	savedRequireInit := requireInit
	requireInit = func() error { return nil }
	t.Cleanup(func() { requireInit = savedRequireInit })

	savedNewAgentCommand := newAgentCommand
	newAgentCommand = s.newAgentCommand
	t.Cleanup(func() { newAgentCommand = savedNewAgentCommand })

	savedSupportsSessionTemp := launchHelperSupportsSessionTemp
	launchHelperSupportsSessionTemp = func(string) bool { return false }
	t.Cleanup(func() { launchHelperSupportsSessionTemp = savedSupportsSessionTemp })

	savedAgentPathForDirectIO := agentPathForDirectIO
	agentPathForDirectIO = s.mapAgentPath
	t.Cleanup(func() { agentPathForDirectIO = savedAgentPathForDirectIO })

	savedPrepareSessionRuntime := prepareSessionRuntime
	prepareSessionRuntime = s.prepareSessionRuntime
	t.Cleanup(func() { prepareSessionRuntime = savedPrepareSessionRuntime })

	savedRunAgentSeatbeltScriptWithPlan := runAgentSeatbeltScriptWithPlan
	runAgentSeatbeltScriptWithPlan = s.runAgentSeatbeltScriptWithPlan
	t.Cleanup(func() { runAgentSeatbeltScriptWithPlan = savedRunAgentSeatbeltScriptWithPlan })

	savedExecuteSessionMutationPlan := executeSessionMutationPlan
	executeSessionMutationPlan = s.executeSessionMutationPlan
	t.Cleanup(func() { executeSessionMutationPlan = savedExecuteSessionMutationPlan })

	savedEnsureHermesStateRoot := ensureHermesStateRoot
	ensureHermesStateRoot = s.ensureHermesStateRoot
	t.Cleanup(func() { ensureHermesStateRoot = savedEnsureHermesStateRoot })

	savedHostSudoPath := hostSudoPath
	hostSudoPath = filepath.Join(s.forbiddenBin, "sudo")
	t.Cleanup(func() { hostSudoPath = savedHostSudoPath })

	savedAgentZshrcPath := agentZshrcPath
	agentZshrcPath = filepath.Join(s.agentHome, ".zshrc")
	t.Cleanup(func() { agentZshrcPath = savedAgentZshrcPath })

	savedConfigFilePath := configFilePath
	configFilePath = filepath.Join(s.hostHome, ".hazmat", "config.yaml")
	t.Cleanup(func() { configFilePath = savedConfigFilePath })

	savedStateFilePath := stateFilePath
	stateFilePath = filepath.Join(s.hostHome, ".hazmat", "state.json")
	t.Cleanup(func() { stateFilePath = savedStateFilePath })
}

func (s *hermeticHarnessSmoke) assertManagedHarnessCoverage() {
	s.t.Helper()

	covered := make(map[HarnessID]bool, len(hermeticSmokeHarnesses))
	for _, id := range hermeticSmokeHarnesses {
		covered[id] = true
	}
	managed := make(map[HarnessID]bool, len(managedHarnessRegistry))
	for _, harness := range managedHarnessRegistry {
		id := harness.Spec.ID
		managed[id] = true
		if !covered[id] {
			s.t.Fatalf("managed harness %q is missing from hermetic smoke coverage", id)
		}
	}
	for id := range covered {
		if !managed[id] {
			s.t.Fatalf("hermetic smoke declares unknown harness %q", id)
		}
	}
}

func (s *hermeticHarnessSmoke) seedProviderSecrets() {
	s.writeProviderSecret("ANTHROPIC_API_KEY", "stored-anthropic-provider")
	s.writeProviderSecret("OPENAI_API_KEY", "stored-openai-provider")
	s.writeProviderSecret("GEMINI_API_KEY", "stored-gemini-provider")
	s.writeProviderSecret("OPENROUTER_API_KEY", "stored-openrouter-provider")
}

func (s *hermeticHarnessSmoke) runHermes() {
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "hermes"), `#!/bin/sh
set -eu
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 40; }
case "${HERMES_HOME:-}" in
  "$HOME/.hazmat/hermes/projects/"*) ;;
  *) echo "unexpected HERMES_HOME=${HERMES_HOME:-}" >&2; exit 41 ;;
esac
test "${ANTHROPIC_API_KEY:-}" = "stored-anthropic-provider" || { echo "missing ANTHROPIC_API_KEY" >&2; exit 42; }
test "${OPENAI_API_KEY:-}" = "stored-openai-provider" || { echo "missing OPENAI_API_KEY" >&2; exit 43; }
test "${GEMINI_API_KEY:-}" = "stored-gemini-provider" || { echo "missing GEMINI_API_KEY" >&2; exit 44; }
test "${OPENROUTER_API_KEY:-}" = "stored-openrouter-provider" || { echo "missing OPENROUTER_API_KEY" >&2; exit 45; }
if [ "${1:-}" = "--version" ]; then
  mkdir -p "$HERMES_HOME"
  echo "hermes fake smoke"
  exit 0
fi
echo "unexpected Hermes args: $*" >&2
exit 46
`)

	s.executeHarnessCommand(HarnessHermes, newHermesCmd(),
		"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "--", "--version")
	s.assertOutputContains(HarnessHermes, "hermes fake smoke")
	s.assertDirExists(filepath.Join(s.agentHome, ".hazmat", "hermes", "projects"), "Hermes managed project state root")
}

func (s *hermeticHarnessSmoke) runClaude() {
	s.writeHostSecret(claudeCredentialStorePathForHome(s.hostHome),
		`{"sessionKey":"stored-token","refreshToken":"stored-refresh"}`)
	s.writeHostSecret(claudeStateStorePathForHome(s.hostHome),
		`{"oauthAccount":{"emailAddress":"smoke@example.com"},"userID":"u-smoke","hasAvailableSubscription":true}`)
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "claude"), `#!/bin/sh
set -eu
cred="$HOME/.claude/.credentials.json"
state="$HOME/.claude.json"
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 50; }
test "${ANTHROPIC_API_KEY:-}" = "stored-anthropic-provider" || { echo "missing ANTHROPIC_API_KEY" >&2; exit 51; }
test -f "$cred" || { echo "missing materialized Claude credentials" >&2; exit 52; }
test -f "$state" || { echo "missing materialized Claude state" >&2; exit 53; }
grep -Fq "stored-token" "$cred" || { echo "missing stored token" >&2; exit 54; }
grep -Fq "oauthAccount" "$state" || { echo "missing stored state" >&2; exit 55; }
printf '{}\n' > "$cred"
printf '{"projects":{"hazmat-smoke":true}}\n' > "$state"
echo "FAKE_CLAUDE_OK"
`)

	s.executeHarnessCommand(HarnessClaude, newClaudeCmd(),
		"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "-p", "auth smoke")
	s.assertOutputContains(HarnessClaude, "FAKE_CLAUDE_OK")
	s.assertFileContains(claudeCredentialStorePathForHome(s.hostHome), "stored-token", "host Claude credentials")
	s.assertFileContains(claudeStateStorePathForHome(s.hostHome), "oauthAccount", "host Claude state")
	s.assertAgentFileAbsent(agentHome+"/.claude/.credentials.json", "Claude credential residue")
}

func (s *hermeticHarnessSmoke) runCodex() {
	s.writeHostSecret(codexAuthStorePathForHome(s.hostHome),
		`{"tokens":{"access":"stored-codex-access"},"refresh":"stored-codex-refresh"}`)
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "codex"), `#!/bin/sh
set -eu
auth="$HOME/.codex/auth.json"
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 60; }
test "${OPENAI_API_KEY:-}" = "stored-openai-provider" || { echo "missing OPENAI_API_KEY" >&2; exit 61; }
test -f "$auth" || { echo "missing materialized Codex auth" >&2; exit 62; }
grep -Fq "stored-codex-access" "$auth" || { echo "missing stored Codex auth" >&2; exit 63; }
printf '{"tokens":{"access":"updated-codex-access"}}\n' > "$auth"
case " $* " in
  *" exec "*"codex smoke"*) ;;
  *) echo "unexpected Codex args: $*" >&2; exit 64 ;;
esac
echo "FAKE_CODEX_OK"
`)

	s.executeHarnessCommand(HarnessCodex, newCodexCmd(),
		"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "exec", "codex smoke")
	s.assertOutputContains(HarnessCodex, "FAKE_CODEX_OK")
	s.assertFileContains(codexAuthStorePathForHome(s.hostHome), "updated-codex-access", "host Codex auth")
	s.assertAgentFileAbsent(agentHome+"/.codex/auth.json", "Codex auth residue")
}

func (s *hermeticHarnessSmoke) runOpenCode() {
	s.writeHostSecret(openCodeAuthStorePathForHome(s.hostHome),
		`{"providers":{"anthropic":{"token":"stored-opencode-token"}}}`)
	s.writeExecutable(filepath.Join(s.agentHome, ".opencode", "bin", "opencode"), `#!/bin/sh
set -eu
auth="$HOME/.local/share/opencode/auth.json"
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 70; }
test -f "$auth" || { echo "missing materialized OpenCode auth" >&2; exit 71; }
grep -Fq "stored-opencode-token" "$auth" || { echo "missing stored OpenCode auth" >&2; exit 72; }
printf '{"providers":{"anthropic":{"token":"updated-opencode-token"}}}\n' > "$auth"
case " $* " in
  *" run "*"opencode smoke"*) ;;
  *) echo "unexpected OpenCode args: $*" >&2; exit 73 ;;
esac
echo "FAKE_OPENCODE_OK"
`)

	s.executeHarnessCommand(HarnessOpenCode, newOpenCodeCmd(),
		"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "run", "opencode smoke")
	s.assertOutputContains(HarnessOpenCode, "FAKE_OPENCODE_OK")
	s.assertFileContains(openCodeAuthStorePathForHome(s.hostHome), "updated-opencode-token", "host OpenCode auth")
	s.assertAgentFileAbsent(agentHome+"/.local/share/opencode/auth.json", "OpenCode auth residue")
}

func (s *hermeticHarnessSmoke) runGemini() {
	s.writeHostSecret(geminiOAuthStorePathForHome(s.hostHome),
		`{"access_token":"stored-gemini-access","refresh_token":"stored-gemini-refresh"}`)
	s.writeHostSecret(geminiAccountsStorePathForHome(s.hostHome),
		`{"active":"stored-gemini-account"}`)
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "gemini"), `#!/bin/sh
set -eu
oauth="$HOME/.gemini/oauth_creds.json"
accounts="$HOME/.gemini/google_accounts.json"
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 80; }
test "${GEMINI_API_KEY:-}" = "stored-gemini-provider" || { echo "missing GEMINI_API_KEY" >&2; exit 81; }
test -f "$oauth" || { echo "missing materialized Gemini OAuth" >&2; exit 82; }
test -f "$accounts" || { echo "missing materialized Gemini accounts" >&2; exit 83; }
grep -Fq "stored-gemini-access" "$oauth" || { echo "missing stored Gemini OAuth" >&2; exit 84; }
grep -Fq "stored-gemini-account" "$accounts" || { echo "missing stored Gemini accounts" >&2; exit 85; }
printf '{"access_token":"updated-gemini-access"}\n' > "$oauth"
printf '{"active":"updated-gemini-account"}\n' > "$accounts"
case " $* " in
  *" -p "*"gemini smoke"*) ;;
  *) echo "unexpected Gemini args: $*" >&2; exit 86 ;;
esac
echo "FAKE_GEMINI_OK"
`)

	s.executeHarnessCommand(HarnessGemini, newGeminiCmd(),
		"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "-p", "gemini smoke")
	s.assertOutputContains(HarnessGemini, "FAKE_GEMINI_OK")
	s.assertFileContains(geminiOAuthStorePathForHome(s.hostHome), "updated-gemini-access", "host Gemini OAuth")
	s.assertFileContains(geminiAccountsStorePathForHome(s.hostHome), "updated-gemini-account", "host Gemini accounts")
	s.assertAgentFileAbsent(agentHome+"/.gemini/oauth_creds.json", "Gemini OAuth residue")
	s.assertAgentFileAbsent(agentHome+"/.gemini/google_accounts.json", "Gemini account residue")
}

func (s *hermeticHarnessSmoke) runQwen() {
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "qwen"), `#!/bin/sh
set -eu
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 90; }
if [ "${1:-}" != "--yolo" ]; then
  echo "expected --yolo as first Qwen arg, got: $*" >&2
  exit 91
fi
mkdir -p "$HOME/.qwen"
case " $* " in
  *" -p "*"qwen smoke"*) ;;
  *) echo "unexpected Qwen args: $*" >&2; exit 92 ;;
esac
echo "FAKE_QWEN_OK"
`)

	s.executeHarnessCommand(HarnessQwen, newQwenCmd(),
		"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "--yolo", "-p", "qwen smoke")
	s.assertOutputContains(HarnessQwen, "FAKE_QWEN_OK")
	s.assertDirExists(filepath.Join(s.agentHome, ".qwen"), "Qwen contained state directory")
}

func (s *hermeticHarnessSmoke) runCursorAgent() {
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "cursor-agent"), `#!/bin/sh
set -eu
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 100; }
test "$#" -eq 8 || { echo "unexpected Cursor Agent arg count: $#" >&2; exit 101; }
test "${1:-}" = "--print" || { echo "missing --print" >&2; exit 102; }
test "${2:-}" = "--output-format" || { echo "missing --output-format" >&2; exit 103; }
test "${3:-}" = "stream-json" || { echo "unexpected output format: ${3:-}" >&2; exit 104; }
test "${4:-}" = "--stream-partial-output" || { echo "missing --stream-partial-output" >&2; exit 105; }
test "${5:-}" = "--force" || { echo "missing --force" >&2; exit 106; }
test "${6:-}" = "--trust" || { echo "missing --trust" >&2; exit 107; }
test "${7:-}" = "--workspace" || { echo "missing --workspace" >&2; exit 108; }
actual_workspace="$(cd "${8:-}" && pwd -P)"
expected_workspace="$(cd "$SANDBOX_PROJECT_DIR" && pwd -P)"
test "$actual_workspace" = "$expected_workspace" || { echo "unexpected workspace: ${8:-}" >&2; exit 109; }
mkdir -p "$HOME/.cursor"
echo "FAKE_CURSOR_AGENT_OK"
`)

	s.executeHarnessCommand(HarnessCursorAgent, newCursorAgentCmd(),
		"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "--",
		"--print", "--output-format", "stream-json", "--stream-partial-output",
		"--force", "--trust", "--workspace", s.project)
	s.assertOutputContains(HarnessCursorAgent, "FAKE_CURSOR_AGENT_OK")
	s.assertDirExists(filepath.Join(s.agentHome, ".cursor"), "Cursor Agent contained state directory")
}

func (s *hermeticHarnessSmoke) executeHarnessCommand(id HarnessID, cmd *cobra.Command, args ...string) {
	s.t.Helper()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		s.t.Fatalf("hazmat %s %s: %v\noutput:\n%s", id, strings.Join(args, " "), err, s.outputs[id])
	}
}

func (s *hermeticHarnessSmoke) prepareSessionRuntime(cfg sessionConfig) (preparedSessionRuntime, error) {
	var runtimes []preparedSessionRuntime
	cleanupPrepared := func() {
		for i := len(runtimes) - 1; i >= 0; i-- {
			if runtimes[i].Cleanup != nil {
				runtimes[i].Cleanup()
			}
		}
	}

	tempRuntime, err := prepareAgentTempRuntime()
	if err != nil {
		return preparedSessionRuntime{}, err
	}
	runtimes = append(runtimes, tempRuntime)

	harnessRuntime, err := prepareHarnessAuthRuntime(cfg)
	if err != nil {
		cleanupPrepared()
		return preparedSessionRuntime{}, err
	}
	runtimes = append(runtimes, harnessRuntime)

	return mergePreparedSessionRuntimes(runtimes...), nil
}

func (s *hermeticHarnessSmoke) runAgentSeatbeltScriptWithPlan(cfg sessionConfig, plan sessionBackendPlan, _ sessionLaunchUI, script string, args ...string) error {
	runtime, err := prepareSessionRuntime(cfg)
	if err != nil {
		return err
	}
	defer runtime.Cleanup()
	if runtime.TempDir != "" {
		cfg.TempDir = runtime.TempDir
	}

	cmdArgs := append([]string{"-c", script, "hazmat-" + string(cfg.HarnessID)}, args...)
	cmd := exec.Command("/bin/sh", cmdArgs...)
	cmd.Dir = "/"
	cmd.Env = s.launchEnv(cfg, plan, runtime.EnvPairs)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err = cmd.Run()
	s.outputs[cfg.HarnessID] = output.String()
	if err != nil {
		return fmt.Errorf("fixture launch %s: %w\n%s", cfg.HarnessID, err, output.String())
	}
	return nil
}

func (s *hermeticHarnessSmoke) executeSessionMutationPlan(plan sessionMutationPlan) error {
	for _, mutation := range plan.Mutations {
		if mutation.Metadata.Summary != "Hermes state root" {
			continue
		}
		if _, err := mutation.Apply(); err != nil {
			return err
		}
	}
	return nil
}

func (s *hermeticHarnessSmoke) ensureHermesStateRoot(path string) error {
	for _, dir := range []string{
		agentHome + "/.hazmat",
		hermesStateDir(),
		hermesProjectsDir(),
		path,
	} {
		if err := os.MkdirAll(s.mapAgentPath(dir), 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (s *hermeticHarnessSmoke) newAgentCommand(args ...string) *exec.Cmd {
	mapped := make([]string, len(args))
	for i, arg := range args {
		mapped[i] = s.mapAgentPathValue(arg)
	}
	cmd := exec.Command(mapped[0], mapped[1:]...)
	cmd.Dir = "/"
	cmd.Env = append([]string{}, "HOME="+s.agentHome, "USER="+agentUser, "LOGNAME="+agentUser, "PATH="+s.agentPath())
	return cmd
}

func (s *hermeticHarnessSmoke) launchEnv(cfg sessionConfig, plan sessionBackendPlan, runtimeEnvPairs []string) []string {
	env := make(map[string]string)
	for _, pair := range agentEnvPairsWithPlan(cfg, plan) {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		env[key] = s.mapAgentPathValue(value)
	}
	for _, pair := range runtimeEnvPairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		env[key] = s.mapAgentPathValue(value)
	}
	env["HOME"] = s.agentHome
	env["USER"] = agentUser
	env["LOGNAME"] = agentUser
	env["PATH"] = s.agentPath()
	env["TMPDIR"] = filepath.Join(s.root, "tmp")
	env["TMP"] = env["TMPDIR"]
	env["TEMP"] = env["TMPDIR"]
	env["BUN_TMPDIR"] = env["TMPDIR"]
	env["XDG_CACHE_HOME"] = filepath.Join(s.agentHome, ".cache")
	env["XDG_CONFIG_HOME"] = filepath.Join(s.agentHome, ".config")
	env["XDG_DATA_HOME"] = filepath.Join(s.agentHome, ".local", "share")

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func (s *hermeticHarnessSmoke) agentPath() string {
	return strings.Join([]string{
		filepath.Join(s.agentHome, ".opencode", "bin"),
		filepath.Join(s.agentHome, ".local", "bin"),
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}, string(os.PathListSeparator))
}

func (s *hermeticHarnessSmoke) mapAgentPath(path string) string {
	if path == agentHome {
		return s.agentHome
	}
	prefix := agentHome + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return filepath.Join(s.agentHome, strings.TrimPrefix(path, prefix))
	}
	return path
}

func (s *hermeticHarnessSmoke) mapAgentPathValue(value string) string {
	if value == agentHome {
		return s.agentHome
	}
	return strings.ReplaceAll(value, agentHome+string(os.PathSeparator), s.agentHome+string(os.PathSeparator))
}

func (s *hermeticHarnessSmoke) writeProviderSecret(envVar, value string) {
	s.t.Helper()
	path, err := providerSecretStorePathForHome(s.hostHome, envVar)
	if err != nil {
		s.t.Fatalf("resolve provider secret path for %s: %v", envVar, err)
	}
	s.writeHostSecret(path, value)
}

func (s *hermeticHarnessSmoke) writeHostSecret(path, content string) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		s.t.Fatalf("create host secret dir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content+"\n"), 0o600); err != nil {
		s.t.Fatalf("write host secret %s: %v", path, err)
	}
}

func (s *hermeticHarnessSmoke) writeExecutable(path, content string) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		s.t.Fatalf("create executable dir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		s.t.Fatalf("write executable %s: %v", path, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		s.t.Fatalf("chmod executable %s: %v", path, err)
	}
}

func (s *hermeticHarnessSmoke) assertOutputContains(id HarnessID, needle string) {
	s.t.Helper()
	if !strings.Contains(s.outputs[id], needle) {
		s.t.Fatalf("%s output missing %q:\n%s", id, needle, s.outputs[id])
	}
}

func (s *hermeticHarnessSmoke) assertFileContains(path, needle, label string) {
	s.t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		s.t.Fatalf("read %s at %s: %v", label, path, err)
	}
	if !strings.Contains(string(raw), needle) {
		s.t.Fatalf("%s missing %q in %s: %s", label, needle, path, raw)
	}
}

func (s *hermeticHarnessSmoke) assertAgentFileAbsent(path, label string) {
	s.t.Helper()
	mapped := s.mapAgentPath(path)
	if _, err := os.Stat(mapped); !os.IsNotExist(err) {
		s.t.Fatalf("%s should be absent at %s, got err=%v", label, mapped, err)
	}
}

func (s *hermeticHarnessSmoke) assertDirExists(path, label string) {
	s.t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		s.t.Fatalf("%s missing at %s: %v", label, path, err)
	}
	if !info.IsDir() {
		s.t.Fatalf("%s at %s is not a directory", label, path)
	}
}

func (s *hermeticHarnessSmoke) assertNoSudo() {
	s.t.Helper()
	if raw, err := os.ReadFile(s.sudoMarker); err == nil {
		s.t.Fatalf("hermetic smoke invoked sudo:\n%s", raw)
	} else if !os.IsNotExist(err) {
		s.t.Fatalf("inspect sudo marker: %v", err)
	}
}

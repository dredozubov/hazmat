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
	"time"

	"hazmat/sessionlaunch"

	"github.com/spf13/cobra"
)

var hermeticSmokeHarnesses = []HarnessID{
	HarnessClaude,
	HarnessCodex,
	HarnessOpenCode,
	HarnessAntigravity,
	HarnessHermes,
	HarnessQwen,
	HarnessCursorAgent,
	HarnessPi,
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
	updates      map[HarnessID]int

	// claudeRotationKeychain selects the keychain-backed OAuth rotation fake
	// for Claude instead of the default fake. See the keychain rotation
	// regression test.
	claudeRotationKeychain bool
}

func TestHermeticHarnessSmoke(t *testing.T) {
	smoke := newHermeticHarnessSmoke(t)
	smoke.installTestSeams()
	smoke.assertManagedHarnessCoverage()
	smoke.seedProviderSecrets()
	smoke.seedHarnessAssets()

	smoke.runHermes()
	smoke.runClaude()
	smoke.runCodex()
	smoke.runOpenCode()
	smoke.runAntigravity()
	smoke.runQwen()
	smoke.runCursorAgent()
	smoke.runPi()

	smoke.assertFakeHarnessFailureModes()
	smoke.assertFakeHarnessCredentialProbeMode()
	smoke.assertFakeHarnessSlowCancellableMode()
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
		updates:      make(map[HarnessID]int),
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

	savedHarnessAssetPathForDirectIO := harnessAssetPathForDirectIO
	harnessAssetPathForDirectIO = s.mapAgentPath
	t.Cleanup(func() { harnessAssetPathForDirectIO = savedHarnessAssetPathForDirectIO })

	// Stub the agent-keychain seams so the hermetic smoke never shells out to
	// macOS `security`. The fake Claude harness models a keychain-backed OAuth
	// rotation by writing the credential JSON to a stand-in file at the agent
	// login keychain path; these seams read and clear that stand-in.
	savedReadClaudeKeychain := readClaudeAgentKeychainCredential
	readClaudeAgentKeychainCredential = func() ([]byte, bool, error) {
		raw, err := os.ReadFile(s.mapAgentPath(agentLoginKeychainPath()))
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			return nil, false, nil
		}
		return trimmed, true, nil
	}
	t.Cleanup(func() { readClaudeAgentKeychainCredential = savedReadClaudeKeychain })

	savedWriteClaudeKeychain := writeClaudeAgentKeychainCredential
	writeClaudeAgentKeychainCredential = func(data harnessAuthData) error {
		raw, _ := data.([]byte)
		path := s.mapAgentPath(agentLoginKeychainPath())
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		return os.WriteFile(path, bytes.TrimSpace(raw), 0o600)
	}
	t.Cleanup(func() { writeClaudeAgentKeychainCredential = savedWriteClaudeKeychain })

	savedClearClaudeKeychain := clearClaudeAgentKeychainCredential
	clearClaudeAgentKeychainCredential = func() error {
		if err := os.Remove(s.mapAgentPath(agentLoginKeychainPath())); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	t.Cleanup(func() { clearClaudeAgentKeychainCredential = savedClearClaudeKeychain })

	savedRunClaudeAgentKeychainScript := runClaudeAgentKeychainScript
	runClaudeAgentKeychainScript = func(string) (string, error) {
		return "", nil
	}
	t.Cleanup(func() { runClaudeAgentKeychainScript = savedRunClaudeAgentKeychainScript })

	savedPrepareSessionRuntime := prepareSessionRuntime
	prepareSessionRuntime = s.prepareSessionRuntime
	t.Cleanup(func() { prepareSessionRuntime = savedPrepareSessionRuntime })

	savedRunAgentSeatbeltScriptWithPlan := runAgentSeatbeltScriptWithPlan
	runAgentSeatbeltScriptWithPlan = s.runAgentSeatbeltScriptWithPlan
	t.Cleanup(func() { runAgentSeatbeltScriptWithPlan = savedRunAgentSeatbeltScriptWithPlan })

	savedExecuteSessionMutationPlan := executeSessionMutationPlan
	executeSessionMutationPlan = s.executeSessionMutationPlan
	t.Cleanup(func() { executeSessionMutationPlan = savedExecuteSessionMutationPlan })

	savedManagedHarnessUpdateForLaunch := managedHarnessUpdateForLaunch
	managedHarnessUpdateForLaunch = s.managedHarnessUpdateForLaunch
	t.Cleanup(func() { managedHarnessUpdateForLaunch = savedManagedHarnessUpdateForLaunch })

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

	savedHarnessAssetsFilePath := harnessAssetsFilePath
	harnessAssetsFilePath = filepath.Join(s.hostHome, ".hazmat", "harness-assets.json")
	t.Cleanup(func() { harnessAssetsFilePath = savedHarnessAssetsFilePath })
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
	s.writeProviderSecret("ANTIGRAVITY_API_KEY", "stored-antigravity-provider")
	s.writeProviderSecret("GEMINI_API_KEY", "stored-gemini-provider")
	s.writeProviderSecret("OPENROUTER_API_KEY", "stored-openrouter-provider")
}

func (s *hermeticHarnessSmoke) seedHarnessAssets() {
	s.writeHostAsset(".claude/CLAUDE.md", "claude shared instructions")
	s.writeHostAsset(".claude/commands/review.md", "claude command asset")
	s.writeHostAsset(".claude/skills/planning-with-files/SKILL.md", "claude skill asset")
	s.writeHostAsset(".claude/agents/reviewer.md", "claude agent asset")

	s.writeHostAsset(".codex/AGENTS.md", "codex shared instructions")
	s.writeHostAsset(".codex/prompts/review.md", "codex prompt asset")
	s.writeHostAsset(".codex/rules/house.md", "codex rule asset")
	s.writeHostAsset(".agents/skills/codex-smoke/SKILL.md", "codex skill asset")

	s.writeHostAsset(".config/opencode/commands/review.md", "opencode command asset")
	s.writeHostAsset(".config/opencode/agents/reviewer.md", "opencode agent asset")
	s.writeHostAsset(".config/opencode/skills/opencode-smoke/SKILL.md", "opencode skill asset")

	s.writeHostAsset(".qwen/QWEN.md", "qwen shared instructions")
	s.writeHostAsset(".qwen/extensions/qwen-smoke/extension.json", `{"name":"qwen-smoke"}`)
}

func (s *hermeticHarnessSmoke) runHermes() {
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "hermes"), fakeHarnessScript(HarnessHermes, `
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
`))

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
	s.assertAgentFileAbsent(agentHome+"/.local/bin/claude", "Claude executable before launch update")

	s.executeHarnessCommand(HarnessClaude, newClaudeCmd(),
		"--no-backup", "-C", s.project, "-p", "auth smoke")
	s.assertOutputContains(HarnessClaude, "FAKE_CLAUDE_OK")
	if got := s.updates[HarnessClaude]; got != 1 {
		s.t.Fatalf("Claude harness updates = %d, want 1", got)
	}
	s.assertAgentFileContains(agentHome+"/.claude/skills/planning-with-files/SKILL.md", "claude skill asset", "Claude skill asset")
	s.assertFileContains(claudeCredentialStorePathForHome(s.hostHome), "stored-token", "host Claude credentials")
	s.assertFileContains(claudeStateStorePathForHome(s.hostHome), "oauthAccount", "host Claude state")
	s.assertAgentFileAbsent(agentHome+"/.claude/.credentials.json", "Claude credential residue")
}

func (s *hermeticHarnessSmoke) writeFakeClaudeExecutable() {
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "claude"), fakeHarnessScript(HarnessClaude, `
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
`))
}

func (s *hermeticHarnessSmoke) runCodex() {
	s.writeHostSecret(codexAuthStorePathForHome(s.hostHome),
		`{"tokens":{"access":"stored-codex-access"},"refresh":"stored-codex-refresh"}`)
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "codex"), fakeHarnessScript(HarnessCodex, `
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
`))

	s.executeHarnessCommand(HarnessCodex, newCodexCmd(),
		"--no-backup", "-C", s.project, "exec", "codex smoke")
	s.assertOutputContains(HarnessCodex, "FAKE_CODEX_OK")
	s.assertAgentFileContains(agentHome+"/.codex/prompts/review.md", "codex prompt asset", "Codex prompt asset")
	s.assertFileContains(codexAuthStorePathForHome(s.hostHome), "updated-codex-access", "host Codex auth")
	s.assertAgentFileAbsent(agentHome+"/.codex/auth.json", "Codex auth residue")
}

func (s *hermeticHarnessSmoke) runOpenCode() {
	s.writeHostSecret(openCodeAuthStorePathForHome(s.hostHome),
		`{"providers":{"anthropic":{"token":"stored-opencode-token"}}}`)
	s.writeExecutable(filepath.Join(s.agentHome, ".opencode", "bin", "opencode"), fakeHarnessScript(HarnessOpenCode, `
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
`))

	s.executeHarnessCommand(HarnessOpenCode, newOpenCodeCmd(),
		"--no-backup", "-C", s.project, "run", "opencode smoke")
	s.assertOutputContains(HarnessOpenCode, "FAKE_OPENCODE_OK")
	s.assertAgentFileContains(agentHome+"/.config/opencode/commands/review.md", "opencode command asset", "OpenCode command asset")
	s.assertFileContains(openCodeAuthStorePathForHome(s.hostHome), "updated-opencode-token", "host OpenCode auth")
	s.assertAgentFileAbsent(agentHome+"/.local/share/opencode/auth.json", "OpenCode auth residue")
}

func (s *hermeticHarnessSmoke) runAntigravity() {
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "agy"), fakeHarnessScript(HarnessAntigravity, `
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 80; }
test "${ANTIGRAVITY_API_KEY:-}" = "stored-antigravity-provider" || { echo "missing ANTIGRAVITY_API_KEY" >&2; exit 81; }
test "${GEMINI_API_KEY:-}" = "stored-gemini-provider" || { echo "missing GEMINI_API_KEY" >&2; exit 82; }
case " $* " in
  *" -p "*"antigravity smoke"*) ;;
  *) echo "unexpected Antigravity args: $*" >&2; exit 86 ;;
esac
echo "FAKE_ANTIGRAVITY_OK"
`))

	s.executeHarnessCommand(HarnessAntigravity, newAntigravityCmd(),
		"--no-backup", "-C", s.project, "-p", "antigravity smoke")
	s.assertOutputContains(HarnessAntigravity, "FAKE_ANTIGRAVITY_OK")
}

func (s *hermeticHarnessSmoke) runQwen() {
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "qwen"), fakeHarnessScript(HarnessQwen, `
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
`))

	s.executeHarnessCommand(HarnessQwen, newQwenCmd(),
		"--no-backup", "-C", s.project, "--yolo", "-p", "qwen smoke")
	s.assertOutputContains(HarnessQwen, "FAKE_QWEN_OK")
	s.assertAgentFileContains(agentHome+"/.qwen/extensions/qwen-smoke/extension.json", "qwen-smoke", "Qwen extension asset")
	s.assertDirExists(filepath.Join(s.agentHome, ".qwen"), "Qwen contained state directory")
}

func (s *hermeticHarnessSmoke) runCursorAgent() {
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "cursor-agent"), fakeHarnessScript(HarnessCursorAgent, `
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
`))

	s.executeHarnessCommand(HarnessCursorAgent, newCursorAgentCmd(),
		"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "--",
		"--print", "--output-format", "stream-json", "--stream-partial-output",
		"--force", "--trust", "--workspace", s.project)
	s.assertOutputContains(HarnessCursorAgent, "FAKE_CURSOR_AGENT_OK")
	s.assertDirExists(filepath.Join(s.agentHome, ".cursor"), "Cursor Agent contained state directory")
}

func (s *hermeticHarnessSmoke) runPi() {
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "pi"), fakeHarnessScript(HarnessPi, `
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 110; }
test "$#" -eq 2 || { echo "unexpected Pi arg count: $#" >&2; exit 111; }
test "${1:-}" = "--mode" || { echo "missing --mode" >&2; exit 112; }
test "${2:-}" = "rpc" || { echo "unexpected mode: ${2:-}" >&2; exit 113; }
mkdir -p "$HOME/.pi/agent"
echo "FAKE_PI_OK"
`))

	s.executeHarnessCommand(HarnessPi, newPiCmd(),
		"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "--", "--mode", "rpc")
	s.assertOutputContains(HarnessPi, "FAKE_PI_OK")
	s.assertDirExists(filepath.Join(s.agentHome, ".pi", "agent"), "Pi contained state directory")
}

type hermeticHarnessInvocation struct {
	id     HarnessID
	newCmd func() *cobra.Command
	args   []string
}

func fakeHarnessScript(id HarnessID, body string) string {
	return "#!/bin/sh\nset -eu\n" + fakeHarnessModePrelude(id) + body
}

func fakeHarnessModePrelude(id HarnessID) string {
	label := strings.ToUpper(strings.ReplaceAll(string(id), "-", "_"))
	return fmt.Sprintf(`fake_mode="${HAZMAT_HERMETIC_HARNESS_FAKE_MODE:-}"
case "$fake_mode" in
  "")
    ;;
  stderr-fail)
    echo "FAKE_%s_STDERR failure" >&2
    exit 77
    ;;
  credential-probe)
    probe="$HOME/.ssh/id_rsa"
    echo "FAKE_%s_CREDENTIAL_PROBE $probe" >&2
    test ! -e "$probe" || exit 78
    echo "FAKE_%s_CREDENTIAL_PROBE_BLOCKED"
    exit 0
    ;;
  slow)
    trap 'echo "FAKE_%s_CANCELLED" >&2; exit 130' INT TERM
    sleep "${HAZMAT_HERMETIC_HARNESS_FAKE_SLEEP_SECONDS:-1}"
    echo "FAKE_%s_SLOW_OK"
    exit 0
    ;;
  *)
    echo "unknown fake mode: $fake_mode" >&2
    exit 79
    ;;
esac
`, label, label, label, label, label)
}

func (s *hermeticHarnessSmoke) harnessInvocations() []hermeticHarnessInvocation {
	return []hermeticHarnessInvocation{
		{id: HarnessHermes, newCmd: newHermesCmd, args: []string{"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "--", "--version"}},
		{id: HarnessClaude, newCmd: newClaudeCmd, args: []string{"--no-backup", "-C", s.project, "-p", "auth smoke"}},
		{id: HarnessCodex, newCmd: newCodexCmd, args: []string{"--no-backup", "-C", s.project, "exec", "codex smoke"}},
		{id: HarnessOpenCode, newCmd: newOpenCodeCmd, args: []string{"--no-backup", "-C", s.project, "run", "opencode smoke"}},
		{id: HarnessAntigravity, newCmd: newAntigravityCmd, args: []string{"--no-backup", "-C", s.project, "-p", "antigravity smoke"}},
		{id: HarnessQwen, newCmd: newQwenCmd, args: []string{"--no-backup", "-C", s.project, "--yolo", "-p", "qwen smoke"}},
		{id: HarnessCursorAgent, newCmd: newCursorAgentCmd, args: []string{"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "--", "--print", "--output-format", "stream-json", "--stream-partial-output", "--force", "--trust", "--workspace", s.project}},
		{id: HarnessPi, newCmd: newPiCmd, args: []string{"--no-backup", "--skip-harness-assets-sync", "-C", s.project, "--", "--mode", "rpc"}},
	}
}

func (s *hermeticHarnessSmoke) assertFakeHarnessFailureModes() {
	s.withFakeHarnessMode("stderr-fail", func() {
		for _, invocation := range s.harnessInvocations() {
			s.executeHarnessCommandWantError(invocation, "STDERR failure")
		}
	})
}

func (s *hermeticHarnessSmoke) assertFakeHarnessCredentialProbeMode() {
	s.withFakeHarnessMode("credential-probe", func() {
		for _, invocation := range s.harnessInvocations() {
			s.executeHarnessCommand(invocation.id, invocation.newCmd(), invocation.args...)
			s.assertOutputContains(invocation.id, "CREDENTIAL_PROBE_BLOCKED")
		}
	})
}

func (s *hermeticHarnessSmoke) assertFakeHarnessSlowCancellableMode() {
	claudePath := filepath.Join(s.agentHome, ".local", "bin", "claude")
	cmd := exec.Command(claudePath)
	cmd.Env = append(os.Environ(),
		"HOME="+s.agentHome,
		"HAZMAT_HERMETIC_HARNESS_FAKE_MODE=slow",
		"HAZMAT_HERMETIC_HARNESS_FAKE_SLEEP_SECONDS=10",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		s.t.Fatalf("start cancellable fake harness: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		s.t.Fatalf("signal cancellable fake harness: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		s.t.Fatalf("cancellable fake harness exited successfully; output:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "FAKE_CLAUDE_CANCELLED") {
		s.t.Fatalf("cancellable fake harness output missing cancellation marker:\n%s", output.String())
	}
}

func (s *hermeticHarnessSmoke) withFakeHarnessMode(mode string, fn func()) {
	oldMode, hadMode := os.LookupEnv("HAZMAT_HERMETIC_HARNESS_FAKE_MODE")
	if err := os.Setenv("HAZMAT_HERMETIC_HARNESS_FAKE_MODE", mode); err != nil {
		s.t.Fatalf("set fake harness mode: %v", err)
	}
	defer func() {
		if hadMode {
			_ = os.Setenv("HAZMAT_HERMETIC_HARNESS_FAKE_MODE", oldMode)
		} else {
			_ = os.Unsetenv("HAZMAT_HERMETIC_HARNESS_FAKE_MODE")
		}
	}()
	fn()
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

func (s *hermeticHarnessSmoke) executeHarnessCommandWantError(invocation hermeticHarnessInvocation, wantOutput string) {
	s.t.Helper()
	cmd := invocation.newCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(invocation.args)
	if err := cmd.Execute(); err == nil {
		s.t.Fatalf("hazmat %s unexpectedly succeeded in fake failure mode\noutput:\n%s", invocation.id, s.outputs[invocation.id])
	}
	if !strings.Contains(s.outputs[invocation.id], wantOutput) {
		s.t.Fatalf("%s failure output missing %q:\n%s", invocation.id, wantOutput, s.outputs[invocation.id])
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

	return mergeHermeticPreparedSessionRuntimes(runtimes...), nil
}

func mergeHermeticPreparedSessionRuntimes(runtimes ...preparedSessionRuntime) preparedSessionRuntime {
	converted := make([]sessionlaunch.PreparedRuntime, 0, len(runtimes))
	for _, runtime := range runtimes {
		converted = append(converted, preparedSessionRuntimeToLaunch(runtime))
	}
	return preparedSessionRuntimeFromLaunch(sessionlaunch.MergePreparedRuntimes(converted...))
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
		if mutation.Metadata.Summary != "Hermes state root" &&
			!strings.HasSuffix(mutation.Metadata.Summary, " asset sync") &&
			!strings.HasSuffix(mutation.Metadata.Summary, " harness update") {
			continue
		}
		if _, err := mutation.Apply(); err != nil {
			return err
		}
	}
	return nil
}

func (s *hermeticHarnessSmoke) managedHarnessUpdateForLaunch(harness ManagedHarness) error {
	s.updates[harness.Spec.ID]++
	switch harness.Spec.ID {
	case HarnessClaude:
		if s.claudeRotationKeychain {
			s.writeFakeClaudeKeychainRotationExecutable()
		} else {
			s.writeFakeClaudeExecutable()
		}
		return nil
	default:
		return fmt.Errorf("unexpected harness update for %s", harness.Spec.ID)
	}
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
	if len(mapped) >= 3 && mapped[0] == "/bin/chmod" {
		if info, err := os.Stat(mapped[len(mapped)-1]); err == nil && info.IsDir() && strings.HasPrefix(mapped[len(mapped)-1], s.agentHome) {
			return exec.Command("/usr/bin/false")
		}
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
	for _, key := range []string{
		"HAZMAT_HERMETIC_HARNESS_FAKE_MODE",
		"HAZMAT_HERMETIC_HARNESS_FAKE_SLEEP_SECONDS",
	} {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}

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

func (s *hermeticHarnessSmoke) writeHostAsset(path, content string) {
	s.t.Helper()
	fullPath := filepath.Join(s.hostHome, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		s.t.Fatalf("create host asset dir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content+"\n"), 0o600); err != nil {
		s.t.Fatalf("write host asset %s: %v", fullPath, err)
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

func (s *hermeticHarnessSmoke) assertAgentFileContains(path, needle, label string) {
	s.t.Helper()
	s.assertFileContains(s.mapAgentPath(path), needle, label)
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

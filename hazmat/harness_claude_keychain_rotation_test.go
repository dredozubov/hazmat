//go:build hazmat_smoke_fixture

package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHermeticClaudeKeychainRotationLogout reproduces the "interactive Claude
// session logs me out" bug.
//
// Symptom: after running `hazmat claude` interactively, a later Claude session
// (inside or outside hazmat) is logged out. It is intermittent because it only
// fires when Claude Code actually refreshes its OAuth token *during* the
// session — i.e. when the short-lived access token expires mid-session.
//
// Mechanism: Claude's OAuth refresh rotates the refresh token server-side,
// invalidating the previous one. On keychain-preferring Claude releases the
// rotated token is written into the agent login keychain while
// ~/.claude/.credentials.json is left as the logged-out empty object (see the
// comment at harness_auth_runtime.go: "Claude updates can rewrite the runtime
// credential file to an empty logged-out object"). The harvest path only reads
// back .credentials.json, and isHarvestableClaudeCredentialData deliberately
// refuses to harvest the empty object — so hazmat's host-owned store keeps the
// OLD, now server-invalidated refresh token. The next session replays the dead
// token and is logged out.
//
// This test models that rotation and asserts the contract that fixes the bug:
// a session that rotates its credential must leave hazmat's host-owned store
// holding the live token, never the stale one. It FAILS against current code
// (harvest has no keychain awareness) and is the regression anchor for the fix.
func TestHermeticClaudeKeychainRotationLogout(t *testing.T) {
	smoke := newHermeticHarnessSmoke(t)
	smoke.claudeRotationKeychain = true
	smoke.installTestSeams()
	// Deliberately do NOT seed ANTHROPIC_API_KEY: a provider key forces Claude
	// into --bare mode, which sidesteps OAuth/keychain. The reported logout
	// only happens on the OAuth (subscription) login path.
	smoke.seedHarnessAssets()

	smoke.runClaudeKeychainRotation()

	smoke.assertNoSudo()
}

func (s *hermeticHarnessSmoke) runClaudeKeychainRotation() {
	s.t.Helper()

	store := claudeCredentialStorePathForHome(s.hostHome)
	s.writeHostSecret(store,
		`{"sessionKey":"stored-token","refreshToken":"stored-refresh"}`)
	s.writeHostSecret(claudeStateStorePathForHome(s.hostHome),
		`{"oauthAccount":{"emailAddress":"smoke@example.com"},"userID":"u-smoke","hasAvailableSubscription":true}`)

	s.executeHarnessCommand(HarnessClaude, newClaudeCmd(),
		"--no-backup", "-C", s.project, "-p", "auth smoke")
	s.assertOutputContains(HarnessClaude, "FAKE_CLAUDE_ROTATED_OK")

	// REGRESSION: the server rotated "stored-refresh" away during the session,
	// so it is now invalid. hazmat must capture the live rotated token (which
	// the agent wrote into the login keychain) into its host-owned store. If
	// the stale token survives here, the next session is logged out — which is
	// exactly the reported bug.
	s.assertFileContains(store, "rotated-refresh", "host Claude credentials after keychain rotation")
	s.assertFileLacks(store, "stored-refresh", "stale Claude refresh token after keychain rotation")

	// Both runtime sinks must be cleaned up: the file residue and the keychain
	// item (so the rotated value is never both live and never lingers stale).
	s.assertAgentFileAbsent(agentHome+"/.claude/.credentials.json", "Claude credential residue")
	s.assertAgentFileAbsent(agentLoginKeychainPath(), "Claude keychain residue")
}

func (s *hermeticHarnessSmoke) writeFakeClaudeKeychainRotationExecutable() {
	s.writeExecutable(filepath.Join(s.agentHome, ".local", "bin", "claude"), `#!/bin/sh
set -eu
cred="$HOME/.claude/.credentials.json"
state="$HOME/.claude.json"
kc="$HOME/Library/Keychains/login.keychain-db"
test "$(pwd)" = "$SANDBOX_PROJECT_DIR" || { echo "unexpected cwd=$(pwd)" >&2; exit 50; }
test -f "$state" || { echo "missing materialized Claude state" >&2; exit 53; }
if [ -f "$kc" ]; then
  grep -Fq "stored-refresh" "$kc" || { echo "missing materialized keychain refresh token" >&2; exit 54; }
else
  test -f "$cred" || { echo "missing materialized Claude credentials" >&2; exit 52; }
  grep -Fq "stored-refresh" "$cred" || { echo "missing materialized file refresh token" >&2; exit 54; }
fi
# Simulate a keychain-backed OAuth refresh: the rotated credential lands in the
# agent login keychain (here a stand-in file holding the same JSON shape that
# security find-generic-password -w would return), and the file credential
# store is rewritten to the logged-out empty object. This matches Claude Code on
# keychain-preferring releases.
mkdir -p "$(dirname "$kc")"
printf '{"claudeAiOauth":{"accessToken":"rotated-access","refreshToken":"rotated-refresh"}}\n' > "$kc"
printf '{}\n' > "$cred"
printf '{"projects":{"hazmat-smoke":true}}\n' > "$state"
echo "FAKE_CLAUDE_ROTATED_OK"
`)
}

func (s *hermeticHarnessSmoke) assertFileLacks(path, needle, label string) {
	s.t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		s.t.Fatalf("read %s at %s: %v", label, path, err)
	}
	if strings.Contains(string(raw), needle) {
		s.t.Fatalf("%s unexpectedly present %q in %s: %s", label, needle, path, raw)
	}
}

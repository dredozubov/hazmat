package hazmat

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClaudeAgentKeychainPrepareScriptCreatesOrRepairsManagedKeychain(t *testing.T) {
	script := claudeAgentKeychainPrepareScript()
	for _, want := range []string{
		`marker="$HOME/Library/Keychains/.hazmat-login-keychain"`,
		`reset_agent_keychain() {`,
		`if [ ! -e "$kc" ]; then`,
		`elif [ ! -f "$marker" ]; then`,
		`elif ! /usr/bin/security unlock-keychain -p "" "$kc" >/dev/null 2>&1; then`,
		`for kc_file in "$kc" "$kc-shm" "$kc-wal"; do`,
		`/bin/mv -f "$kc_file" "$backup_dir/$(/usr/bin/basename "$kc_file")"`,
		`/usr/bin/security create-keychain -p "" "$kc"`,
		`printf '%s\n' "hazmat-managed claude agent login keychain" > "$marker"`,
		`/usr/bin/security unlock-keychain -p "" "$kc"`,
		`best_effort_security /usr/bin/security login-keychain -s "$kc"`,
		`best_effort_security /usr/bin/security set-keychain-settings -lut 21600 "$kc"`,
		`/usr/bin/security default-keychain -s "$kc"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("prepare script missing %q:\n%s", want, script)
		}
	}
}

func TestClaudeAgentKeychainResetScriptBacksUpAndRecreates(t *testing.T) {
	script := claudeAgentKeychainResetScript()
	for _, want := range []string{
		`backup_dir="$HOME/Library/Keychains/hazmat-login-keychain-backups/$ts"`,
		`for kc_file in "$kc" "$kc-shm" "$kc-wal"; do`,
		`/bin/mv -f "$kc_file" "$backup_dir/$(/usr/bin/basename "$kc_file")"`,
		`/usr/bin/security create-keychain -p "" "$kc"`,
		`printf '%s\n' "hazmat-managed claude agent login keychain" > "$marker"`,
		`/usr/bin/security unlock-keychain -p "" "$kc"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("reset script missing %q:\n%s", want, script)
		}
	}
}

func TestPrepareClaudeAgentKeychainForLaunchSurfacesResetCommand(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Claude agent keychain preparation is macOS-only")
	}
	original := runClaudeAgentKeychainScript
	runClaudeAgentKeychainScript = func(string) (string, error) {
		return "SecKeychainUnlock: The user name or passphrase you entered is not correct.", errors.New("unlock failed")
	}
	t.Cleanup(func() { runClaudeAgentKeychainScript = original })

	err := prepareClaudeAgentKeychainForLaunch()
	if err == nil {
		t.Fatal("expected prepare failure")
	}
	if !strings.Contains(err.Error(), "hazmat claude-keychain reset") {
		t.Fatalf("prepare error should point at reset command, got: %v", err)
	}
}

func TestParseSecurityKeychainModifiedTime(t *testing.T) {
	raw := []byte(`keychain: "/Users/dr/Library/Keychains/login.keychain-db"
class: "genp"
attributes:
    "mdat"<timedate>=0x32303236303631373130323533305A00  "20260617102530Z\000"
    "svce"<blob>="Claude Code-credentials"
`)
	got, ok := parseSecurityKeychainModifiedTime(raw)
	if !ok {
		t.Fatal("parseSecurityKeychainModifiedTime ok=false, want true")
	}
	want := time.Date(2026, 6, 17, 10, 25, 30, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("modified time = %s, want %s", got, want)
	}
}

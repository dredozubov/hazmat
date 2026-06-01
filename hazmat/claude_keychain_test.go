package main

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestClaudeAgentKeychainPrepareScriptCreatesAndUnlocksWithoutReset(t *testing.T) {
	script := claudeAgentKeychainPrepareScript()
	for _, want := range []string{
		`if [ ! -e "$kc" ]; then`,
		`/usr/bin/security create-keychain -p "" "$kc"`,
		`/usr/bin/security login-keychain -s "$kc" >/dev/null 2>&1 || true`,
		`/usr/bin/security default-keychain -s "$kc"`,
		`/usr/bin/security unlock-keychain -p "" "$kc"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("prepare script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "mv -f") {
		t.Fatalf("prepare script must not reset or move an existing keychain:\n%s", script)
	}
}

func TestClaudeAgentKeychainResetScriptBacksUpAndRecreates(t *testing.T) {
	script := claudeAgentKeychainResetScript()
	for _, want := range []string{
		`backup_dir="$HOME/Library/Keychains/hazmat-login-keychain-backups/$ts"`,
		`for path in "$kc" "$kc-shm" "$kc-wal"; do`,
		`mv -f "$path" "$backup_dir/$(basename "$path")"`,
		`/usr/bin/security create-keychain -p "" "$kc"`,
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

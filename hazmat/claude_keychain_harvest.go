package hazmat

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// claudeKeychainCredentialService is the macOS Keychain generic-password
// service name Claude Code uses to persist OAuth credentials. The stored
// password field is the same JSON shape as ~/.claude/.credentials.json.
const claudeKeychainCredentialService = "Claude Code-credentials"

// readClaudeAgentKeychainCredential reads Claude Code's OAuth credential JSON
// from the agent login keychain. It is best-effort: ok=false (with no error)
// when the item is absent or the password field cannot be read without user
// interaction (errSecInteractionNotAllowed). Harvest then degrades to the
// file-only behavior rather than aborting.
var readClaudeAgentKeychainCredential = func() ([]byte, bool, error) {
	out, err := asAgentOutput(
		"/usr/bin/security", "find-generic-password",
		"-a", agentUser,
		"-s", claudeKeychainCredentialService,
		"-w",
		agentLoginKeychainPath(),
	)
	if err != nil {
		// Item missing (44) or ACL/interaction denied (36/45): nothing we can
		// promote from the keychain on this run.
		return nil, false, nil
	}
	raw := bytes.TrimSpace([]byte(out))
	if len(raw) == 0 {
		return nil, false, nil
	}
	return raw, true, nil
}

// clearClaudeAgentKeychainCredential removes the harvested Claude credential
// from the agent login keychain so a rotated value never lingers across
// sessions. Best-effort: a missing item is not an error.
var clearClaudeAgentKeychainCredential = func() error {
	_ = asAgentQuiet(
		"/usr/bin/security", "delete-generic-password",
		"-a", agentUser,
		"-s", claudeKeychainCredentialService,
		agentLoginKeychainPath(),
	)
	return nil
}

// readClaudeHostKeychainCredential reads the controlling user's Claude OAuth
// credential from the default login keychain, plus the Keychain item's modified
// time. Missing/inaccessible items degrade to ok=false so file-backed store
// launch can continue.
var readClaudeHostKeychainCredential = func() (harnessAuthKeychainData, bool, error) {
	out, err := commandStdout(
		hostSecurityPath, "find-generic-password",
		"-s", claudeKeychainCredentialService,
		"-w",
	)
	if err != nil {
		return harnessAuthKeychainData{}, false, nil
	}
	raw := bytes.TrimSpace([]byte(out))
	if len(raw) == 0 {
		return harnessAuthKeychainData{}, false, nil
	}

	meta, err := exec.Command(
		hostSecurityPath, "find-generic-password",
		"-s", claudeKeychainCredentialService,
		"-g",
	).CombinedOutput()
	updatedAt := time.Time{}
	if err == nil {
		updatedAt, _ = parseSecurityKeychainModifiedTime(meta)
	}
	return harnessAuthKeychainData{Data: raw, UpdatedAt: updatedAt}, true, nil
}

// writeClaudeHostKeychainCredential writes the harvested live OAuth credential
// back to the host user's default login keychain. The service selector matches
// Claude Code's own generic-password item; the password field is the same JSON
// shape as ~/.claude/.credentials.json.
var writeClaudeHostKeychainCredential = func(data harnessAuthData) error {
	raw, _ := data.([]byte)
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	if _, err := exec.Command(
		hostSecurityPath, "add-generic-password",
		"-U",
		"-s", claudeKeychainCredentialService,
		"-w", string(raw),
	).CombinedOutput(); err != nil {
		return fmt.Errorf("security add-generic-password failed: %w", err)
	}
	return nil
}

func parseSecurityKeychainModifiedTime(raw []byte) (time.Time, bool) {
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.Contains(line, `"mdat"<timedate>=`) {
			continue
		}
		for _, quoted := range strings.Split(line, `"`) {
			value := strings.TrimSuffix(quoted, `\000`)
			parsed, err := time.Parse("20060102150405Z", value)
			if err == nil {
				return parsed, true
			}
		}
	}
	return time.Time{}, false
}

// withClaudeKeychainHarvest attaches the agent-keychain read/clear hooks to a
// Claude credential artifact on macOS, where Claude Code may store OAuth
// material in either the host user's login keychain or the agent login
// keychain instead of the materialized file.
func withClaudeKeychainHarvest(a harnessAuthArtifact) harnessAuthArtifact {
	if runtime.GOOS != "darwin" {
		return a
	}
	a.ReadAgentKeychain = func() (harnessAuthData, bool, error) {
		raw, ok, err := readClaudeAgentKeychainCredential()
		if err != nil || !ok {
			return nil, ok, err
		}
		return raw, true, nil
	}
	a.ClearAgentKeychain = clearClaudeAgentKeychainCredential
	a.ReadHostKeychain = readClaudeHostKeychainCredential
	a.WriteHostKeychain = writeClaudeHostKeychainCredential
	return a
}

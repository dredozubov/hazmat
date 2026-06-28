package hazmat

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"
)

// claudeKeychainCredentialService is the macOS Keychain generic-password
// service name Claude Code uses to persist OAuth credentials. The stored
// password field is the same JSON shape as ~/.claude/.credentials.json.
const claudeKeychainCredentialService = "Claude Code-credentials"
const claudeHostKeychainSyncEnv = "HAZMAT_CLAUDE_HOST_KEYCHAIN_SYNC"

// claudeKeychainAddArgs builds the `security add-generic-password` argument
// vector for seeding Claude Code's OAuth credential.
//
// The `-a` account AND `-s` service attributes are BOTH required by the macOS
// security tool: omitting `-a` makes add-generic-password print usage and exit
// 2, so the write can never succeed. Claude Code stores its item under the OS
// short username of whoever runs it, so callers pass the matching account (the
// agent account for the agent keychain, the invoking user for the host
// keychain). updateExisting enables `-U` for the host keychain, where replacing
// the user's normal item is correct. Agent writes deliberately delete-then-add
// instead because `-U` can still invoke Keychain item access UI when changing an
// existing item's access list. keychainPath is appended only when non-empty; an
// empty path targets the default keychain. allowAnyApplication is for the
// contained agent keychain only: it prevents macOS item-access UI for
// Claude/Node while the sandbox already limits access to the managed agent
// keychain. Host keychain writes must keep the normal per-app warning ACLs.
func claudeKeychainAddArgs(account string, raw []byte, keychainPath string, updateExisting, allowAnyApplication bool) []string {
	args := []string{
		"add-generic-password",
		"-a", account,
		"-s", claudeKeychainCredentialService,
		"-w", string(raw),
	}
	if updateExisting {
		args = append(args, "-U")
	}
	if allowAnyApplication {
		args = append(args, "-A")
	}
	if keychainPath != "" {
		args = append(args, keychainPath)
	}
	return args
}

// currentHostKeychainAccount returns the account name Claude Code uses for the
// invoking user's keychain item: the OS short username.
func currentHostKeychainAccount() string {
	if u, err := user.Current(); err == nil {
		if name := strings.TrimSpace(u.Username); name != "" {
			return name
		}
	}
	return strings.TrimSpace(os.Getenv("USER"))
}

func claudeHostKeychainSyncEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(claudeHostKeychainSyncEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// securityOutputDetail formats trimmed `security` output for an error message.
// The output is the tool's own diagnostic text (usage or a SecKeychain
// message); it never echoes the -w password, so it is safe to surface. Without
// this, agent-side writes ran through asAgentQuiet and the failure reason was
// lost behind a bare "exit status N".
func securityOutputDetail(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	return "\nsecurity output: " + out
}

// readClaudeAgentKeychainCredential reads Claude Code's OAuth credential JSON
// from the agent login keychain. It is best-effort: ok=false (with no error)
// when the item is absent or the password field cannot be read without user
// interaction (errSecInteractionNotAllowed). Harvest then degrades to the
// file-only behavior rather than aborting.
var readClaudeAgentKeychainCredential = func() ([]byte, bool, error) {
	out, err := asAgentOutput(
		"/usr/bin/security", "find-generic-password",
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

// writeClaudeAgentKeychainCredential seeds Claude Code's OAuth credential into
// the agent login keychain for keychain-preferring releases.
var writeClaudeAgentKeychainCredential = func(data harnessAuthData) error {
	raw, _ := data.([]byte)
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	_ = asAgentQuiet(
		"/usr/bin/security", "delete-generic-password",
		"-s", claudeKeychainCredentialService,
		agentLoginKeychainPath(),
	)
	args := append([]string{"/usr/bin/security"},
		claudeKeychainAddArgs(agentUser, raw, agentLoginKeychainPath(), false, true)...)
	if out, err := asAgentCombinedOutput(args...); err != nil {
		return fmt.Errorf("security add-generic-password failed: %w%s", err, securityOutputDetail(out))
	}
	return nil
}

// clearClaudeAgentKeychainCredential removes the harvested Claude credential
// from the agent login keychain so a rotated value never lingers across
// sessions. Best-effort: a missing item is not an error.
var clearClaudeAgentKeychainCredential = func() error {
	_ = asAgentQuiet(
		"/usr/bin/security", "delete-generic-password",
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
	args := claudeKeychainAddArgs(currentHostKeychainAccount(), raw, "", true, false)
	if out, err := exec.Command(hostSecurityPath, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("security add-generic-password failed: %w%s", err, securityOutputDetail(string(out)))
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
	a.WriteAgentKeychain = writeClaudeAgentKeychainCredential
	a.ClearAgentKeychain = clearClaudeAgentKeychainCredential
	// Host `security find-generic-password -w` can summon SecurityAgent when the
	// login keychain is locked, so startup must not touch it unless requested.
	if claudeHostKeychainSyncEnabled() {
		a.ReadHostKeychain = readClaudeHostKeychainCredential
		a.WriteHostKeychain = writeClaudeHostKeychainCredential
	}
	return a
}

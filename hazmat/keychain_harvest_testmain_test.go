package hazmat

import (
	"os"
	"testing"
)

// TestMain neutralizes the agent login-keychain seams by default so no unit
// test shells out to macOS `security` against the real agent keychain. Without
// this, tests that build the Claude credential artifact and harvest a
// logged-out file would invoke `security find-generic-password` /
// `delete-generic-password` as the agent — which, on a machine where Hazmat's
// helper is installed, could read or delete the developer's real Claude
// credential. Tests that exercise keychain harvest (the hermetic smoke)
// override these seams explicitly.
func TestMain(m *testing.M) {
	readClaudeAgentKeychainCredential = func() ([]byte, bool, error) { return nil, false, nil }
	clearClaudeAgentKeychainCredential = func() error { return nil }
	readClaudeHostKeychainCredential = func() (harnessAuthKeychainData, bool, error) {
		return harnessAuthKeychainData{}, false, nil
	}
	writeClaudeHostKeychainCredential = func(harnessAuthData) error { return nil }
	os.Exit(m.Run())
}

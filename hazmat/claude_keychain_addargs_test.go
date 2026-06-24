package hazmat

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// argValue returns the value following flag in args, or "" if flag is absent
// or has no following token.
func argValue(args []string, flag string) (string, bool) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1], true
		}
	}
	return "", false
}

// TestClaudeKeychainAddArgsCarriesRequiredAccount guards the exact regression
// that made `hazmat claude` abort at launch: `security add-generic-password`
// requires both -a (account) and -s (service); a missing -a makes it print
// usage and exit 2. This asserts the argv shape so the required account can
// never silently drop out again.
func TestClaudeKeychainAddArgsCarriesRequiredAccount(t *testing.T) {
	raw := []byte(`{"claudeAiOauth":{"accessToken":"tok"}}`)

	t.Run("agent keychain write", func(t *testing.T) {
		args := claudeKeychainAddArgs("agent", raw, "/Users/agent/Library/Keychains/login.keychain-db")
		if got, ok := argValue(args, "-a"); !ok || got != "agent" {
			t.Fatalf("expected -a agent, got %q (ok=%v) in %v", got, ok, args)
		}
		if got, ok := argValue(args, "-s"); !ok || got != claudeKeychainCredentialService {
			t.Fatalf("expected -s %q, got %q (ok=%v)", claudeKeychainCredentialService, got, ok)
		}
		if got, ok := argValue(args, "-w"); !ok || got != string(raw) {
			t.Fatalf("expected -w to carry the credential, got %q (ok=%v)", got, ok)
		}
		if args[len(args)-1] != "/Users/agent/Library/Keychains/login.keychain-db" {
			t.Fatalf("expected explicit keychain path as last arg, got %v", args)
		}
	})

	t.Run("host keychain write omits explicit keychain", func(t *testing.T) {
		args := claudeKeychainAddArgs("dr", raw, "")
		if got, ok := argValue(args, "-a"); !ok || got != "dr" {
			t.Fatalf("expected -a dr, got %q (ok=%v) in %v", got, ok, args)
		}
		for _, a := range args {
			if strings.HasSuffix(a, "login.keychain-db") {
				t.Fatalf("host write must not pin a keychain path, got %v", args)
			}
		}
	})
}

// TestClaudeKeychainAddArgsAcceptedBySecurity runs the real argv against a
// throwaway keychain (never the agent or host login keychain) and asserts the
// macOS security tool accepts it and that -U updates in place. This is a real
// outcome test, not just a shape check: before -a was added, this command
// exited 2 every time.
func TestClaudeKeychainAddArgsAcceptedBySecurity(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("security keychain tool is macOS-only")
	}
	securityPath, err := exec.LookPath("security")
	if err != nil {
		t.Skipf("security not available: %v", err)
	}

	kc := filepath.Join(t.TempDir(), "throwaway.keychain-db")
	if out, err := exec.Command(securityPath, "create-keychain", "-p", "", kc).CombinedOutput(); err != nil {
		t.Skipf("cannot create throwaway keychain in this environment: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command(securityPath, "delete-keychain", kc).Run() })
	if out, err := exec.Command(securityPath, "unlock-keychain", "-p", "", kc).CombinedOutput(); err != nil {
		t.Skipf("cannot unlock throwaway keychain: %v: %s", err, out)
	}

	first := []byte(`{"claudeAiOauth":{"accessToken":"first"}}`)
	updated := []byte(`{"claudeAiOauth":{"accessToken":"updated"}}`)

	add := func(raw []byte) ([]byte, error) {
		args := append([]string{}, claudeKeychainAddArgs("agent", raw, kc)...)
		return exec.Command(securityPath, args...).CombinedOutput()
	}

	if out, err := add(first); err != nil {
		t.Fatalf("add-generic-password rejected the argv (the bug): %v: %s", err, out)
	}
	if out, err := add(updated); err != nil {
		t.Fatalf("-U update rejected: %v: %s", err, out)
	}

	// The harvest read path matches by service only; confirm it sees the update.
	got, err := exec.Command(securityPath, "find-generic-password", "-s", claudeKeychainCredentialService, "-w", kc).Output()
	if err != nil {
		t.Fatalf("read-back failed: %v", err)
	}
	if strings.TrimSpace(string(got)) != string(updated) {
		t.Fatalf("expected updated credential, got %q", strings.TrimSpace(string(got)))
	}
}

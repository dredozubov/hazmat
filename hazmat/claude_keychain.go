package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const agentLoginKeychainRelPath = "Library/Keychains/login.keychain-db"

var runClaudeAgentKeychainScript = func(script string) (string, error) {
	return asAgentCombinedOutput("/bin/zsh", "-lc", script)
}

func agentLoginKeychainPath() string {
	return agentHome + "/" + agentLoginKeychainRelPath
}

func newClaudeKeychainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude-keychain",
		Short: "Inspect or reset the agent login keychain used by Claude Code",
		Long: `Inspect or reset the agent account login keychain used by Claude Code.

Newer Claude Code releases may read Apple Keychain unless launched with --bare.
Hazmat prepares the dedicated agent account keychain before native Claude OAuth
sessions so macOS does not prompt for the wrong user's keychain password.`,
	}
	cmd.AddCommand(newClaudeKeychainDoctorCmd())
	cmd.AddCommand(newClaudeKeychainResetCmd())
	return cmd
}

func newClaudeKeychainDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check and unlock the agent Claude keychain",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := checkPlatform(); err != nil {
				return err
			}
			if flagDryRun {
				fmt.Fprintf(os.Stderr, "hazmat: dry-run: would prepare and unlock %s\n", agentLoginKeychainPath())
				return nil
			}
			if err := requireInit(); err != nil {
				return err
			}
			if err := prepareClaudeAgentKeychainForLaunch(); err != nil {
				return err
			}
			fmt.Printf("Claude agent keychain is ready: %s\n", agentLoginKeychainPath())
			return nil
		},
	}
}

func newClaudeKeychainResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Back up and recreate the agent Claude keychain",
		Long: `Back up and recreate the dedicated agent account login keychain.

This affects only /Users/agent/Library/Keychains/login.keychain-db. It does not
touch the invoking user's keychain. Claude Code will need to authenticate again
after reset if it used keychain-backed OAuth.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := checkPlatform(); err != nil {
				return err
			}
			if flagDryRun {
				fmt.Fprintf(os.Stderr, "hazmat: dry-run: would back up and recreate %s\n", agentLoginKeychainPath())
				return nil
			}
			if err := requireInit(); err != nil {
				return err
			}
			if !flagYesAll {
				ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
				if !ui.Ask("Reset the agent account login keychain for Claude Code?") {
					return fmt.Errorf("Claude agent keychain reset cancelled")
				}
			}
			out, err := resetClaudeAgentKeychain()
			if strings.TrimSpace(out) != "" {
				fmt.Println(strings.TrimSpace(out))
			}
			return err
		},
	}
}

func prepareClaudeAgentKeychainForLaunch() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	out, err := runClaudeAgentKeychainScript(claudeAgentKeychainPrepareScript())
	if err != nil {
		detail := strings.TrimSpace(out)
		if detail != "" {
			detail = "\nsecurity output: " + detail
		} else {
			detail = "\nsecurity error: " + err.Error()
		}
		return fmt.Errorf("prepare Claude agent keychain: could not unlock %s with Hazmat's empty-password keychain profile%s\nRun: hazmat claude-keychain reset",
			agentLoginKeychainPath(), detail)
	}
	return nil
}

func resetClaudeAgentKeychain() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("Claude agent keychain reset is supported only on macOS")
	}
	return runClaudeAgentKeychainScript(claudeAgentKeychainResetScript())
}

func claudeAgentKeychainPrepareScript() string {
	return `set -e
kc="$HOME/Library/Keychains/login.keychain-db"
mkdir -p "$HOME/Library/Keychains"

if [ ! -e "$kc" ]; then
  /usr/bin/security create-keychain -p "" "$kc"
fi

# Helper-backed maintenance mode can reject this login-keychain preference
# write even when the default/search-list/unlock path succeeds.
/usr/bin/security login-keychain -s "$kc" >/dev/null 2>&1 || true
/usr/bin/security default-keychain -s "$kc"
/usr/bin/security list-keychains -d user -s "$kc" /System/Library/Keychains/SystemRootCertificates.keychain /Library/Keychains/System.keychain
/usr/bin/security set-keychain-settings -lut 21600 "$kc"
/usr/bin/security unlock-keychain -p "" "$kc"
`
}

func claudeAgentKeychainResetScript() string {
	return `set -e
kc="$HOME/Library/Keychains/login.keychain-db"
ts="$(date +%Y%m%d-%H%M%S)"
backup_dir="$HOME/Library/Keychains/hazmat-login-keychain-backups/$ts"

mkdir -p "$HOME/Library/Keychains"

moved=0
for path in "$kc" "$kc-shm" "$kc-wal"; do
  if [ -e "$path" ]; then
    mkdir -p "$backup_dir"
    mv -f "$path" "$backup_dir/$(basename "$path")"
    moved=1
  fi
done

if [ "$moved" = "1" ]; then
  echo "Backed up: $backup_dir"
fi

/usr/bin/security create-keychain -p "" "$kc"
/usr/bin/security login-keychain -s "$kc" >/dev/null 2>&1 || true
/usr/bin/security default-keychain -s "$kc"
/usr/bin/security list-keychains -d user -s "$kc" /System/Library/Keychains/SystemRootCertificates.keychain /Library/Keychains/System.keychain
/usr/bin/security set-keychain-settings -lut 21600 "$kc"
/usr/bin/security unlock-keychain -p "" "$kc"
echo "Created unlocked agent login keychain: $kc"
`
}

package hazmat

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
			if err := prepareAgentLoginKeychainForLaunch(); err != nil {
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

// prepareAgentLoginKeychainForLaunch provisions and unlocks the shared agent
// account login keychain with Hazmat's empty-password profile before a native
// launch. It is harness-neutral: every agent-user harness that reads or writes
// OAuth through the macOS Keychain (Claude Code, Antigravity) uses this same
// /Users/agent/Library/Keychains/login.keychain-db.
func prepareAgentLoginKeychainForLaunch() error {
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
		return fmt.Errorf("prepare agent login keychain: could not unlock %s with Hazmat's empty-password keychain profile%s\nRun: hazmat claude-keychain reset",
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
marker="$HOME/Library/Keychains/.hazmat-login-keychain"
/bin/mkdir -p "$HOME/Library/Keychains"

best_effort_security() {
  "$@" >/dev/null 2>&1 &
  pid=$!
  (
    sleep 5
    kill "$pid" >/dev/null 2>&1 || true
    sleep 1
    kill -KILL "$pid" >/dev/null 2>&1 || true
  ) &
  watchdog=$!
  wait "$pid" >/dev/null 2>&1 || true
  kill "$watchdog" >/dev/null 2>&1 || true
  wait "$watchdog" >/dev/null 2>&1 || true
  return 0
}

reset_agent_keychain() {
  ts="$(/bin/date +%Y%m%d-%H%M%S)"
  backup_dir="$HOME/Library/Keychains/hazmat-login-keychain-backups/$ts"
  moved=0
  for kc_file in "$kc" "$kc-shm" "$kc-wal"; do
    if [ -e "$kc_file" ]; then
      /bin/mkdir -p "$backup_dir"
      /bin/mv -f "$kc_file" "$backup_dir/$(/usr/bin/basename "$kc_file")"
      moved=1
    fi
  done
  if [ "$moved" = "1" ]; then
    echo "Backed up existing agent login keychain: $backup_dir"
  fi
  /usr/bin/security create-keychain -p "" "$kc"
  printf '%s\n' "hazmat-managed claude agent login keychain" > "$marker"
  /bin/chmod 0600 "$marker" >/dev/null 2>&1 || true
  /usr/bin/security unlock-keychain -p "" "$kc"
  best_effort_security /usr/bin/security login-keychain -s "$kc"
  /usr/bin/security default-keychain -s "$kc"
  /usr/bin/security list-keychains -d user -s "$kc" /System/Library/Keychains/SystemRootCertificates.keychain /Library/Keychains/System.keychain
  best_effort_security /usr/bin/security set-keychain-settings -lut 21600 "$kc"
}

if [ ! -e "$kc" ]; then
  reset_agent_keychain
elif [ ! -f "$marker" ]; then
  reset_agent_keychain
elif ! /usr/bin/security unlock-keychain -p "" "$kc" >/dev/null 2>&1; then
  reset_agent_keychain
fi

/usr/bin/security unlock-keychain -p "" "$kc"
/usr/bin/security default-keychain -s "$kc"
/usr/bin/security list-keychains -d user -s "$kc" /System/Library/Keychains/SystemRootCertificates.keychain /Library/Keychains/System.keychain
/usr/bin/security unlock-keychain -p "" "$kc"
`
}

func claudeAgentKeychainResetScript() string {
	return `set -e
kc="$HOME/Library/Keychains/login.keychain-db"
marker="$HOME/Library/Keychains/.hazmat-login-keychain"
ts="$(/bin/date +%Y%m%d-%H%M%S)"
backup_dir="$HOME/Library/Keychains/hazmat-login-keychain-backups/$ts"

/bin/mkdir -p "$HOME/Library/Keychains"

best_effort_security() {
  "$@" >/dev/null 2>&1 &
  pid=$!
  (
    sleep 5
    kill "$pid" >/dev/null 2>&1 || true
    sleep 1
    kill -KILL "$pid" >/dev/null 2>&1 || true
  ) &
  watchdog=$!
  wait "$pid" >/dev/null 2>&1 || true
  kill "$watchdog" >/dev/null 2>&1 || true
  wait "$watchdog" >/dev/null 2>&1 || true
  return 0
}

moved=0
for kc_file in "$kc" "$kc-shm" "$kc-wal"; do
  if [ -e "$kc_file" ]; then
    /bin/mkdir -p "$backup_dir"
    /bin/mv -f "$kc_file" "$backup_dir/$(/usr/bin/basename "$kc_file")"
    moved=1
  fi
done

if [ "$moved" = "1" ]; then
  echo "Backed up: $backup_dir"
fi

/usr/bin/security create-keychain -p "" "$kc"
printf '%s\n' "hazmat-managed claude agent login keychain" > "$marker"
/bin/chmod 0600 "$marker" >/dev/null 2>&1 || true
/usr/bin/security unlock-keychain -p "" "$kc"
best_effort_security /usr/bin/security login-keychain -s "$kc"
/usr/bin/security default-keychain -s "$kc"
/usr/bin/security list-keychains -d user -s "$kc" /System/Library/Keychains/SystemRootCertificates.keychain /Library/Keychains/System.keychain
best_effort_security /usr/bin/security set-keychain-settings -lut 21600 "$kc"
echo "Created unlocked agent login keychain: $kc"
`
}

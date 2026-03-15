package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// flagVerbose and flagDryRun are persistent flags bound to the root command
// so they are available on every subcommand without repetition.
var (
	flagVerbose bool
	flagDryRun  bool
)

// Sandbox configuration — must match setup.sh exactly.
const (
	agentUser       = "agent"
	agentUID        = "599"
	agentHome       = "/Users/agent"
	sharedWorkspace = "/Users/Shared/workspace"
	sharedGroup     = "dev"
	sharedGID       = "599"
	pfAnchorName    = "agent"
	pfAnchorFile    = "/etc/pf.anchors/agent"
	pfDaemonLabel   = "com.local.pf-agent"
	pfDaemonPlist   = "/Library/LaunchDaemons/com.local.pf-agent.plist"
	sudoersFile     = "/etc/sudoers.d/agent"
	hostsMarker     = "# === AI Agent Blocklist ==="

	seatbeltProfileDir  = agentHome + "/.config/sandbox"
	seatbeltProfilePath = agentHome + "/.config/sandbox/claude.sb"
	seatbeltWrapperPath = agentHome + "/.local/bin/claude-sandboxed"
)

func main() {
	root := &cobra.Command{
		Use:           "sandbox",
		Short:         "macOS Claude Code sandbox management",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false,
		"Print each command before executing")
	root.PersistentFlags().BoolVarP(&flagDryRun, "dry-run", "n", false,
		"Print all commands without executing (implies --verbose)")

	root.AddCommand(
		newSetupCmd(),
		newTestCmd(),
		newBackupCmd(),
		newRestoreCmd(),
		newRollbackCmd(),
		newConnectCmd(), // hidden: used internally for agent-user network probes
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newConnectCmd is a hidden internal subcommand that dials host:port and exits
// 0 on success, 1 on failure.  Invoked as: sudo -u agent sandbox _connect host port
// This lets the test command probe network reachability as the agent user using
// Go's net.Dial rather than bash's /dev/tcp, without requiring any special setup.
func newConnectCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_connect <host> <port>",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		Run: func(_ *cobra.Command, args []string) {
			conn, err := net.DialTimeout("tcp",
				net.JoinHostPort(args[0], args[1]),
				5*time.Second,
			)
			if err != nil {
				os.Exit(1)
			}
			conn.Close()
		},
	}
}

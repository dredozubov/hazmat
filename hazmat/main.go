package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags:
//
//	go build -ldflags "-X main.version=v0.1.0"
var version = "dev"

// flagVerbose, flagDryRun, and flagYesAll are persistent flags bound to the
// root command so they are available on every subcommand without repetition.
var (
	flagVerbose bool
	flagDryRun  bool
	flagYesAll  bool
)

// agentUID and sharedGID are vars so setup can override them via --agent-uid
// and --group-gid flags when the defaults conflict with existing UIDs/GIDs.
var (
	agentUID  = "599"
	sharedGID = "599"
)

// cloudBackupDir is the directory that `hazmat backup --cloud` snapshots.
// There is no "managed workspace" concept — any directory is a valid project.
// This is solely for the cloud backup scope.
var cloudBackupDir = filepath.Join(os.Getenv("HOME"), "workspace")

// Hazmat configuration shared by the Go-based setup, test, and rollback flows.
const (
	agentUser     = "agent"
	agentHome     = "/Users/agent"
	launchHelper  = "/usr/local/libexec/hazmat-launch"
	sharedGroup   = "dev"
	pfAnchorName  = "agent"
	pfAnchorFile  = "/etc/pf.anchors/agent"
	pfDaemonLabel = "com.local.pf-agent"
	pfDaemonPlist = "/Library/LaunchDaemons/com.local.pf-agent.plist"
	sudoersFile   = "/etc/sudoers.d/agent"
	hostsMarker   = "# === AI Agent Blocklist ==="

	seatbeltProfileDir  = agentHome + "/.config/hazmat"
	seatbeltWrapperPath = agentHome + "/.local/bin/claude-sandboxed"
	agentEnvPath        = seatbeltProfileDir + "/agent-env.zsh"

	agentShellBlockStart = "# >>> hazmat agent shell >>>"
	agentShellBlockEnd   = "# <<< hazmat agent shell <<<"
	userPathBlockStart   = "# >>> hazmat user path >>>"
	userPathBlockEnd     = "# <<< hazmat user path <<<"
	umaskBlockStart      = "# >>> hazmat umask >>>"
	umaskBlockEnd        = "# <<< hazmat umask <<<"

	hostWrapperDirRel      = ".local/bin"
	hostClaudeWrapperName  = "claude-hazmat"
	hostExecWrapperName    = "agent-exec"
	hostShellWrapperName   = "agent-shell"
	defaultAgentPath       = agentHome + "/.local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	defaultAgentCacheHome  = agentHome + "/.cache"
	defaultAgentConfigHome = agentHome + "/.config"
	defaultAgentDataHome   = agentHome + "/.local/share"
	defaultAgentTmpDir     = "/private/tmp"
)

func main() {
	root := &cobra.Command{
		Use:           "hazmat",
		Short:         "Hazmat — AI agent containment for macOS",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// No subcommand: show status checklist + hint to use --help.
			if err := runStatus(false); err != nil {
				return err
			}
			cDim.Println("  Run hazmat --help for all commands.")
			fmt.Println()
			return nil
		},
	}

	root.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false,
		"Print each command before executing")
	root.PersistentFlags().BoolVarP(&flagDryRun, "dry-run", "n", false,
		"Print all commands without executing (implies --verbose)")
	root.PersistentFlags().BoolVarP(&flagYesAll, "yes", "y", false,
		"Answer yes to all prompts (for non-interactive / scripted use)")

	// ── Setup ──
	initCmd := newInitCmd()
	initCmd.GroupID = "setup"
	initCmd.AddCommand(newInitCloudCmd())
	rollbackCmd := newRollbackCmd()
	rollbackCmd.GroupID = "setup"
	checkCmd := newInitCheckCmd()
	checkCmd.GroupID = "setup"

	// ── Run agents ──
	claudeCmd := newClaudeCmd()
	claudeCmd.GroupID = "run"
	shellCmd := newShellCmd()
	shellCmd.GroupID = "run"
	execCmd := newExecCmd()
	execCmd.GroupID = "run"

	// ── Snapshots ──
	snapshotsCmd := newSnapshotsCmd()
	snapshotsCmd.GroupID = "snap"
	diffCmd := newDiffCmd()
	diffCmd.GroupID = "snap"
	restoreCmd := newRestoreCmd()
	restoreCmd.GroupID = "snap"

	// ── Workspace ──
	configCmd := newConfigCmd()
	configCmd.GroupID = "ws"
	backupCmd := newBackupCmd()
	backupCmd.GroupID = "ws"
	statusCmd := newStatusCmd()
	statusCmd.GroupID = "ws"

	root.AddGroup(
		&cobra.Group{ID: "setup", Title: "Setup:"},
		&cobra.Group{ID: "run", Title: "Run agents:"},
		&cobra.Group{ID: "snap", Title: "Snapshots:"},
		&cobra.Group{ID: "ws", Title: "Workspace:"},
	)
	root.AddCommand(
		initCmd, rollbackCmd, checkCmd,
		claudeCmd, shellCmd, execCmd,
		snapshotsCmd, diffCmd, restoreCmd,
		configCmd, backupCmd, statusCmd,
		newConnectCmd(),
	)
	root.SetHelpCommandGroupID("ws")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newConnectCmd is a hidden internal subcommand that dials host:port and exits
// 0 on success, 1 on failure.  Invoked as: sudo -u agent hazmat _connect host port
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

package main

import (
	"fmt"
	"io"
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
// Native platform paths and host integration defaults live in platform_paths_*.go.
const (
	agentShellBlockStart = "# >>> hazmat agent shell >>>"
	agentShellBlockEnd   = "# <<< hazmat agent shell <<<"
	userPathBlockStart   = "# >>> hazmat user path >>>"
	userPathBlockEnd     = "# <<< hazmat user path <<<"
	completionBlockStart = "# >>> hazmat completions >>>"
	completionBlockEnd   = "# <<< hazmat completions <<<"
	umaskBlockStart      = "# >>> hazmat umask >>>"
	umaskBlockEnd        = "# <<< hazmat umask <<<"

	hostWrapperDirRel     = ".local/bin"
	hostClaudeWrapperName = "claude-hazmat"
	hostExecWrapperName   = "agent-exec"
	hostShellWrapperName  = "agent-shell"
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
	bootstrapCmd := newBootstrapCmd()
	bootstrapCmd.GroupID = "setup"
	rollbackCmd := newRollbackCmd()
	rollbackCmd.GroupID = "setup"
	checkCmd := newInitCheckCmd()
	checkCmd.GroupID = "setup"
	sandboxCmd := newSandboxCmd()
	sandboxCmd.GroupID = "setup"

	// ── Run agents ──
	claudeCmd := newClaudeCmd()
	claudeCmd.GroupID = "run"
	codexCmd := newCodexCmd()
	codexCmd.GroupID = "run"
	opencodeCmd := newOpenCodeCmd()
	opencodeCmd.GroupID = "run"
	geminiCmd := newGeminiCmd()
	geminiCmd.GroupID = "run"
	shellCmd := newShellCmd()
	shellCmd.GroupID = "run"
	execCmd := newExecCmd()
	execCmd.GroupID = "run"
	explainCmd := newExplainCmd()
	explainCmd.GroupID = "run"

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
	integrationCmd := newIntegrationCmd()
	integrationCmd.GroupID = "ws"
	backupCmd := newBackupCmd()
	backupCmd.GroupID = "ws"
	statusCmd := newStatusCmd()
	statusCmd.GroupID = "ws"
	exportCmd := newExportCmd()
	exportCmd.GroupID = "ws"
	hooksCmd := newHooksCmd()
	hooksCmd.GroupID = "ws"

	root.AddGroup(
		&cobra.Group{ID: "setup", Title: "Setup:"},
		&cobra.Group{ID: "run", Title: "Run agents:"},
		&cobra.Group{ID: "snap", Title: "Snapshots:"},
		&cobra.Group{ID: "ws", Title: "Workspace:"},
	)
	root.AddCommand(
		initCmd, bootstrapCmd, rollbackCmd, checkCmd, sandboxCmd,
		claudeCmd, codexCmd, opencodeCmd, geminiCmd, shellCmd, execCmd, explainCmd,
		snapshotsCmd, diffCmd, restoreCmd,
		configCmd, integrationCmd, backupCmd, statusCmd, exportCmd, hooksCmd,
		newConnectCmd(), newGitSSHTransportCmd(), newGitHTTPSCredentialCmd(), newStackCheckCmd(), newCompletionCmd(root),
		newGitHookWrapperCmd(), newGitHookDispatchCmd(), newGitHookFallbackCmd(),
	)
	root.SetHelpCommandGroupID("ws")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newConnectCmd is a hidden internal subcommand that dials host:port and exits
// 0 on success, 1 on failure. Invoked through Hazmat's helper-backed
// agent-maintenance path so the TCP dial runs as the agent user. This lets the
// test command probe network reachability using Go's net.Dial rather than
// bash's /dev/tcp, without requiring any special setup.
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
			conn.Close() //nolint:errcheck // diagnostic probe; process exits immediately
		},
	}
}

func newGitSSHTransportCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "_git_ssh_transport <socket> [ssh-args...]",
		Hidden:             true,
		DisableFlagParsing: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("_git_ssh_transport requires a broker socket and ssh arguments")
			}
			return nil
		},
		Run: func(_ *cobra.Command, args []string) {
			os.Exit(runGitSSHTransportHelper(args[0], args[1:]))
		},
	}
}

func newGitHTTPSCredentialCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_git_https_credential <socket> <operation>",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			payload, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read Git HTTPS credential request from stdin: %w", err)
			}
			resp, err := requestGitHTTPSCredential(args[0], args[1], payload)
			if len(resp.Stdout) > 0 {
				if _, writeErr := os.Stdout.Write(resp.Stdout); writeErr != nil && err == nil {
					err = writeErr
				}
			}
			if len(resp.Stderr) > 0 {
				if _, writeErr := os.Stderr.Write(resp.Stderr); writeErr != nil && err == nil {
					err = writeErr
				}
			}
			return err
		},
	}
}

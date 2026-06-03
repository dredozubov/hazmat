package hazmat

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"hazmat/internal/frontend/cli"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags:
//
//	go build -ldflags "-X hazmat.version=v0.1.0"
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

func Main() {
	if err := NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func NewRootCommand() *cobra.Command {
	initCmd := withUpdateNotifications(newInitCmd())
	initCmd.AddCommand(newInitCloudCmd())

	return cli.NewRootCommand(cli.RootConfig{
		Version: version,
		Flags: cli.PersistentFlags{
			Verbose: &flagVerbose,
			DryRun:  &flagDryRun,
			YesAll:  &flagYesAll,
		},
		DefaultRun: defaultRootRun,
		Completion: newCompletionCmd,
		AddDebug:   addDebugCommands,
		Setup: []*cobra.Command{
			initCmd,
			withUpdateNotifications(newBootstrapCmd()),
			withUpdateNotifications(newHarnessCmd()),
			withUpdateNotifications(newRollbackCmd()),
			withUpdateNotifications(newInitCheckCmd()),
			newSandboxCmd(),
		},
		Run: []*cobra.Command{
			withUpdateNotifications(newClaudeCmd()),
			newClaudeKeychainCmd(),
			withUpdateNotifications(newCodexCmd()),
			withUpdateNotifications(newCodexAppServerCmd()),
			withUpdateNotifications(newCodexAppShimCmd()),
			withUpdateNotifications(newOpenCodeCmd()),
			withUpdateNotifications(newGeminiCmd()),
			withUpdateNotifications(newHermesCmd()),
			withUpdateNotifications(newQwenCmd()),
			withUpdateNotifications(newCursorAgentCmd()),
			withUpdateNotifications(newShellCmd()),
			withUpdateNotifications(newExecCmd()),
			newExplainCmd(),
		},
		Snapshots: []*cobra.Command{
			newSnapshotsCmd(),
			newDiffCmd(),
			newRestoreCmd(),
		},
		Workspace: []*cobra.Command{
			newConfigCmd(),
			newMigrateCmd(),
			newIntegrationCmd(),
			newBackupCmd(),
			withUpdateNotifications(newStatusCmd()),
			newExportCmd(),
			newHooksCmd(),
		},
		Hidden: []*cobra.Command{
			newConnectCmd(),
			newGitSSHTransportCmd(),
			newGitHTTPSCredentialCmd(),
			newStackCheckCmd(),
			newGitHookWrapperCmd(),
			newGitHookDispatchCmd(),
			newGitHookFallbackCmd(),
		},
	})
}

func defaultRootRun(_ *cobra.Command, _ []string) error {
	maybeNotifyUpdateAvailable(os.Stderr)
	defer maybeNotifyUpdateAvailable(os.Stderr)
	if err := runStatus(false); err != nil {
		return err
	}
	cDim.Println("  Run hazmat --help for all commands.")
	fmt.Println()
	return nil
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

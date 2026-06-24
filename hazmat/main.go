package hazmat

import (
	"fmt"
	"os"
	"time"

	"hazmat/internal/agententry"
	"hazmat/internal/diagnostics"
	"hazmat/internal/frontend/cli"
	"hazmat/internal/hookruntime"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags:
//
//	go build -ldflags "-X hazmat.version=v0.1.0"
var version = "dev"
var processStartTime = time.Now()

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
			withUpdateNotifications(diagnostics.NewCheckCommand(runTest)),
			withUpdateNotifications(diagnostics.NewDoctorCommand(runTest)),
			withUpdateNotifications(newRepairCmd()),
			newSandboxCmd(),
		},
		Run: []*cobra.Command{
			withUpdateNotifications(newClaudeCmd()),
			newClaudeKeychainCmd(),
			withUpdateNotifications(newCodexCmd()),
			withUpdateNotifications(newCodexAppServerCmd()),
			withUpdateNotifications(newCodexAppShimCmd()),
			withUpdateNotifications(newOpenCodeCmd()),
			withUpdateNotifications(newAntigravityCmd()),
			withUpdateNotifications(newHermesCmd()),
			withUpdateNotifications(newQwenCmd()),
			withUpdateNotifications(newCursorAgentCmd()),
			withUpdateNotifications(newPiCmd()),
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
			newRepairAgentDirCmd(),
			agententry.NewConnectCommand(),
			agententry.NewGitSSHTransportCommand(runGitSSHTransportHelper),
			agententry.NewGitHTTPSCredentialCommand(requestGitHTTPSCredentialForAgentEntry),
			agententry.NewLaunchBrokerCommand(runLaunchBrokerAgentEntry),
			diagnostics.NewStackCheckCommand(diagnostics.StackcheckCommandConfig{
				RequireInitialized: requireAgentUserForDiagnostics,
			}),
			hookruntime.NewGitHookWrapperCommand(runProjectHookGitWrapper),
			hookruntime.NewGitHookDispatchCommand(requestGitHookDispatchForHookRuntime),
			hookruntime.NewGitHookFallbackCommand(requestGitHookFallbackForHookRuntime),
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

func requestGitHTTPSCredentialForAgentEntry(socketPath, operation string, payload []byte) (agententry.GitHTTPSCredentialResponse, error) {
	resp, err := requestGitHTTPSCredential(socketPath, operation, payload)
	return agententry.GitHTTPSCredentialResponse{
		Stdout: resp.Stdout,
		Stderr: resp.Stderr,
	}, err
}

func requireAgentUserForDiagnostics() error {
	_, err := requireAgentUser()
	return err
}

func requestGitHookDispatchForHookRuntime(projectDir, hookName string, args []string) error {
	return runApprovedProjectHook(projectDir, hookType(hookName), args)
}

func requestGitHookFallbackForHookRuntime(projectDir, hookName string) error {
	return fallbackProjectHookRefusal(projectDir, hookType(hookName))
}

package hazmat

import (
	"fmt"
	"io"
	"os"
	goruntime "runtime"
	"time"

	"github.com/spf13/cobra"

	applecontainerspec "hazmat/containment/applecontainer"
	"hazmat/internal/backupruntime"
	applecontainerruntime "hazmat/internal/runtime/applecontainer"
)

// runAppleContainerExecSession is the experimental Apple Container session
// path for `hazmat exec --backend=apple-container`. It follows the proved
// MC_AppleContainerLaunch ordering: forbidden features and mount inputs are
// rejected at compile time, admission gates launch, only network default is
// supported, and the session container is removed by exact name with any
// cleanup failure reported, never silently dropped.
//
// Identity model (revised 2026-06-10): the container CLI runs as the
// invoking user. Host account isolation is NOT provided by this backend —
// the boundary is the VM plus exact mount planning — and the session
// contract states that before launch.
func runAppleContainerExecSession(cmd *cobra.Command, flags sessionCommandFlags, image string, args []string) error {
	if !applecontainerruntime.GateEnabled() {
		return applecontainerruntime.GateError()
	}
	if image == "" {
		return fmt.Errorf("--backend=apple-container requires --image (the backend never guesses an image)")
	}
	if cmd.Flags().Changed("docker") && flags.dockerModeValue != string(dockerModeNone) {
		return fmt.Errorf("--backend=apple-container cannot be combined with --docker=%s", flags.dockerModeValue)
	}

	// Resolve through the plan-only session resolver: project, read-only,
	// and read-write inputs get the same typed validation (including
	// credential deny-zone rejection) as every other session, and no native
	// host mutations are planned — this backend repairs nothing on the host.
	opts := flags.harnessSessionOpts(cmd)
	opts.planOnly = true
	cfg, _, err := resolveExplainSession("exec", opts)
	if err != nil {
		return err
	}
	policy, err := buildNativeSessionPolicy(cfg)
	if err != nil {
		return err
	}

	host := applecontainerruntime.ProbeHost(applecontainerruntime.HostRunner(), goruntime.GOOS, goruntime.GOARCH)
	spec, err := applecontainerspec.Compile(policy.Contract, applecontainerspec.CompileOptions{
		Harness:            "exec",
		Image:              image,
		SessionID:          fmt.Sprintf("%d-%d", os.Getpid(), time.Now().Unix()),
		Command:            args,
		GuestUID:           os.Getuid(),
		GuestGID:           os.Getgid(),
		IntegrationEnvKeys: integrationEnvKeyNames(cfg.IntegrationEnv),
		ExecutableRuntime:  true,
		Host:               host,
	})
	if err != nil {
		return err
	}

	printAppleContainerSessionContract(cmd.ErrOrStderr(), spec)
	backupruntime.PreSessionSnapshot(backupruntime.PreSessionSnapshotOptions{
		ProjectDir:     cfg.ProjectDir,
		Command:        "exec",
		BackupExcludes: cfg.BackupExcludes,
		Skip:           opts.noBackup,
		Snapshot:       snapshotProject,
	})

	result, runErr := applecontainerruntime.Run(spec, applecontainerruntime.RunOptions{
		Stdin:  cmd.InOrStdin(),
		Stdout: cmd.OutOrStdout(),
		Stderr: cmd.ErrOrStderr(),
	})
	for _, failure := range result.CleanupFailures {
		fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: session cleanup failure (remove manually): %s\n", failure)
	}
	if runErr != nil {
		return runErr
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("apple-container session command exited with status %d", result.ExitCode)
	}
	return nil
}

func printAppleContainerSessionContract(w io.Writer, spec applecontainerspec.LaunchSpec) {
	fmt.Fprintln(w, "Apple Container backend (EXPERIMENTAL): Linux VM-per-session execution with")
	fmt.Fprintln(w, "Hazmat-planned host mounts. Host file IO occurs as the invoking macOS user.")
	fmt.Fprintln(w, "Host account isolation is not provided by this backend; use native")
	fmt.Fprintln(w, "containment for that.")
	fmt.Fprintln(w, "")
	printAppleContainerPlan(w, spec)
}

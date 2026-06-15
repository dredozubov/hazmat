package hazmat

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"hazmat/internal/runtime/launchbroker"
)

const (
	launchBrokerExperimentalEnv = "HAZMAT_EXPERIMENTAL_LAUNCH_BROKER"
	launchBrokerSocketEnv       = "HAZMAT_LAUNCH_BROKER_SOCKET"
	defaultLaunchBrokerTimeout  = 5 * time.Second
)

var (
	launchBrokerRoundTrip               = defaultLaunchBrokerRoundTrip
	launchBrokerEnsureDefault           = defaultEnsureLaunchBroker
	launchBrokerStdout        io.Writer = os.Stdout
	launchBrokerStderr        io.Writer = os.Stderr
)

type launchBrokerExitError struct {
	code int
}

func (e launchBrokerExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.code)
}

func (e launchBrokerExitError) ExitCode() int {
	return e.code
}

func tryRunNativeLaunchViaBroker(cfg sessionConfig, plan sessionBackendPlan, ui sessionLaunchUI, policy nativeLaunchPolicyArtifact, runtimeEnvPairs []string, metadataJSON, launchHelperTempDir, script string, args ...string) (bool, error) {
	socketPath, explicit, err := configuredLaunchBrokerSocketPath(os.Getenv)
	if err != nil {
		return true, err
	}
	if socketPath == "" || !launchBrokerBufferedEligible(ui) {
		return false, nil
	}

	req := nativeLaunchBrokerRequestWithMetadataPlanAndRuntime(cfg, plan, policy, runtimeEnvPairs, metadataJSON, launchHelperTempDir, script, args...)
	resp, err := launchBrokerRoundTrip(context.Background(), socketPath, req)
	if err != nil && !explicit {
		if ensureErr := launchBrokerEnsureDefault(context.Background()); ensureErr == nil {
			resp, err = launchBrokerRoundTrip(context.Background(), socketPath, req)
		}
	}
	if err != nil {
		if explicit {
			return true, err
		}
		return false, nil
	}
	return true, writeLaunchBrokerResponse(resp, metadataJSON, launchBrokerStdout, launchBrokerStderr)
}

func configuredLaunchBrokerSocketPath(getenv func(string) string) (socketPath string, explicit bool, err error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if path := getenv(launchBrokerSocketEnv); path != "" {
		clean, err := cleanLaunchBrokerSocketPath(path)
		return clean, true, err
	}
	if getenv(launchBrokerExperimentalEnv) != "1" {
		return "", false, nil
	}
	return defaultLaunchBrokerSocketPath(os.Getuid()), false, nil
}

func cleanLaunchBrokerSocketPath(path string) (string, error) {
	clean := filepath.Clean(path)
	if path == "" || !filepath.IsAbs(clean) || clean != path {
		return "", fmt.Errorf("launch broker socket path %q must be absolute and clean", path)
	}
	return clean, nil
}

func launchBrokerBufferedEligible(ui sessionLaunchUI) bool {
	return !ui.clearScreen && !ui.showStatusBar && !ui.waitForAltScreen
}

func nativeLaunchBrokerRequestWithMetadataPlanAndRuntime(cfg sessionConfig, plan sessionBackendPlan, policy nativeLaunchPolicyArtifact, runtimeEnvPairs []string, metadataJSON, launchHelperTempDir, script string, args ...string) launchbroker.LaunchRequest {
	directExec := script == nativeDirectProjectExecScript && launchHelperSupportsDirectExec(launchHelperPath())
	workingDir := ""
	if directExec {
		workingDir = cfg.ProjectDir
	}
	return launchbroker.LaunchRequest{
		PolicyPath:      policy.Path,
		MetadataJSON:    metadataJSON,
		DirectExec:      directExec,
		WorkingDir:      workingDir,
		SessionTempDir:  launchHelperTempDir,
		EnvPairs:        newNativeLaunchBackend().AgentEnvPairs(nativeLaunchEnvRequest{Config: cfg, Plan: plan}),
		RuntimeEnvPairs: append([]string(nil), runtimeEnvPairs...),
		Script:          script,
		Args:            append([]string(nil), args...),
	}
}

func defaultLaunchBrokerRoundTrip(ctx context.Context, socketPath string, req launchbroker.LaunchRequest) (launchbroker.LaunchResponse, error) {
	return launchbroker.Client{
		SocketPath: socketPath,
		Timeout:    defaultLaunchBrokerTimeout,
	}.Launch(ctx, req)
}

func defaultEnsureLaunchBroker(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	uid := os.Getuid()
	runtimeDir, err := prepareDefaultLaunchBrokerRuntimeDir(uid)
	if err != nil {
		return err
	}
	hazmatPath, err := currentExecutablePath()
	if err != nil {
		return fmt.Errorf("resolve hazmat executable for launch broker: %w", err)
	}
	_, err = startLaunchBrokerSupervisor(ctx, launchBrokerSupervisorConfig{
		RuntimeDir:       runtimeDir,
		SocketName:       defaultLaunchBrokerSocketName(uid),
		ExpectedPeerUID:  uid,
		HazmatPath:       hazmatPath,
		LaunchHelperPath: launchHelperPath(),
		ReadyTimeout:     defaultLaunchBrokerReadyTimeout,
	})
	return err
}

func prepareDefaultLaunchBrokerRuntimeDir(uid int) (string, error) {
	if uid <= 0 {
		return "", fmt.Errorf("launch broker uid must be positive, got %d", uid)
	}
	root := filepath.Clean(brokerRuntimeRoot)
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("broker runtime root %q must be absolute", brokerRuntimeRoot)
	}
	if err := os.MkdirAll(root, 0o733|os.ModeSticky); err != nil {
		return "", fmt.Errorf("create broker runtime root: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("inspect broker runtime root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s: broker runtime root is a symlink", root)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s: broker runtime root is not a directory", root)
	}
	if err := os.Chmod(root, 0o733|os.ModeSticky); err != nil {
		return "", fmt.Errorf("set broker runtime root mode: %w", err)
	}

	runtimeDir := filepath.Join(root, fmt.Sprintf("launch-%d", uid))
	if info, err := os.Lstat(runtimeDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s: launch broker runtime dir is a symlink", runtimeDir)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s: launch broker runtime path is not a directory", runtimeDir)
		}
		return runtimeDir, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect launch broker runtime dir: %w", err)
	}

	if err := brokerRuntimeAgentEnsureSharedDir(runtimeDir, 0o2770); err != nil {
		return "", err
	}
	return runtimeDir, nil
}

func defaultLaunchBrokerSocketName(uid int) string {
	return fmt.Sprintf("launch-broker-%d.sock", uid)
}

func defaultLaunchBrokerSocketPath(uid int) string {
	return filepath.Join(filepath.Clean(brokerRuntimeRoot), fmt.Sprintf("launch-%d", uid), defaultLaunchBrokerSocketName(uid))
}

func writeLaunchBrokerResponse(resp launchbroker.LaunchResponse, expectedMetadata string, stdout, stderr io.Writer) error {
	if expectedMetadata != "" && resp.MetadataJSON == "" {
		return fmt.Errorf("launch broker did not confirm containment metadata")
	}
	if resp.MetadataJSON != "" {
		if _, err := fmt.Fprintln(stderr, resp.MetadataJSON); err != nil {
			return err
		}
	}
	if resp.Stdout != "" {
		if _, err := io.WriteString(stdout, resp.Stdout); err != nil {
			return err
		}
	}
	if resp.Stderr != "" {
		if _, err := io.WriteString(stderr, resp.Stderr); err != nil {
			return err
		}
	}
	if resp.ExitCode != 0 {
		return launchBrokerExitError{code: resp.ExitCode}
	}
	return nil
}

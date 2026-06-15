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
	launchBrokerRoundTrip           = defaultLaunchBrokerRoundTrip
	launchBrokerStdout    io.Writer = os.Stdout
	launchBrokerStderr    io.Writer = os.Stderr
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
	return filepath.Join(defaultBrokerRuntimeRoot, fmt.Sprintf("launch-%d", os.Getuid()), fmt.Sprintf("launch-broker-%d.sock", os.Getuid())), false, nil
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

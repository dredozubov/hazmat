package hazmat

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type commandLaunchTestFixture struct {
	t            *testing.T
	hostHome     string
	agentHomeDir string
	projectDir   string
	onLaunch     func(sessionConfig, []string) error
}

func newCommandLaunchTestFixture(t *testing.T) *commandLaunchTestFixture {
	t.Helper()
	isolateConfig(t)
	skipInitCheck(t)

	root := t.TempDir()
	f := &commandLaunchTestFixture{
		t:            t,
		hostHome:     filepath.Join(root, "host-home"),
		agentHomeDir: filepath.Join(root, "agent-home"),
		projectDir:   filepath.Join(root, "project"),
	}
	for _, dir := range []string{f.hostHome, f.agentHomeDir, f.projectDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	t.Setenv("HOME", f.hostHome)

	savedAgentPathForDirectIO := agentPathForDirectIO
	agentPathForDirectIO = f.mapAgentPath
	t.Cleanup(func() { agentPathForDirectIO = savedAgentPathForDirectIO })

	savedAgentZshrcPath := agentZshrcPath
	agentZshrcPath = filepath.Join(f.agentHomeDir, ".zshrc")
	t.Cleanup(func() { agentZshrcPath = savedAgentZshrcPath })

	savedNewAgentCommand := newAgentCommand
	newAgentCommand = f.newAgentCommand
	t.Cleanup(func() { newAgentCommand = savedNewAgentCommand })

	savedSupportsSessionTemp := launchHelperSupportsSessionTemp
	launchHelperSupportsSessionTemp = func(string) bool { return true }
	t.Cleanup(func() { launchHelperSupportsSessionTemp = savedSupportsSessionTemp })

	savedStartRemoval := startAgentTempRuntimeRemoval
	startAgentTempRuntimeRemoval = func(string) error { return nil }
	t.Cleanup(func() { startAgentTempRuntimeRemoval = savedStartRemoval })

	savedPrepareGitHTTPS := prepareGitHTTPSCredentialRuntime
	prepareGitHTTPSCredentialRuntime = func() (preparedSessionRuntime, error) {
		return preparedSessionRuntime{Cleanup: func() {}}, nil
	}
	t.Cleanup(func() { prepareGitHTTPSCredentialRuntime = savedPrepareGitHTTPS })

	savedExecuteMutationPlan := executeSessionMutationPlan
	executeSessionMutationPlan = func(sessionMutationPlan) error { return nil }
	t.Cleanup(func() { executeSessionMutationPlan = savedExecuteMutationPlan })

	savedInspectHooks := inspectProjectHooksForPrompt
	inspectProjectHooksForPrompt = func(string) (string, inspectedProjectHooks, error) {
		return "", inspectedProjectHooks{}, nil
	}
	t.Cleanup(func() { inspectProjectHooksForPrompt = savedInspectHooks })

	savedDetectDockerProject := detectDockerProjectForSession
	detectDockerProjectForSession = func(string) dockerProjectDetection { return dockerProjectDetection{} }
	t.Cleanup(func() { detectDockerProjectForSession = savedDetectDockerProject })

	savedRunLaunch := runAgentSeatbeltScriptWithPlan
	runAgentSeatbeltScriptWithPlan = func(cfg sessionConfig, _ sessionBackendPlan, _ sessionLaunchUI, _ string, args ...string) error {
		if f.onLaunch == nil {
			return nil
		}
		return f.onLaunch(cfg, append([]string(nil), args...))
	}
	t.Cleanup(func() { runAgentSeatbeltScriptWithPlan = savedRunLaunch })

	return f
}

func (f *commandLaunchTestFixture) mapAgentPath(path string) string {
	if path == agentHome {
		return f.agentHomeDir
	}
	prefix := agentHome + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return filepath.Join(f.agentHomeDir, strings.TrimPrefix(path, prefix))
	}
	return path
}

func (f *commandLaunchTestFixture) mapAgentValue(value string) string {
	if value == agentHome {
		return f.agentHomeDir
	}
	return strings.ReplaceAll(value, agentHome+string(os.PathSeparator), f.agentHomeDir+string(os.PathSeparator))
}

func (f *commandLaunchTestFixture) newAgentCommand(args ...string) *exec.Cmd {
	mapped := make([]string, len(args))
	for i, arg := range args {
		mapped[i] = f.mapAgentValue(arg)
	}
	cmd := exec.Command(mapped[0], mapped[1:]...)
	cmd.Dir = "/"
	cmd.Env = append(os.Environ(), "HOME="+f.agentHomeDir, "USER="+agentUser, "LOGNAME="+agentUser)
	return cmd
}

func (f *commandLaunchTestFixture) installHarnessBinary(relPath string) {
	f.t.Helper()
	path := f.mapAgentPath(agentHome + relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatalf("mkdir fake harness bin dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		f.t.Fatalf("write fake harness bin: %v", err)
	}
}

func (f *commandLaunchTestFixture) executeCommand(cmd *cobra.Command, forwarded ...string) error {
	args := []string{"--no-backup", "--skip-harness-assets-sync", "-C", f.projectDir, "--"}
	args = append(args, forwarded...)
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}

func TestCommandLaunchMaterializesFileBackedHarnessAuth(t *testing.T) {
	tests := []struct {
		name      string
		harness   HarnessID
		binRel    string
		command   func() *cobra.Command
		forwarded []string
	}{
		{name: "codex", harness: HarnessCodex, binRel: codexBinRel, command: newCodexCmd, forwarded: []string{"exec", "echo ok"}},
		{name: "opencode", harness: HarnessOpenCode, binRel: openCodeCurrentBinRel, command: newOpenCodeCmd, forwarded: []string{"run", "echo ok"}},
		{name: "gemini", harness: HarnessGemini, binRel: geminiBinRel, command: newGeminiCmd, forwarded: []string{"-p", "ok"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newCommandLaunchTestFixture(t)
			f.installHarnessBinary(tc.binRel)

			wantAgentFiles := map[string][]byte{}
			for _, artifact := range harnessAuthArtifactsForRuntimeHome(tc.harness, f.hostHome, agentHome) {
				credential := []byte(fmt.Sprintf(`{"token":"stored-%s"}`, artifact.CredentialID))
				if err := writeHostStoredSecretFile(artifact.StorePath, credential); err != nil {
					t.Fatalf("seed %s store: %v", artifact.Name, err)
				}
				wantAgentFiles[artifact.AgentPath] = credential
			}

			var launched bool
			f.onLaunch = func(cfg sessionConfig, args []string) error {
				launched = true
				if cfg.HarnessID != tc.harness {
					return fmt.Errorf("HarnessID = %q, want %q", cfg.HarnessID, tc.harness)
				}
				if tc.harness == HarnessCodex && !slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
					return fmt.Errorf("Codex launch args missing skip-permissions flag: %v", args)
				}

				runtime, err := prepareSessionRuntime(cfg)
				if err != nil {
					return err
				}
				for agentPath, want := range wantAgentFiles {
					got, err := os.ReadFile(f.mapAgentPath(agentPath))
					if err != nil {
						return fmt.Errorf("read materialized %s: %w", agentPath, err)
					}
					if !bytes.Equal(got, want) {
						return fmt.Errorf("materialized %s = %q, want %q", agentPath, got, want)
					}
				}
				runtime.Cleanup()
				for agentPath := range wantAgentFiles {
					if _, err := os.Stat(f.mapAgentPath(agentPath)); !os.IsNotExist(err) {
						return fmt.Errorf("agent auth residue at %s after cleanup: %w", agentPath, err)
					}
				}
				return nil
			}

			if err := f.executeCommand(tc.command(), tc.forwarded...); err != nil {
				t.Fatalf("%s command: %v", tc.name, err)
			}
			if !launched {
				t.Fatalf("%s launch was not reached", tc.name)
			}
		})
	}
}

func TestCommandLaunchHandoffForContainedStateHarnesses(t *testing.T) {
	tests := []struct {
		name      string
		harness   HarnessID
		binRel    string
		command   func() *cobra.Command
		forwarded []string
		wantArgs  []string
	}{
		{name: "hermes", harness: HarnessHermes, binRel: hermesBinRel, command: newHermesCmd, forwarded: []string{"chat"}, wantArgs: []string{"chat"}},
		{name: "qwen", harness: HarnessQwen, binRel: qwenBinRel, command: newQwenCmd, forwarded: []string{"-p", "ok"}, wantArgs: []string{"--yolo", "-p", "ok"}},
		{name: "cursor-agent", harness: HarnessCursorAgent, binRel: cursorAgentBinRel, command: newCursorAgentCmd, forwarded: []string{"--print", "ok"}, wantArgs: []string{"--print", "ok"}},
		{name: "pi", harness: HarnessPi, binRel: piBinRel, command: newPiCmd, forwarded: []string{"--mode", "rpc"}, wantArgs: []string{"--mode", "rpc"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newCommandLaunchTestFixture(t)
			f.installHarnessBinary(tc.binRel)

			var launched bool
			f.onLaunch = func(cfg sessionConfig, args []string) error {
				launched = true
				if cfg.HarnessID != tc.harness {
					return fmt.Errorf("HarnessID = %q, want %q", cfg.HarnessID, tc.harness)
				}
				if !slices.Equal(args, tc.wantArgs) {
					return fmt.Errorf("launch args = %v, want %v", args, tc.wantArgs)
				}
				runtime, err := prepareSessionRuntime(cfg)
				if err != nil {
					return err
				}
				runtime.Cleanup()
				return nil
			}

			if err := f.executeCommand(tc.command(), tc.forwarded...); err != nil {
				t.Fatalf("%s command: %v", tc.name, err)
			}
			if !launched {
				t.Fatalf("%s launch was not reached", tc.name)
			}
		})
	}
}

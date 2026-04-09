package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func commandStdout(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

func commandStdoutCmd(cmd *exec.Cmd) (string, error) {
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// newSudoCommand forces sudo to start from / so the target user never inherits
// a host cwd it cannot traverse yet (for example during bootstrap before ACL
// repair has happened).
func newSudoCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("sudo", args...)
	cmd.Dir = "/"
	return cmd
}

func newSudoNoPromptCommand(args ...string) *exec.Cmd {
	full := append([]string{"-n"}, args...)
	return newSudoCommand(full...)
}

func newAgentCommand(args ...string) *exec.Cmd {
	full := append([]string{"-u", agentUser}, args...)
	return newSudoCommand(full...)
}

// dscl runs a read-only dscl query without sudo.
// Directory Service reads for UIDs, GIDs, and group membership are
// world-readable on macOS and do not require elevated privileges.
func dscl(args ...string) (string, error) {
	full := append([]string{"."}, args...)
	out, err := exec.Command("dscl", full...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// execOutput runs a command as the current user and returns stdout.
func execOutput(name string, args ...string) (string, error) {
	return commandStdout(name, args...)
}

// sudo runs a command with sudo, discarding stdout/stderr.
func sudo(args ...string) error {
	cmd := newSudoCommand(args...)
	return cmd.Run()
}

func sudoNoPrompt(args ...string) error {
	return newSudoNoPromptCommand(args...).Run()
}

// sudoOutput runs a command with sudo and returns combined stdout+stderr.
func sudoOutput(args ...string) (string, error) {
	out, err := newSudoCommand(args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// sudoWriteFile writes content to path as root using "sudo /usr/bin/tee path".
// Stdout from tee is discarded so the content is not echoed to the terminal.
func sudoWriteFile(path, content string) error {
	cmd := newSudoCommand("/usr/bin/tee", path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

// sudoAppendFile appends content to path as root using "sudo /usr/bin/tee -a path".
func sudoAppendFile(path, content string) error {
	cmd := newSudoCommand("/usr/bin/tee", "-a", path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

// asAgentQuiet runs args as the agent user via "sudo -u agent <args>",
// discarding stdout/stderr.  Returns exit code only.
//
// Requires the full sudoers rule (NOPASSWD: ALL or the specific command).
// For operations covered by the narrow NOPASSWD rule (sandbox-exec only),
// use agentSandboxExecQuiet instead.
func asAgentQuiet(args ...string) error {
	cmd := newAgentCommand(args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// asAgentOutput runs args as the agent user and returns stdout only.
// This prevents stderr from failed reads like "cat missing-file" from being
// mistaken for file content by callers that ignore the returned error.
func asAgentOutput(args ...string) (string, error) {
	return commandStdoutCmd(newAgentCommand(args...))
}

// asAgentShellQuiet runs a bash command string as the agent user.
// Use only with hardcoded scripts — never interpolate user input.
func asAgentShellQuiet(script string) error {
	return asAgentQuiet("bash", "-c", script)
}

// agentTCPConnect tests whether the agent user can reach host:port.
// It invokes the binary itself as the agent user via "sudo -u agent hazmat _connect",
// so the actual TCP dial runs under the agent user's UID and is subject to pf rules.
// Falls back to bash /dev/tcp if os.Executable() fails (e.g. go run).
func agentTCPConnect(selfPath, host, port string) bool {
	if selfPath != "" {
		err := asAgentQuiet(selfPath, "_connect", host, port)
		return err == nil
	}
	// Fallback: bash's /dev/tcp (bash-specific, but we require macOS+bash)
	script := fmt.Sprintf(
		"timeout 3 bash -c 'echo > /dev/tcp/%s/%s' 2>/dev/null",
		host, port,
	)
	return asAgentShellQuiet(script) == nil
}

// runInteractive runs a command with stdin/stdout/stderr connected to the terminal.
// Use for interactive subprocesses: sudo passwd, rsync --progress, etc.
func runInteractive(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// pfctlLoadRules runs "sudo pfctl -f /etc/pf.conf", capturing stderr so
// parse errors are surfaced rather than silently swallowed.
func pfctlLoadRules() error {
	cmd := newSudoCommand("pfctl", "-f", "/etc/pf.conf")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// launchctlBootstrap runs "sudo launchctl bootstrap system <plist>".
// Treats "already loaded" as success so the step stays idempotent.
func launchctlBootstrap(plist string) error {
	cmd := newSudoCommand("launchctl", "bootstrap", "system", plist)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		// "Bootstrap failed: 5: Input/output error" = service already loaded
		if strings.Contains(msg, "Bootstrap failed: 5") ||
			strings.Contains(msg, "already loaded") ||
			strings.Contains(msg, "service already exists") {
			return nil
		}
		return fmt.Errorf("launchctl bootstrap: %s", strings.TrimSpace(msg))
	}
	return nil
}

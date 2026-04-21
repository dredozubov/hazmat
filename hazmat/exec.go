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
// repair has happened). sudo itself is resolved to /usr/bin/sudo absolutely
// so an attacker-controlled early-PATH sudo binary cannot intercept the
// privilege elevation — once sudo is entered, its secure_path takes over for
// the invoked command.
func newSudoCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(hostSudoPath, args...)
	cmd.Dir = "/"
	return cmd
}

func newSudoNoPromptCommand(args ...string) *exec.Cmd {
	full := append([]string{"-n"}, args...)
	return newSudoCommand(full...)
}

func agentCommandArgs(args ...string) []string {
	full := []string{"-u", agentUser, "-H", launchHelperPath(), "exec"}
	return append(full, args...)
}

func newAgentCommand(args ...string) *exec.Cmd {
	return newSudoCommand(agentCommandArgs(args...)...)
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
	cmd := newSudoCommand(hostTeePath, path)
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
	cmd := newSudoCommand(hostTeePath, "-a", path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

// asAgentQuiet runs args as the agent user via Hazmat's helper-backed
// maintenance path, discarding stdout/stderr. Returns exit code only.
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

// asAgentCombinedOutput runs args as the agent user and returns combined
// stdout/stderr. Callers should surface stderr intentionally.
func asAgentCombinedOutput(args ...string) (string, error) {
	out, err := newAgentCommand(args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// asAgentShellQuiet runs a bash command string as the agent user.
// Use only with hardcoded scripts — never interpolate user input.
func asAgentShellQuiet(script string) error {
	return asAgentQuiet("bash", "-c", script)
}

// agentTCPConnect tests whether the agent user can reach host:port.
// It invokes the binary itself as the agent user via Hazmat's helper-backed
// maintenance path, so the actual TCP dial runs under the agent user's UID
// and is subject to pf rules.
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

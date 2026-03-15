package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// sudo runs a command with sudo, discarding stdout/stderr.
func sudo(args ...string) error {
	cmd := exec.Command("sudo", args...)
	return cmd.Run()
}

// sudoOutput runs a command with sudo and returns combined stdout+stderr.
func sudoOutput(args ...string) (string, error) {
	out, err := exec.Command("sudo", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// sudoWriteFile writes content to path as root using "sudo /usr/bin/tee path".
// Stdout from tee is discarded so the content is not echoed to the terminal.
func sudoWriteFile(path, content string) error {
	cmd := exec.Command("sudo", "/usr/bin/tee", path)
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
	cmd := exec.Command("sudo", "/usr/bin/tee", "-a", path)
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
func asAgentQuiet(args ...string) error {
	full := append([]string{"-u", agentUser}, args...)
	cmd := exec.Command("sudo", full...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// asAgentOutput runs args as the agent user and returns combined output.
func asAgentOutput(args ...string) (string, error) {
	full := append([]string{"-u", agentUser}, args...)
	out, err := exec.Command("sudo", full...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// asAgentShellQuiet runs a bash command string as the agent user.
// Use only with hardcoded scripts — never interpolate user input.
func asAgentShellQuiet(script string) error {
	return asAgentQuiet("bash", "-c", script)
}

// agentTCPConnect tests whether the agent user can reach host:port.
// It invokes the binary itself as the agent user via "sudo -u agent sandbox _connect",
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
	cmd := exec.Command("sudo", "pfctl", "-f", "/etc/pf.conf")
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
	cmd := exec.Command("sudo", "launchctl", "bootstrap", "system", plist)
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

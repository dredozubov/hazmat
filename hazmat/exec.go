package hazmat

import (
	"os/exec"
	"strings"

	"hazmat/internal/diagnostics"
	"hazmat/internal/hostexec"
)

func commandStdout(name string, args ...string) (string, error) {
	return hostexec.CommandStdout(name, args...)
}

// newSudoCommand forces sudo to start from / so the target user never inherits
// a host cwd it cannot traverse yet (for example during bootstrap before ACL
// repair has happened). sudo itself is resolved to /usr/bin/sudo absolutely
// so an attacker-controlled early-PATH sudo binary cannot intercept the
// privilege elevation — once sudo is entered, its secure_path takes over for
// the invoked command.
func newSudoCommand(args ...string) *exec.Cmd {
	return hostexec.NewSudoCommand(hostExecEnv(), args...)
}

func newSudoNoPromptCommand(args ...string) *exec.Cmd {
	return hostexec.NewSudoNoPromptCommand(hostExecEnv(), args...)
}

func agentCommandArgs(args ...string) []string {
	return hostexec.AgentCommandArgs(hostExecEnv(), args...)
}

var newAgentCommand = defaultNewAgentCommand

func defaultNewAgentCommand(args ...string) *exec.Cmd {
	return hostexec.NewAgentCommand(hostExecEnv(), args...)
}

// sudo runs a command with sudo, discarding stdout/stderr.
func sudo(args ...string) error {
	return hostexec.Sudo(hostExecEnv(), args...)
}

func sudoNoPrompt(args ...string) error {
	return hostexec.SudoNoPrompt(hostExecEnv(), args...)
}

// sudoOutput runs a command with sudo and returns combined stdout+stderr.
func sudoOutput(args ...string) (string, error) {
	return hostexec.SudoOutput(hostExecEnv(), args...)
}

// sudoWriteFile writes content to path as root using "sudo /usr/bin/tee path".
// Stdout from tee is discarded so the content is not echoed to the terminal.
func sudoWriteFile(path, content string) error {
	return hostexec.SudoWriteFile(hostExecEnv(), path, content)
}

// sudoAppendFile appends content to path as root using "sudo /usr/bin/tee -a path".
func sudoAppendFile(path, content string) error {
	return hostexec.SudoAppendFile(hostExecEnv(), path, content)
}

// asAgentQuiet runs args as the agent user via Hazmat's helper-backed
// maintenance path, discarding stdout/stderr. Returns exit code only.
// Built through the newAgentCommand seam so tests can interpose every
// agent exec, not just direct newAgentCommand callers.
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
	return hostexec.CommandStdoutCmd(newAgentCommand(args...))
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

func agentTCPConnect(selfPath, host, port string) bool {
	return diagnostics.AgentTCPConnect(hostExecEnv(), selfPath, host, port)
}

// runInteractive runs a command with stdin/stdout/stderr connected to the terminal.
// Use for interactive subprocesses: sudo passwd, rsync --progress, etc.
func runInteractive(name string, args ...string) error {
	return hostexec.RunInteractive(name, args...)
}

func hostExecEnv() hostexec.Env {
	return hostexec.Env{
		SudoPath:         hostSudoPath,
		TeePath:          hostTeePath,
		AgentUser:        agentUser,
		LaunchHelperPath: launchHelperPath(),
	}
}

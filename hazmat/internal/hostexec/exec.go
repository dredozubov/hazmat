package hostexec

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Env struct {
	SudoPath         string
	TeePath          string
	AgentUser        string
	LaunchHelperPath string
}

func CommandStdout(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

func CommandStdoutCmd(cmd *exec.Cmd) (string, error) {
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// NewSudoCommand forces sudo to start from / so the target user never inherits
// a host cwd it cannot traverse yet.
func NewSudoCommand(env Env, args ...string) *exec.Cmd {
	cmd := exec.Command(env.SudoPath, args...)
	cmd.Dir = "/"
	return cmd
}

func NewSudoNoPromptCommand(env Env, args ...string) *exec.Cmd {
	full := append([]string{"-n"}, args...)
	return NewSudoCommand(env, full...)
}

func AgentCommandArgs(env Env, args ...string) []string {
	full := []string{"-u", env.AgentUser, "-H", env.LaunchHelperPath, "exec"}
	return append(full, args...)
}

func NewAgentCommand(env Env, args ...string) *exec.Cmd {
	return NewSudoCommand(env, AgentCommandArgs(env, args...)...)
}

func Sudo(env Env, args ...string) error {
	return NewSudoCommand(env, args...).Run()
}

func SudoNoPrompt(env Env, args ...string) error {
	return NewSudoNoPromptCommand(env, args...).Run()
}

func SudoOutput(env Env, args ...string) (string, error) {
	out, err := NewSudoCommand(env, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func SudoWriteFile(env Env, path, content string) error {
	cmd := NewSudoCommand(env, env.TeePath, path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

func SudoAppendFile(env Env, path, content string) error {
	cmd := NewSudoCommand(env, env.TeePath, "-a", path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

func AsAgentQuiet(env Env, args ...string) error {
	cmd := NewAgentCommand(env, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func AsAgentOutput(env Env, args ...string) (string, error) {
	return CommandStdoutCmd(NewAgentCommand(env, args...))
}

func AsAgentCombinedOutput(env Env, args ...string) (string, error) {
	out, err := NewAgentCommand(env, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func AsAgentShellQuiet(env Env, script string) error {
	return AsAgentQuiet(env, "bash", "-c", script)
}

func RunInteractive(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

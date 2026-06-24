package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type StepStatusUI interface {
	Step(string)
	SkipDone(string)
	WarnMsg(string)
	Ok(string)
}

type ToolingRunner interface {
	Sudo(reason string, args ...string) error
	SudoOutput(args ...string) (string, error)
	SudoWriteFile(reason, path, content string) error
	AgentOutput(args ...string) (string, error)
	UserWriteFile(path, content string) error
	MkdirAll(path string, mode os.FileMode) error
	Chmod(path string, mode os.FileMode) error
}

type ShellProfile struct {
	Name           string
	RCPath         string
	PathBlockLines []string
}

type ToolingEnv struct {
	AgentUser              string
	AgentHome              string
	SeatbeltProfileDir     string
	SeatbeltWrapperPath    string
	SeatbeltWrapper        string
	AgentEnvPath           string
	DefaultAgentPath       string
	DefaultAgentCacheHome  string
	DefaultAgentConfigHome string
	DefaultAgentDataHome   string
	DefaultAgentStateHome  string
	HostWrapperDir         string
	HostClaudeWrapperName  string
	HostExecWrapperName    string
	HostShellWrapperName   string
	AgentShellBlockStart   string
	AgentShellBlockEnd     string
	UserPathBlockStart     string
	UserPathBlockEnd       string
	UmaskBlockStart        string
	UmaskBlockEnd          string
	ShellName              string
	ShellProfiles          []ShellProfile
	Executable             func() (string, error)
	EvalSymlinks           func(string) (string, error)
}

func SetupSeatbelt(env ToolingEnv, ui StepStatusUI, runner ToolingRunner) error {
	ui.Step("Install Claude compatibility wrapper")

	if err := runner.Sudo("create seatbelt config directory",
		"install", "-d", "-o", env.AgentUser, "-g", "staff", "-m", "755", env.SeatbeltProfileDir); err != nil {
		return fmt.Errorf("ensure %s: %w", env.SeatbeltProfileDir, err)
	}

	wrapperDir := filepath.Join(env.AgentHome, ".local", "bin")
	if err := runner.Sudo("create agent bin directory",
		"install", "-d", "-o", env.AgentUser, "-g", "staff", "-m", "755", wrapperDir); err != nil {
		return fmt.Errorf("ensure %s: %w", wrapperDir, err)
	}

	// The wrapper is a managed artifact; re-write on every run so setup
	// doubles as an upgrade path.
	if err := runner.SudoWriteFile("install seatbelt wrapper", env.SeatbeltWrapperPath, env.SeatbeltWrapper); err != nil {
		return fmt.Errorf("write seatbelt wrapper: %w", err)
	}
	if err := runner.Sudo("set seatbelt wrapper ownership", "chown", env.AgentUser+":staff", env.SeatbeltWrapperPath); err != nil {
		return fmt.Errorf("chown seatbelt wrapper: %w", err)
	}
	if err := runner.Sudo("make seatbelt wrapper executable", "chmod", "755", env.SeatbeltWrapperPath); err != nil {
		return fmt.Errorf("chmod seatbelt wrapper: %w", err)
	}
	ui.Ok(fmt.Sprintf("Seatbelt wrapper installed at %s", env.SeatbeltWrapperPath))

	return nil
}

func SetupUserExperience(env ToolingEnv, ui StepStatusUI, runner ToolingRunner) error {
	ui.Step("Install command wrappers and toolchain env")

	if err := EnsureAgentToolchainDirs(env, runner); err != nil {
		return err
	}

	if err := runner.SudoWriteFile("write agent toolchain env", env.AgentEnvPath, AgentEnvContent(env.DefaultAgentPath)); err != nil {
		return fmt.Errorf("write agent env file: %w", err)
	}
	if err := runner.Sudo("set agent env file ownership", "chown", env.AgentUser+":staff", env.AgentEnvPath); err != nil {
		return fmt.Errorf("chown agent env file: %w", err)
	}
	if err := runner.Sudo("set agent env file permissions", "chmod", "644", env.AgentEnvPath); err != nil {
		return fmt.Errorf("chmod agent env file: %w", err)
	}
	ui.Ok(fmt.Sprintf("Agent toolchain env written to %s", env.AgentEnvPath))

	agentZshrc := filepath.Join(env.AgentHome, ".zshrc")
	agentZshrcData, _ := runner.AgentOutput("cat", agentZshrc)
	updatedAgentZshrc := UpsertManagedBlock(agentZshrcData,
		env.AgentShellBlockStart,
		env.AgentShellBlockEnd,
		fmt.Sprintf(`[[ -f "%s" ]] && source "%s"`, shellHomeRelative(env.AgentEnvPath, env.AgentHome), shellHomeRelative(env.AgentEnvPath, env.AgentHome)),
	)
	if err := runner.SudoWriteFile("write agent shell bootstrap to .zshrc", agentZshrc, updatedAgentZshrc); err != nil {
		return fmt.Errorf("update %s: %w", agentZshrc, err)
	}
	if err := runner.Sudo("set agent .zshrc ownership", "chown", env.AgentUser+":staff", agentZshrc); err != nil {
		return fmt.Errorf("chown %s: %w", agentZshrc, err)
	}
	ui.Ok(fmt.Sprintf("Agent shell bootstraps %s", env.AgentEnvPath))

	if err := runner.MkdirAll(env.HostWrapperDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", env.HostWrapperDir, err)
	}

	hazmatBin, err := executablePath(env)
	if err != nil {
		return fmt.Errorf("resolve hazmat binary path: %w", err)
	}

	wrappers := []struct {
		name       string
		subcommand string
	}{
		{name: env.HostClaudeWrapperName, subcommand: "claude"},
		{name: env.HostExecWrapperName, subcommand: "exec"},
		{name: env.HostShellWrapperName, subcommand: "shell"},
	}
	for _, wrapper := range wrappers {
		path := filepath.Join(env.HostWrapperDir, wrapper.name)
		if err := runner.UserWriteFile(path, HostWrapperContent(hazmatBin, wrapper.subcommand)); err != nil {
			return fmt.Errorf("write wrapper %s: %w", path, err)
		}
		if err := runner.Chmod(path, 0o755); err != nil {
			return fmt.Errorf("chmod %s: %w", path, err)
		}
		ui.Ok(fmt.Sprintf("Installed host wrapper %s", path))
	}

	profile, ok := CurrentUserShellProfile(env)
	if !ok {
		ui.WarnMsg(fmt.Sprintf("Shell %q is not auto-configured — add %s to your PATH manually", env.ShellName, env.HostWrapperDir))
		return nil
	}

	userRCData, _ := os.ReadFile(profile.RCPath)
	if strings.Contains(string(userRCData), env.UserPathBlockStart) {
		ui.SkipDone(fmt.Sprintf("%s already has a hazmat PATH block", profile.RCPath))
		return nil
	}

	updatedUserRC := UpsertManagedBlock(string(userRCData),
		env.UserPathBlockStart,
		env.UserPathBlockEnd,
		profile.PathBlockLines...,
	)
	if err := runner.MkdirAll(filepath.Dir(profile.RCPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(profile.RCPath), err)
	}
	if err := runner.UserWriteFile(profile.RCPath, updatedUserRC); err != nil {
		return fmt.Errorf("update %s: %w", profile.RCPath, err)
	}
	ui.Ok(fmt.Sprintf("Added %s PATH block to %s", env.HostWrapperDir, profile.RCPath))

	return nil
}

// EnsureAgentToolchainDirs creates and repairs agent-owned XDG/toolchain roots.
//
// All directories are created in a SINGLE privileged `install -d` call rather
// than one per directory: sudo invocations here run with stdio detached from
// the terminal, so each separate `sudo` re-prompts for the password instead of
// reusing a cached tty ticket. One call means the user is prompted at most once
// for this step instead of once per directory. `install -d` accepts multiple
// operands, creates intermediate parents, and applies the same owner/group/mode
// to each, so batching is behavior-equivalent to the previous loop.
func EnsureAgentToolchainDirs(env ToolingEnv, runner ToolingRunner) error {
	dirs := agentToolchainDirs(env)
	if len(dirs) == 0 {
		return nil
	}
	args := append([]string{"install", "-d", "-o", env.AgentUser, "-g", "staff", "-m", "755"}, dirs...)
	if err := runner.Sudo("create agent toolchain directories", args...); err != nil {
		return fmt.Errorf("ensure agent toolchain directories: %w", err)
	}
	return nil
}

func agentToolchainDirs(env ToolingEnv) []string {
	dirs := []string{
		env.DefaultAgentCacheHome,
		env.DefaultAgentConfigHome,
		filepath.Dir(env.AgentEnvPath),
		filepath.Join(env.AgentHome, ".local"),
		filepath.Join(env.AgentHome, ".local", "bin"),
		filepath.Join(env.AgentHome, ".local", "lib"),
		env.DefaultAgentDataHome,
		env.DefaultAgentStateHome,
		filepath.Join(env.AgentHome, ".npm"),
	}
	return compactUniquePaths(dirs)
}

func compactUniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func RollbackSeatbelt(env ToolingEnv, ui StepStatusUI, runner ToolingRunner) {
	ui.Step("Remove seatbelt profile and wrapper")

	for _, path := range []string{env.SeatbeltWrapperPath} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			ui.SkipDone(fmt.Sprintf("%s not present", path))
			continue
		}
		if err := runner.Sudo("remove seatbelt wrapper", "rm", "-f", path); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", path, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed %s", path))
		}
	}
}

func RollbackUserExperience(env ToolingEnv, ui StepStatusUI, runner ToolingRunner) {
	ui.Step("Remove command wrappers and shell integration")

	if _, err := os.Stat(env.AgentEnvPath); os.IsNotExist(err) {
		ui.SkipDone(fmt.Sprintf("%s not present", env.AgentEnvPath))
	} else if err := runner.Sudo("remove agent environment file", "rm", "-f", env.AgentEnvPath); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", env.AgentEnvPath, err))
	} else {
		ui.Ok(fmt.Sprintf("Removed %s", env.AgentEnvPath))
	}

	for _, path := range []string{
		filepath.Join(env.HostWrapperDir, env.HostClaudeWrapperName),
		filepath.Join(env.HostWrapperDir, env.HostExecWrapperName),
		filepath.Join(env.HostWrapperDir, env.HostShellWrapperName),
	} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			ui.SkipDone(fmt.Sprintf("%s not present", path))
			continue
		}
		if err := os.Remove(path); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", path, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed %s", path))
		}
	}

	agentZshrc := filepath.Join(env.AgentHome, ".zshrc")
	if data, err := runner.AgentOutput("cat", agentZshrc); err == nil &&
		strings.Contains(data, env.AgentShellBlockStart) {
		cleaned := RemoveManagedBlock(data, env.AgentShellBlockStart, env.AgentShellBlockEnd)
		if err := runner.SudoWriteFile("remove hazmat shell block from agent .zshrc", agentZshrc, cleaned); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", agentZshrc, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed hazmat shell block from %s", agentZshrc))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Hazmat shell block not present in %s", agentZshrc))
	}

	for _, profile := range env.ShellProfiles {
		if data, err := os.ReadFile(profile.RCPath); err == nil &&
			strings.Contains(string(data), env.UserPathBlockStart) {
			cleaned := RemoveManagedBlock(string(data), env.UserPathBlockStart, env.UserPathBlockEnd)
			if err := runner.UserWriteFile(profile.RCPath, cleaned); err != nil {
				ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", profile.RCPath, err))
			} else {
				ui.Ok(fmt.Sprintf("Removed hazmat PATH block from %s", profile.RCPath))
			}
		} else {
			ui.SkipDone(fmt.Sprintf("Hazmat PATH block not present in %s", profile.RCPath))
		}
	}
}

func RollbackUmask(env ToolingEnv, ui StepStatusUI, runner ToolingRunner) {
	ui.Step("Remove umask managed block from shell rc files")

	// Agent .zshrc — only remove the block this tool added.
	agentZshrc := filepath.Join(env.AgentHome, ".zshrc")
	if data, err := runner.AgentOutput("cat", agentZshrc); err == nil &&
		strings.Contains(data, env.UmaskBlockStart) {
		cleaned := RemoveManagedBlock(data, env.UmaskBlockStart, env.UmaskBlockEnd)
		if err := runner.SudoWriteFile("remove umask block from agent .zshrc", agentZshrc, cleaned); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", agentZshrc, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed umask block from %s", agentZshrc))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Umask block not present in %s", agentZshrc))
	}

	for _, profile := range env.ShellProfiles {
		if data, err := os.ReadFile(profile.RCPath); err == nil &&
			strings.Contains(string(data), env.UmaskBlockStart) {
			cleaned := RemoveManagedBlock(string(data), env.UmaskBlockStart, env.UmaskBlockEnd)
			if err := runner.UserWriteFile(profile.RCPath, cleaned); err != nil {
				ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", profile.RCPath, err))
			} else {
				ui.Ok(fmt.Sprintf("Removed umask block from %s", profile.RCPath))
			}
		} else {
			ui.SkipDone(fmt.Sprintf("Umask block not present in %s", profile.RCPath))
		}
	}
}

func CurrentUserShellProfile(env ToolingEnv) (ShellProfile, bool) {
	for _, profile := range env.ShellProfiles {
		if profile.Name == env.ShellName {
			return profile, true
		}
	}
	return ShellProfile{}, false
}

func AgentEnvContent(defaultAgentPath string) string {
	return fmt.Sprintf(`# Managed by hazmat init.
export PATH="%s"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-$HOME/.cache}"
export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
export XDG_DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
export XDG_STATE_HOME="${XDG_STATE_HOME:-$HOME/.local/state}"
export HOMEBREW_NO_AUTO_UPDATE="${HOMEBREW_NO_AUTO_UPDATE:-1}"

mkdir -p "$XDG_CACHE_HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_STATE_HOME" "$HOME/.npm" >/dev/null 2>&1 || true

if [[ -x "$HOME/.local/bin/claude-sandboxed" ]]; then
  alias claude="$HOME/.local/bin/claude-sandboxed"
fi

if [[ -n "${SANDBOX_ACTIVE:-}" && -o interactive ]]; then
  PROMPT="%%F{red}[agent:hazmat]%%f %%~ %%# "
fi
`, defaultAgentPath)
}

func HostWrapperContent(hazmatBin, subcommand string) string {
	// No fallback to `command -v hazmat`: on macOS `command -v sandbox`
	// resolves to /usr/bin/sandbox (Apple's SBPL tool), not this binary.
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail

HAZMAT_BIN=%q
if [[ ! -x "$HAZMAT_BIN" ]]; then
  printf 'error: hazmat binary not found: %%s\n' "$HAZMAT_BIN" >&2
  printf 'Setup drift detected: refresh Hazmat-owned wrappers with "hazmat doctor --fix".\n' >&2
  printf 'Preview the repair plan with "hazmat doctor --dry-run".\n' >&2
  exit 1
fi

exec "$HAZMAT_BIN" %s "$@"
`, hazmatBin, subcommand)
}

func ManagedBlock(start, end string, lines ...string) string {
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	return start + "\n" + body + end + "\n"
}

func UpsertManagedBlock(existing, start, end string, lines ...string) string {
	block := ManagedBlock(start, end, lines...)
	cleaned := RemoveManagedBlock(existing, start, end)
	cleaned = strings.TrimRight(cleaned, "\n")
	if cleaned == "" {
		return block
	}
	return cleaned + "\n\n" + block
}

func RemoveManagedBlock(existing, start, end string) string {
	var kept []string
	inside := false
	for _, line := range strings.Split(existing, "\n") {
		switch {
		case strings.TrimSpace(line) == start:
			inside = true
			continue
		case inside && strings.TrimSpace(line) == end:
			inside = false
			continue
		case inside:
			continue
		default:
			kept = append(kept, line)
		}
	}
	cleaned := strings.Join(kept, "\n")
	cleaned = strings.TrimRight(cleaned, "\n")
	if cleaned == "" {
		return ""
	}
	return cleaned + "\n"
}

func executablePath(env ToolingEnv) (string, error) {
	executable := env.Executable
	if executable == nil {
		executable = os.Executable
	}
	hazmatBin, err := executable()
	if err != nil {
		return "", err
	}

	evalSymlinks := env.EvalSymlinks
	if evalSymlinks == nil {
		evalSymlinks = filepath.EvalSymlinks
	}
	if resolved, err := evalSymlinks(hazmatBin); err == nil {
		hazmatBin = resolved
	}
	return hazmatBin, nil
}

func shellHomeRelative(path, home string) string {
	prefix := strings.TrimRight(home, "/") + "/"
	if strings.HasPrefix(path, prefix) {
		return "$HOME/" + strings.TrimPrefix(path, prefix)
	}
	return path
}

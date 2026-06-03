package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type StatusUI interface {
	SkipDone(string)
	WarnMsg(string)
	Ok(string)
}

type SudoersRunner interface {
	Sudo(reason string, args ...string) error
	SudoOutput(args ...string) (string, error)
	SudoWriteFile(reason, path, content string) error
}

type SudoersEnv struct {
	AgentUser                   string
	SudoersFile                 string
	AgentMaintenanceSudoersFile string
	LaunchHelperPath            func() string
	LaunchHelperUsesDigest      func(string) bool
	RemoveInvalidFile           func(string) error
}

func LaunchSudoersEntry(env SudoersEnv, currentUser string) (string, error) {
	helperPath := env.launchHelperPath()
	commandSpec := helperPath

	if env.launchHelperUsesDigest(helperPath) {
		data, err := os.ReadFile(helperPath)
		if err != nil {
			return "", fmt.Errorf("read %s for sudoers digest: %w", helperPath, err)
		}
		sum := sha256.Sum256(data)
		commandSpec = fmt.Sprintf("sha256:%s %s", hex.EncodeToString(sum[:]), helperPath)
	}

	return fmt.Sprintf("%s ALL=(%s) NOPASSWD: %s\n", currentUser, env.AgentUser, commandSpec), nil
}

func AgentMaintenanceSudoersEntry(env SudoersEnv, currentUser string) string {
	return strings.Join([]string{
		"# Optional hazmat agent-maintenance passwordless rule.",
		"# Broader than the default launch-helper rule: allows the current user",
		"# to run arbitrary commands as the agent user without a password.",
		fmt.Sprintf("%s ALL=(%s) NOPASSWD: ALL", currentUser, env.AgentUser),
		"",
	}, "\n")
}

func LaunchSudoersInstalled(env SudoersEnv) bool {
	_, err := os.Stat(env.SudoersFile)
	return err == nil
}

func AgentMaintenanceSudoersInstalled(env SudoersEnv) bool {
	_, err := os.Stat(env.AgentMaintenanceSudoersFile)
	return err == nil
}

func InstallLaunchSudoers(env SudoersEnv, ui StatusUI, runner SudoersRunner, currentUser string) error {
	helperPath := env.launchHelperPath()
	entry, err := LaunchSudoersEntry(env, currentUser)
	if err != nil {
		return err
	}
	if data, err := runner.SudoOutput("cat", env.SudoersFile); err == nil &&
		strings.Contains(data, strings.TrimSpace(entry)) {
		ui.SkipDone(fmt.Sprintf("Sudoers entry already targets %s", helperPath))
		return nil
	} else if err == nil && strings.Contains(data, currentUser) {
		ui.WarnMsg(fmt.Sprintf("Existing sudoers entry does not target %s — replacing with narrow rule", helperPath))
	}

	if err := WriteManagedSudoersFile(env, runner,
		"write launch-helper sudoers entry for passwordless agent access",
		env.SudoersFile,
		entry,
	); err != nil {
		return fmt.Errorf("write launch-helper sudoers: %w", err)
	}

	ui.Ok(fmt.Sprintf("Sudoers entry written: %s can run %s as %s without password",
		currentUser, helperPath, env.AgentUser))
	return nil
}

func InstallAgentMaintenanceSudoers(env SudoersEnv, ui StatusUI, runner SudoersRunner, currentUser string) error {
	entry := AgentMaintenanceSudoersEntry(env, currentUser)
	if data, err := runner.SudoOutput("cat", env.AgentMaintenanceSudoersFile); err == nil &&
		strings.Contains(data, "NOPASSWD: ALL") &&
		strings.Contains(data, currentUser) {
		ui.SkipDone(fmt.Sprintf("Optional agent-maintenance sudoers entry already present at %s", env.AgentMaintenanceSudoersFile))
		return nil
	} else if err == nil {
		ui.WarnMsg(fmt.Sprintf("Existing optional agent-maintenance sudoers entry will be replaced at %s", env.AgentMaintenanceSudoersFile))
	}

	if err := WriteManagedSudoersFile(env, runner,
		"write optional passwordless sudoers entry for generic agent-user commands",
		env.AgentMaintenanceSudoersFile,
		entry,
	); err != nil {
		return fmt.Errorf("write optional agent-maintenance sudoers: %w", err)
	}

	ui.Ok(fmt.Sprintf("Optional passwordless sudo enabled: %s can run generic commands as %s without password",
		currentUser, env.AgentUser))
	return nil
}

func UninstallAgentMaintenanceSudoers(env SudoersEnv, ui StatusUI, runner SudoersRunner) error {
	if _, err := os.Stat(env.AgentMaintenanceSudoersFile); os.IsNotExist(err) {
		ui.SkipDone("Optional agent-maintenance sudoers entry not present")
		return nil
	}
	if err := runner.Sudo("remove optional agent-maintenance sudoers entry", "rm", "-f", env.AgentMaintenanceSudoersFile); err != nil {
		return err
	}
	ui.Ok(fmt.Sprintf("Removed %s", env.AgentMaintenanceSudoersFile))
	return nil
}

func WriteManagedSudoersFile(env SudoersEnv, runner SudoersRunner, reason, path, content string) error {
	if err := runner.SudoWriteFile(reason, path, content); err != nil {
		return err
	}
	if err := runner.Sudo("set "+filepath.Base(path)+" permissions", "chmod", "440", path); err != nil {
		return err
	}
	if err := runner.Sudo("validate "+filepath.Base(path)+" syntax", "visudo", "-c", "-f", path); err != nil {
		removeInvalidFile(env, path)
		return fmt.Errorf("sudoers syntax invalid for %s — entry removed: %w", path, err)
	}
	return nil
}

func (env SudoersEnv) launchHelperPath() string {
	if env.LaunchHelperPath != nil {
		return env.LaunchHelperPath()
	}
	return ""
}

func (env SudoersEnv) launchHelperUsesDigest(path string) bool {
	return env.LaunchHelperUsesDigest != nil && env.LaunchHelperUsesDigest(path)
}

func removeInvalidFile(env SudoersEnv, path string) {
	if env.RemoveInvalidFile != nil {
		env.RemoveInvalidFile(path) //nolint:errcheck
	}
}

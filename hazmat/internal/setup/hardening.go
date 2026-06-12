package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type HardeningEnv struct {
	AgentUser       string
	AgentHome       string
	HostHome        string
	UmaskBlockStart string
	UmaskBlockEnd   string
	DockerSocket    string
}

type HostCredentialHardeningSpec struct {
	Rel      string
	DirMode  os.FileMode
	FileMode os.FileMode
}

type HostCredentialHardeningTarget struct {
	Path string
	Mode os.FileMode
}

var HostCredentialHardeningSpecs = []HostCredentialHardeningSpec{
	{Rel: ".ssh", DirMode: 0o700, FileMode: 0o600},
	{Rel: ".aws", DirMode: 0o700, FileMode: 0o600},
	{Rel: ".gnupg", DirMode: 0o700, FileMode: 0o600},
	{Rel: "Library/Keychains", DirMode: 0o700, FileMode: 0o600},
	{Rel: ".config/gh", DirMode: 0o700, FileMode: 0o600},
	{Rel: ".docker", DirMode: 0o700, FileMode: 0o600},
	{Rel: ".kube", DirMode: 0o700, FileMode: 0o600},
	{Rel: ".netrc", DirMode: 0o700, FileMode: 0o600},
	{Rel: ".m2/settings.xml", DirMode: 0o700, FileMode: 0o600},
	{Rel: ".config/gcloud", DirMode: 0o700, FileMode: 0o600},
	{Rel: ".azure", DirMode: 0o700, FileMode: 0o600},
	{Rel: ".oci", DirMode: 0o700, FileMode: 0o600},
}

func SetupHardeningGaps(env HardeningEnv, ui StepStatusUI, runner ToolingRunner) error {
	ui.Step("Harden known macOS isolation gaps")

	dockerSock := env.dockerSocket()
	if info, err := os.Stat(dockerSock); err == nil && info.Mode()&os.ModeSocket != 0 {
		current := info.Mode().Perm()
		if current == 0o700 {
			ui.SkipDone("Docker socket already restricted (700)")
		} else {
			if err := runner.Chmod(dockerSock, 0o700); err != nil {
				return fmt.Errorf("chmod docker socket: %w", err)
			}
			ui.Ok(fmt.Sprintf("Docker socket restricted to owner only (was %04o)", current))
		}
	} else {
		ui.SkipDone("Docker socket not found (Docker Desktop not running or not installed)")
	}

	if fixed, skipped, err := HardenHostCredentialPaths(runner, env.HostHome); err != nil {
		return err
	} else {
		for _, path := range skipped {
			ui.WarnMsg(fmt.Sprintf("Host credential path is a symlink; leaving unchanged: %s", path))
		}
		if fixed == 0 {
			ui.SkipDone("Host credential paths already restricted")
		} else {
			ui.Ok(fmt.Sprintf("Restricted %d host credential path(s) to owner-only access", fixed))
		}
	}

	// Restrictive umask for agent user; rollback removes this exact block.
	agentZshrc := filepath.Join(env.AgentHome, ".zshrc")
	agentZshrcData, _ := runner.AgentOutput("cat", agentZshrc)
	if strings.Contains(agentZshrcData, env.UmaskBlockStart) {
		ui.SkipDone("umask 007 already set in agent's .zshrc")
	} else {
		updated := UpsertManagedBlock(agentZshrcData, env.UmaskBlockStart, env.UmaskBlockEnd, "umask 007")
		if err := runner.SudoWriteFile("write agent umask to .zshrc", agentZshrc, updated); err != nil {
			return fmt.Errorf("set umask in agent .zshrc: %w", err)
		}
		if err := runner.Sudo("set agent .zshrc ownership", "chown", env.AgentUser+":staff", agentZshrc); err != nil {
			return fmt.Errorf("chown agent .zshrc: %w", err)
		}
		ui.Ok("Set umask 007 in agent's .zshrc")
	}

	ui.SkipDone("Host shell umask left unchanged")

	return nil
}

func HostCredentialHardeningTargets(home string) ([]HostCredentialHardeningTarget, []string) {
	var targets []HostCredentialHardeningTarget
	var skippedSymlinks []string
	for _, spec := range HostCredentialHardeningSpecs {
		path := filepath.Join(home, filepath.FromSlash(spec.Rel))
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			skippedSymlinks = append(skippedSymlinks, path)
			continue
		}

		mode := spec.FileMode
		if info.IsDir() {
			mode = spec.DirMode
		}
		if mode == 0 || info.Mode().Perm() == mode {
			continue
		}
		targets = append(targets, HostCredentialHardeningTarget{
			Path: path,
			Mode: mode,
		})
	}
	return targets, skippedSymlinks
}

func HardenHostCredentialPaths(runner ToolingRunner, home string) (int, []string, error) {
	targets, skippedSymlinks := HostCredentialHardeningTargets(home)
	for _, target := range targets {
		if err := runner.Chmod(target.Path, target.Mode); err != nil {
			return 0, skippedSymlinks, fmt.Errorf("chmod host credential path %s: %w", target.Path, err)
		}
	}
	return len(targets), skippedSymlinks, nil
}

func (env HardeningEnv) dockerSocket() string {
	if env.DockerSocket != "" {
		return env.DockerSocket
	}
	return filepath.Join(env.HostHome, ".docker", "run", "docker.sock")
}

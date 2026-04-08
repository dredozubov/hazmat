package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const hazmatSafeDirMarker = " # hazmat-managed"

var detectGitRepoTopLevel = detectGitRepoTopLevelImpl
var readSystemGitSafeDirectoryEntries = systemSafeDirectoryEntries
var readAgentGlobalGitSafeDirectoryEntries = agentGlobalSafeDirectoryEntries
var appendAgentGlobalSafeDirectoryEntry = appendAgentGlobalSafeDirectoryEntryImpl

func managedSafeDirectoryEntries(readDirs []string) []string {
	seen := make(map[string]struct{}, len(readDirs))
	var entries []string
	for _, dir := range readDirs {
		if dir == "" {
			continue
		}
		entry := filepath.Clean(expandTilde(dir)) + "/*"
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		entries = append(entries, entry)
	}
	sort.Strings(entries)
	return entries
}

func parseSystemGitConfigOrigin(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "file:") {
			continue
		}
		origin, _, _ := strings.Cut(line, "\t")
		return strings.TrimPrefix(origin, "file:")
	}
	return ""
}

func normalizeSafeDirectoryEntry(entry string) string {
	entry = strings.TrimSpace(expandTilde(entry))
	if entry == "" || entry == "*" {
		return entry
	}
	if strings.HasSuffix(entry, "/*") {
		return filepath.Clean(strings.TrimSuffix(entry, "/*")) + "/*"
	}
	return filepath.Clean(entry)
}

func dedupeSafeDirectoryEntries(entries []string) []string {
	if len(entries) == 0 {
		return nil
	}
	var deduped []string
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		normalized := normalizeSafeDirectoryEntry(entry)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		deduped = append(deduped, normalized)
	}
	return deduped
}

func safeDirectoryCovers(entries []string, repoDir string) bool {
	repoDir = normalizeSafeDirectoryEntry(repoDir)
	if repoDir == "" {
		return false
	}
	for _, entry := range entries {
		switch normalized := normalizeSafeDirectoryEntry(entry); {
		case normalized == "":
			continue
		case normalized == "*":
			return true
		case normalized == repoDir:
			return true
		case strings.HasSuffix(normalized, "/*"):
			base := strings.TrimSuffix(normalized, "/*")
			if repoDir != base && isWithinDir(base, repoDir) {
				return true
			}
		}
	}
	return false
}

func detectGitRepoTopLevelImpl(projectDir string) (string, bool) {
	if projectDir == "" {
		return "", false
	}
	out, err := exec.Command("git", "-C", projectDir, "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		return "", false
	}
	repoDir := normalizeSafeDirectoryEntry(string(bytes.TrimSpace(out)))
	if repoDir == "" {
		return "", false
	}
	return repoDir, true
}

func readGitSafeDirectoryEntriesCommand(cmd *exec.Cmd) ([]string, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, msg)
	}
	return dedupeSafeDirectoryEntries(strings.Split(strings.TrimSpace(string(out)), "\n")), nil
}

func systemSafeDirectoryEntries() ([]string, error) {
	return readGitSafeDirectoryEntriesCommand(exec.Command("git", "config", "--system", "--get-all", "safe.directory"))
}

func agentGlobalSafeDirectoryEntries() ([]string, error) {
	// Read the agent's gitconfig directly instead of using sudo -u agent.
	// The file is group-readable by dev, which both users belong to.
	agentGitconfig := agentHome + "/.gitconfig"
	return readGitSafeDirectoryEntriesCommand(exec.Command("git", "config", "--file", agentGitconfig, "--get-all", "safe.directory"))
}

func currentGitSafeDirectoryEntries() ([]string, error) {
	systemEntries, err := readSystemGitSafeDirectoryEntries()
	if err != nil {
		return nil, err
	}
	agentEntries, err := readAgentGlobalGitSafeDirectoryEntries()
	if err != nil {
		return nil, err
	}
	return dedupeSafeDirectoryEntries(append(systemEntries, agentEntries...)), nil
}

func gitSafeDirectoryTrustedForAgent(repoDir string) (bool, error) {
	entries, err := currentGitSafeDirectoryEntries()
	if err != nil {
		return false, err
	}
	return safeDirectoryCovers(entries, repoDir), nil
}

func plannedProjectGitSafeDirectory(projectDir string) string {
	repoDir, ok := detectGitRepoTopLevel(projectDir)
	if !ok {
		return ""
	}
	trusted, err := gitSafeDirectoryTrustedForAgent(repoDir)
	if err == nil && trusted {
		return ""
	}
	return repoDir
}

func appendAgentGlobalSafeDirectoryEntryImpl(repoDir string) error {
	repoDir = normalizeSafeDirectoryEntry(repoDir)
	if repoDir == "" {
		return nil
	}
	// Write via sudo -u agent because git config needs to create a lock file
	// in the agent's home directory, which is only writable by agent.
	// This sudo call is covered by the NOPASSWD rule (runs after init).
	// Use / as cwd so git doesn't fail when the host's cwd is inaccessible
	// to the agent user (the traverse ACL may not have been applied yet).
	cmd := exec.Command("sudo", "-u", agentUser, "-H", "git", "config", "--global", "--add", "safe.directory", repoDir)
	cmd.Dir = "/"
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

func ensureAgentGitSafeDirectory(projectDir string) (bool, error) {
	repoDir, ok := detectGitRepoTopLevel(projectDir)
	if !ok {
		return false, nil
	}

	trusted, err := gitSafeDirectoryTrustedForAgent(repoDir)
	if err == nil && trusted {
		return false, nil
	}

	if err := appendAgentGlobalSafeDirectoryEntry(repoDir); err != nil {
		return false, fmt.Errorf("add agent git safe.directory %s: %w", repoDir, err)
	}

	trusted, err = gitSafeDirectoryTrustedForAgent(repoDir)
	if err != nil {
		return false, fmt.Errorf("verify agent git safe.directory %s: %w", repoDir, err)
	}
	if !trusted {
		return false, fmt.Errorf("agent git still does not trust %s after updating safe.directory", repoDir)
	}
	return true, nil
}

func fallbackSystemGitConfigPath() string {
	out, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		return ""
	}
	execPath := strings.TrimSpace(string(out))
	if execPath == "" {
		return ""
	}
	prefix := filepath.Dir(filepath.Dir(execPath))
	return filepath.Join(prefix, "etc", "gitconfig")
}

// systemGitConfigPath returns the path to git's system-level config file.
func systemGitConfigPath() string {
	out, _ := exec.Command("git", "config", "--system", "--show-origin", "--list").CombinedOutput()
	if path := parseSystemGitConfigOrigin(string(out)); path != "" {
		return path
	}
	return fallbackSystemGitConfigPath()
}

func rewriteHazmatSafeDirectoryConfig(content string, wanted []string) string {
	sections := parseINI(content)
	var updated []iniSection
	var wantedLines []string
	for _, entry := range wanted {
		wantedLines = append(wantedLines, "\tdirectory = "+entry+hazmatSafeDirMarker)
	}

	inserted := false
	for _, section := range sections {
		if section.name != "safe" {
			if section.name == "" && len(trimSectionEdgeBlankLines(section.lines)) == 0 {
				continue
			}
			updated = append(updated, section)
			continue
		}

		var kept []string
		for _, line := range section.lines {
			if strings.Contains(line, hazmatSafeDirMarker) {
				continue
			}
			kept = append(kept, line)
		}
		kept = trimSectionEdgeBlankLines(kept)

		if !inserted {
			kept = append(kept, wantedLines...)
			inserted = true
		}
		if len(kept) == 0 {
			continue
		}
		section.lines = kept
		updated = append(updated, section)
	}

	if !inserted && len(wantedLines) > 0 {
		updated = append(updated, iniSection{name: "safe", lines: wantedLines})
	}

	return renderINI(updated)
}

func trimSectionEdgeBlankLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func syncHazmatSafeDirectoryConfig(content string, wanted []string) (string, bool) {
	normalized := renderINI(parseINI(content))
	updated := rewriteHazmatSafeDirectoryConfig(content, wanted)
	return updated, updated != normalized
}

func setupGitSafeDirectory(ui *UI, r *Runner) error {
	ui.Step("Configure git safe.directory for agent user")

	gitconfig := systemGitConfigPath()
	if gitconfig == "" {
		ui.WarnMsg("Could not determine system gitconfig path — skipping")
		return nil
	}

	cfg, _ := loadConfig()
	wanted := managedSafeDirectoryEntries(cfg.SessionReadDirs())
	if len(wanted) == 0 {
		ui.SkipDone("No session.read_dirs configured — nothing to add")
		return nil
	}

	content, err := os.ReadFile(gitconfig)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", gitconfig, err)
	}

	updated, changed := syncHazmatSafeDirectoryConfig(string(content), wanted)
	if !changed {
		ui.SkipDone(fmt.Sprintf("safe.directory already configured for %d workspace root(s)", len(wanted)))
		return nil
	}

	if err := r.Sudo("create system gitconfig directory", "mkdir", "-p", filepath.Dir(gitconfig)); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(gitconfig), err)
	}
	if err := r.SudoWriteFile("write hazmat-managed git safe.directory entries", gitconfig, updated); err != nil {
		return fmt.Errorf("write system gitconfig: %w", err)
	}
	if err := r.Sudo("set system gitconfig permissions", "chmod", "644", gitconfig); err != nil {
		return fmt.Errorf("chmod %s: %w", gitconfig, err)
	}

	for _, entry := range wanted {
		ui.Ok(fmt.Sprintf("safe.directory = %s", entry))
	}
	ui.Ok(fmt.Sprintf("Written to %s", gitconfig))
	return nil
}

func rollbackGitSafeDirectory(ui *UI, r *Runner) {
	ui.Step("Remove hazmat-managed git safe.directory entries from system gitconfig")

	gitconfig := systemGitConfigPath()
	if gitconfig == "" {
		ui.SkipDone("Could not determine system gitconfig path")
		return
	}

	content, err := os.ReadFile(gitconfig)
	if err != nil {
		ui.SkipDone("System gitconfig not readable")
		return
	}

	updated, changed := syncHazmatSafeDirectoryConfig(string(content), nil)
	if !changed {
		ui.SkipDone("No hazmat-managed safe.directory entries in system gitconfig")
		return
	}

	if err := r.SudoWriteFile("remove hazmat-managed git safe.directory entries", gitconfig, updated); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", gitconfig, err))
		return
	}
	if err := r.Sudo("set system gitconfig permissions", "chmod", "644", gitconfig); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not chmod %s: %v", gitconfig, err))
		return
	}
	ui.Ok(fmt.Sprintf("Removed hazmat-managed safe.directory entries from %s", gitconfig))
}

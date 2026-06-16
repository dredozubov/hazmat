package hazmat

import (
	"bytes"
	"errors"
	"fmt"
	"hazmat/internal/setup"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const hazmatSafeDirMarker = " # hazmat-managed"

var detectGitRepoTopLevel = detectGitRepoTopLevelImpl
var readSystemGitSafeDirectoryEntries = systemSafeDirectoryEntries
var readAgentGlobalGitSafeDirectoryEntries = agentGlobalSafeDirectoryEntries
var appendAgentGlobalSafeDirectoryEntry = appendAgentGlobalSafeDirectoryEntryImpl
var systemGitConfigReadCandidates = defaultSystemGitConfigReadCandidates
var readAgentGitConfigFile = readAgentGitConfigFileImpl
var readAgentGlobalGitSafeDirectoryEntriesWithGit = agentGlobalSafeDirectoryEntriesWithGit

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
	if repoDir, ok := detectGitRepoTopLevelFast(projectDir); ok {
		return repoDir, true
	}
	out, err := hostGitCombinedOutput("-C", projectDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	repoDir := normalizeSafeDirectoryEntry(string(bytes.TrimSpace(out)))
	if repoDir == "" {
		return "", false
	}
	return repoDir, true
}

func detectGitRepoTopLevelFast(projectDir string) (string, bool) {
	dir := filepath.Clean(projectDir)
	for {
		gitMarker := filepath.Join(dir, ".git")
		info, err := os.Stat(gitMarker)
		switch {
		case err == nil && info.IsDir():
			return normalizeSafeDirectoryEntry(dir), true
		case err == nil && gitFilePointsToMetadata(gitMarker):
			return normalizeSafeDirectoryEntry(dir), true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func gitFilePointsToMetadata(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(data)), "gitdir:")
}

func readGitSafeDirectoryEntriesCommand(cmd *exec.Cmd) ([]string, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			if msg := strings.TrimSpace(string(out)); msg != "" {
				return nil, fmt.Errorf("%w: %s", err, msg)
			}
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
	if entries, ok := systemSafeDirectoryEntriesFromReadableConfig(); ok {
		return entries, nil
	}
	cmd, err := hostGitCommand("config", "--system", "--get-all", "safe.directory")
	if err != nil {
		return nil, err
	}
	return readGitSafeDirectoryEntriesCommand(cmd)
}

func systemSafeDirectoryEntriesFromReadableConfig() ([]string, bool) {
	for _, path := range systemGitConfigReadCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entries, complete := safeDirectoryEntriesFromGitConfigContent(string(data))
		if !complete {
			return nil, false
		}
		return entries, true
	}
	return nil, false
}

func defaultSystemGitConfigReadCandidates() []string {
	var candidates []string
	if path, err := hostGitPath(); err == nil && path != "" {
		prefix := filepath.Dir(filepath.Dir(path))
		candidates = append(candidates, filepath.Join(prefix, "etc", "gitconfig"))
		if path == "/usr/bin/git" {
			candidates = append(candidates, "/Library/Developer/CommandLineTools/usr/etc/gitconfig")
		}
	}
	candidates = append(candidates, "/etc/gitconfig")
	return dedupeStrings(candidates)
}

func agentGlobalSafeDirectoryEntries() ([]string, error) {
	agentGitconfig := agentHome + "/.gitconfig"
	if data, err := readAgentGitConfigFile(agentGitconfig); err == nil {
		entries, complete := safeDirectoryEntriesFromGitConfigContent(string(data))
		if complete {
			return entries, nil
		}
	}
	return readAgentGlobalGitSafeDirectoryEntriesWithGit(agentGitconfig)
}

func readAgentGitConfigFileImpl(path string) ([]byte, error) {
	return newAgentCommand("/bin/cat", path).Output()
}

func agentGlobalSafeDirectoryEntriesWithGit(agentGitconfig string) ([]string, error) {
	cmd := newAgentCommand("git", "config", "--file", agentGitconfig, "--get-all", "safe.directory")
	return readGitSafeDirectoryEntriesCommand(cmd)
}

func safeDirectoryEntriesFromGitConfigContent(content string) ([]string, bool) {
	var entries []string
	for _, section := range parseINI(content) {
		name := strings.TrimSpace(section.name)
		if name == "include" || strings.HasPrefix(name, "includeIf ") {
			return nil, false
		}
		if name != "safe" {
			continue
		}
		for _, line := range section.lines {
			value, ok := parseINIKeyValue(strings.TrimSpace(line), "directory")
			if !ok {
				continue
			}
			value = normalizeGitConfigSafeDirectoryValue(value)
			if value == "" {
				continue
			}
			entries = append(entries, value)
		}
	}
	return dedupeSafeDirectoryEntries(entries), true
}

func normalizeGitConfigSafeDirectoryValue(value string) string {
	value = strings.TrimSpace(value)
	if before, _, ok := strings.Cut(value, hazmatSafeDirMarker); ok {
		value = strings.TrimSpace(before)
	}
	if unquoted, err := strconv.Unquote(value); err == nil {
		value = unquoted
	}
	return strings.TrimSpace(value)
}

func gitSafeDirectoryTrustedForAgent(repoDir string) (bool, error) {
	systemEntries, err := readSystemGitSafeDirectoryEntries()
	if err != nil {
		return false, err
	}
	if safeDirectoryCovers(systemEntries, repoDir) {
		return true, nil
	}

	agentEntries, err := readAgentGlobalGitSafeDirectoryEntries()
	if err != nil {
		return false, err
	}
	return safeDirectoryCovers(agentEntries, repoDir), nil
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

func appendAgentGlobalSafeDirectoryCommand(repoDir string) *exec.Cmd {
	return newAgentCommand("git", "config", "--file", agentHome+"/.gitconfig", "--add", "safe.directory", repoDir)
}

func appendAgentGlobalSafeDirectoryEntryImpl(repoDir string) error {
	repoDir = normalizeSafeDirectoryEntry(repoDir)
	if repoDir == "" {
		return nil
	}
	// Write through Hazmat's helper-backed agent maintenance path because
	// git config needs to create a lock file in the agent's home directory.
	// Use / as cwd so git doesn't fail when the host's cwd is inaccessible
	// to the agent user (the traverse ACL may not have been applied yet).
	cmd := appendAgentGlobalSafeDirectoryCommand(repoDir)
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
	execPath, err := hostGitOutput("--exec-path")
	if err != nil || execPath == "" {
		return ""
	}
	prefix := filepath.Dir(filepath.Dir(execPath))
	return filepath.Join(prefix, "etc", "gitconfig")
}

// systemGitConfigPath returns the path to git's system-level config file.
func systemGitConfigPath() string {
	out, _ := hostGitCombinedOutput("config", "--system", "--show-origin", "--list")
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
	return setup.SetupGitSafeDirectory(setupGitSafeDirectoryEnv(), ui, r)
}

func rollbackGitSafeDirectory(ui *UI, r *Runner) {
	setup.RollbackGitSafeDirectory(setupGitSafeDirectoryEnv(), ui, r)
}

func setupGitSafeDirectoryEnv() setup.GitSafeDirectoryEnv {
	return setup.GitSafeDirectoryEnv{
		SystemGitConfigPath: systemGitConfigPath,
		ManagedEntries: func() []string {
			cfg, _ := loadConfig()
			return managedSafeDirectoryEntries(cfg.SessionReadDirs())
		},
		SyncConfig: syncHazmatSafeDirectoryConfig,
	}
}

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const hazmatSafeDirMarker = " # hazmat-managed"

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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type shellProfile struct {
	name           string
	rcPath         string
	pathBlockLines []string
}

func supportedUserShellProfiles() []shellProfile {
	home := os.Getenv("HOME")
	return []shellProfile{
		{
			name:           "zsh",
			rcPath:         filepath.Join(home, ".zshrc"),
			pathBlockLines: []string{`export PATH="$HOME/.local/bin:$PATH"`},
		},
		{
			name:           "bash",
			rcPath:         filepath.Join(home, ".bashrc"),
			pathBlockLines: []string{`export PATH="$HOME/.local/bin:$PATH"`},
		},
		{
			name:   "fish",
			rcPath: filepath.Join(home, ".config", "fish", "config.fish"),
			pathBlockLines: []string{
				`if not contains "$HOME/.local/bin" $PATH`,
				`    set -gx PATH "$HOME/.local/bin" $PATH`,
				`end`,
			},
		},
	}
}

func currentUserShellProfile() (shellProfile, bool) {
	shell := filepath.Base(os.Getenv("SHELL"))
	for _, profile := range supportedUserShellProfiles() {
		if profile.name == shell {
			return profile, true
		}
	}
	return shellProfile{}, false
}

func userZshrcPath() string {
	if profile, ok := currentUserShellProfile(); ok {
		return profile.rcPath
	}
	return filepath.Join(os.Getenv("HOME"), ".zshrc")
}

func hostWrapperDir() string {
	return filepath.Join(os.Getenv("HOME"), hostWrapperDirRel)
}

func hostWrapperPath(name string) string {
	return filepath.Join(hostWrapperDir(), name)
}

func agentEnvContent() string {
	return fmt.Sprintf(`# Managed by hazmat init.
export PATH="%s"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-$HOME/.cache}"
export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
export XDG_DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
export HOMEBREW_NO_AUTO_UPDATE="${HOMEBREW_NO_AUTO_UPDATE:-1}"

mkdir -p "$XDG_CACHE_HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$HOME/.npm" >/dev/null 2>&1 || true

if [[ -x "$HOME/.local/bin/claude-sandboxed" ]]; then
  alias claude="$HOME/.local/bin/claude-sandboxed"
fi

if [[ -n "${SANDBOX_ACTIVE:-}" && -o interactive ]]; then
  PROMPT="%%F{red}[agent:hazmat]%%f %%~ %%# "
fi
`, defaultAgentPath)
}

func hostWrapperContent(hazmatBin, subcommand string) string {
	// No fallback to `command -v hazmat`: on macOS `command -v sandbox`
	// resolves to /usr/bin/sandbox (Apple's SBPL tool), not this binary.
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail

HAZMAT_BIN=%q
if [[ ! -x "$HAZMAT_BIN" ]]; then
  printf 'error: hazmat binary not found: %%s\n' "$HAZMAT_BIN" >&2
  printf 'Re-run "hazmat init" to refresh the wrappers.\n' >&2
  exit 1
fi

exec "$HAZMAT_BIN" %s "$@"
`, hazmatBin, subcommand)
}

func managedBlock(start, end string, lines ...string) string {
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	return start + "\n" + body + end + "\n"
}

func upsertManagedBlock(existing, start, end string, lines ...string) string {
	block := managedBlock(start, end, lines...)
	cleaned := removeManagedBlock(existing, start, end)
	cleaned = strings.TrimRight(cleaned, "\n")
	if cleaned == "" {
		return block
	}
	return cleaned + "\n\n" + block
}

func removeManagedBlock(existing, start, end string) string {
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

package hazmat

import (
	"hazmat/internal/setup"
	"os"
	"path/filepath"
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

func managedBlock(start, end string, lines ...string) string {
	return setup.ManagedBlock(start, end, lines...)
}

func upsertManagedBlock(existing, start, end string, lines ...string) string {
	return setup.UpsertManagedBlock(existing, start, end, lines...)
}

func removeManagedBlock(existing, start, end string) string {
	return setup.RemoveManagedBlock(existing, start, end)
}

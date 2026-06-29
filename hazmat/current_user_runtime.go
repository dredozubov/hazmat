package hazmat

import (
	"os"
	"path/filepath"
	"strings"
)

const currentUserFallbackPath = "/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

func currentUserName() string {
	if value := strings.TrimSpace(os.Getenv("USER")); value != "" && value != agentUser {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("LOGNAME")); value != "" && value != agentUser {
		return value
	}
	return "current"
}

func currentUserLaunchPath() string {
	raw := strings.TrimSpace(os.Getenv("PATH"))
	if raw == "" {
		return currentUserFallbackPath
	}
	var kept []string
	for _, entry := range filepath.SplitList(raw) {
		entry = strings.TrimSpace(entry)
		if entry == "" || entry == agentHome || strings.HasPrefix(entry, agentHome+string(os.PathSeparator)) {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == 0 {
		return currentUserFallbackPath
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

func currentUserSessionDirsForConfig(cfg sessionConfig) currentUserSessionDirs {
	if cfg.CurrentUserSession != nil {
		return *cfg.CurrentUserSession
	}
	root := filepath.Join(os.TempDir(), "hazmat-current-user-preview")
	home := filepath.Join(root, "home")
	return currentUserSessionDirs{
		Root:       root,
		Home:       home,
		CacheHome:  filepath.Join(home, ".cache"),
		ConfigHome: filepath.Join(home, ".config"),
		DataHome:   filepath.Join(home, ".local", "share"),
		TempDir:    filepath.Join(root, "tmp"),
	}
}

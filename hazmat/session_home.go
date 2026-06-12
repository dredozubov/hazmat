package hazmat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultSessionHomeRoot = "/private/tmp/hazmat-home"
	sessionHomeMarkerFile  = ".hazmat-session-home"
)

type sessionHomeLayout struct {
	SessionID  string
	Root       string
	SessionDir string
	Home       string
	CacheHome  string
	ConfigHome string
	DataHome   string
	MarkerPath string
}

func newSessionHomeLayout(root, sessionID string) (sessionHomeLayout, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return sessionHomeLayout{}, fmt.Errorf("session home root %q must be absolute", root)
	}
	if err := validateSessionHomeID(sessionID); err != nil {
		return sessionHomeLayout{}, err
	}
	sessionDir := filepath.Join(root, sessionID)
	home := filepath.Join(sessionDir, "home")
	return sessionHomeLayout{
		SessionID:  sessionID,
		Root:       root,
		SessionDir: sessionDir,
		Home:       home,
		CacheHome:  filepath.Join(home, ".cache"),
		ConfigHome: filepath.Join(home, ".config"),
		DataHome:   filepath.Join(home, ".local", "share"),
		MarkerPath: filepath.Join(sessionDir, sessionHomeMarkerFile),
	}, nil
}

func validateSessionHomeID(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session home id is required")
	}
	for _, r := range sessionID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("session home id %q contains unsupported character %q", sessionID, r)
	}
	if sessionID == "." || sessionID == ".." || strings.HasPrefix(sessionID, ".") {
		return fmt.Errorf("session home id %q is reserved", sessionID)
	}
	return nil
}

func createSessionHomeLayout(layout sessionHomeLayout) error {
	if err := os.MkdirAll(layout.CacheHome, 0o700); err != nil {
		return fmt.Errorf("create session XDG cache dir: %w", err)
	}
	if err := os.MkdirAll(layout.ConfigHome, 0o700); err != nil {
		return fmt.Errorf("create session XDG config dir: %w", err)
	}
	if err := os.MkdirAll(layout.DataHome, 0o700); err != nil {
		return fmt.Errorf("create session XDG data dir: %w", err)
	}
	if err := os.WriteFile(layout.MarkerPath, []byte("hazmat session home\n"), 0o600); err != nil {
		return fmt.Errorf("write session home marker: %w", err)
	}
	return nil
}

func cleanupStaleSessionHomes(root string, now time.Time, maxAge time.Duration) ([]string, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("session home root %q must be absolute", root)
	}
	if maxAge <= 0 {
		return nil, fmt.Errorf("session home cleanup max age must be positive")
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session home root: %w", err)
	}
	var removed []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		sessionDir := filepath.Join(root, entry.Name())
		marker := filepath.Join(sessionDir, sessionHomeMarkerFile)
		info, err := os.Stat(marker)
		if err != nil || info.IsDir() {
			continue
		}
		if now.Sub(info.ModTime()) < maxAge {
			continue
		}
		if err := os.RemoveAll(sessionDir); err != nil {
			return removed, fmt.Errorf("remove stale session home %s: %w", sessionDir, err)
		}
		removed = append(removed, sessionDir)
	}
	return removed, nil
}

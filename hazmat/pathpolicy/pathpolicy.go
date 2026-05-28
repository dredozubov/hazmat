package pathpolicy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var credentialDenySubpaths = []string{
	"/.ssh",
	"/.aws",
	"/.gnupg",
	"/Library/Keychains",
	"/.config/gh",
	"/.docker",
	"/.kube",
	"/.netrc",
	"/.m2/settings.xml",
	"/.config/gcloud",
	"/.azure",
	"/.oci",
}

var hostStateDenySubpaths = []string{
	"/.codex/sqlite",
	"/Library/Application Support/Codex",
	"/Library/HTTPStorages/com.openai.codex",
	"/Library/Caches/com.openai.codex",
	"/Library/Preferences/com.openai.codex.plist",
	"/Library/Logs/com.openai.codex",
}

type DenyPolicy struct {
	BaseHomes              []string
	CredentialDenySubpaths []string
	HostStateDenySubpaths  []string
}

func CredentialDenySubpaths() []string {
	return append([]string(nil), credentialDenySubpaths...)
}

func HostStateDenySubpaths() []string {
	return append([]string(nil), hostStateDenySubpaths...)
}

func NewDenyPolicy(baseHomes []string) DenyPolicy {
	return DenyPolicy{
		BaseHomes:              append([]string(nil), baseHomes...),
		CredentialDenySubpaths: CredentialDenySubpaths(),
		HostStateDenySubpaths:  HostStateDenySubpaths(),
	}
}

func DefaultDenyPolicy(agentHome, invokerHome string) DenyPolicy {
	return NewDenyPolicy(DenyBaseHomes(agentHome, invokerHome))
}

func DenyBaseHomes(paths ...string) []string {
	seen := map[string]struct{}{}
	var homes []string
	add := func(path string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; !ok {
			seen[clean] = struct{}{}
			homes = append(homes, clean)
		}
		if resolved, err := filepath.EvalSymlinks(clean); err == nil {
			resolved = filepath.Clean(resolved)
			if _, ok := seen[resolved]; !ok {
				seen[resolved] = struct{}{}
				homes = append(homes, resolved)
			}
		}
	}
	for _, path := range paths {
		add(path)
	}
	return homes
}

func (p DenyPolicy) CredentialDenyPath(canonical string) bool {
	for _, home := range p.BaseHomes {
		for _, sub := range p.CredentialDenySubpaths {
			denyPath := home + sub
			if canonical == denyPath {
				return true
			}
			if strings.HasPrefix(denyPath, canonical+"/") {
				return true
			}
		}
	}
	return false
}

func (p DenyPolicy) HostStateDenyPath(canonical string) bool {
	for _, home := range p.BaseHomes {
		for _, sub := range p.HostStateDenySubpaths {
			if PathsOverlap(canonical, home+sub) {
				return true
			}
		}
	}
	return false
}

func PathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func Canonicalize(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks for %q: %w", path, err)
	}
	return resolved, nil
}

func ResolveDir(target string, defaultToCwd bool) (string, error) {
	if target == "" && defaultToCwd {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determine current directory: %w", err)
		}
		target = wd
	}
	if target == "" {
		return "", fmt.Errorf("path is required")
	}

	resolved, err := Canonicalize(target)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", resolved)
	}
	return resolved, nil
}

func ExpandTilde(path string, userHome func() (string, error)) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	if userHome == nil {
		userHome = os.UserHomeDir
	}
	home, err := userHome()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

func AppendUniqueDirs(existing, additions []string) ([]string, []string) {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, dir := range existing {
		seen[dir] = struct{}{}
	}

	merged := append([]string(nil), existing...)
	var added []string
	for _, dir := range additions {
		if _, dup := seen[dir]; dup {
			continue
		}
		seen[dir] = struct{}{}
		merged = append(merged, dir)
		added = append(added, dir)
	}
	return merged, added
}

func SubtractResolvedDirs(candidates, existing []string) []string {
	existingResolved := make(map[string]struct{}, len(existing))
	for _, dir := range existing {
		resolved, err := canonicalizeIfPossible(dir)
		if err != nil {
			continue
		}
		existingResolved[resolved] = struct{}{}
	}

	var filtered []string
	seen := make(map[string]struct{}, len(candidates))
	for _, dir := range candidates {
		resolved, err := canonicalizeIfPossible(dir)
		if err != nil {
			continue
		}
		if _, dup := existingResolved[resolved]; dup {
			continue
		}
		if _, dup := seen[resolved]; dup {
			continue
		}
		seen[resolved] = struct{}{}
		filtered = append(filtered, dir)
	}
	return filtered
}

func canonicalizeIfPossible(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

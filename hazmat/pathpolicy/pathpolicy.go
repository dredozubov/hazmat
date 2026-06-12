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
	"/.cache/huggingface/token",
	"/.cache/huggingface/stored_tokens",
	"/.ollama/id_ed25519",
	"/.jupyter",
	"/.local/share/jupyter/runtime",
	"/.langsmith",
	"/.config/amp",
	"/.config/devin",
	"/.config/kilo",
	"/.kimi-code",
	"/.kimi",
	"/.kiro",
	"/.vibe",
	"/.traecli",
	"/.pi/agent",
	"/.config/crush",
	"/.local/share/crush",
	"/.openclaw",
	"/.qoder",
	"/.copilot",
	"/.deepseek",
	"/.codewhale",
	"/.continue",
	"/.cline",
	"/.aider.conf.yml",
	"/.config/goose",
	"/.local/share/goose",
	"/.local/state/goose",
	"/Library/Application Support/Claude",
	"/Library/Application Support/Cursor",
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
	baseHomes              []string
	credentialDenySubpaths []string
	hostStateDenySubpaths  []string
	hostAuthorityDenyPaths []string
	configured             bool
}

// AbsolutePath is an absolute, cleaned filesystem path.
type AbsolutePath struct {
	path string
}

// ExistingDir is an existing directory path.
type ExistingDir struct {
	path string
}

// CanonicalDir is an existing directory after symlink resolution.
type CanonicalDir struct {
	path string
}

// ProjectRoot is a canonical session project directory that does not overlap
// a credential or host-state deny zone.
type ProjectRoot struct {
	dir CanonicalDir
}

// ReadOnlyGrant is a canonical read-only path grant that does not overlap a
// credential or host-state deny zone.
type ReadOnlyGrant struct {
	dir CanonicalDir
}

// ReadWriteGrant is a canonical read-write path grant that does not overlap a
// credential or host-state deny zone.
type ReadWriteGrant struct {
	dir CanonicalDir
}

// DenyZoneError reports a denied path constructor input.
type DenyZoneError struct {
	Label string
	Path  string
	Zone  string
}

func (e DenyZoneError) Error() string {
	return fmt.Sprintf("%s %q resolves to %s deny zone", e.Label, e.Path, e.Zone)
}

func CredentialDenySubpaths() []string {
	return append([]string(nil), credentialDenySubpaths...)
}

func HostStateDenySubpaths() []string {
	return append([]string(nil), hostStateDenySubpaths...)
}

func NewDenyPolicy(baseHomes []string) DenyPolicy {
	return DenyPolicy{
		baseHomes:              append([]string(nil), baseHomes...),
		credentialDenySubpaths: CredentialDenySubpaths(),
		hostStateDenySubpaths:  HostStateDenySubpaths(),
		configured:             true,
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
	for _, home := range p.baseHomes {
		if strings.TrimSpace(home) == "" {
			continue
		}
		home = filepath.Clean(home)
		for _, sub := range p.credentialSubpaths() {
			denyPath := home + sub
			if PathsOverlap(canonical, denyPath) {
				return true
			}
		}
	}
	return false
}

// WithHostAuthorityDenyPaths returns a copy of the policy that additionally
// denies the given absolute host-authority paths (for example a dr-owned
// Beadpost broker attestation key). These are dr-owned ABSOLUTE paths, not
// agent-home-relative subpaths, and they are a deny category distinct from the
// credential and host-state zones: no keychain-style re-allow exception ever
// applies to them. Empty and non-absolute entries are ignored.
func (p DenyPolicy) WithHostAuthorityDenyPaths(paths ...string) DenyPolicy {
	merged := append([]string(nil), p.hostAuthorityDenyPaths...)
	seen := make(map[string]struct{}, len(merged)+len(paths))
	for _, existing := range merged {
		seen[existing] = struct{}{}
	}
	for _, raw := range paths {
		if strings.TrimSpace(raw) == "" || !filepath.IsAbs(raw) {
			continue
		}
		clean := filepath.Clean(raw)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		merged = append(merged, clean)
	}
	p.hostAuthorityDenyPaths = merged
	return p
}

// HostAuthorityDenyPath reports whether canonical overlaps a registered
// host-authority deny path.
func (p DenyPolicy) HostAuthorityDenyPath(canonical string) bool {
	for _, denyPath := range p.hostAuthorityDenyPaths {
		if strings.TrimSpace(denyPath) == "" {
			continue
		}
		if PathsOverlap(canonical, denyPath) {
			return true
		}
	}
	return false
}

func (p DenyPolicy) HostStateDenyPath(canonical string) bool {
	for _, home := range p.baseHomes {
		if strings.TrimSpace(home) == "" {
			continue
		}
		home = filepath.Clean(home)
		for _, sub := range p.hostStateSubpaths() {
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

func (p DenyPolicy) credentialSubpaths() []string {
	if len(p.credentialDenySubpaths) == 0 {
		return credentialDenySubpaths
	}
	return p.credentialDenySubpaths
}

func (p DenyPolicy) hostStateSubpaths() []string {
	if len(p.hostStateDenySubpaths) == 0 {
		return hostStateDenySubpaths
	}
	return p.hostStateDenySubpaths
}

func (p DenyPolicy) ValidateAllowedPath(label, canonical string) error {
	if !p.configured {
		return fmt.Errorf("deny policy is required")
	}
	if p.CredentialDenyPath(canonical) {
		return DenyZoneError{Label: label, Path: canonical, Zone: "credential"}
	}
	if p.HostStateDenyPath(canonical) {
		return DenyZoneError{Label: label, Path: canonical, Zone: "host-state"}
	}
	if p.HostAuthorityDenyPath(canonical) {
		return DenyZoneError{Label: label, Path: canonical, Zone: "host-authority"}
	}
	return nil
}

func NewAbsolutePath(path string) (AbsolutePath, error) {
	if strings.TrimSpace(path) == "" {
		return AbsolutePath{}, fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return AbsolutePath{}, fmt.Errorf("resolve %q: %w", path, err)
	}
	return AbsolutePath{path: filepath.Clean(abs)}, nil
}

func (p AbsolutePath) String() string {
	return p.path
}

func NewExistingDir(path AbsolutePath) (ExistingDir, error) {
	info, err := os.Stat(path.String())
	if err != nil {
		return ExistingDir{}, fmt.Errorf("stat %q: %w", path.String(), err)
	}
	if !info.IsDir() {
		return ExistingDir{}, fmt.Errorf("%q is not a directory", path.String())
	}
	return ExistingDir{path: path.String()}, nil
}

func ResolveExistingDir(target string, defaultToCwd bool) (ExistingDir, error) {
	dir, err := ResolveCanonicalDir(target, defaultToCwd)
	if err != nil {
		return ExistingDir{}, err
	}
	return ExistingDir{path: dir.String()}, nil
}

func (d ExistingDir) String() string {
	return d.path
}

func NewCanonicalDir(dir ExistingDir) (CanonicalDir, error) {
	resolved, err := filepath.EvalSymlinks(dir.String())
	if err != nil {
		return CanonicalDir{}, fmt.Errorf("resolve symlinks for %q: %w", dir.String(), err)
	}
	return CanonicalDir{path: resolved}, nil
}

func ResolveCanonicalDir(target string, defaultToCwd bool) (CanonicalDir, error) {
	resolved, err := ResolveDir(target, defaultToCwd)
	if err != nil {
		return CanonicalDir{}, err
	}
	return CanonicalDir{path: resolved}, nil
}

func (d CanonicalDir) String() string {
	return d.path
}

func NewProjectRoot(dir CanonicalDir, policy DenyPolicy) (ProjectRoot, error) {
	if err := validateCanonicalDir(dir); err != nil {
		return ProjectRoot{}, err
	}
	if err := policy.ValidateAllowedPath("project dir", dir.String()); err != nil {
		return ProjectRoot{}, err
	}
	return ProjectRoot{dir: dir}, nil
}

func ResolveProjectRoot(target string, defaultToCwd bool, policy DenyPolicy) (ProjectRoot, error) {
	dir, err := ResolveCanonicalDir(target, defaultToCwd)
	if err != nil {
		return ProjectRoot{}, err
	}
	return NewProjectRoot(dir, policy)
}

func (r ProjectRoot) String() string {
	return r.dir.String()
}

func NewReadOnlyGrant(dir CanonicalDir, policy DenyPolicy) (ReadOnlyGrant, error) {
	if err := validateCanonicalDir(dir); err != nil {
		return ReadOnlyGrant{}, err
	}
	if err := policy.ValidateAllowedPath("read dir", dir.String()); err != nil {
		return ReadOnlyGrant{}, err
	}
	return ReadOnlyGrant{dir: dir}, nil
}

func ResolveReadOnlyGrant(target string, policy DenyPolicy) (ReadOnlyGrant, error) {
	dir, err := ResolveCanonicalDir(target, false)
	if err != nil {
		return ReadOnlyGrant{}, fmt.Errorf("read dir %q: %w", target, err)
	}
	return NewReadOnlyGrant(dir, policy)
}

func (g ReadOnlyGrant) String() string {
	return g.dir.String()
}

func NewReadWriteGrant(dir CanonicalDir, policy DenyPolicy) (ReadWriteGrant, error) {
	if err := validateCanonicalDir(dir); err != nil {
		return ReadWriteGrant{}, err
	}
	if err := policy.ValidateAllowedPath("write dir", dir.String()); err != nil {
		return ReadWriteGrant{}, err
	}
	return ReadWriteGrant{dir: dir}, nil
}

func ResolveReadWriteGrant(target string, policy DenyPolicy) (ReadWriteGrant, error) {
	dir, err := ResolveCanonicalDir(target, false)
	if err != nil {
		return ReadWriteGrant{}, fmt.Errorf("read dir %q: %w", target, err)
	}
	return NewReadWriteGrant(dir, policy)
}

func (g ReadWriteGrant) String() string {
	return g.dir.String()
}

func validateCanonicalDir(dir CanonicalDir) error {
	if strings.TrimSpace(dir.String()) == "" {
		return fmt.Errorf("canonical dir is required")
	}
	return nil
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

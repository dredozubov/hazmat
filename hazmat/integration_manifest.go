package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ── Credential deny paths ──────────────────────────────────────────────────
// These must match the seatbelt deny list in generateSBPL() exactly.

var credentialDenySubs = []string{
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

// ── Canonical path helpers ─────────────────────────────────────────────────

// canonicalizePath resolves a path to its absolute, symlink-free form.
// This is the same canonicalization used by resolveDir in session.go.
func canonicalizePath(path string) (string, error) {
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

// isCredentialDenyPath checks whether the given canonical path is a credential
// deny path or a proper parent of one. The seatbelt deny list uses agentHome
// as the base, but integration read_dirs are resolved against the invoker's
// home. We check both agent home and invoker home as bases, since a path
// under the invoker's home could still overlap with the agent's deny zone
// if it's an unusual layout.
//
// The check is: reject if the path IS a credential deny path, or if the
// path is a proper parent of one (which would grant broad access covering
// the credential subpath).
func isCredentialDenyPath(canonical string) bool {
	homes := []string{agentHome}
	if h, err := os.UserHomeDir(); err == nil && h != agentHome {
		homes = append(homes, h)
	}

	for _, home := range homes {
		for _, sub := range credentialDenySubs {
			credPath := home + sub

			// Exact match: path IS a credential deny path.
			if canonical == credPath {
				return true
			}

			// Parent check: path is a proper parent of a credential path.
			// e.g., /Users/dr would be a parent of /Users/dr/.ssh
			if strings.HasPrefix(credPath, canonical+"/") {
				return true
			}
		}
	}
	return false
}

// ── Safe environment key set ───────────────────────────────────────────────
// Integrations may only request passthrough for these keys. Each must be a passive
// path pointer or selector, NOT a flag-injection or code-preload vector.
//
// Excluded by design: NODE_OPTIONS, PYTHONPATH, GOFLAGS, MAVEN_OPTS,
// CGO_CFLAGS, CFLAGS, CXXFLAGS, LDFLAGS, BUNDLE_PATH, CC, CXX, etc.

var safeEnvKeys = map[string]bool{
	// Go — passive path pointers and module config
	"GOPATH":       true,
	"GOROOT":       true,
	"GOPROXY":      true, // registry redirect — residual risk, surfaced in UX
	"GONOPROXY":    true,
	"GONOSUMCHECK": true,
	"GOPRIVATE":    true,
	"CGO_ENABLED":  true, // "0" or "1", not flag injection

	// Rust — path pointers only
	"RUSTUP_HOME":      true,
	"CARGO_HOME":       true,
	"CARGO_TARGET_DIR": true,

	// Node — mode selector and registry
	"NODE_ENV":            true,
	"NPM_CONFIG_REGISTRY": true, // registry redirect — residual risk, surfaced in UX

	// Python — mode flag and venv pointer
	"VIRTUAL_ENV": true,

	// Java — path pointer only
	"JAVA_HOME": true,

	// Ruby — path pointer only
	"GEM_HOME": true,

	// Editor preference
	"EDITOR": true,
	"VISUAL": true,
}

// registryEnvKeys are safeEnvKeys that can redirect downloads to different
// servers. When active, these are surfaced in session UX as a residual risk.
var registryEnvKeys = map[string]bool{
	"GOPROXY":             true,
	"NPM_CONFIG_REGISTRY": true,
}

// ── Integration manifest types ─────────────────────────────────────────────

type IntegrationSpec struct {
	Meta     IntegrationMeta    `yaml:"integration"`
	Detect   IntegrationDetect  `yaml:"detect"`
	Session  IntegrationSession `yaml:"session"`
	Backup   IntegrationBackup  `yaml:"backup"`
	Warnings []string           `yaml:"warnings"`
	Commands map[string]string  `yaml:"commands"`
}

type IntegrationMeta struct {
	Name        string `yaml:"name"`
	Version     int    `yaml:"version"`
	Description string `yaml:"description"`
}

type IntegrationDetect struct {
	Files []string `yaml:"files"`
}

type IntegrationSession struct {
	ReadDirs       []string `yaml:"read_dirs"`
	EnvPassthrough []string `yaml:"env_passthrough"`
}

type IntegrationBackup struct {
	Excludes []string `yaml:"excludes"`
}

// ── Validation ─────────────────────────────────────────────────────────────

const (
	integrationMaxSize        = 8192 // 8KB manifest limit
	integrationMaxReadDirs    = 20
	integrationMaxEnvKeys     = 20
	integrationMaxExcludes    = 50
	integrationMaxWarnings    = 10
	integrationMaxCommands    = 20
	integrationMaxDetectFiles = 10
)

var integrationNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validateIntegrationSchema checks structural validity (V1, V3, V4, V5, V6).
// This runs at load time before paths are resolved.
func validateIntegrationSchema(p IntegrationSpec) error {
	// V1: required fields and types.
	if p.Meta.Name == "" {
		return fmt.Errorf("integration: name is required")
	}
	if !integrationNameRe.MatchString(p.Meta.Name) {
		return fmt.Errorf("integration %q: name must match %s", p.Meta.Name, integrationNameRe)
	}
	if p.Meta.Version != 1 {
		return fmt.Errorf("integration %q: version must be 1, got %d", p.Meta.Name, p.Meta.Version)
	}

	// V3: env passthrough keys must be in safe set.
	for _, key := range p.Session.EnvPassthrough {
		if !safeEnvKeys[key] {
			return fmt.Errorf("integration %q: env key %q not in safe passthrough set", p.Meta.Name, key)
		}
	}

	// V4: exclude patterns.
	for _, pat := range p.Backup.Excludes {
		if pat == "" {
			return fmt.Errorf("integration %q: empty exclude pattern", p.Meta.Name)
		}
		if strings.HasPrefix(pat, "!") {
			return fmt.Errorf("integration %q: negation excludes not supported: %q", p.Meta.Name, pat)
		}
	}

	// V5: detect files must be basenames or basename globs, no path separators.
	for _, f := range p.Detect.Files {
		if strings.Contains(f, "/") {
			return fmt.Errorf("integration %q: detect file %q must be a filename, not a path", p.Meta.Name, f)
		}
	}

	// V6: bounds.
	if len(p.Session.ReadDirs) > integrationMaxReadDirs {
		return fmt.Errorf("integration %q: too many read_dirs (%d, max %d)", p.Meta.Name, len(p.Session.ReadDirs), integrationMaxReadDirs)
	}
	if len(p.Session.EnvPassthrough) > integrationMaxEnvKeys {
		return fmt.Errorf("integration %q: too many env_passthrough keys (%d, max %d)", p.Meta.Name, len(p.Session.EnvPassthrough), integrationMaxEnvKeys)
	}
	if len(p.Backup.Excludes) > integrationMaxExcludes {
		return fmt.Errorf("integration %q: too many excludes (%d, max %d)", p.Meta.Name, len(p.Backup.Excludes), integrationMaxExcludes)
	}
	if len(p.Warnings) > integrationMaxWarnings {
		return fmt.Errorf("integration %q: too many warnings (%d, max %d)", p.Meta.Name, len(p.Warnings), integrationMaxWarnings)
	}
	if len(p.Commands) > integrationMaxCommands {
		return fmt.Errorf("integration %q: too many commands (%d, max %d)", p.Meta.Name, len(p.Commands), integrationMaxCommands)
	}
	if len(p.Detect.Files) > integrationMaxDetectFiles {
		return fmt.Errorf("integration %q: too many detect files (%d, max %d)", p.Meta.Name, len(p.Detect.Files), integrationMaxDetectFiles)
	}

	return nil
}

// validateIntegrationPaths checks read_dirs against credential deny zones (V2).
// This runs at session start after tilde expansion and canonicalization.
// Returns the canonical paths for use in session merge.
func validateIntegrationPaths(p IntegrationSpec) ([]string, error) {
	var canonical []string
	for _, dir := range p.Session.ReadDirs {
		expanded := expandTilde(dir)

		// Path must exist (skip silently if it doesn't — same as defaultReadDirs).
		if _, err := os.Stat(expanded); err != nil {
			continue
		}

		resolved, err := canonicalizePath(expanded)
		if err != nil {
			continue // skip unresolvable paths
		}

		if isCredentialDenyPath(resolved) {
			return nil, fmt.Errorf("integration %q: read_dir %q resolves to credential deny zone %q",
				p.Meta.Name, dir, resolved)
		}

		canonical = append(canonical, resolved)
	}
	return canonical, nil
}

// ── Loading ────────────────────────────────────────────────────────────────

//go:embed integrations/*.yaml
var builtinIntegrationsFS embed.FS

const (
	repoRecommendedIntegrationsFile       = ".hazmat/integrations.yaml"
	legacyRepoRecommendedIntegrationsFile = ".hazmat/packs.yaml"
)

var (
	userIntegrationDir           = filepath.Join(os.Getenv("HOME"), ".hazmat/integrations")
	legacyUserIntegrationDir     = filepath.Join(os.Getenv("HOME"), ".hazmat/packs")
	integrationApprovalsFilePath = filepath.Join(os.Getenv("HOME"), ".hazmat/integration-approvals.yaml")
)

func hasLegacyTopLevelKey(data []byte, key string) bool {
	trimmed := bytes.TrimSpace(data)
	prefix := []byte(key + ":")
	return bytes.HasPrefix(trimmed, prefix) || bytes.Contains(data, append([]byte("\n"), prefix...))
}

// loadIntegrationSpec parses and schema-validates a single integration manifest
// from YAML bytes. Unknown fields are rejected (fail closed) so typos and
// unsupported keys are caught at load time rather than silently ignored.
func loadIntegrationSpec(data []byte) (IntegrationSpec, error) {
	if len(data) > integrationMaxSize {
		return IntegrationSpec{}, fmt.Errorf("integration manifest exceeds %d byte limit", integrationMaxSize)
	}
	if hasLegacyTopLevelKey(data, "pack") {
		return IntegrationSpec{}, fmt.Errorf("legacy integration manifest schema detected: rename top-level key 'pack:' to 'integration:'")
	}

	var p IntegrationSpec
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return IntegrationSpec{}, fmt.Errorf("parse integration manifest: %w", err)
	}

	if err := validateIntegrationSchema(p); err != nil {
		return IntegrationSpec{}, err
	}

	return p, nil
}

// loadBuiltinIntegrationSpec loads a built-in integration spec by name from the
// embedded filesystem.
func loadBuiltinIntegrationSpec(name string) (IntegrationSpec, error) {
	data, err := builtinIntegrationsFS.ReadFile("integrations/" + name + ".yaml")
	if err != nil {
		return IntegrationSpec{}, fmt.Errorf("built-in integration %q not found", name)
	}
	return loadIntegrationSpec(data)
}

// loadUserIntegrationSpec loads a user-installed integration spec from
// ~/.hazmat/integrations/<name>.yaml.
func loadUserIntegrationSpec(name string) (IntegrationSpec, error) {
	path := filepath.Join(userIntegrationDir, name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		legacyPath := filepath.Join(legacyUserIntegrationDir, name+".yaml")
		if _, legacyErr := os.Stat(legacyPath); legacyErr == nil {
			return IntegrationSpec{}, fmt.Errorf("legacy user integration manifest %q detected; move it to %q", legacyPath, path)
		}
		return IntegrationSpec{}, fmt.Errorf("user integration %q: %w", name, err)
	}
	return loadIntegrationSpec(data)
}

// loadIntegrationSpecByName tries built-in first, then user-installed.
func loadIntegrationSpecByName(name string) (IntegrationSpec, error) {
	if p, err := loadBuiltinIntegrationSpec(name); err == nil {
		return p, nil
	}
	return loadUserIntegrationSpec(name)
}

// allBuiltinIntegrationNames returns the names of all embedded integrations.
func allBuiltinIntegrationNames() []string {
	entries, err := builtinIntegrationsFS.ReadDir("integrations")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".yaml") {
			names = append(names, strings.TrimSuffix(name, ".yaml"))
		}
	}
	return names
}

// ── Repo-recommended integrations ──────────────────────────────────────────
//
// A repo may declare recommended integrations in .hazmat/integrations.yaml.
// The file is pure data: a list of integration names referencing existing
// built-in or user-installed integration specs. No inline definitions, no
// paths, no env keys.
//
// Repo owns intent; host owns trust. Recommendations are never auto-activated.
// On first encounter (or after file change), hazmat prompts the user for
// approval. Approval is stored outside the repo in
// ~/.hazmat/integration-approvals.yaml, keyed by canonical project path +
// SHA-256 of the recommendations file.

// repoRecommendedIntegrations is the schema for .hazmat/integrations.yaml.
type repoRecommendedIntegrations struct {
	Integrations []string `yaml:"integrations"`
}

// integrationApprovalRecord is one entry in the approvals file.
type integrationApprovalRecord struct {
	ProjectDir string `yaml:"project"`
	FileHash   string `yaml:"hash"`
}

// integrationApprovalsFile is the top-level schema for
// ~/.hazmat/integration-approvals.yaml.
type integrationApprovalsFile struct {
	Approvals []integrationApprovalRecord `yaml:"approvals"`
}

// loadRepoRecommendations reads .hazmat/integrations.yaml from the project
// directory. Returns the integration names and the SHA-256 of the file
// contents. Returns nil names if the file doesn't exist.
func loadRepoRecommendations(projectDir string) ([]string, string, error) {
	legacyPath := filepath.Join(projectDir, legacyRepoRecommendedIntegrationsFile)
	if _, err := os.Stat(legacyPath); err == nil {
		return nil, "", fmt.Errorf("legacy repo integration file detected: rename %s to %s", legacyRepoRecommendedIntegrationsFile, repoRecommendedIntegrationsFile)
	}

	path := filepath.Join(projectDir, repoRecommendedIntegrationsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("read %s: %w", repoRecommendedIntegrationsFile, err)
	}

	if len(data) > integrationMaxSize {
		return nil, "", fmt.Errorf("%s exceeds %d byte limit", repoRecommendedIntegrationsFile, integrationMaxSize)
	}

	if hasLegacyTopLevelKey(data, "packs") {
		return nil, "", fmt.Errorf("%s uses legacy 'packs:' schema; rename the key to 'integrations:'", repoRecommendedIntegrationsFile)
	}

	var rec repoRecommendedIntegrations
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&rec); err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", repoRecommendedIntegrationsFile, err)
	}

	// Validate: every name must resolve through existing loaders.
	for _, name := range rec.Integrations {
		if _, err := loadIntegrationSpecByName(name); err != nil {
			return nil, "", fmt.Errorf("%s: unknown integration %q", repoRecommendedIntegrationsFile, name)
		}
	}

	hash := sha256.Sum256(data)
	return rec.Integrations, hex.EncodeToString(hash[:]), nil
}

// loadIntegrationApprovals reads the approval file.
func loadIntegrationApprovals() integrationApprovalsFile {
	data, err := os.ReadFile(integrationApprovalsFilePath)
	if err != nil {
		return integrationApprovalsFile{}
	}
	var af integrationApprovalsFile
	_ = yaml.Unmarshal(data, &af)
	return af
}

// saveIntegrationApprovals writes the approval file.
func saveIntegrationApprovals(af integrationApprovalsFile) error {
	dir := filepath.Dir(integrationApprovalsFilePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(&af)
	if err != nil {
		return err
	}
	return os.WriteFile(integrationApprovalsFilePath, data, 0o600)
}

// isApproved checks whether the given project + hash combination is approved.
func isApproved(projectDir, fileHash string) bool {
	af := loadIntegrationApprovals()
	for _, rec := range af.Approvals {
		if rec.ProjectDir == projectDir && rec.FileHash == fileHash {
			return true
		}
	}
	return false
}

// recordApproval stores approval for a project + hash. Replaces any existing
// approval for the same project (since hash changed = re-approve).
func recordApproval(projectDir, fileHash string) error {
	af := loadIntegrationApprovals()

	// Remove stale approval for same project.
	filtered := af.Approvals[:0]
	for _, rec := range af.Approvals {
		if rec.ProjectDir != projectDir {
			filtered = append(filtered, rec)
		}
	}
	filtered = append(filtered, integrationApprovalRecord{
		ProjectDir: projectDir,
		FileHash:   fileHash,
	})
	af.Approvals = filtered

	return saveIntegrationApprovals(af)
}

// promptIntegrationApproval asks the user to approve repo-recommended integrations.
// Returns true if approved. Non-interactive sessions (no TTY) return false.
func promptIntegrationApproval(projectDir string, integrationNames []string) bool {
	fmt.Fprintf(os.Stderr, "\nhazmat: this repo recommends integrations: %s\n",
		strings.Join(integrationNames, ", "))
	fmt.Fprintf(os.Stderr, "hazmat: source: %s/%s\n", projectDir, repoRecommendedIntegrationsFile)
	fmt.Fprintf(os.Stderr, "hazmat: approve these integrations for this repo? [y/N] ")

	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		return false
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

// ── Resolution: CLI flags + config pinning + repo recommendations ────────

// resolveActiveIntegrations determines which integrations to load for a session.
// Sources (in priority order):
//  1. --integration CLI flags (always active, no approval needed)
//  2. Config pinning for the project (always active, user configured)
//  3. Repo .hazmat/integrations.yaml (requires host approval)
//
// Returns loaded, validated integration specs.
func resolveActiveIntegrations(integrationFlags []string, projectDir string) ([]IntegrationSpec, error) {
	// Collect integration names from CLI flags.
	names := make(map[string]struct{})
	for _, n := range integrationFlags {
		names[n] = struct{}{}
	}

	// Add pinned integrations from config if not already specified via CLI.
	cfg, _ := loadConfig()
	for _, pin := range cfg.PinnedIntegrations() {
		pinned, err := canonicalizePath(expandTilde(pin.ProjectDir))
		if err != nil {
			continue
		}
		if pinned == projectDir {
			for _, n := range pin.Integrations {
				names[n] = struct{}{}
			}
		}
	}

	// Add repo-recommended integrations if approved by host.
	if recNames, fileHash, err := loadRepoRecommendations(projectDir); err == nil && len(recNames) > 0 {
		if isApproved(projectDir, fileHash) {
			for _, n := range recNames {
				names[n] = struct{}{}
			}
		} else {
			// Not yet approved — prompt regardless of other integration sources.
			// Approval is a one-time cost that establishes the trust record.
			if promptIntegrationApproval(projectDir, recNames) {
				if err := recordApproval(projectDir, fileHash); err != nil {
					fmt.Fprintf(os.Stderr, "hazmat: warning: could not save approval: %v\n", err)
				}
				for _, n := range recNames {
					names[n] = struct{}{}
				}
			} else {
				fmt.Fprintln(os.Stderr, "hazmat: repo integrations declined. Use --integration to activate manually.")
			}
		}
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "hazmat: warning: %v\n", err)
	}

	if len(names) == 0 {
		return nil, nil
	}

	orderedNames := make([]string, 0, len(names))
	for name := range names {
		orderedNames = append(orderedNames, name)
	}
	sort.Strings(orderedNames)

	var integrations []IntegrationSpec
	for _, name := range orderedNames {
		spec, err := loadIntegrationSpecByName(name)
		if err != nil {
			return nil, err
		}
		integrations = append(integrations, spec)
	}
	return integrations, nil
}

// ── Detection / suggestion ─────────────────────────────────────────────────

var integrationDetectIgnoredDirs = map[string]struct{}{
	".beads":       {},
	".git":         {},
	".next":        {},
	".nuxt":        {},
	".terraform":   {},
	".turbo":       {},
	".venv":        {},
	"build":        {},
	"dist":         {},
	"node_modules": {},
	"target":       {},
	"vendor":       {},
	"venv":         {},
}

const integrationDetectMaxDepth = 4

func detectPatternHasWildcard(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

func detectFileMatches(pattern, name string) bool {
	if !detectPatternHasWildcard(pattern) {
		return pattern == name
	}
	matched, err := filepath.Match(pattern, name)
	return err == nil && matched
}

func projectMatchesDetectFile(projectDir, pattern string) bool {
	if pattern == "" {
		return false
	}

	if !detectPatternHasWildcard(pattern) {
		if _, err := os.Stat(filepath.Join(projectDir, pattern)); err == nil {
			return true
		}
	}

	matched := false
	filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error { //nolint:errcheck // best-effort suggestion probe
		if matched || err != nil {
			return nil
		}
		if path == projectDir {
			return nil
		}

		rel, relErr := filepath.Rel(projectDir, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if d.IsDir() {
			if depth > integrationDetectMaxDepth {
				return filepath.SkipDir
			}
			if _, skip := integrationDetectIgnoredDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if depth > integrationDetectMaxDepth {
			return nil
		}
		if detectFileMatches(pattern, d.Name()) {
			matched = true
		}
		return nil
	})
	return matched
}

// suggestIntegrations checks detect.files against the project directory and returns
// names of built-in integrations that match but are not already active.
func suggestIntegrations(projectDir string, activeNames map[string]struct{}) []string {
	var suggestions []string
	for _, name := range allBuiltinIntegrationNames() {
		if _, active := activeNames[name]; active {
			continue
		}
		p, err := loadBuiltinIntegrationSpec(name)
		if err != nil {
			continue
		}
		if len(p.Detect.Files) == 0 {
			continue
		}
		for _, f := range p.Detect.Files {
			if projectMatchesDetectFile(projectDir, f) {
				suggestions = append(suggestions, name)
				break
			}
		}
	}
	return suggestions
}

// ── Session merge ──────────────────────────────────────────────────────────

// integrationMergeResult holds the merged output of all active integrations, ready for
// injection into session setup.
type integrationMergeResult struct {
	ReadDirs       []string          // canonical paths to add as -R
	EnvPassthrough map[string]string // key=value pairs resolved from invoker env
	Excludes       []string          // backup exclude patterns
	Warnings       []string          // messages to show at session start
	RegistryKeys   []string          // active registry-redirect env keys (for UX)
}

// mergeIntegrations validates paths and merges all active integrations into a
// single result.
func mergeIntegrations(integrations []IntegrationSpec) (integrationMergeResult, error) {
	resolved := make([]resolvedIntegration, 0, len(integrations))
	for _, spec := range integrations {
		resolved = append(resolved, resolvedIntegration{Spec: spec})
	}
	return mergeResolvedIntegrations(resolved)
}

func mergeResolvedIntegrations(integrations []resolvedIntegration) (integrationMergeResult, error) {
	var result integrationMergeResult
	result.EnvPassthrough = make(map[string]string)

	readDirSeen := make(map[string]struct{})
	excludeSeen := make(map[string]struct{})
	warnSeen := make(map[string]struct{})
	registrySeen := make(map[string]struct{})

	for _, integration := range integrations {
		if !integration.ReplaceDeclaredReadDirs {
			dirs, err := validateIntegrationPaths(integration.Spec)
			if err != nil {
				return integrationMergeResult{}, err
			}
			for _, d := range dirs {
				if _, dup := readDirSeen[d]; !dup {
					result.ReadDirs = append(result.ReadDirs, d)
					readDirSeen[d] = struct{}{}
				}
			}
		}

		for _, d := range integration.AdditionalReadDirs {
			if _, dup := readDirSeen[d]; !dup {
				result.ReadDirs = append(result.ReadDirs, d)
				readDirSeen[d] = struct{}{}
			}
		}

		// Env passthrough: resolve from invoker's environment.
		for _, key := range integration.Spec.Session.EnvPassthrough {
			if _, set := result.EnvPassthrough[key]; set {
				continue
			}
			if val := os.Getenv(key); val != "" {
				result.EnvPassthrough[key] = val
				if registryEnvKeys[key] && val != "" {
					result.RegistryKeys = append(result.RegistryKeys, key)
					registrySeen[key] = struct{}{}
				}
			}
		}
		for key, value := range integration.ResolvedEnv {
			if value == "" {
				continue
			}
			result.EnvPassthrough[key] = value
			if registryEnvKeys[key] {
				if _, dup := registrySeen[key]; !dup {
					result.RegistryKeys = append(result.RegistryKeys, key)
					registrySeen[key] = struct{}{}
				}
			}
		}

		// Backup excludes.
		for _, pat := range integration.Spec.Backup.Excludes {
			if _, dup := excludeSeen[pat]; !dup {
				result.Excludes = append(result.Excludes, pat)
				excludeSeen[pat] = struct{}{}
			}
		}

		// Warnings.
		for _, w := range integration.Spec.Warnings {
			if _, dup := warnSeen[w]; !dup {
				result.Warnings = append(result.Warnings, w)
				warnSeen[w] = struct{}{}
			}
		}
		for _, w := range integration.AdditionalWarnings {
			if _, dup := warnSeen[w]; !dup {
				result.Warnings = append(result.Warnings, w)
				warnSeen[w] = struct{}{}
			}
		}
	}

	return result, nil
}

// ── CLI command ────────────────────────────────────────────────────────────

func newIntegrationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "integration",
		Short: "List and inspect session integrations",
		Long: `Session integrations configure session ergonomics for technology stacks.

They set read-only paths, backup excludes, and env passthrough for common
development environments. Integrations cannot widen trust boundaries — they may
only reduce friction or tighten defaults.

  hazmat integration list        List available integrations
  hazmat integration show <name> Show integration details`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runIntegrationList()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available session integrations",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runIntegrationList()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show details of a session integration",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runIntegrationShow(args[0])
		},
	})

	return cmd
}

func runIntegrationList() error {
	fmt.Println()
	fmt.Println("  Built-in integrations:")
	fmt.Println()
	for _, name := range allBuiltinIntegrationNames() {
		spec, err := loadBuiltinIntegrationSpec(name)
		if err != nil {
			continue
		}
		desc := spec.Meta.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Printf("    %-20s %s\n", name, desc)
	}

	// List user integrations.
	entries, err := os.ReadDir(userIntegrationDir)
	if err == nil {
		var userIntegrations []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				userIntegrations = append(userIntegrations, strings.TrimSuffix(e.Name(), ".yaml"))
			}
		}
		if len(userIntegrations) > 0 {
			fmt.Println()
			fmt.Printf("  User integrations (%s):\n", userIntegrationDir)
			fmt.Println()
			for _, name := range userIntegrations {
				spec, err := loadUserIntegrationSpec(name)
				if err != nil {
					fmt.Printf("    %-20s (load error: %v)\n", name, err)
					continue
				}
				desc := spec.Meta.Description
				if desc == "" {
					desc = "(no description)"
				}
				fmt.Printf("    %-20s %s\n", name, desc)
			}
		}
	}

	// Show pinned projects.
	cfg, _ := loadConfig()
	if pins := cfg.PinnedIntegrations(); len(pins) > 0 {
		fmt.Println()
		fmt.Println("  Pinned projects:")
		fmt.Println()
		for _, pin := range pins {
			fmt.Printf("    %-30s %s\n", pin.ProjectDir, strings.Join(pin.Integrations, ", "))
		}
	}

	fmt.Println()
	fmt.Println("  Activate: hazmat claude|codex|opencode|shell|exec --integration <name>")
	fmt.Println("  Pin:      hazmat config set integrations.pin \"~/workspace/app:node,go\"")
	fmt.Println()
	return nil
}

func runIntegrationShow(name string) error {
	spec, err := loadIntegrationSpecByName(name)
	if err != nil {
		return err
	}

	projectDir, err := os.Getwd()
	if err != nil {
		projectDir = "."
	}
	resolved := resolvedIntegration{Spec: spec}
	if resolvedSet, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{spec}); err == nil && len(resolvedSet) == 1 {
		resolved = resolvedSet[0]
	}
	merged, err := mergeResolvedIntegrations([]resolvedIntegration{resolved})
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  Integration: %s\n", spec.Meta.Name)
	if spec.Meta.Description != "" {
		fmt.Printf("  %s\n", spec.Meta.Description)
	}
	fmt.Println()

	if len(spec.Detect.Files) > 0 {
		fmt.Printf("  Detect:          %s\n", strings.Join(spec.Detect.Files, ", "))
	}
	if spec, ok := integrationResolverFor(name); ok {
		fmt.Printf("  Resolver:        %s\n", spec.Summary)
	}
	if len(spec.Session.ReadDirs) > 0 {
		fmt.Printf("  Declared read dirs: %s\n", strings.Join(spec.Session.ReadDirs, ", "))
	}
	if len(merged.ReadDirs) > 0 {
		fmt.Printf("  Resolved read dirs: %s\n", strings.Join(merged.ReadDirs, ", "))
	}
	if len(spec.Session.EnvPassthrough) > 0 {
		fmt.Printf("  Env passthrough: %s\n", strings.Join(spec.Session.EnvPassthrough, ", "))
	}
	if len(merged.EnvPassthrough) > 0 {
		var envPairs []string
		for key, value := range merged.EnvPassthrough {
			envPairs = append(envPairs, key+"="+value)
		}
		sort.Strings(envPairs)
		fmt.Printf("  Resolved env:    %s\n", strings.Join(envPairs, ", "))
	}
	if len(spec.Backup.Excludes) > 0 {
		fmt.Printf("  Excludes:        %s\n", strings.Join(spec.Backup.Excludes, ", "))
	}
	if resolved.Source != "" {
		fmt.Printf("  Source:          %s\n", resolved.Source)
	}
	if len(resolved.Details) > 0 {
		fmt.Println()
		fmt.Println("  Resolution:")
		for _, detail := range resolved.Details {
			fmt.Printf("    - %s\n", detail)
		}
	}
	if len(spec.Warnings) > 0 {
		fmt.Println()
		fmt.Println("  Warnings:")
		for _, w := range spec.Warnings {
			fmt.Printf("    - %s\n", w)
		}
	}
	if len(spec.Commands) > 0 {
		fmt.Println()
		fmt.Println("  Commands:")
		for name, cmd := range spec.Commands {
			fmt.Printf("    %-12s %s\n", name, cmd)
		}
	}
	fmt.Println()
	return nil
}

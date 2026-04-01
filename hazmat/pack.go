package main

import (
	"embed"
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
// as the base, but pack read_dirs are resolved against the invoker's home.
// We check both agent home and invoker home as bases, since a pack path
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
// Packs may only request passthrough for these keys. Each must be a passive
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

// ── Pack manifest types ────────────────────────────────────────────────────

type Pack struct {
	PackMeta PackMeta          `yaml:"pack"`
	Detect   PackDetect        `yaml:"detect"`
	Session  PackSession       `yaml:"session"`
	Backup   PackBackup        `yaml:"backup"`
	Warnings []string          `yaml:"warnings"`
	Commands map[string]string `yaml:"commands"`
}

type PackMeta struct {
	Name        string `yaml:"name"`
	Version     int    `yaml:"version"`
	Description string `yaml:"description"`
}

type PackDetect struct {
	Files []string `yaml:"files"`
}

type PackSession struct {
	ReadDirs       []string `yaml:"read_dirs"`
	EnvPassthrough []string `yaml:"env_passthrough"`
}

type PackBackup struct {
	Excludes []string `yaml:"excludes"`
}

// ── Validation ─────────────────────────────────────────────────────────────

const (
	packMaxSize        = 8192 // 8KB manifest limit
	packMaxReadDirs    = 20
	packMaxEnvKeys     = 20
	packMaxExcludes    = 50
	packMaxWarnings    = 10
	packMaxCommands    = 20
	packMaxDetectFiles = 10
)

var packNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validatePackSchema checks structural validity (V1, V3, V4, V5, V6).
// This runs at load time before paths are resolved.
func validatePackSchema(p Pack) error {
	// V1: required fields and types.
	if p.PackMeta.Name == "" {
		return fmt.Errorf("pack: name is required")
	}
	if !packNameRe.MatchString(p.PackMeta.Name) {
		return fmt.Errorf("pack %q: name must match %s", p.PackMeta.Name, packNameRe)
	}
	if p.PackMeta.Version != 1 {
		return fmt.Errorf("pack %q: version must be 1, got %d", p.PackMeta.Name, p.PackMeta.Version)
	}

	// V3: env passthrough keys must be in safe set.
	for _, key := range p.Session.EnvPassthrough {
		if !safeEnvKeys[key] {
			return fmt.Errorf("pack %q: env key %q not in safe passthrough set", p.PackMeta.Name, key)
		}
	}

	// V4: exclude patterns.
	for _, pat := range p.Backup.Excludes {
		if pat == "" {
			return fmt.Errorf("pack %q: empty exclude pattern", p.PackMeta.Name)
		}
		if strings.HasPrefix(pat, "!") {
			return fmt.Errorf("pack %q: negation excludes not supported: %q", p.PackMeta.Name, pat)
		}
	}

	// V5: detect files must be filenames, no path separators.
	for _, f := range p.Detect.Files {
		if strings.Contains(f, "/") {
			return fmt.Errorf("pack %q: detect file %q must be a filename, not a path", p.PackMeta.Name, f)
		}
	}

	// V6: bounds.
	if len(p.Session.ReadDirs) > packMaxReadDirs {
		return fmt.Errorf("pack %q: too many read_dirs (%d, max %d)", p.PackMeta.Name, len(p.Session.ReadDirs), packMaxReadDirs)
	}
	if len(p.Session.EnvPassthrough) > packMaxEnvKeys {
		return fmt.Errorf("pack %q: too many env_passthrough keys (%d, max %d)", p.PackMeta.Name, len(p.Session.EnvPassthrough), packMaxEnvKeys)
	}
	if len(p.Backup.Excludes) > packMaxExcludes {
		return fmt.Errorf("pack %q: too many excludes (%d, max %d)", p.PackMeta.Name, len(p.Backup.Excludes), packMaxExcludes)
	}
	if len(p.Warnings) > packMaxWarnings {
		return fmt.Errorf("pack %q: too many warnings (%d, max %d)", p.PackMeta.Name, len(p.Warnings), packMaxWarnings)
	}
	if len(p.Commands) > packMaxCommands {
		return fmt.Errorf("pack %q: too many commands (%d, max %d)", p.PackMeta.Name, len(p.Commands), packMaxCommands)
	}
	if len(p.Detect.Files) > packMaxDetectFiles {
		return fmt.Errorf("pack %q: too many detect files (%d, max %d)", p.PackMeta.Name, len(p.Detect.Files), packMaxDetectFiles)
	}

	return nil
}

// validatePackPaths checks read_dirs against credential deny zones (V2).
// This runs at session start after tilde expansion and canonicalization.
// Returns the canonical paths for use in session merge.
func validatePackPaths(p Pack) ([]string, error) {
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
			return nil, fmt.Errorf("pack %q: read_dir %q resolves to credential deny zone %q",
				p.PackMeta.Name, dir, resolved)
		}

		canonical = append(canonical, resolved)
	}
	return canonical, nil
}

// ── Loading ────────────────────────────────────────────────────────────────

//go:embed packs/*.yaml
var builtinPacksFS embed.FS

var userPackDir = filepath.Join(os.Getenv("HOME"), ".hazmat/packs")

// loadPack parses and schema-validates a single pack manifest from YAML bytes.
func loadPack(data []byte) (Pack, error) {
	if len(data) > packMaxSize {
		return Pack{}, fmt.Errorf("pack manifest exceeds %d byte limit", packMaxSize)
	}

	var p Pack
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Pack{}, fmt.Errorf("parse pack: %w", err)
	}

	if err := validatePackSchema(p); err != nil {
		return Pack{}, err
	}

	return p, nil
}

// loadBuiltinPack loads a built-in pack by name from the embedded filesystem.
func loadBuiltinPack(name string) (Pack, error) {
	data, err := builtinPacksFS.ReadFile("packs/" + name + ".yaml")
	if err != nil {
		return Pack{}, fmt.Errorf("built-in pack %q not found", name)
	}
	return loadPack(data)
}

// loadUserPack loads a user-installed pack from ~/.hazmat/packs/<name>.yaml.
func loadUserPack(name string) (Pack, error) {
	path := filepath.Join(userPackDir, name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Pack{}, fmt.Errorf("user pack %q: %w", name, err)
	}
	return loadPack(data)
}

// loadPackByName tries built-in first, then user-installed.
func loadPackByName(name string) (Pack, error) {
	if p, err := loadBuiltinPack(name); err == nil {
		return p, nil
	}
	return loadUserPack(name)
}

// allBuiltinPackNames returns the names of all embedded packs.
func allBuiltinPackNames() []string {
	entries, err := builtinPacksFS.ReadDir("packs")
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

// ── Resolution: CLI flags + config pinning ─────────────────────────────────

// resolveActivePacks determines which packs to load for a session.
// Sources (in order): --pack CLI flags, then config pinning for the project.
// Returns loaded, validated packs.
func resolveActivePacks(packFlags []string, projectDir string) ([]Pack, error) {
	// Collect pack names from CLI flags.
	names := make(map[string]struct{})
	for _, n := range packFlags {
		names[n] = struct{}{}
	}

	// Add pinned packs from config if not already specified via CLI.
	cfg, _ := loadConfig()
	for _, pin := range cfg.PackPins() {
		pinned, err := canonicalizePath(expandTilde(pin.ProjectDir))
		if err != nil {
			continue
		}
		if pinned == projectDir {
			for _, n := range pin.Packs {
				names[n] = struct{}{}
			}
		}
	}

	if len(names) == 0 {
		return nil, nil
	}

	orderedNames := make([]string, 0, len(names))
	for name := range names {
		orderedNames = append(orderedNames, name)
	}
	sort.Strings(orderedNames)

	var packs []Pack
	for _, name := range orderedNames {
		p, err := loadPackByName(name)
		if err != nil {
			return nil, err
		}
		packs = append(packs, p)
	}
	return packs, nil
}

// ── Detection / suggestion ─────────────────────────────────────────────────

// suggestPacks checks detect.files against the project directory and returns
// names of built-in packs that match but are not already active.
func suggestPacks(projectDir string, activeNames map[string]struct{}) []string {
	var suggestions []string
	for _, name := range allBuiltinPackNames() {
		if _, active := activeNames[name]; active {
			continue
		}
		p, err := loadBuiltinPack(name)
		if err != nil {
			continue
		}
		if len(p.Detect.Files) == 0 {
			continue
		}
		for _, f := range p.Detect.Files {
			if _, err := os.Stat(filepath.Join(projectDir, f)); err == nil {
				suggestions = append(suggestions, name)
				break
			}
		}
	}
	return suggestions
}

// ── Session merge ──────────────────────────────────────────────────────────

// packMergeResult holds the merged output of all active packs, ready for
// injection into session setup.
type packMergeResult struct {
	ReadDirs       []string          // canonical paths to add as -R
	EnvPassthrough map[string]string // key=value pairs resolved from invoker env
	Excludes       []string          // backup exclude patterns
	Warnings       []string          // messages to show at session start
	RegistryKeys   []string          // active registry-redirect env keys (for UX)
}

// mergePacks validates paths and merges all active packs into a single result.
func mergePacks(packs []Pack) (packMergeResult, error) {
	var result packMergeResult
	result.EnvPassthrough = make(map[string]string)

	readDirSeen := make(map[string]struct{})
	excludeSeen := make(map[string]struct{})
	warnSeen := make(map[string]struct{})

	for _, p := range packs {
		// V2: validate and canonicalize paths.
		dirs, err := validatePackPaths(p)
		if err != nil {
			return packMergeResult{}, err
		}
		for _, d := range dirs {
			if _, dup := readDirSeen[d]; !dup {
				result.ReadDirs = append(result.ReadDirs, d)
				readDirSeen[d] = struct{}{}
			}
		}

		// Env passthrough: resolve from invoker's environment.
		for _, key := range p.Session.EnvPassthrough {
			if _, set := result.EnvPassthrough[key]; set {
				continue
			}
			if val := os.Getenv(key); val != "" {
				result.EnvPassthrough[key] = val
				if registryEnvKeys[key] {
					result.RegistryKeys = append(result.RegistryKeys, key)
				}
			}
		}

		// Backup excludes.
		for _, pat := range p.Backup.Excludes {
			if _, dup := excludeSeen[pat]; !dup {
				result.Excludes = append(result.Excludes, pat)
				excludeSeen[pat] = struct{}{}
			}
		}

		// Warnings.
		for _, w := range p.Warnings {
			if _, dup := warnSeen[w]; !dup {
				result.Warnings = append(result.Warnings, w)
				warnSeen[w] = struct{}{}
			}
		}
	}

	return result, nil
}

// ── CLI command ────────────────────────────────────────────────────────────

func newPackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "List and inspect stack packs",
		Long: `Stack packs configure session ergonomics for technology stacks.

They set read-only paths, backup excludes, and env passthrough for common
development environments. Packs cannot widen trust boundaries — they may
only reduce friction or tighten defaults.

  hazmat pack list          List available packs
  hazmat pack show <name>   Show pack details`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runPackList()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available stack packs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runPackList()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show details of a stack pack",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runPackShow(args[0])
		},
	})

	return cmd
}

func runPackList() error {
	fmt.Println()
	fmt.Println("  Built-in packs:")
	fmt.Println()
	for _, name := range allBuiltinPackNames() {
		p, err := loadBuiltinPack(name)
		if err != nil {
			continue
		}
		desc := p.PackMeta.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Printf("    %-20s %s\n", name, desc)
	}

	// List user packs.
	entries, err := os.ReadDir(userPackDir)
	if err == nil {
		var userPacks []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				userPacks = append(userPacks, strings.TrimSuffix(e.Name(), ".yaml"))
			}
		}
		if len(userPacks) > 0 {
			fmt.Println()
			fmt.Println("  User packs (~/.hazmat/packs/):")
			fmt.Println()
			for _, name := range userPacks {
				p, err := loadUserPack(name)
				if err != nil {
					fmt.Printf("    %-20s (load error: %v)\n", name, err)
					continue
				}
				desc := p.PackMeta.Description
				if desc == "" {
					desc = "(no description)"
				}
				fmt.Printf("    %-20s %s\n", name, desc)
			}
		}
	}

	// Show pinned projects.
	cfg, _ := loadConfig()
	if pins := cfg.PackPins(); len(pins) > 0 {
		fmt.Println()
		fmt.Println("  Pinned projects:")
		fmt.Println()
		for _, pin := range pins {
			fmt.Printf("    %-30s %s\n", pin.ProjectDir, strings.Join(pin.Packs, ", "))
		}
	}

	fmt.Println()
	fmt.Println("  Activate: hazmat claude --pack <name>")
	fmt.Println("  Pin:      hazmat config set packs.pin \"~/workspace/app:node,go\"")
	fmt.Println()
	return nil
}

func runPackShow(name string) error {
	p, err := loadPackByName(name)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  Pack: %s\n", p.PackMeta.Name)
	if p.PackMeta.Description != "" {
		fmt.Printf("  %s\n", p.PackMeta.Description)
	}
	fmt.Println()

	if len(p.Detect.Files) > 0 {
		fmt.Printf("  Detect:          %s\n", strings.Join(p.Detect.Files, ", "))
	}
	if len(p.Session.ReadDirs) > 0 {
		fmt.Printf("  Read dirs:       %s\n", strings.Join(p.Session.ReadDirs, ", "))
	}
	if len(p.Session.EnvPassthrough) > 0 {
		fmt.Printf("  Env passthrough: %s\n", strings.Join(p.Session.EnvPassthrough, ", "))
	}
	if len(p.Backup.Excludes) > 0 {
		fmt.Printf("  Excludes:        %s\n", strings.Join(p.Backup.Excludes, ", "))
	}
	if len(p.Warnings) > 0 {
		fmt.Println()
		fmt.Println("  Warnings:")
		for _, w := range p.Warnings {
			fmt.Printf("    - %s\n", w)
		}
	}
	if len(p.Commands) > 0 {
		fmt.Println()
		fmt.Println("  Commands:")
		for name, cmd := range p.Commands {
			fmt.Printf("    %-12s %s\n", name, cmd)
		}
	}
	fmt.Println()
	return nil
}

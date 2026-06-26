package containment

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// AgentHomeStateKind describes the filesystem shape of a persistent
// agent-home entry.
type AgentHomeStateKind string

const (
	AgentHomeStateDir  AgentHomeStateKind = "dir"
	AgentHomeStateFile AgentHomeStateKind = "file"
)

// AgentHomeStateClass describes why a persistent agent-home path exists.
type AgentHomeStateClass string

const (
	AgentHomeStateShellConfig    AgentHomeStateClass = "shell-config"
	AgentHomeStateGitConfig      AgentHomeStateClass = "git-config"
	AgentHomeStateHarnessState   AgentHomeStateClass = "harness-state"
	AgentHomeStateTranscript     AgentHomeStateClass = "transcript"
	AgentHomeStateXDGCache       AgentHomeStateClass = "xdg-cache"
	AgentHomeStateXDGConfig      AgentHomeStateClass = "xdg-config"
	AgentHomeStateXDGData        AgentHomeStateClass = "xdg-data"
	AgentHomeStateToolchainState AgentHomeStateClass = "toolchain-state"
	AgentHomeStateExecutable     AgentHomeStateClass = "executable-tooling"
)

// AgentHomeStateEntry classifies one durable path below the persistent agent
// home. RelPath and CoveredRelPaths are always relative to the agent home.
type AgentHomeStateEntry struct {
	RelPath            string
	Kind               AgentHomeStateKind
	Class              AgentHomeStateClass
	CoveredPaths       []AgentHomeCoveredPath
	ExecutableRelPaths []string
}

// AgentHomeCoveredPath classifies an important durable child path beneath a
// broader manifest root.
type AgentHomeCoveredPath struct {
	RelPath string
	Class   AgentHomeStateClass
}

// PersistentAgentHomeManifest returns the durable agent-home paths Hazmat
// intentionally supports while HOME remains /Users/agent. The same manifest is
// the starting audit inventory for the future session-local HOME assembly.
func PersistentAgentHomeManifest() []AgentHomeStateEntry {
	return copyAgentHomeStateEntries(persistentAgentHomeManifest)
}

// AgentHomeWritableSubpaths projects the manifest to directory grants for
// native policy compilers.
func AgentHomeWritableSubpaths(home string) []string {
	return agentHomeManifestPaths(home, AgentHomeStateDir)
}

// AgentHomeWritableFiles projects the manifest to file grants for native
// policy compilers.
func AgentHomeWritableFiles(home string) []string {
	return agentHomeManifestPaths(home, AgentHomeStateFile)
}

// AgentHomeExecutableSubpaths projects executable durable tooling paths for
// native policy compilers.
func AgentHomeExecutableSubpaths(home string) []string {
	home = filepath.Clean(home)
	var out []string
	for _, entry := range persistentAgentHomeManifest {
		for _, rel := range entry.ExecutableRelPaths {
			out = append(out, filepath.Join(home, rel))
		}
	}
	sort.Strings(out)
	return out
}

// ValidatePersistentAgentHomeManifest verifies that the durable-home inventory
// is suitable for policy projection and future home assembly.
func ValidatePersistentAgentHomeManifest() error {
	seen := map[string]bool{}
	for _, entry := range persistentAgentHomeManifest {
		if err := validateAgentHomeRelPath(entry.RelPath); err != nil {
			return fmt.Errorf("manifest entry: %w", err)
		}
		if entry.Kind != AgentHomeStateDir && entry.Kind != AgentHomeStateFile {
			return fmt.Errorf("%s: unsupported kind %q", entry.RelPath, entry.Kind)
		}
		if entry.Class == "" {
			return fmt.Errorf("%s: class is required", entry.RelPath)
		}
		if seen[entry.RelPath] {
			return fmt.Errorf("%s: duplicate manifest entry", entry.RelPath)
		}
		seen[entry.RelPath] = true
		for _, covered := range entry.CoveredPaths {
			if err := validateAgentHomeRelPath(covered.RelPath); err != nil {
				return fmt.Errorf("%s covers invalid path: %w", entry.RelPath, err)
			}
			if covered.Class == "" {
				return fmt.Errorf("%s covers %s without a class", entry.RelPath, covered.RelPath)
			}
			if !agentHomeRelWithin(entry.RelPath, covered.RelPath) {
				return fmt.Errorf("%s covers %s outside its durable root", entry.RelPath, covered.RelPath)
			}
		}
		for _, rel := range entry.ExecutableRelPaths {
			if err := validateAgentHomeRelPath(rel); err != nil {
				return fmt.Errorf("%s covers invalid path: %w", entry.RelPath, err)
			}
			if !agentHomeRelWithin(entry.RelPath, rel) {
				return fmt.Errorf("%s covers %s outside its durable root", entry.RelPath, rel)
			}
		}
	}
	return nil
}

// PersistentAgentHomeManifestCovers reports whether relPath is explicitly
// classified by the manifest, either as a top-level entry or as a covered
// subpath under a broader durable root.
func PersistentAgentHomeManifestCovers(relPath string) bool {
	_, ok := PersistentAgentHomePathClass(relPath)
	return ok
}

// PersistentAgentHomePathClass returns the most specific manifest class for a
// relative agent-home path.
func PersistentAgentHomePathClass(relPath string) (AgentHomeStateClass, bool) {
	clean, err := cleanAgentHomeRelPath(relPath)
	if err != nil {
		return "", false
	}
	var best AgentHomeStateClass
	bestLen := -1
	for _, entry := range persistentAgentHomeManifest {
		if agentHomeRelWithin(entry.RelPath, clean) {
			if len(entry.RelPath) > bestLen {
				best = entry.Class
				bestLen = len(entry.RelPath)
			}
		}
		for _, covered := range entry.CoveredPaths {
			if agentHomeRelWithin(covered.RelPath, clean) {
				if len(covered.RelPath) > bestLen {
					best = covered.Class
					bestLen = len(covered.RelPath)
				}
			}
		}
	}
	return best, bestLen >= 0
}

var persistentAgentHomeManifest = []AgentHomeStateEntry{
	{RelPath: ".agents", Kind: AgentHomeStateDir, Class: AgentHomeStateHarnessState},
	{RelPath: ".bun", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState, ExecutableRelPaths: []string{".bun/bin"}},
	{RelPath: ".cache", Kind: AgentHomeStateDir, Class: AgentHomeStateXDGCache},
	{RelPath: ".cargo", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState, ExecutableRelPaths: []string{".cargo/bin"}},
	{RelPath: ".claude", Kind: AgentHomeStateDir, Class: AgentHomeStateHarnessState, CoveredPaths: []AgentHomeCoveredPath{
		{RelPath: ".claude/commands", Class: AgentHomeStateHarnessState},
		{RelPath: ".claude/skills", Class: AgentHomeStateHarnessState},
		{RelPath: ".claude/projects", Class: AgentHomeStateTranscript},
	}, ExecutableRelPaths: []string{".claude/hooks"}},
	{RelPath: ".claude.json", Kind: AgentHomeStateFile, Class: AgentHomeStateHarnessState},
	{RelPath: ".codex", Kind: AgentHomeStateDir, Class: AgentHomeStateHarnessState, ExecutableRelPaths: []string{".codex/packages"}},
	{RelPath: ".config", Kind: AgentHomeStateDir, Class: AgentHomeStateXDGConfig, CoveredPaths: []AgentHomeCoveredPath{
		{RelPath: ".config/git", Class: AgentHomeStateGitConfig},
		{RelPath: ".config/opencode", Class: AgentHomeStateHarnessState},
		{RelPath: ".config/mcp", Class: AgentHomeStateHarnessState},
	}},
	{RelPath: ".cursor", Kind: AgentHomeStateDir, Class: AgentHomeStateHarnessState},
	{RelPath: ".deno", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState, ExecutableRelPaths: []string{".deno/bin"}},
	{RelPath: ".gem", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState, ExecutableRelPaths: []string{".gem"}},
	// Antigravity (agy) stores its config under ~/.gemini/antigravity-cli, so
	// the ~/.gemini grant covers the Antigravity harness state.
	{RelPath: ".gemini", Kind: AgentHomeStateDir, Class: AgentHomeStateHarnessState},
	{RelPath: ".gradle", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState},
	{RelPath: ".hazmat", Kind: AgentHomeStateDir, Class: AgentHomeStateHarnessState, CoveredPaths: []AgentHomeCoveredPath{
		{RelPath: ".hazmat/hermes", Class: AgentHomeStateHarnessState},
		{RelPath: ".hazmat/hermes/projects", Class: AgentHomeStateTranscript},
	}},
	{RelPath: ".ivy2", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState},
	{RelPath: ".local", Kind: AgentHomeStateDir, Class: AgentHomeStateXDGData, CoveredPaths: []AgentHomeCoveredPath{
		{RelPath: ".local/bin", Class: AgentHomeStateExecutable},
		{RelPath: ".local/lib", Class: AgentHomeStateToolchainState},
		{RelPath: ".local/share", Class: AgentHomeStateXDGData},
	}, ExecutableRelPaths: []string{".local/bin", ".local/lib", ".local/share/claude/versions"}},
	{RelPath: ".m2", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState},
	{RelPath: ".node-gyp", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState},
	{RelPath: ".npm", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState},
	{RelPath: ".opencode", Kind: AgentHomeStateDir, Class: AgentHomeStateHarnessState, ExecutableRelPaths: []string{".opencode/bin"}},
	{RelPath: ".pi", Kind: AgentHomeStateDir, Class: AgentHomeStateHarnessState},
	{RelPath: ".pub-cache", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState, ExecutableRelPaths: []string{".pub-cache/bin"}},
	{RelPath: ".qwen", Kind: AgentHomeStateDir, Class: AgentHomeStateHarnessState},
	{RelPath: ".rustup", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState},
	{RelPath: ".sbt", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState},
	{RelPath: ".swiftpm", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState},
	{RelPath: ".terraform.d", Kind: AgentHomeStateDir, Class: AgentHomeStateToolchainState},
	{RelPath: ".bash_profile", Kind: AgentHomeStateFile, Class: AgentHomeStateShellConfig},
	{RelPath: ".bashrc", Kind: AgentHomeStateFile, Class: AgentHomeStateShellConfig},
	{RelPath: ".gitconfig", Kind: AgentHomeStateFile, Class: AgentHomeStateGitConfig},
	{RelPath: ".npmrc", Kind: AgentHomeStateFile, Class: AgentHomeStateToolchainState},
	{RelPath: ".profile", Kind: AgentHomeStateFile, Class: AgentHomeStateShellConfig},
	{RelPath: ".pypirc", Kind: AgentHomeStateFile, Class: AgentHomeStateToolchainState},
	{RelPath: ".zprofile", Kind: AgentHomeStateFile, Class: AgentHomeStateShellConfig},
	{RelPath: ".zshenv", Kind: AgentHomeStateFile, Class: AgentHomeStateShellConfig},
	{RelPath: ".zshrc", Kind: AgentHomeStateFile, Class: AgentHomeStateShellConfig},
}

func agentHomeManifestPaths(home string, kind AgentHomeStateKind) []string {
	home = filepath.Clean(home)
	var out []string
	for _, entry := range persistentAgentHomeManifest {
		if entry.Kind == kind {
			out = append(out, filepath.Join(home, entry.RelPath))
		}
	}
	sort.Strings(out)
	return out
}

func copyAgentHomeStateEntries(entries []AgentHomeStateEntry) []AgentHomeStateEntry {
	out := make([]AgentHomeStateEntry, len(entries))
	for i, entry := range entries {
		out[i] = entry
		out[i].CoveredPaths = copyAgentHomeCoveredPaths(entry.CoveredPaths)
		out[i].ExecutableRelPaths = copyAgentHomeStrings(entry.ExecutableRelPaths)
	}
	return out
}

func copyAgentHomeCoveredPaths(values []AgentHomeCoveredPath) []AgentHomeCoveredPath {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentHomeCoveredPath, len(values))
	copy(out, values)
	return out
}

func copyAgentHomeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func validateAgentHomeRelPath(path string) error {
	_, err := cleanAgentHomeRelPath(path)
	return err
}

func cleanAgentHomeRelPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("relative path is required")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("%q must be relative", path)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q must stay below agent home", path)
	}
	return clean, nil
}

func agentHomeRelWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

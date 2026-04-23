package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	projectHooksDirRel          = ".hazmat/hooks"
	projectHooksManifestRelPath = ".hazmat/hooks/hooks.yaml"
	projectHooksManifestVersion = 1
	projectHooksManifestMaxSize = 16 * 1024
)

type hookType string

const (
	hookTypePreCommit hookType = "pre-commit"
	hookTypePrePush   hookType = "pre-push"
	hookTypeCommitMsg hookType = "commit-msg"
)

type hookReviewKind string

const (
	hookReviewInstall hookReviewKind = "install"
	hookReviewDrift   hookReviewKind = "drift"
)

type projectHooksManifest struct {
	Version int                `yaml:"version"`
	Hooks   []projectHookEntry `yaml:"hooks"`
}

type projectHookEntry struct {
	Type        hookType `yaml:"type"`
	Script      string   `yaml:"script"`
	Purpose     string   `yaml:"purpose"`
	Interpreter string   `yaml:"interpreter"`
	Requires    []string `yaml:"requires"`
}

type loadedProjectHookBundle struct {
	ProjectDir   string
	HooksDir     string
	ManifestPath string
	Manifest     projectHooksManifest
	Hooks        []loadedProjectHook
	BundleHash   string
}

type loadedProjectHook struct {
	Type        hookType
	ScriptPath  string
	ScriptAbs   string
	Purpose     string
	Interpreter string
	Requires    []string
	ScriptHash  string
}

type projectHookReviewSummary struct {
	Kind       hookReviewKind
	BundleHash string
	Hooks      []projectHookSummaryEntry
}

type projectHookSummaryEntry struct {
	Type        hookType
	ScriptPath  string
	Purpose     string
	Interpreter string
	Requires    []string
}

var (
	validProjectHookTypes = map[hookType]struct{}{
		hookTypePreCommit: {},
		hookTypePrePush:   {},
		hookTypeCommitMsg: {},
	}
	projectHookCommandNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
)

func loadProjectHookBundle(projectDir string) (*loadedProjectHookBundle, error) {
	path := filepath.Join(projectDir, projectHooksManifestRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", projectHooksManifestRelPath, err)
	}

	manifest, err := loadProjectHooksManifest(data)
	if err != nil {
		return nil, err
	}

	hooksDir := filepath.Join(projectDir, projectHooksDirRel)
	loadedHooks := make([]loadedProjectHook, 0, len(manifest.Hooks))
	for _, hook := range manifest.Hooks {
		scriptPath, scriptAbs, raw, err := resolveProjectHookScript(hooksDir, hook.Script)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", hook.Type, hook.Script, err)
		}

		sum := sha256.Sum256(raw)
		loadedHooks = append(loadedHooks, loadedProjectHook{
			Type:        hook.Type,
			ScriptPath:  scriptPath,
			ScriptAbs:   scriptAbs,
			Purpose:     hook.Purpose,
			Interpreter: hook.Interpreter,
			Requires:    append([]string(nil), hook.Requires...),
			ScriptHash:  "sha256:" + hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(loadedHooks, func(i, j int) bool {
		return string(loadedHooks[i].Type) < string(loadedHooks[j].Type)
	})

	return &loadedProjectHookBundle{
		ProjectDir:   projectDir,
		HooksDir:     hooksDir,
		ManifestPath: path,
		Manifest:     manifest,
		Hooks:        loadedHooks,
		BundleHash:   hashProjectHookBundle(manifest, loadedHooks),
	}, nil
}

func loadProjectHooksManifest(data []byte) (projectHooksManifest, error) {
	if len(data) > projectHooksManifestMaxSize {
		return projectHooksManifest{}, fmt.Errorf("%s exceeds %d byte limit", projectHooksManifestRelPath, projectHooksManifestMaxSize)
	}

	var manifest projectHooksManifest
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&manifest); err != nil {
		return projectHooksManifest{}, fmt.Errorf("parse %s: %w", projectHooksManifestRelPath, err)
	}

	if err := validateProjectHooksManifest(manifest); err != nil {
		return projectHooksManifest{}, err
	}
	return manifest, nil
}

func validateProjectHooksManifest(manifest projectHooksManifest) error {
	if manifest.Version != projectHooksManifestVersion {
		return fmt.Errorf("%s: version = %d, want %d", projectHooksManifestRelPath, manifest.Version, projectHooksManifestVersion)
	}
	if len(manifest.Hooks) == 0 {
		return fmt.Errorf("%s: hooks must not be empty", projectHooksManifestRelPath)
	}

	seenTypes := make(map[hookType]struct{}, len(manifest.Hooks))
	for i := range manifest.Hooks {
		entry := &manifest.Hooks[i]
		if _, ok := validProjectHookTypes[entry.Type]; !ok {
			return fmt.Errorf("%s: unsupported hook type %q", projectHooksManifestRelPath, entry.Type)
		}
		if _, dup := seenTypes[entry.Type]; dup {
			return fmt.Errorf("%s: duplicate hook type %q", projectHooksManifestRelPath, entry.Type)
		}
		seenTypes[entry.Type] = struct{}{}

		entry.Script = filepath.ToSlash(filepath.Clean(strings.TrimSpace(entry.Script)))
		if entry.Script == "." || entry.Script == "" {
			return fmt.Errorf("%s %q: script is required", projectHooksManifestRelPath, entry.Type)
		}
		if filepath.IsAbs(entry.Script) {
			return fmt.Errorf("%s %q: script must be relative to %s", projectHooksManifestRelPath, entry.Type, projectHooksDirRel)
		}
		if entry.Script == ".." || strings.HasPrefix(entry.Script, "../") {
			return fmt.Errorf("%s %q: script %q escapes %s", projectHooksManifestRelPath, entry.Type, entry.Script, projectHooksDirRel)
		}

		entry.Purpose = strings.TrimSpace(entry.Purpose)
		if entry.Purpose == "" {
			return fmt.Errorf("%s %q: purpose is required", projectHooksManifestRelPath, entry.Type)
		}

		entry.Interpreter = strings.TrimSpace(entry.Interpreter)
		if !projectHookCommandNameRe.MatchString(entry.Interpreter) {
			return fmt.Errorf("%s %q: interpreter %q must be a bare command name", projectHooksManifestRelPath, entry.Type, entry.Interpreter)
		}

		normalizedRequires, err := normalizeProjectHookCommands(entry.Requires)
		if err != nil {
			return fmt.Errorf("%s %q: %w", projectHooksManifestRelPath, entry.Type, err)
		}
		entry.Requires = normalizedRequires
	}

	return nil
}

func normalizeProjectHookCommands(commands []string) ([]string, error) {
	if len(commands) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(commands))
	normalized := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		if !projectHookCommandNameRe.MatchString(command) {
			return nil, fmt.Errorf("required binary %q must be a bare command name", command)
		}
		if _, ok := seen[command]; ok {
			continue
		}
		seen[command] = struct{}{}
		normalized = append(normalized, command)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func resolveProjectHookScript(hooksDir, script string) (string, string, []byte, error) {
	if script == "" {
		return "", "", nil, fmt.Errorf("script is required")
	}
	scriptPath := filepath.FromSlash(script)
	scriptAbs := filepath.Join(hooksDir, scriptPath)
	if !isWithinDir(hooksDir, scriptAbs) {
		return "", "", nil, fmt.Errorf("script %q escapes %s", script, projectHooksDirRel)
	}
	canonicalHooksDir, err := canonicalizePath(hooksDir)
	if err != nil {
		return "", "", nil, err
	}
	canonicalParentDir, err := canonicalizePath(filepath.Dir(scriptAbs))
	if err != nil {
		return "", "", nil, err
	}
	if !isWithinDir(canonicalHooksDir, canonicalParentDir) {
		return "", "", nil, fmt.Errorf("script %q escapes %s via symlinked parent", script, projectHooksDirRel)
	}

	info, err := os.Lstat(scriptAbs)
	if err != nil {
		return "", "", nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", nil, fmt.Errorf("top-level symlinks are not supported at %s", script)
	}
	if !info.Mode().IsRegular() {
		return "", "", nil, fmt.Errorf("script %q must be a regular file", script)
	}

	raw, err := os.ReadFile(scriptAbs)
	if err != nil {
		return "", "", nil, err
	}
	return filepath.ToSlash(scriptPath), scriptAbs, raw, nil
}

func hashProjectHookBundle(manifest projectHooksManifest, hooks []loadedProjectHook) string {
	var payload bytes.Buffer
	fmt.Fprintf(&payload, "version\x00%d\x00", manifest.Version)
	for _, hook := range hooks {
		payload.WriteString("type\x00")
		payload.WriteString(string(hook.Type))
		payload.WriteByte(0)
		payload.WriteString("script\x00")
		payload.WriteString(hook.ScriptPath)
		payload.WriteByte(0)
		payload.WriteString("purpose\x00")
		payload.WriteString(hook.Purpose)
		payload.WriteByte(0)
		payload.WriteString("interpreter\x00")
		payload.WriteString(hook.Interpreter)
		payload.WriteByte(0)
		payload.WriteString("requires\x00")
		for _, command := range hook.Requires {
			payload.WriteString(command)
			payload.WriteByte(0)
		}
		payload.WriteString("script-hash\x00")
		payload.WriteString(hook.ScriptHash)
		payload.WriteByte(0)
	}

	sum := sha256.Sum256(payload.Bytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

func summarizeProjectHookBundle(bundle *loadedProjectHookBundle, approvedHash string) projectHookReviewSummary {
	kind := hookReviewInstall
	if approvedHash != "" {
		kind = hookReviewDrift
	}

	summary := projectHookReviewSummary{
		Kind:       kind,
		BundleHash: bundle.BundleHash,
		Hooks:      make([]projectHookSummaryEntry, 0, len(bundle.Hooks)),
	}
	for _, hook := range bundle.Hooks {
		summary.Hooks = append(summary.Hooks, projectHookSummaryEntry{
			Type:        hook.Type,
			ScriptPath:  hook.ScriptPath,
			Purpose:     hook.Purpose,
			Interpreter: hook.Interpreter,
			Requires:    append([]string(nil), hook.Requires...),
		})
	}
	return summary
}

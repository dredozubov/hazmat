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
	Files   []string           `yaml:"files,omitempty"`
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
	ManifestData []byte
	Manifest     projectHooksManifest
	Files        []loadedProjectHookFile
	Hooks        []loadedProjectHook
	BundleHash   string
}

type loadedProjectHookFile struct {
	Path string
	Abs  string
	Data []byte
	Hash string
}

type loadedProjectHook struct {
	Type        hookType
	ScriptPath  string
	ScriptAbs   string
	ScriptData  []byte
	Purpose     string
	Interpreter string
	Requires    []string
	ScriptHash  string
}

type projectHookReviewSummary struct {
	Kind       hookReviewKind            `yaml:"kind"`
	BundleHash string                    `yaml:"bundle_hash"`
	Hooks      []projectHookSummaryEntry `yaml:"hooks"`
}

type projectHookSummaryEntry struct {
	Type        hookType `yaml:"type"`
	ScriptPath  string   `yaml:"script"`
	Purpose     string   `yaml:"purpose"`
	Interpreter string   `yaml:"interpreter"`
	Requires    []string `yaml:"requires,omitempty"`
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
	return loadProjectHookBundleFromPaths(projectDir, filepath.Join(projectDir, projectHooksDirRel), filepath.Join(projectDir, projectHooksManifestRelPath))
}

func loadProjectHookSnapshot(snapshotDir string) (*loadedProjectHookBundle, error) {
	return loadProjectHookBundleFromPaths(snapshotDir, snapshotDir, filepath.Join(snapshotDir, "hooks.yaml"))
}

func loadProjectHookBundleFromPaths(projectDir, hooksDir, manifestPath string) (*loadedProjectHookBundle, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}

	manifest, err := loadProjectHooksManifest(data)
	if err != nil {
		return nil, err
	}

	loadedHooks := make([]loadedProjectHook, 0, len(manifest.Hooks))
	for _, hook := range manifest.Hooks {
		scriptPath, scriptAbs, raw, err := resolveProjectHookBundleFile(hooksDir, hook.Script, "script")
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", hook.Type, hook.Script, err)
		}

		sum := sha256.Sum256(raw)
		loadedHooks = append(loadedHooks, loadedProjectHook{
			Type:        hook.Type,
			ScriptPath:  scriptPath,
			ScriptAbs:   scriptAbs,
			ScriptData:  append([]byte(nil), raw...),
			Purpose:     hook.Purpose,
			Interpreter: hook.Interpreter,
			Requires:    append([]string(nil), hook.Requires...),
			ScriptHash:  "sha256:" + hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(loadedHooks, func(i, j int) bool {
		return string(loadedHooks[i].Type) < string(loadedHooks[j].Type)
	})

	loadedFiles := make([]loadedProjectHookFile, 0, len(manifest.Files))
	for _, bundleFile := range manifest.Files {
		path, abs, raw, err := resolveProjectHookBundleFile(hooksDir, bundleFile, "file")
		if err != nil {
			return nil, fmt.Errorf("bundle file %q: %w", bundleFile, err)
		}

		sum := sha256.Sum256(raw)
		loadedFiles = append(loadedFiles, loadedProjectHookFile{
			Path: path,
			Abs:  abs,
			Data: append([]byte(nil), raw...),
			Hash: "sha256:" + hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(loadedFiles, func(i, j int) bool {
		return loadedFiles[i].Path < loadedFiles[j].Path
	})

	return &loadedProjectHookBundle{
		ProjectDir:   projectDir,
		HooksDir:     hooksDir,
		ManifestPath: manifestPath,
		ManifestData: append([]byte(nil), data...),
		Manifest:     manifest,
		Files:        loadedFiles,
		Hooks:        loadedHooks,
		BundleHash:   hashProjectHookBundle(manifest, loadedFiles, loadedHooks),
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

	if err := validateProjectHooksManifest(&manifest); err != nil {
		return projectHooksManifest{}, err
	}
	return manifest, nil
}

func validateProjectHooksManifest(manifest *projectHooksManifest) error {
	if manifest.Version != projectHooksManifestVersion {
		return fmt.Errorf("%s: version = %d, want %d", projectHooksManifestRelPath, manifest.Version, projectHooksManifestVersion)
	}
	if len(manifest.Hooks) == 0 {
		return fmt.Errorf("%s: hooks must not be empty", projectHooksManifestRelPath)
	}

	seenTypes := make(map[hookType]struct{}, len(manifest.Hooks))
	seenPaths := make(map[string]string, len(manifest.Hooks)+len(manifest.Files))
	for i := range manifest.Hooks {
		entry := &manifest.Hooks[i]
		if _, ok := validProjectHookTypes[entry.Type]; !ok {
			return fmt.Errorf("%s: unsupported hook type %q", projectHooksManifestRelPath, entry.Type)
		}
		if _, dup := seenTypes[entry.Type]; dup {
			return fmt.Errorf("%s: duplicate hook type %q", projectHooksManifestRelPath, entry.Type)
		}
		seenTypes[entry.Type] = struct{}{}

		scriptPath, err := normalizeProjectHookRelativePath(entry.Script)
		if err != nil {
			return fmt.Errorf("%s %q: %w", projectHooksManifestRelPath, entry.Type, err)
		}
		entry.Script = scriptPath
		if previous, dup := seenPaths[entry.Script]; dup {
			return fmt.Errorf("%s %q: script %q duplicates %s", projectHooksManifestRelPath, entry.Type, entry.Script, previous)
		}
		seenPaths[entry.Script] = fmt.Sprintf("hook %q", entry.Type)

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

	normalizedFiles := make([]string, 0, len(manifest.Files))
	for _, bundleFile := range manifest.Files {
		normalizedPath, err := normalizeProjectHookRelativePath(bundleFile)
		if err != nil {
			return fmt.Errorf("%s: bundle file %q: %w", projectHooksManifestRelPath, bundleFile, err)
		}
		if previous, dup := seenPaths[normalizedPath]; dup {
			return fmt.Errorf("%s: bundle file %q duplicates %s", projectHooksManifestRelPath, normalizedPath, previous)
		}
		seenPaths[normalizedPath] = "another bundle file"
		normalizedFiles = append(normalizedFiles, normalizedPath)
	}
	sort.Strings(normalizedFiles)
	manifest.Files = normalizedFiles

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

func normalizeProjectHookRelativePath(path string) (string, error) {
	path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if path == "." || path == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be relative to %s", projectHooksDirRel)
	}
	if path == ".." || strings.HasPrefix(path, "../") {
		return "", fmt.Errorf("path %q escapes %s", path, projectHooksDirRel)
	}
	return path, nil
}

func resolveProjectHookBundleFile(hooksDir, path, label string) (string, string, []byte, error) {
	normalizedPath, err := normalizeProjectHookRelativePath(path)
	if err != nil {
		return "", "", nil, err
	}
	pathOnDisk := filepath.FromSlash(normalizedPath)
	pathAbs := filepath.Join(hooksDir, pathOnDisk)
	if !isWithinDir(hooksDir, pathAbs) {
		return "", "", nil, fmt.Errorf("%s %q escapes %s", label, normalizedPath, projectHooksDirRel)
	}
	canonicalHooksDir, err := canonicalizePath(hooksDir)
	if err != nil {
		return "", "", nil, err
	}
	canonicalParentDir, err := canonicalizePath(filepath.Dir(pathAbs))
	if err != nil {
		return "", "", nil, err
	}
	if !isWithinDir(canonicalHooksDir, canonicalParentDir) {
		return "", "", nil, fmt.Errorf("%s %q escapes %s via symlinked parent", label, normalizedPath, projectHooksDirRel)
	}

	info, err := os.Lstat(pathAbs)
	if err != nil {
		return "", "", nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", nil, fmt.Errorf("top-level symlinks are not supported at %s", normalizedPath)
	}
	if !info.Mode().IsRegular() {
		return "", "", nil, fmt.Errorf("%s %q must be a regular file", label, normalizedPath)
	}

	raw, err := os.ReadFile(pathAbs)
	if err != nil {
		return "", "", nil, err
	}
	return normalizedPath, pathAbs, raw, nil
}

func hashProjectHookBundle(manifest projectHooksManifest, files []loadedProjectHookFile, hooks []loadedProjectHook) string {
	var payload bytes.Buffer
	fmt.Fprintf(&payload, "version\x00%d\x00", manifest.Version)
	for _, bundleFile := range files {
		payload.WriteString("file\x00")
		payload.WriteString(bundleFile.Path)
		payload.WriteByte(0)
		payload.WriteString("file-hash\x00")
		payload.WriteString(bundleFile.Hash)
		payload.WriteByte(0)
	}
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

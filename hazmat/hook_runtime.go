package hazmat

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const (
	projectHookManagedDirRel = "hazmat-hooks/managed"
	projectHookWrapperName   = "git"

	projectHookChainBeginMarker = "# BEGIN HAZMAT MANAGED HOOK"
	projectHookChainEndMarker   = "# END HAZMAT MANAGED HOOK"
)

type projectHookRuntime struct {
	ProjectDir      string
	GitDir          string
	ManagedDir      string
	FallbackDir     string
	WrapperPath     string
	Bundle          *loadedProjectHookBundle
	Approval        *projectHookApprovalRecord
	SnapshotBundle  *loadedProjectHookBundle
	DeclaredHookSet []hookType
}

type projectHookRuntimeInstallOptions struct {
	ReplaceExisting bool
	ChainExisting   bool
}

type projectHookRuntimePaths struct {
	WrapperPath string
	ManagedDir  string
	FallbackDir string
	GitDir      string
}

type projectHookHooksPathDriftError struct {
	ConfiguredHooksPath string
	WantedHooksPath     string
}

func (e *projectHookHooksPathDriftError) Error() string {
	return fmt.Sprintf("git core.hooksPath drifted to %q (want %q)", e.ConfiguredHooksPath, e.WantedHooksPath)
}

func installProjectHookRuntime(projectDir, hazmatBinPath string) (*projectHookRuntime, error) {
	return installProjectHookRuntimeWithOptions(projectDir, hazmatBinPath, projectHookRuntimeInstallOptions{})
}

func installProjectHookRuntimeWithOptions(projectDir, hazmatBinPath string, options projectHookRuntimeInstallOptions) (*projectHookRuntime, error) {
	runtime, err := buildProjectHookRuntime(projectDir)
	if err != nil {
		return nil, err
	}
	if runtime.Bundle == nil {
		return nil, fmt.Errorf("repo does not declare managed hooks")
	}
	if runtime.Approval == nil || runtime.Approval.BundleHash != runtime.Bundle.BundleHash {
		return nil, fmt.Errorf("repo hook bundle is not approved")
	}

	configuredHooksPath, err := readLocalGitHooksPath(runtime.ProjectDir)
	if err != nil {
		return nil, err
	}
	if options.ReplaceExisting && options.ChainExisting {
		return nil, fmt.Errorf("--replace and --chain-existing cannot be used together")
	}
	if options.ChainExisting {
		return installProjectHookRuntimeChained(runtime, hazmatBinPath, configuredHooksPath)
	}
	if !options.ReplaceExisting && configuredHooksPath != "" && configuredHooksPath != runtime.ManagedDir {
		return nil, fmt.Errorf("git core.hooksPath is already owned by %q; refusing to replace it silently", configuredHooksPath)
	}
	if err := refuseUnknownHookEntries(runtime.FallbackDir, runtime.DeclaredHookSet, true); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(runtime.ManagedDir, 0o700); err != nil {
		return nil, fmt.Errorf("create managed hooks dir: %w", err)
	}
	if err := os.MkdirAll(runtime.FallbackDir, 0o700); err != nil {
		return nil, fmt.Errorf("create fallback hooks dir: %w", err)
	}

	for _, hook := range runtime.DeclaredHookSet {
		managedScript := buildProjectHookDispatcherScript(hazmatBinPath, runtime.ProjectDir, hook, false)
		if err := os.WriteFile(filepath.Join(runtime.ManagedDir, string(hook)), []byte(managedScript), 0o700); err != nil {
			return nil, fmt.Errorf("write managed %s dispatcher: %w", hook, err)
		}

		fallbackScript := buildProjectHookDispatcherScript(hazmatBinPath, runtime.ProjectDir, hook, true)
		if err := os.WriteFile(filepath.Join(runtime.FallbackDir, string(hook)), []byte(fallbackScript), 0o700); err != nil {
			return nil, fmt.Errorf("write fallback %s dispatcher: %w", hook, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(runtime.WrapperPath), 0o700); err != nil {
		return nil, fmt.Errorf("create hook wrapper dir: %w", err)
	}
	wrapperScript := buildProjectHookWrapperScript(hazmatBinPath, runtime.ProjectDir)
	if err := os.WriteFile(runtime.WrapperPath, []byte(wrapperScript), 0o700); err != nil {
		return nil, fmt.Errorf("write git hook wrapper: %w", err)
	}

	if err := writeLocalGitHooksPath(runtime.ProjectDir, runtime.ManagedDir); err != nil {
		return nil, err
	}
	approval, err := updateProjectHookApprovalChain(runtime.ProjectDir, nil)
	if err != nil {
		return nil, err
	}
	runtime.Approval = approval

	return runtime, nil
}

func installProjectHookRuntimeChained(runtime *projectHookRuntime, hazmatBinPath, configuredHooksPath string) (*projectHookRuntime, error) {
	chainHooksPath, chainDir, err := resolveProjectHookChainHooksPath(runtime.ProjectDir, configuredHooksPath)
	if err != nil {
		return nil, err
	}
	if err := ensureProjectHookChainDir(runtime.ProjectDir, chainDir); err != nil {
		return nil, err
	}
	if err := refuseUnknownHookEntries(runtime.FallbackDir, runtime.DeclaredHookSet, true); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(runtime.FallbackDir, 0o700); err != nil {
		return nil, fmt.Errorf("create fallback hooks dir: %w", err)
	}
	for _, hook := range runtime.DeclaredHookSet {
		fallbackScript := buildProjectHookDispatcherScript(hazmatBinPath, runtime.ProjectDir, hook, true)
		if err := os.WriteFile(filepath.Join(runtime.FallbackDir, string(hook)), []byte(fallbackScript), 0o700); err != nil {
			return nil, fmt.Errorf("write fallback %s dispatcher: %w", hook, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(runtime.WrapperPath), 0o700); err != nil {
		return nil, fmt.Errorf("create hook wrapper dir: %w", err)
	}
	wrapperScript := buildProjectHookWrapperScript(hazmatBinPath, runtime.ProjectDir)
	if err := os.WriteFile(runtime.WrapperPath, []byte(wrapperScript), 0o700); err != nil {
		return nil, fmt.Errorf("write git hook wrapper: %w", err)
	}

	var chainHooks []projectHookChainHookApproval
	for _, hook := range runtime.DeclaredHookSet {
		hookPath := filepath.Join(chainDir, string(hook))
		if err := installProjectHookChainBlock(hookPath, buildProjectHookChainBlock(hazmatBinPath, runtime.ProjectDir, hook)); err != nil {
			return nil, fmt.Errorf("install chained %s hook: %w", hook, err)
		}
		fileHash, err := hashProjectHookChainFile(hookPath)
		if err != nil {
			return nil, err
		}
		chainHooks = append(chainHooks, projectHookChainHookApproval{
			Type:     hook,
			FileHash: fileHash,
		})
	}

	if err := os.RemoveAll(runtime.ManagedDir); err != nil {
		return nil, fmt.Errorf("remove stale managed hooks dir: %w", err)
	}
	approval, err := updateProjectHookApprovalChain(runtime.ProjectDir, &projectHookChainApproval{
		HooksPath: chainHooksPath,
		Hooks:     chainHooks,
	})
	if err != nil {
		return nil, err
	}
	runtime.Approval = approval
	return runtime, nil
}

func uninstallProjectHookRuntime(projectDir string) error {
	canonicalProjectDir, err := canonicalizePath(projectDir)
	if err != nil {
		return err
	}
	approval, err := loadProjectHookApproval(canonicalProjectDir)
	if err != nil {
		return err
	}

	paths := projectHookRuntimePathsForProject(canonicalProjectDir)
	hookTypes := projectHookDeclaredTypes(nil, approval)
	if paths.GitDir != "" {
		currentHooksPath, err := readLocalGitHooksPath(canonicalProjectDir)
		if err != nil {
			return err
		}
		if currentHooksPath == paths.ManagedDir {
			if err := unsetLocalGitHooksPath(canonicalProjectDir); err != nil {
				return err
			}
		}
		if approval != nil && approval.Chain != nil {
			if err := removeProjectHookChainRuntime(canonicalProjectDir, approval.Chain); err != nil {
				return err
			}
		}

		for _, hook := range hookTypes {
			_ = os.Remove(filepath.Join(paths.ManagedDir, string(hook)))
			_ = os.Remove(filepath.Join(paths.FallbackDir, string(hook)))
		}
		_ = os.RemoveAll(filepath.Join(paths.GitDir, "hazmat-hooks"))
	}
	_ = os.Remove(paths.WrapperPath)
	return removeProjectHookApproval(canonicalProjectDir)
}

func validateProjectHookRuntime(projectDir string) (*projectHookRuntime, error) {
	runtime, err := buildProjectHookRuntime(projectDir)
	if err != nil {
		return nil, err
	}
	if runtime.Bundle == nil {
		return nil, fmt.Errorf("repo does not declare managed hooks")
	}
	if runtime.Approval == nil {
		return nil, fmt.Errorf("repo hook bundle is not approved")
	}
	if runtime.Approval.BundleHash != runtime.Bundle.BundleHash {
		return nil, fmt.Errorf("repo hook bundle drifted from the approved snapshot")
	}

	configuredHooksPath, err := readLocalGitHooksPath(runtime.ProjectDir)
	if err != nil {
		return nil, err
	}
	if runtime.Approval.Chain != nil {
		if err := validateProjectHookChainRuntime(runtime, configuredHooksPath); err != nil {
			return nil, err
		}
	} else if configuredHooksPath != runtime.ManagedDir {
		return nil, &projectHookHooksPathDriftError{
			ConfiguredHooksPath: configuredHooksPath,
			WantedHooksPath:     runtime.ManagedDir,
		}
	} else {
		if err := validateHookDispatcherLayout(runtime.ManagedDir, runtime.DeclaredHookSet, false); err != nil {
			return nil, err
		}
	}
	if err := validateHookDispatcherLayout(runtime.FallbackDir, runtime.DeclaredHookSet, true); err != nil {
		return nil, err
	}

	snapshotBundle, err := loadProjectHookSnapshot(runtime.Approval.SnapshotDir)
	if err != nil {
		return nil, fmt.Errorf("load approved hook snapshot: %w", err)
	}
	if snapshotBundle == nil {
		return nil, fmt.Errorf("approved hook snapshot is missing")
	}
	if snapshotBundle.BundleHash != runtime.Approval.BundleHash {
		return nil, fmt.Errorf("approved hook snapshot hash drifted from recorded approval")
	}
	runtime.SnapshotBundle = snapshotBundle
	return runtime, nil
}

func runProjectHookGitWrapper(projectDir string, args []string) error {
	if gitCommandMayRunManagedHooks(args) {
		if _, err := validateProjectHookRuntime(projectDir); err != nil {
			return err
		}
	}
	return runHookPassthroughCommand(hostGitCommand, args...)
}

func runApprovedProjectHook(projectDir string, hook hookType, args []string) error {
	runtime, err := validateProjectHookRuntime(projectDir)
	if err != nil {
		return err
	}

	hookEntry := findLoadedProjectHook(runtime.SnapshotBundle, hook)
	if hookEntry == nil {
		return fmt.Errorf("approved hook %q is not installed", hook)
	}

	scriptPath := filepath.Join(runtime.Approval.SnapshotDir, filepath.FromSlash(hookEntry.ScriptPath))
	commandArgs := append([]string{scriptPath}, args...)
	cmd := exec.Command(hookEntry.Interpreter, commandArgs...)
	cmd.Dir = runtime.ProjectDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

func fallbackProjectHookRefusal(projectDir string, hook hookType) error {
	runtime, err := buildProjectHookRuntime(projectDir)
	if err != nil {
		return err
	}
	configuredHooksPath, err := readLocalGitHooksPath(runtime.ProjectDir)
	if err != nil {
		return err
	}
	wantedHooksPath := runtimeWantedHooksPath(runtime)
	if configuredHooksPath == wantedHooksPath {
		return fmt.Errorf("hazmat hook %q reached the fallback dispatcher unexpectedly; reinstall the hook layout", hook)
	}
	return fmt.Errorf("hazmat hook %q refused because git core.hooksPath drifted to %q (want %q)", hook, configuredHooksPath, wantedHooksPath)
}

func buildProjectHookRuntime(projectDir string) (*projectHookRuntime, error) {
	canonicalProjectDir, err := canonicalizePath(projectDir)
	if err != nil {
		return nil, err
	}
	gitDir := gitMetadataDir(canonicalProjectDir)
	if gitDir == "" {
		return nil, fmt.Errorf("%s is not a git repository with a directory .git", canonicalProjectDir)
	}

	bundle, err := loadProjectHookBundle(canonicalProjectDir)
	if err != nil {
		return nil, err
	}
	approval, err := loadProjectHookApproval(canonicalProjectDir)
	if err != nil {
		return nil, err
	}

	projectKey := strings.TrimPrefix(hashProjectHookProject(canonicalProjectDir), "sha256:")
	return &projectHookRuntime{
		ProjectDir:      canonicalProjectDir,
		GitDir:          gitDir,
		ManagedDir:      filepath.Join(gitDir, projectHookManagedDirRel),
		FallbackDir:     filepath.Join(gitDir, "hooks"),
		WrapperPath:     filepath.Join(projectHookSnapshotsRootDir, projectKey, projectHookWrapperName),
		Bundle:          bundle,
		Approval:        approval,
		DeclaredHookSet: projectHookDeclaredTypes(bundle, approval),
	}, nil
}

func projectHookRuntimePathsForProject(projectDir string) projectHookRuntimePaths {
	projectKey := strings.TrimPrefix(hashProjectHookProject(projectDir), "sha256:")
	gitDir := gitMetadataDir(projectDir)
	return projectHookRuntimePaths{
		WrapperPath: filepath.Join(projectHookSnapshotsRootDir, projectKey, projectHookWrapperName),
		ManagedDir:  filepath.Join(gitDir, projectHookManagedDirRel),
		FallbackDir: filepath.Join(gitDir, "hooks"),
		GitDir:      gitDir,
	}
}

func projectHookManagedRuntimeArtifactsExist(runtime *projectHookRuntime) bool {
	if runtime.Approval != nil && runtime.Approval.Chain != nil {
		return true
	}
	for _, path := range []string{runtime.ManagedDir, runtime.WrapperPath} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	for _, hook := range runtime.DeclaredHookSet {
		if projectHookDispatcherHasMarker(filepath.Join(runtime.FallbackDir, string(hook)), "hazmat-hook-fallback") {
			return true
		}
	}
	return false
}

func projectHookDeclaredTypes(bundle *loadedProjectHookBundle, approval *projectHookApprovalRecord) []hookType {
	if bundle != nil {
		types := make([]hookType, 0, len(bundle.Hooks))
		for _, hook := range bundle.Hooks {
			types = append(types, hook.Type)
		}
		return types
	}
	if approval != nil {
		types := make([]hookType, 0, len(approval.Summary.Hooks))
		for _, hook := range approval.Summary.Hooks {
			types = append(types, hook.Type)
		}
		sortHookTypes(types)
		return types
	}
	return nil
}

func sortHookTypes(types []hookType) {
	slices.SortFunc(types, func(a, b hookType) int {
		switch {
		case a < b:
			return -1
		case a > b:
			return 1
		default:
			return 0
		}
	})
}

func findLoadedProjectHook(bundle *loadedProjectHookBundle, hook hookType) *loadedProjectHook {
	if bundle == nil {
		return nil
	}
	for _, entry := range bundle.Hooks {
		if entry.Type == hook {
			copy := entry
			return &copy
		}
	}
	return nil
}

func readLocalGitHooksPath(projectDir string) (string, error) {
	cmd, err := hostGitCommand("-C", projectDir, "config", "--local", "--get", "core.hooksPath")
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("read local git core.hooksPath: %s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func writeLocalGitHooksPath(projectDir, hooksPath string) error {
	cmd, err := hostGitCommand("-C", projectDir, "config", "--local", "core.hooksPath", hooksPath)
	if err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("set local git core.hooksPath: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func unsetLocalGitHooksPath(projectDir string) error {
	cmd, err := hostGitCommand("-C", projectDir, "config", "--local", "--unset", "core.hooksPath")
	if err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 {
			return nil
		}
		return fmt.Errorf("unset local git core.hooksPath: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func refuseUnknownHookEntries(dir string, declared []hookType, fallback bool) error {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	expected := make(map[string]struct{}, len(declared))
	for _, hook := range declared {
		expected[string(hook)] = struct{}{}
	}
	for _, entry := range entries {
		name := entry.Name()
		if fallback && strings.HasSuffix(name, ".sample") {
			continue
		}
		if _, ok := expected[name]; ok {
			continue
		}
		return fmt.Errorf("hook directory %s already contains unexpected entry %q", dir, name)
	}
	return nil
}

func validateHookDispatcherLayout(dir string, declared []hookType, fallback bool) error {
	if err := refuseUnknownHookEntries(dir, declared, fallback); err != nil {
		return err
	}
	for _, hook := range declared {
		path := filepath.Join(dir, string(hook))
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("required hook dispatcher %s is missing", path)
		}
		if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
			return fmt.Errorf("hook dispatcher %s is not executable", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read hook dispatcher %s: %w", path, err)
		}
		marker := "hazmat-hook-dispatch"
		if fallback {
			marker = "hazmat-hook-fallback"
		}
		if !strings.Contains(string(raw), marker) {
			return fmt.Errorf("hook dispatcher %s is not Hazmat-managed", path)
		}
	}
	return nil
}

func validateProjectHookChainRuntime(runtime *projectHookRuntime, configuredHooksPath string) error {
	chain := runtime.Approval.Chain
	if chain == nil {
		return fmt.Errorf("repo hook chain approval is missing")
	}

	approvedHooksPath, chainDir, err := resolveProjectHookChainHooksPath(runtime.ProjectDir, chain.HooksPath)
	if err != nil {
		return err
	}
	configuredChainPath, _, err := resolveProjectHookChainHooksPath(runtime.ProjectDir, configuredHooksPath)
	if err != nil || configuredChainPath != approvedHooksPath {
		return &projectHookHooksPathDriftError{
			ConfiguredHooksPath: configuredHooksPath,
			WantedHooksPath:     approvedHooksPath,
		}
	}
	if err := ensureProjectHookChainDir(runtime.ProjectDir, chainDir); err != nil {
		return err
	}

	records := make(map[hookType]projectHookChainHookApproval, len(chain.Hooks))
	for _, record := range chain.Hooks {
		if _, ok := validProjectHookTypes[record.Type]; !ok {
			return fmt.Errorf("approved chained hook type %q is unsupported", record.Type)
		}
		if _, dup := records[record.Type]; dup {
			return fmt.Errorf("approved chained hook type %q is duplicated", record.Type)
		}
		records[record.Type] = record
	}
	if len(records) != len(runtime.DeclaredHookSet) {
		return fmt.Errorf("approved chained hook set drifted from declared hook set")
	}
	for _, hook := range runtime.DeclaredHookSet {
		record, ok := records[hook]
		if !ok {
			return fmt.Errorf("approved chained hook %q is missing", hook)
		}
		path := filepath.Join(chainDir, string(hook))
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("required chained hook dispatcher %s is missing", path)
		}
		if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
			return fmt.Errorf("chained hook dispatcher %s is not executable", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read chained hook dispatcher %s: %w", path, err)
		}
		text := string(raw)
		if !strings.Contains(text, projectHookChainBeginMarker) || !strings.Contains(text, projectHookChainEndMarker) {
			return fmt.Errorf("chained hook dispatcher %s is missing the Hazmat-managed block", path)
		}
		fileHash := hashProjectHookBytes(raw)
		if fileHash != record.FileHash {
			return fmt.Errorf("chained hook dispatcher %s drifted from approved hash", path)
		}
	}
	return nil
}

func resolveProjectHookChainHooksPath(projectDir, hooksPath string) (string, string, error) {
	trimmed := strings.TrimSpace(hooksPath)
	if trimmed == "" {
		return "", "", fmt.Errorf("--chain-existing requires a non-empty local core.hooksPath")
	}
	if filepath.IsAbs(trimmed) {
		return "", "", fmt.Errorf("--chain-existing only supports repo-relative core.hooksPath values, got %q", hooksPath)
	}
	normalized := filepath.ToSlash(filepath.Clean(trimmed))
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", "", fmt.Errorf("--chain-existing hooksPath %q must stay inside the project", hooksPath)
	}
	if normalized == ".git" || strings.HasPrefix(normalized, ".git/") {
		return "", "", fmt.Errorf("--chain-existing refuses to compose inside .git; use the managed install path instead")
	}
	chainDir := filepath.Join(projectDir, filepath.FromSlash(normalized))
	if !isWithinDir(projectDir, chainDir) {
		return "", "", fmt.Errorf("--chain-existing hooksPath %q escapes the project", hooksPath)
	}
	return normalized, chainDir, nil
}

func ensureProjectHookChainDir(projectDir, chainDir string) error {
	info, err := os.Stat(chainDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("--chain-existing hooksPath directory %s does not exist", chainDir)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("--chain-existing hooksPath %s is not a directory", chainDir)
	}
	canonicalProjectDir, err := canonicalizePath(projectDir)
	if err != nil {
		return err
	}
	canonicalChainDir, err := canonicalizePath(chainDir)
	if err != nil {
		return err
	}
	if !isWithinDir(canonicalProjectDir, canonicalChainDir) {
		return fmt.Errorf("--chain-existing hooksPath %s escapes the project via symlink", chainDir)
	}
	return nil
}

func installProjectHookChainBlock(path, block string) error {
	mode := os.FileMode(0o700)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		mode = info.Mode().Perm() | 0o100
	}

	cleaned, _, err := removeProjectHookChainBlock(raw)
	if err != nil {
		return err
	}
	updated := addProjectHookChainBlock(cleaned, block)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), mode)
}

func removeProjectHookChainRuntime(projectDir string, chain *projectHookChainApproval) error {
	if chain == nil {
		return nil
	}
	_, chainDir, err := resolveProjectHookChainHooksPath(projectDir, chain.HooksPath)
	if err != nil {
		return err
	}
	for _, hook := range chain.Hooks {
		if err := removeProjectHookChainBlockFromFile(filepath.Join(chainDir, string(hook.Type))); err != nil {
			return err
		}
	}
	return nil
}

func removeProjectHookChainBlockFromFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cleaned, removed, err := removeProjectHookChainBlock(raw)
	if err != nil {
		return err
	}
	if !removed {
		return nil
	}
	if strings.TrimSpace(string(cleaned)) == "" {
		return os.Remove(path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, cleaned, info.Mode().Perm())
}

func addProjectHookChainBlock(raw []byte, block string) string {
	text := string(raw)
	shebang, body := splitProjectHookShebang(text)
	var b strings.Builder
	if shebang == "" {
		shebang = "#!/bin/sh"
	}
	b.WriteString(shebang)
	b.WriteByte('\n')
	b.WriteString(block)
	if body != "" {
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func splitProjectHookShebang(text string) (string, string) {
	if !strings.HasPrefix(text, "#!") {
		return "", text
	}
	idx := strings.IndexByte(text, '\n')
	if idx == -1 {
		return strings.TrimSuffix(text, "\r"), ""
	}
	return strings.TrimSuffix(text[:idx], "\r"), text[idx+1:]
}

func removeProjectHookChainBlock(raw []byte) ([]byte, bool, error) {
	text := string(raw)
	start := strings.Index(text, projectHookChainBeginMarker)
	end := strings.Index(text, projectHookChainEndMarker)
	switch {
	case start == -1 && end == -1:
		return raw, false, nil
	case start == -1 || end == -1 || end < start:
		return nil, false, fmt.Errorf("malformed Hazmat-managed hook block")
	}
	end += len(projectHookChainEndMarker)
	if end < len(text) && text[end] == '\r' {
		end++
	}
	if end < len(text) && text[end] == '\n' {
		end++
	}
	cleaned := text[:start] + text[end:]
	return []byte(cleaned), true, nil
}

func buildProjectHookChainBlock(hazmatBinPath, projectDir string, hook hookType) string {
	quoted := shellQuote([]string{hazmatBinPath, projectDir, string(hook)})[0:3]
	return strings.Join([]string{
		projectHookChainBeginMarker,
		quoted[0] + " _git-hook-dispatch --project " + quoted[1] + " --hook " + quoted[2] + ` "$@"`,
		"hazmat_status=$?",
		`if [ "$hazmat_status" -ne 0 ]; then`,
		`  exit "$hazmat_status"`,
		"fi",
		projectHookChainEndMarker,
		"",
	}, "\n")
}

func hashProjectHookChainFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read chained hook dispatcher %s: %w", path, err)
	}
	return hashProjectHookBytes(raw), nil
}

func hashProjectHookBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func runtimeWantedHooksPath(runtime *projectHookRuntime) string {
	if runtime != nil && runtime.Approval != nil && runtime.Approval.Chain != nil {
		return runtime.Approval.Chain.HooksPath
	}
	if runtime == nil {
		return ""
	}
	return runtime.ManagedDir
}

func projectHookDispatcherHasMarker(path, marker string) bool {
	raw, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(raw), marker)
}

func buildProjectHookDispatcherScript(hazmatBinPath, projectDir string, hook hookType, fallback bool) string {
	command := "_git-hook-dispatch"
	marker := "hazmat-hook-dispatch"
	if fallback {
		command = "_git-hook-fallback"
		marker = "hazmat-hook-fallback"
	}
	quoted := shellQuote([]string{hazmatBinPath, projectDir, string(hook)})[0:3]
	return strings.Join([]string{
		"#!/bin/sh",
		"# " + marker,
		"exec " + quoted[0] + " " + command + " --project " + quoted[1] + " --hook " + quoted[2] + ` "$@"`,
		"",
	}, "\n")
}

func buildProjectHookWrapperScript(hazmatBinPath, projectDir string) string {
	quoted := shellQuote([]string{hazmatBinPath, projectDir})[0:2]
	return strings.Join([]string{
		"#!/bin/sh",
		"# hazmat-git-hook-wrapper",
		"exec " + quoted[0] + " _git-hook-wrapper --project " + quoted[1] + ` "$@"`,
		"",
	}, "\n")
}

func gitCommandMayRunManagedHooks(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		switch arg {
		case "commit", "push":
			return true
		default:
			return false
		}
	}
	return false
}

func runHookPassthroughCommand(command func(args ...string) (*exec.Cmd, error), args ...string) error {
	cmd, err := command(args...)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

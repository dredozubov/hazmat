package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

const (
	projectHookManagedDirRel = "hazmat-hooks/managed"
	projectHookWrapperName   = "git"
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

func newGitHookWrapperCmd() *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:    "_git-hook-wrapper",
		Hidden: true,
		Args:   cobra.ArbitraryArgs,
		Run: func(_ *cobra.Command, args []string) {
			if err := runProjectHookGitWrapper(projectDir, args); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "", "Canonical project directory")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newGitHookDispatchCmd() *cobra.Command {
	var projectDir string
	var hookName string
	cmd := &cobra.Command{
		Use:    "_git-hook-dispatch",
		Hidden: true,
		Args:   cobra.ArbitraryArgs,
		Run: func(_ *cobra.Command, args []string) {
			if err := runApprovedProjectHook(projectDir, hookType(hookName), args); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "", "Canonical project directory")
	cmd.Flags().StringVar(&hookName, "hook", "", "Hook type")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("hook")
	return cmd
}

func newGitHookFallbackCmd() *cobra.Command {
	var projectDir string
	var hookName string
	cmd := &cobra.Command{
		Use:    "_git-hook-fallback",
		Hidden: true,
		Args:   cobra.ArbitraryArgs,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Fprintln(os.Stderr, fallbackProjectHookRefusal(projectDir, hookType(hookName)))
			os.Exit(1)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "", "Canonical project directory")
	cmd.Flags().StringVar(&hookName, "hook", "", "Hook type")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("hook")
	return cmd
}

func installProjectHookRuntime(projectDir, hazmatBinPath string) (*projectHookRuntime, error) {
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
	if configuredHooksPath != "" && configuredHooksPath != runtime.ManagedDir {
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

	return runtime, nil
}

func uninstallProjectHookRuntime(projectDir string) error {
	runtime, err := buildProjectHookRuntime(projectDir)
	if err != nil {
		return err
	}
	if runtime.GitDir == "" {
		return nil
	}

	currentHooksPath, err := readLocalGitHooksPath(runtime.ProjectDir)
	if err != nil {
		return err
	}
	if currentHooksPath == runtime.ManagedDir {
		if err := unsetLocalGitHooksPath(runtime.ProjectDir); err != nil {
			return err
		}
	}

	for _, hook := range runtime.DeclaredHookSet {
		_ = os.Remove(filepath.Join(runtime.ManagedDir, string(hook)))
		_ = os.Remove(filepath.Join(runtime.FallbackDir, string(hook)))
	}
	_ = os.Remove(runtime.WrapperPath)
	_ = os.RemoveAll(filepath.Join(runtime.GitDir, "hazmat-hooks"))
	_ = removeProjectHookApproval(runtime.ProjectDir)
	return nil
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
	if configuredHooksPath != runtime.ManagedDir {
		return nil, fmt.Errorf("git core.hooksPath drifted to %q (want %q)", configuredHooksPath, runtime.ManagedDir)
	}
	if err := validateHookDispatcherLayout(runtime.ManagedDir, runtime.DeclaredHookSet, false); err != nil {
		return nil, err
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
	if configuredHooksPath == runtime.ManagedDir {
		return fmt.Errorf("hazmat hook %q reached the fallback dispatcher unexpectedly; reinstall the managed hook layout", hook)
	}
	return fmt.Errorf("hazmat hook %q refused because git core.hooksPath drifted to %q (want %q)", hook, configuredHooksPath, runtime.ManagedDir)
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

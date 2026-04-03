package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	sessionToolCommandPath       = func(name string) (string, error) { return commandPathFromEnv(name, nil) }
	sessionToolExecutableByAgent = integrationAgentExecCheck
	sessionToolStagingRoot       = "/private/tmp/hazmat-session-tools"
)

func prepareSessionTools(cfg *sessionConfig) error {
	if !projectUsesBeads(cfg.ProjectDir) {
		// Continue: non-beads projects may still need staged session tools.
	} else {
		dir, err := stageSessionToolMirror(cfg.ProjectDir, "bd")
		if err != nil {
			return err
		}
		if dir != "" {
			cfg.StagedToolDirs = appendUniqueStrings(cfg.StagedToolDirs, dir)
		}
	}
	if err := prepareNodePackageManagerTools(cfg); err != nil {
		return err
	}
	return nil
}

func projectUsesBeads(projectDir string) bool {
	for _, candidate := range []string{
		filepath.Join(projectDir, ".beads"),
		filepath.Join(projectDir, ".beads", "issues.jsonl"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}
	return false
}

func stageSessionToolMirror(projectDir, name string) (string, error) {
	if name == "" {
		return "", nil
	}

	stagingDir := filepath.Join(sessionToolStagingRoot, sessionToolProjectKey(projectDir), "bin")
	destPath := filepath.Join(stagingDir, name)

	sourcePath, err := sessionToolCommandPath(name)
	if err != nil || sourcePath == "" {
		if info, statErr := os.Stat(destPath); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return stagingDir, nil
		}
		return "", nil
	}
	if resolved, err := filepath.EvalSymlinks(sourcePath); err == nil && resolved != "" {
		sourcePath = resolved
	}

	info, err := os.Stat(sourcePath)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return "", nil
	}
	if sessionToolExecutableByAgent(sourcePath) {
		return "", nil
	}

	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", fmt.Errorf("prepare %s for staged tool %s: %w", stagingDir, name, err)
	}

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("open %s for staged tool %s: %w", sourcePath, name, err)
	}
	defer sourceFile.Close()

	tmpFile, err := os.CreateTemp(stagingDir, name+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create staged tool %s: %w", name, err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, sourceFile); err != nil {
		tmpFile.Close() //nolint:errcheck // copy error is authoritative
		return "", fmt.Errorf("copy %s into staged tool %s: %w", sourcePath, name, err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close staged tool %s: %w", name, err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return "", fmt.Errorf("chmod staged tool %s: %w", name, err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return "", fmt.Errorf("install staged tool %s into %s: %w", name, destPath, err)
	}
	return stagingDir, nil
}

type packageManagerSpec struct {
	Name    string
	Version string
	Raw     string
}

func prepareNodePackageManagerTools(cfg *sessionConfig) error {
	if !sessionHasIntegration(cfg, "node") {
		return nil
	}

	pm, ok, err := detectProjectPackageManager(cfg.ProjectDir)
	if err != nil || !ok || pm.Name != "pnpm" {
		return err
	}
	if sessionToolAvailableToAgent(cfg, pm.Name) {
		return nil
	}

	if dir, err := stageSessionToolMirror(cfg.ProjectDir, "corepack"); err != nil {
		return err
	} else if dir != "" {
		cfg.StagedToolDirs = appendUniqueStrings(cfg.StagedToolDirs, dir)
	}

	dir, err := stageNodePackageManagerShim(cfg.ProjectDir, pm)
	if err != nil {
		return err
	}
	if dir != "" {
		cfg.StagedToolDirs = appendUniqueStrings(cfg.StagedToolDirs, dir)
	}
	cfg.SessionNotes = appendUniqueStrings(
		cfg.SessionNotes,
		fmt.Sprintf("Package manager bootstrap: %s will be installed into the agent home on first use if it is not already available.", pm.Raw),
	)
	return nil
}

func sessionHasIntegration(cfg *sessionConfig, name string) bool {
	for _, integration := range cfg.ActiveIntegrations {
		if integration == name {
			return true
		}
	}
	return false
}

func detectProjectPackageManager(projectDir string) (packageManagerSpec, bool, error) {
	manifestPath := filepath.Join(projectDir, "package.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return packageManagerSpec{}, false, nil
		}
		return packageManagerSpec{}, false, fmt.Errorf("read %s: %w", manifestPath, err)
	}

	var manifest struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return packageManagerSpec{}, false, fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	spec := strings.TrimSpace(manifest.PackageManager)
	if spec == "" {
		return packageManagerSpec{}, false, nil
	}

	name := spec
	version := ""
	if idx := strings.Index(spec, "@"); idx > 0 {
		name = spec[:idx]
		version = spec[idx+1:]
	}
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" {
		return packageManagerSpec{}, false, nil
	}
	return packageManagerSpec{Name: name, Version: version, Raw: spec}, true, nil
}

func sessionToolAvailableToAgent(cfg *sessionConfig, name string) bool {
	pathEnv := defaultAgentPath
	if len(cfg.IntegrationPathPrefixes) > 0 {
		pathEnv = strings.Join(appendUniquePathPrefixes(cfg.IntegrationPathPrefixes), ":") + ":" + defaultAgentPath
	}
	resolved, err := commandPathFromEnv(name, []string{"PATH=" + pathEnv})
	if err != nil || resolved == "" {
		return false
	}
	return integrationAgentExecCheck(resolved)
}

func stageNodePackageManagerShim(projectDir string, pm packageManagerSpec) (string, error) {
	stagingDir := filepath.Join(sessionToolStagingRoot, sessionToolProjectKey(projectDir), "bin")
	destPath := filepath.Join(stagingDir, pm.Name)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", fmt.Errorf("prepare %s for package manager %s: %w", stagingDir, pm.Name, err)
	}

	tmpFile, err := os.CreateTemp(stagingDir, pm.Name+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create staged package manager %s: %w", pm.Name, err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(packageManagerShimScript(pm)); err != nil {
		tmpFile.Close() //nolint:errcheck // write error is authoritative
		return "", fmt.Errorf("write staged package manager %s: %w", pm.Name, err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close staged package manager %s: %w", pm.Name, err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return "", fmt.Errorf("chmod staged package manager %s: %w", pm.Name, err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return "", fmt.Errorf("install staged package manager %s into %s: %w", pm.Name, destPath, err)
	}
	return stagingDir, nil
}

func packageManagerShimScript(pm packageManagerSpec) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu

pm_name=%q
pm_spec=%q
self=$0
corepack_bin_dir="${XDG_DATA_HOME:-$HOME/.local/share}/hazmat/corepack/bin"
corepack_home="${XDG_DATA_HOME:-$HOME/.local/share}/corepack"
npm_prefix="${XDG_DATA_HOME:-$HOME/.local/share}/hazmat/npm-global"

run_real() {
  candidate=$1
  shift
  if [ -n "$candidate" ] && [ -x "$candidate" ] && [ "$candidate" != "$self" ]; then
    exec "$candidate" "$@"
  fi
}

run_real "$corepack_bin_dir/$pm_name" "$@"
run_real "$npm_prefix/bin/$pm_name" "$@"

if command -v corepack >/dev/null 2>&1; then
  /usr/bin/install -d -m 0700 "$corepack_bin_dir" "$corepack_home"
  COREPACK_ENABLE_DOWNLOAD_PROMPT=0 COREPACK_HOME="$corepack_home" corepack enable --install-directory "$corepack_bin_dir" "$pm_name" >/dev/null 2>&1 || true
  if [ -n "${SANDBOX_PROJECT_DIR:-}" ] && [ -d "$SANDBOX_PROJECT_DIR" ]; then
    (cd "$SANDBOX_PROJECT_DIR" && COREPACK_ENABLE_DOWNLOAD_PROMPT=0 COREPACK_HOME="$corepack_home" corepack install >/dev/null 2>&1) || true
  fi
  run_real "$corepack_bin_dir/$pm_name" "$@"
fi

if command -v npm >/dev/null 2>&1; then
  /usr/bin/install -d -m 0700 "$npm_prefix"
  npm_config_audit=false npm_config_fund=false npm_config_update_notifier=false \
    npm install --global --prefix "$npm_prefix" "$pm_spec" >/dev/null 2>&1 || true
  run_real "$npm_prefix/bin/$pm_name" "$@"
fi

echo "hazmat: unable to bootstrap $pm_name for this session ($pm_spec)" >&2
exit 127
`, pm.Name, pm.Raw)
}

func sessionToolProjectKey(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))
	return fmt.Sprintf("%x", sum[:6])
}

package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var (
	sessionToolCommandPath       = func(name string) (string, error) { return commandPathFromEnv(name, nil) }
	sessionToolExecutableByAgent = integrationAgentExecCheck
	sessionToolStagingRoot       = "/private/tmp/hazmat-session-tools"
)

func prepareSessionTools(cfg *sessionConfig) error {
	if !projectUsesBeads(cfg.ProjectDir) {
		return nil
	}
	dir, err := stageSessionToolMirror(cfg.ProjectDir, "bd")
	if err != nil {
		return err
	}
	if dir != "" {
		cfg.StagedToolDirs = appendUniqueStrings(cfg.StagedToolDirs, dir)
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

func sessionToolProjectKey(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))
	return fmt.Sprintf("%x", sum[:6])
}

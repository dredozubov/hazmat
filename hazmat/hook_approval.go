package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	projectHookApprovalsFilePath = filepath.Join(os.Getenv("HOME"), ".hazmat/hook-approvals.yaml")
	projectHookSnapshotsRootDir  = filepath.Join(os.Getenv("HOME"), ".hazmat/git-hooks")
)

type projectHookApprovalFile struct {
	Approvals []projectHookApprovalRecord `yaml:"approvals"`
}

type projectHookApprovalRecord struct {
	ProjectDir  string                     `yaml:"project"`
	BundleHash  string                     `yaml:"bundle_hash"`
	SnapshotDir string                     `yaml:"snapshot_dir"`
	ApprovedAt  string                     `yaml:"approved_at,omitempty"`
	Summary     projectHookApprovalSummary `yaml:"summary"`
}

type projectHookApprovalSummary struct {
	Hooks []projectHookSummaryEntry `yaml:"hooks"`
}

func loadProjectHookApprovals() projectHookApprovalFile {
	data, err := os.ReadFile(projectHookApprovalsFilePath)
	if err != nil {
		return projectHookApprovalFile{}
	}

	var approvals projectHookApprovalFile
	_ = yaml.Unmarshal(data, &approvals)
	return approvals
}

func saveProjectHookApprovals(approvals projectHookApprovalFile) error {
	dir := filepath.Dir(projectHookApprovalsFilePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	sort.Slice(approvals.Approvals, func(i, j int) bool {
		return approvals.Approvals[i].ProjectDir < approvals.Approvals[j].ProjectDir
	})
	data, err := yaml.Marshal(&approvals)
	if err != nil {
		return err
	}
	return os.WriteFile(projectHookApprovalsFilePath, data, 0o600)
}

func loadProjectHookApproval(projectDir string) (*projectHookApprovalRecord, error) {
	canonicalProjectDir, err := canonicalizePath(projectDir)
	if err != nil {
		return nil, err
	}

	approvals := loadProjectHookApprovals()
	for _, record := range approvals.Approvals {
		if record.ProjectDir == canonicalProjectDir {
			copy := record
			copy.Summary.Hooks = append([]projectHookSummaryEntry(nil), record.Summary.Hooks...)
			return &copy, nil
		}
	}
	return nil, nil
}

func isProjectHookBundleApproved(projectDir, bundleHash string) bool {
	record, err := loadProjectHookApproval(projectDir)
	if err != nil || record == nil {
		return false
	}
	return record.BundleHash == bundleHash
}

func recordProjectHookApproval(bundle *loadedProjectHookBundle) (*projectHookApprovalRecord, error) {
	if bundle == nil {
		return nil, fmt.Errorf("project hook bundle is required")
	}

	canonicalProjectDir, err := canonicalizePath(bundle.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("canonicalize project dir: %w", err)
	}

	snapshotDir := projectHookSnapshotDir(canonicalProjectDir, bundle.BundleHash)
	if err := os.MkdirAll(filepath.Dir(snapshotDir), 0o700); err != nil {
		return nil, fmt.Errorf("create hook snapshot parent: %w", err)
	}

	if _, err := os.Stat(snapshotDir); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat hook snapshot: %w", err)
		}
		tempDir, err := os.MkdirTemp(filepath.Dir(snapshotDir), "snapshot-*")
		if err != nil {
			return nil, fmt.Errorf("create hook snapshot temp dir: %w", err)
		}
		renameTemp := true
		defer func() {
			if renameTemp {
				_ = os.RemoveAll(tempDir)
			}
		}()

		if err := writeProjectHookSnapshot(tempDir, bundle); err != nil {
			return nil, err
		}
		if err := os.Rename(tempDir, snapshotDir); err != nil {
			return nil, fmt.Errorf("install hook snapshot: %w", err)
		}
		renameTemp = false
	}

	record := projectHookApprovalRecord{
		ProjectDir:  canonicalProjectDir,
		BundleHash:  bundle.BundleHash,
		SnapshotDir: snapshotDir,
		ApprovedAt:  time.Now().UTC().Format(time.RFC3339),
		Summary: projectHookApprovalSummary{
			Hooks: summarizeProjectHookBundle(bundle, "").Hooks,
		},
	}

	approvals := loadProjectHookApprovals()
	var staleSnapshotDirs []string
	filtered := approvals.Approvals[:0]
	for _, existing := range approvals.Approvals {
		if existing.ProjectDir == canonicalProjectDir {
			if existing.SnapshotDir != "" && existing.SnapshotDir != snapshotDir {
				staleSnapshotDirs = append(staleSnapshotDirs, existing.SnapshotDir)
			}
			continue
		}
		filtered = append(filtered, existing)
	}
	approvals.Approvals = append(filtered, record)

	if err := saveProjectHookApprovals(approvals); err != nil {
		return nil, fmt.Errorf("save hook approvals: %w", err)
	}

	for _, staleDir := range staleSnapshotDirs {
		if err := os.RemoveAll(staleDir); err != nil {
			return nil, fmt.Errorf("remove stale hook snapshot %s: %w", staleDir, err)
		}
	}

	return &record, nil
}

func removeProjectHookApproval(projectDir string) error {
	canonicalProjectDir, err := canonicalizePath(projectDir)
	if err != nil {
		return err
	}

	approvals := loadProjectHookApprovals()
	filtered := approvals.Approvals[:0]
	var snapshotDirs []string
	for _, record := range approvals.Approvals {
		if record.ProjectDir == canonicalProjectDir {
			if record.SnapshotDir != "" {
				snapshotDirs = append(snapshotDirs, record.SnapshotDir)
			}
			continue
		}
		filtered = append(filtered, record)
	}
	approvals.Approvals = filtered

	if err := saveProjectHookApprovals(approvals); err != nil {
		return fmt.Errorf("save hook approvals: %w", err)
	}
	for _, snapshotDir := range snapshotDirs {
		if err := os.RemoveAll(snapshotDir); err != nil {
			return fmt.Errorf("remove hook snapshot %s: %w", snapshotDir, err)
		}
	}
	return nil
}

func projectHookSnapshotDir(projectDir, bundleHash string) string {
	projectKey := strings.TrimPrefix(hashProjectHookProject(projectDir), "sha256:")
	bundleKey := strings.TrimPrefix(bundleHash, "sha256:")
	return filepath.Join(projectHookSnapshotsRootDir, projectKey, bundleKey)
}

func hashProjectHookProject(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeProjectHookSnapshot(snapshotDir string, bundle *loadedProjectHookBundle) error {
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		return fmt.Errorf("create hook snapshot dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "hooks.yaml"), bundle.ManifestData, 0o600); err != nil {
		return fmt.Errorf("write hook snapshot manifest: %w", err)
	}
	for _, hook := range bundle.Hooks {
		target := filepath.Join(snapshotDir, filepath.FromSlash(hook.ScriptPath))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("create hook snapshot parent: %w", err)
		}
		if err := os.WriteFile(target, hook.ScriptData, 0o700); err != nil {
			return fmt.Errorf("write hook snapshot script %s: %w", hook.ScriptPath, err)
		}
	}
	return nil
}

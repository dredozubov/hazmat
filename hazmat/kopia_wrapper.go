package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kopia/kopia/fs"
	"github.com/kopia/kopia/fs/localfs"
	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/blob/filesystem"
	"github.com/kopia/kopia/repo/blob/s3"
	"github.com/kopia/kopia/snapshot"
	"github.com/kopia/kopia/snapshot/policy"
	"github.com/kopia/kopia/snapshot/restore"
	"github.com/kopia/kopia/snapshot/snapshotfs"
	"github.com/kopia/kopia/snapshot/upload"
)

// ── Local repo paths ────────────────────────────────────────────────────────

var (
	localRepoDir    = filepath.Join(os.Getenv("HOME"), ".hazmat/repo")
	localConfigFile = filepath.Join(os.Getenv("HOME"), ".hazmat/repo.config")
)

// Fixed password for the local repo. This is local-only, protected by
// filesystem permissions. If an attacker can read your home directory
// they can already read your source code directly.
const localRepoPassword = "hazmat-local-snapshots"

// Default retention policy for local snapshots.
const (
	defaultKeepLatest  = 20
	defaultKeepDaily   = 7
	defaultKeepWeekly  = 4
	defaultKeepMonthly = 0
	defaultKeepAnnual  = 0
)

// ── Source info ─────────────────────────────────────────────────────────────

func localSourceInfo(sourcePath string) snapshot.SourceInfo {
	return snapshot.SourceInfo{
		Host:     "hazmat",
		UserName: os.Getenv("USER"),
		Path:     sourcePath,
	}
}

// ── Local repo lifecycle ────────────────────────────────────────────────────

// initLocalRepo creates the local Kopia repository and sets the global
// retention policy. Called during hazmat init.
// Idempotent — returns nil if the repo already exists.
func initLocalRepo() error {
	ctx := context.Background()

	if _, err := os.Stat(localConfigFile); err == nil {
		return nil // already initialized
	}

	if err := os.MkdirAll(localRepoDir, 0o700); err != nil {
		return fmt.Errorf("create local repo dir: %w", err)
	}

	st, err := filesystem.New(ctx, &filesystem.Options{Path: localRepoDir}, false)
	if err != nil {
		return fmt.Errorf("create local storage: %w", err)
	}

	if err := repo.Initialize(ctx, st, &repo.NewRepositoryOptions{}, localRepoPassword); err != nil {
		if !strings.Contains(err.Error(), "already initialized") {
			return fmt.Errorf("initialize local repo: %w", err)
		}
	}

	if err := repo.Connect(ctx, localConfigFile, st, localRepoPassword, &repo.ConnectOptions{}); err != nil {
		return fmt.Errorf("connect local repo: %w", err)
	}

	// Set global retention policy.
	if err := setRetentionPolicy(ctx); err != nil {
		return fmt.Errorf("set retention policy: %w", err)
	}

	return nil
}

// setRetentionPolicy sets the global (host-level) retention policy for the
// local repo. This applies to all sources (all project snapshots).
func setRetentionPolicy(ctx context.Context) error {
	r, err := repo.Open(ctx, localConfigFile, localRepoPassword, &repo.Options{})
	if err != nil {
		return err
	}
	defer r.Close(ctx)

	ctx, wr, err := r.(repo.DirectRepository).NewDirectWriter(ctx, repo.WriteSessionOptions{Purpose: "SetPolicy"})
	if err != nil {
		return err
	}
	defer wr.Close(ctx)

	latest := policy.OptionalInt(defaultKeepLatest)
	daily := policy.OptionalInt(defaultKeepDaily)
	weekly := policy.OptionalInt(defaultKeepWeekly)
	monthly := policy.OptionalInt(defaultKeepMonthly)
	annual := policy.OptionalInt(defaultKeepAnnual)

	// Global policy — empty SourceInfo means it applies to all sources.
	globalSource := snapshot.SourceInfo{}
	pol := &policy.Policy{
		RetentionPolicy: policy.RetentionPolicy{
			KeepLatest:  &latest,
			KeepDaily:   &daily,
			KeepWeekly:  &weekly,
			KeepMonthly: &monthly,
			KeepAnnual:  &annual,
		},
		FilesPolicy: policy.FilesPolicy{
			IgnoreRules: backupBuiltinExcludes,
		},
	}

	if err := policy.SetPolicy(ctx, wr, globalSource, pol); err != nil {
		return err
	}

	return wr.Flush(ctx)
}

// openLocalRepo opens the local Kopia repository. If the repo doesn't exist
// yet (e.g. user never ran hazmat init), it creates it on the fly.
func openLocalRepo(ctx context.Context) (repo.Repository, error) {
	if _, err := os.Stat(localConfigFile); os.IsNotExist(err) {
		if err := initLocalRepo(); err != nil {
			return nil, fmt.Errorf("auto-init local repo: %w", err)
		}
	}

	r, err := repo.Open(ctx, localConfigFile, localRepoPassword, &repo.Options{})
	if err != nil {
		return nil, fmt.Errorf("open local repo: %w", err)
	}
	return r, nil
}

// ── Snapshot operations ─────────────────────────────────────────────────────

func snapshotIgnoreRules(extra []string) []string {
	cfg, err := loadConfig()
	base := backupBuiltinExcludes
	if err == nil && len(cfg.Backup.Excludes) > 0 {
		base = cfg.Backup.Excludes
	}

	rules := make([]string, 0, len(base)+len(extra))
	seen := make(map[string]struct{}, len(base)+len(extra))
	for _, pat := range append(append([]string{}, base...), extra...) {
		if _, dup := seen[pat]; dup {
			continue
		}
		rules = append(rules, pat)
		seen[pat] = struct{}{}
	}
	return rules
}

// snapshotDir creates a Kopia snapshot of sourcePath in the given repo.
func snapshotDir(ctx context.Context, r repo.DirectRepository, sourcePath, description string, ignoreRules ...string) error {
	ctx, wr, err := r.NewDirectWriter(ctx, repo.WriteSessionOptions{Purpose: "Snapshot"})
	if err != nil {
		return fmt.Errorf("create writer: %w", err)
	}
	defer wr.Close(ctx)

	localEntry, err := localfs.Directory(sourcePath)
	if err != nil {
		return fmt.Errorf("open directory %s: %w", sourcePath, err)
	}

	uploader := upload.NewUploader(wr)

	p := &policy.Policy{
		FilesPolicy: policy.FilesPolicy{
			IgnoreRules: snapshotIgnoreRules(ignoreRules),
		},
	}

	si := localSourceInfo(sourcePath)

	policyTree, err := policy.TreeForSourceWithOverride(ctx, wr, si, p)
	if err != nil {
		return fmt.Errorf("create policy tree: %w", err)
	}

	previous, err := snapshot.ListSnapshots(ctx, wr, si)
	if err != nil {
		previous = nil
	}

	manifest, err := uploader.Upload(ctx, localEntry, policyTree, si, previous...)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	manifest.Description = description
	manifest.EndTime = fs.UTCTimestampFromTime(time.Now())

	if _, err := snapshot.SaveSnapshot(ctx, wr, manifest); err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}

	if err := wr.Flush(ctx); err != nil {
		return fmt.Errorf("flush: %w", err)
	}

	return nil
}

// listSnapshots returns all snapshots for the given source path, newest last.
func listSnapshots(ctx context.Context, r repo.Repository, sourcePath string) ([]*snapshot.Manifest, error) {
	si := localSourceInfo(sourcePath)
	return snapshot.ListSnapshots(ctx, r, si)
}

// restoreSnapshotTo restores a snapshot to destPath.
func restoreSnapshotTo(ctx context.Context, r repo.Repository, manifest *snapshot.Manifest, destPath string) (*restore.Stats, error) {
	rootEntry, err := snapshotfs.SnapshotRoot(r, manifest)
	if err != nil {
		return nil, fmt.Errorf("get snapshot root: %w", err)
	}

	// Remove existing contents so Kopia's shallow restore path doesn't
	// refuse to overwrite existing directories ("cowardly refusing to add
	// placeholder"). OverwriteDirectories only covers non-shallow entries.
	entries, _ := os.ReadDir(destPath)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(destPath, e.Name())) //nolint:errcheck // best-effort pre-restore cleanup; restore handles conflicts
	}
	if err := os.MkdirAll(destPath, 0o770); err != nil {
		return nil, fmt.Errorf("create destination: %w", err)
	}

	output := &restore.FilesystemOutput{
		TargetPath:           destPath,
		OverwriteFiles:       true,
		OverwriteDirectories: true,
	}
	if err := output.Init(ctx); err != nil {
		return nil, fmt.Errorf("init restore output: %w", err)
	}

	stats, err := restore.Entry(ctx, r, output, rootEntry, restore.Options{
		Parallel:              8,
		MinSizeForPlaceholder: 1 << 30, // 1 GiB — avoid .kopia-entry placeholders
	})
	if err != nil {
		return nil, fmt.Errorf("restore: %w", err)
	}

	return &stats, nil
}

// ── Pre-session snapshot (called by session commands) ───────────────────────

// snapshotProject takes a pre-session snapshot of the project directory.
// Returns nil on success. Callers should warn but not block on error.
func snapshotProject(projectDir, command string, ignoreRules ...string) error {
	ctx := context.Background()

	r, err := openLocalRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close(ctx)

	desc := fmt.Sprintf("pre-session (%s)", command)
	return snapshotDir(ctx, r.(repo.DirectRepository), projectDir, desc, ignoreRules...)
}

// ── Cloud backup/restore ────────────────────────────────────────────────────

func openCloudRepo(ctx context.Context) (repo.Repository, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Backup.Cloud == nil {
		return nil, fmt.Errorf("cloud backup not configured\nRun: hazmat config cloud")
	}

	secretKey, err := loadCloudSecretKey()
	if err != nil {
		return nil, err
	}

	cloud := cfg.Backup.Cloud
	endpoint := strings.TrimPrefix(cloud.Endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	useTLS := !strings.HasPrefix(cloud.Endpoint, "http://")

	st, err := s3.New(ctx, &s3.Options{
		Endpoint:        endpoint,
		DoNotUseTLS:     !useTLS,
		BucketName:      cloud.Bucket,
		AccessKeyID:     cloud.AccessKey,
		SecretAccessKey: secretKey,
	}, false)
	if err != nil {
		return nil, fmt.Errorf("connect to S3: %w", err)
	}

	kopiaCloudConfig := filepath.Join(os.Getenv("HOME"), ".hazmat/kopia-cloud.config")

	if _, err := os.Stat(kopiaCloudConfig); os.IsNotExist(err) {
		fmt.Println("Initializing Kopia repository in S3...")
		if err := repo.Initialize(ctx, st, &repo.NewRepositoryOptions{}, cloud.RecoveryKey); err != nil {
			fmt.Printf("Initialization note: %v\n", err)
		}
		if err := repo.Connect(ctx, kopiaCloudConfig, st, cloud.RecoveryKey, &repo.ConnectOptions{}); err != nil {
			return nil, fmt.Errorf("connect to cloud repo: %w", err)
		}
	}

	r, err := repo.Open(ctx, kopiaCloudConfig, cloud.RecoveryKey, &repo.Options{})
	if err != nil {
		return nil, fmt.Errorf("open cloud repo: %w", err)
	}
	return r, nil
}

// updateRetentionFromConfig reads the current config and updates the local
// Kopia repo's retention policy to match.
func updateRetentionFromConfig(cfg HazmatConfig) error {
	ctx := context.Background()
	r, err := openLocalRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close(ctx)

	ctx, wr, err := r.(repo.DirectRepository).NewDirectWriter(ctx, repo.WriteSessionOptions{Purpose: "UpdateRetention"})
	if err != nil {
		return err
	}
	defer wr.Close(ctx)

	ret := cfg.Backup.Local.Retention
	latest := policy.OptionalInt(ret.KeepLatest)
	daily := policy.OptionalInt(ret.KeepDaily)
	weekly := policy.OptionalInt(ret.KeepWeekly)
	zero := policy.OptionalInt(0)

	pol := &policy.Policy{
		RetentionPolicy: policy.RetentionPolicy{
			KeepLatest:  &latest,
			KeepDaily:   &daily,
			KeepWeekly:  &weekly,
			KeepMonthly: &zero,
			KeepAnnual:  &zero,
		},
		FilesPolicy: policy.FilesPolicy{
			IgnoreRules: cfg.Backup.Excludes,
		},
	}

	if err := policy.SetPolicy(ctx, wr, snapshot.SourceInfo{}, pol); err != nil {
		return err
	}

	return wr.Flush(ctx)
}

func runCloudBackup() error {
	ctx := context.Background()

	r, err := openCloudRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close(ctx)

	fmt.Printf("Backing up %s to cloud...\n", cloudBackupDir)
	if err := snapshotDir(ctx, r.(repo.DirectRepository), cloudBackupDir, "Hazmat workspace backup"); err != nil {
		return err
	}

	fmt.Println("Cloud backup complete.")
	return nil
}

func runCloudRestore() error {
	ctx := context.Background()

	r, err := openCloudRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close(ctx)

	snapshots, err := listSnapshots(ctx, r, cloudBackupDir)
	if err != nil || len(snapshots) == 0 {
		return fmt.Errorf("no cloud snapshots found for %s", cloudBackupDir)
	}

	latest := snapshots[len(snapshots)-1]
	fmt.Printf("Restoring latest cloud snapshot from %v...\n", latest.StartTime.ToTime())

	// Snapshot current workspace state before restoring so the restore is
	// reversible. Same pattern as runProjectRestore() in restore.go.
	fmt.Print("  Snapshotting current workspace state... ")
	if err := snapshotProject(cloudBackupDir, "pre-cloud-restore"); err != nil {
		fmt.Fprintf(os.Stderr, "\n  Warning: could not snapshot current state: %v\n", err)
		fmt.Fprintln(os.Stderr, "  Proceeding with restore — current state may not be recoverable.")
	} else {
		fmt.Println("done")
	}

	stats, err := restoreSnapshotTo(ctx, r, latest, cloudBackupDir)
	if err != nil {
		return err
	}

	fmt.Printf("Cloud restore complete. Restored %d files (%d bytes).\n",
		stats.RestoredFileCount, stats.RestoredTotalFileSize)
	return nil
}

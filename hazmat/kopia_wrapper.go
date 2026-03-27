package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kopia/kopia/fs"
	"github.com/kopia/kopia/fs/localfs"
	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/blob"
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
	localRepoDir    = filepath.Join(os.Getenv("HOME"), ".local/share/hazmat/repo")
	localConfigFile = filepath.Join(os.Getenv("HOME"), ".local/share/hazmat/repo.config")
)

// Fixed password for the local repo. This is local-only, protected by
// filesystem permissions. If an attacker can read your home directory
// they can already read your source code directly.
const localRepoPassword = "hazmat-local-snapshots"

// ── Source info ─────────────────────────────────────────────────────────────

func localSourceInfo(sourcePath string) snapshot.SourceInfo {
	return snapshot.SourceInfo{
		Host:     "hazmat",
		UserName: os.Getenv("USER"),
		Path:     sourcePath,
	}
}

// ── Local repo lifecycle ────────────────────────────────────────────────────

// initLocalRepo creates the local Kopia repository. Called during hazmat init.
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
		// Already initialized — not an error.
		if !strings.Contains(err.Error(), "already initialized") {
			return fmt.Errorf("initialize local repo: %w", err)
		}
	}

	if err := repo.Connect(ctx, localConfigFile, st, localRepoPassword, &repo.ConnectOptions{}); err != nil {
		return fmt.Errorf("connect local repo: %w", err)
	}

	return nil
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

// snapshotDir creates a Kopia snapshot of sourcePath in the given repo.
func snapshotDir(ctx context.Context, r repo.DirectRepository, sourcePath, description string) error {
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
			IgnoreRules: backupBuiltinExcludes,
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
func snapshotProject(projectDir, command string) error {
	ctx := context.Background()

	r, err := openLocalRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close(ctx)

	desc := fmt.Sprintf("pre-session (%s)", command)
	return snapshotDir(ctx, r.(repo.DirectRepository), projectDir, desc)
}

// ── Cloud backup/restore ────────────────────────────────────────────────────

func loadCloudConfig() (*CloudConfig, error) {
	data, err := os.ReadFile(cloudBackupConfig)
	if err != nil {
		return nil, fmt.Errorf("could not read cloud backup config: %w\nRun 'hazmat init cloud' first.", err)
	}
	var cfg CloudConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("could not parse cloud backup config: %w", err)
	}
	return &cfg, nil
}

func getS3Storage(ctx context.Context, cfg *CloudConfig) (blob.Storage, error) {
	endpoint := strings.TrimPrefix(cfg.Endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	useTLS := !strings.HasPrefix(cfg.Endpoint, "http://")

	return s3.New(ctx, &s3.Options{
		Endpoint:        endpoint,
		DoNotUseTLS:     !useTLS,
		BucketName:      cfg.Bucket,
		AccessKeyID:     cfg.AccessKey,
		SecretAccessKey: cfg.SecretKey,
	}, false)
}

func openCloudRepo(ctx context.Context) (repo.Repository, error) {
	cfg, err := loadCloudConfig()
	if err != nil {
		return nil, err
	}

	st, err := getS3Storage(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to S3: %w", err)
	}

	configFile := filepath.Join(os.Getenv("HOME"), ".config/hazmat/kopia.config")

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("Initializing Kopia repository in S3...")
		if err := repo.Initialize(ctx, st, &repo.NewRepositoryOptions{}, cfg.Password); err != nil {
			fmt.Printf("Initialization note: %v\n", err)
		}
		if err := repo.Connect(ctx, configFile, st, cfg.Password, &repo.ConnectOptions{}); err != nil {
			return nil, fmt.Errorf("connect to cloud repo: %w", err)
		}
	}

	r, err := repo.Open(ctx, configFile, cfg.Password, &repo.Options{})
	if err != nil {
		return nil, fmt.Errorf("open cloud repo: %w", err)
	}
	return r, nil
}

func runCloudBackup() error {
	ctx := context.Background()

	r, err := openCloudRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close(ctx)

	fmt.Printf("Backing up %s to cloud...\n", sharedWorkspace)
	if err := snapshotDir(ctx, r.(repo.DirectRepository), sharedWorkspace, "Hazmat workspace backup"); err != nil {
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

	snapshots, err := listSnapshots(ctx, r, sharedWorkspace)
	if err != nil || len(snapshots) == 0 {
		return fmt.Errorf("no cloud snapshots found for %s", sharedWorkspace)
	}

	latest := snapshots[len(snapshots)-1]
	fmt.Printf("Restoring latest cloud snapshot from %v...\n", latest.StartTime.ToTime())

	// Snapshot current workspace state before restoring so the restore is
	// reversible. Same pattern as runProjectRestore() in restore.go.
	fmt.Print("  Snapshotting current workspace state... ")
	if err := snapshotProject(sharedWorkspace, "pre-cloud-restore"); err != nil {
		fmt.Fprintf(os.Stderr, "\n  Warning: could not snapshot current state: %v\n", err)
		fmt.Fprintln(os.Stderr, "  Proceeding with restore — current state may not be recoverable.")
	} else {
		fmt.Println("done")
	}

	stats, err := restoreSnapshotTo(ctx, r, latest, sharedWorkspace)
	if err != nil {
		return err
	}

	fmt.Printf("Cloud restore complete. Restored %d files (%d bytes).\n",
		stats.RestoredFileCount, stats.RestoredTotalFileSize)
	return nil
}

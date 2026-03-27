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
	"github.com/kopia/kopia/repo/blob/s3"
	"github.com/kopia/kopia/snapshot"
	"github.com/kopia/kopia/snapshot/policy"
	"github.com/kopia/kopia/snapshot/restore"
	"github.com/kopia/kopia/snapshot/snapshotfs"
	"github.com/kopia/kopia/snapshot/upload"
)

func loadCloudConfig() (*CloudConfig, error) {
	data, err := os.ReadFile(cloudBackupConfig)
	if err != nil {
		return nil, fmt.Errorf("could not read cloud backup config: %w\nRun 'sandbox setup --cloud' first.", err)
	}
	var cfg CloudConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("could not parse cloud backup config: %w", err)
	}
	return &cfg, nil
}

func getS3Storage(ctx context.Context, cfg *CloudConfig) (blob.Storage, error) {
	// minio expects a host-only endpoint (no scheme). Strip https:// or http://
	// so that configs entered with a scheme still work, and DoNotUseTLS controls
	// TLS rather than the URL prefix.
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

func runCloudBackup() error {
	ctx := context.Background()
	cfg, err := loadCloudConfig()
	if err != nil {
		return err
	}

	st, err := getS3Storage(ctx, cfg)
	if err != nil {
		return fmt.Errorf("could not connect to S3 storage: %w", err)
	}

	configFile := filepath.Join(os.Getenv("HOME"), ".config/sandbox/kopia.config")

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("Initializing Kopia repository in S3...")
		if err := repo.Initialize(ctx, st, &repo.NewRepositoryOptions{}, cfg.Password); err != nil {
			fmt.Printf("Initialization note: %v\n", err)
		}
		if err := repo.Connect(ctx, configFile, st, cfg.Password, &repo.ConnectOptions{}); err != nil {
			return fmt.Errorf("could not connect to repository: %w", err)
		}
	}

	r, err := repo.Open(ctx, configFile, cfg.Password, &repo.Options{})
	if err != nil {
		return fmt.Errorf("could not open repository: %w", err)
	}
	defer r.Close(ctx)

	ctx, wr, err := r.(repo.DirectRepository).NewDirectWriter(ctx, repo.WriteSessionOptions{Purpose: "Backup"})
	if err != nil {
		return fmt.Errorf("could not create writer: %w", err)
	}
	defer wr.Close(ctx)

	sourcePath := sharedWorkspace
	localEntry, err := localfs.Directory(sourcePath)
	if err != nil {
		return fmt.Errorf("could not open local directory: %w", err)
	}

	fmt.Printf("Backing up %s to cloud...\n", sourcePath)

	uploader := upload.NewUploader(wr)

	// Configure ignores
	userExcludes, _ := loadUserExcludes()
	excludes := append(backupBuiltinExcludes, userExcludes...)
	
	p := &policy.Policy{
		FilesPolicy: policy.FilesPolicy{
			IgnoreRules: excludes,
		},
	}

	sourceInfo := snapshot.SourceInfo{
		Host:     "sandbox-host",
		UserName: os.Getenv("USER"),
		Path:     sourcePath,
	}

	policyTree, err := policy.TreeForSourceWithOverride(ctx, wr, sourceInfo, p)
	if err != nil {
		return fmt.Errorf("could not create policy tree: %w", err)
	}

	previous, err := snapshot.ListSnapshots(ctx, wr, sourceInfo)
	if err != nil {
		previous = nil
	}

	manifest, err := uploader.Upload(ctx, localEntry, policyTree, sourceInfo, previous...)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	manifest.Description = "Sandbox workspace backup"
	manifest.EndTime = fs.UTCTimestampFromTime(time.Now())

	if _, err := snapshot.SaveSnapshot(ctx, wr, manifest); err != nil {
		return fmt.Errorf("could not save snapshot manifest: %w", err)
	}

	if err := wr.Flush(ctx); err != nil {
		return fmt.Errorf("flush failed: %w", err)
	}

	fmt.Println("Cloud backup complete.")
	return nil
}

func runCloudRestore() error {
	ctx := context.Background()
	cfg, err := loadCloudConfig()
	if err != nil {
		return err
	}

	st, err := getS3Storage(ctx, cfg)
	if err != nil {
		return fmt.Errorf("could not connect to S3 storage: %w", err)
	}

	configFile := filepath.Join(os.Getenv("HOME"), ".config/sandbox/kopia.config")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		if err := repo.Connect(ctx, configFile, st, cfg.Password, &repo.ConnectOptions{}); err != nil {
			return fmt.Errorf("could not connect to repository: %w", err)
		}
	}

	r, err := repo.Open(ctx, configFile, cfg.Password, &repo.Options{})
	if err != nil {
		return fmt.Errorf("could not open repository: %w", err)
	}
	defer r.Close(ctx)

	sourceInfo := snapshot.SourceInfo{
		Host:     "sandbox-host",
		UserName: os.Getenv("USER"),
		Path:     sharedWorkspace,
	}

	snapshots, err := snapshot.ListSnapshots(ctx, r, sourceInfo)
	if err != nil || len(snapshots) == 0 {
		return fmt.Errorf("no snapshots found for %s", sharedWorkspace)
	}

	// Use the latest snapshot
	latest := snapshots[len(snapshots)-1]
	fmt.Printf("Restoring latest snapshot from %v...\n", latest.StartTime.ToTime())

	rootEntry, err := snapshotfs.SnapshotRoot(r, latest)
	if err != nil {
		return fmt.Errorf("could not get snapshot root: %w", err)
	}

	destPath := sharedWorkspace
	
	// Create destination if it doesn't exist
	if err := os.MkdirAll(destPath, 0o770); err != nil {
		return fmt.Errorf("could not create destination directory: %w", err)
	}

	output := &restore.FilesystemOutput{
		TargetPath:           destPath,
		OverwriteFiles:       true,
		OverwriteDirectories: true,
	}
	if err := output.Init(ctx); err != nil {
		return fmt.Errorf("could not initialize restore output: %w", err)
	}

	// MinSizeForPlaceholder must exceed any file size to avoid shallow
	// .kopia-entry placeholder files (kopia default creates placeholders
	// for all files when MinSizeForPlaceholder == 0).
	stats, err := restore.Entry(ctx, r, output, rootEntry, restore.Options{
		Parallel:              8,
		MinSizeForPlaceholder: 1 << 30, // 1 GiB
	})
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	fmt.Printf("Cloud restore complete. Restored %d files (%d bytes).\n", stats.RestoredFileCount, stats.RestoredTotalFileSize)
	return nil
}

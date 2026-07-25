package planescapeprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	checkpointDirectoryMode = 0o700
	checkpointFileMode      = 0o600
)

// fileStore persists client checkpoints in a private directory. Keys are
// constructor-validated provider identifiers and become fixed-size opaque file
// names, so callers cannot select paths within the checkpoint root.
type fileStore struct {
	root string
}

var _ CheckpointStore = (*fileStore)(nil)

// NewFileStore returns a durable checkpoint store rooted at an absolute path.
// The directory is created lazily on the first save so constructing product
// wiring does not itself mutate host state.
func NewFileStore(root string) (CheckpointStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("planescapeprovider: checkpoint root is required")
	}
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("planescapeprovider: checkpoint root must be absolute")
	}
	if _, exists, err := inspectCheckpointRoot(root); err != nil {
		return nil, err
	} else if !exists {
		parent, err := os.Stat(filepath.Dir(root))
		if err != nil {
			return nil, fmt.Errorf("planescapeprovider: inspect checkpoint root parent: %w", err)
		}
		if !parent.IsDir() {
			return nil, fmt.Errorf("planescapeprovider: checkpoint root parent must be a directory")
		}
	}
	return &fileStore{root: root}, nil
}

func (s *fileStore) Load(ctx context.Context, key string) ([]byte, bool, error) {
	if err := validateCheckpointContext(ctx); err != nil {
		return nil, false, err
	}
	filename, err := checkpointFilename(key)
	if err != nil {
		return nil, false, err
	}
	if s == nil || s.root == "" {
		return nil, false, fmt.Errorf("planescapeprovider: checkpoint store is unavailable")
	}
	if _, exists, err := inspectCheckpointRoot(s.root); err != nil {
		return nil, false, err
	} else if !exists {
		return nil, false, nil
	}

	path := filepath.Join(s.root, filename)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("planescapeprovider: inspect checkpoint: %w", err)
	}
	if err := validateCheckpointFile(info); err != nil {
		return nil, false, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("planescapeprovider: open checkpoint: %w", err)
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, false, fmt.Errorf("planescapeprovider: inspect open checkpoint: %w", statErr)
	}
	if !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, false, fmt.Errorf("planescapeprovider: checkpoint changed while opening")
	}

	data, readErr := io.ReadAll(io.LimitReader(file, MaxRecordBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, false, fmt.Errorf("planescapeprovider: read checkpoint: %w", readErr)
	}
	if closeErr != nil {
		return nil, false, fmt.Errorf("planescapeprovider: close checkpoint: %w", closeErr)
	}
	if len(data) == 0 || len(data) > MaxRecordBytes {
		return nil, false, fmt.Errorf("planescapeprovider: checkpoint size is invalid")
	}
	if err := validateCheckpointContext(ctx); err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (s *fileStore) Save(ctx context.Context, key string, value []byte) error {
	if err := validateCheckpointContext(ctx); err != nil {
		return err
	}
	filename, err := checkpointFilename(key)
	if err != nil {
		return err
	}
	if s == nil || s.root == "" {
		return fmt.Errorf("planescapeprovider: checkpoint store is unavailable")
	}
	if len(value) == 0 || len(value) > MaxRecordBytes {
		return fmt.Errorf("planescapeprovider: checkpoint size is invalid")
	}
	value = append([]byte(nil), value...)

	if err := ensureCheckpointRoot(s.root); err != nil {
		return err
	}

	temp, err := os.CreateTemp(s.root, ".checkpoint-*")
	if err != nil {
		return fmt.Errorf("planescapeprovider: create checkpoint temp file: %w", err)
	}
	tempPath := temp.Name()
	renamed := false
	defer func() {
		_ = temp.Close()
		if !renamed {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(checkpointFileMode); err != nil {
		return fmt.Errorf("planescapeprovider: set checkpoint permissions: %w", err)
	}
	written, err := temp.Write(value)
	if err != nil {
		return fmt.Errorf("planescapeprovider: write checkpoint: %w", err)
	}
	if written != len(value) {
		return fmt.Errorf("planescapeprovider: short checkpoint write")
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("planescapeprovider: sync checkpoint: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("planescapeprovider: close checkpoint: %w", err)
	}
	if err := validateCheckpointContext(ctx); err != nil {
		return err
	}

	if err := os.Rename(tempPath, filepath.Join(s.root, filename)); err != nil {
		return fmt.Errorf("planescapeprovider: replace checkpoint: %w", err)
	}
	renamed = true
	if err := syncCheckpointDirectory(s.root); err != nil {
		return err
	}
	return nil
}

func checkpointFilename(key string) (string, error) {
	if _, err := NewIdentifier(key); err != nil {
		return "", fmt.Errorf("planescapeprovider: invalid checkpoint key: %w", err)
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:]) + ".json", nil
}

func inspectCheckpointRoot(root string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("planescapeprovider: inspect checkpoint root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("planescapeprovider: checkpoint root must be a directory")
	}
	if info.Mode().Perm() != checkpointDirectoryMode {
		return nil, false, fmt.Errorf("planescapeprovider: checkpoint root permissions must be 0700")
	}
	return info, true, nil
}

func ensureCheckpointRoot(root string) error {
	if _, exists, err := inspectCheckpointRoot(root); err != nil {
		return err
	} else if exists {
		return nil
	}
	if err := os.Mkdir(root, checkpointDirectoryMode); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("planescapeprovider: create checkpoint root: %w", err)
	}
	if _, exists, err := inspectCheckpointRoot(root); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("planescapeprovider: checkpoint root was not created")
	}
	return syncCheckpointDirectory(filepath.Dir(root))
}

func validateCheckpointFile(info os.FileInfo) error {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("planescapeprovider: checkpoint must be a regular file")
	}
	if info.Mode().Perm() != checkpointFileMode {
		return fmt.Errorf("planescapeprovider: checkpoint permissions must be 0600")
	}
	return nil
}

func syncCheckpointDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("planescapeprovider: open checkpoint directory: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return fmt.Errorf("planescapeprovider: sync checkpoint directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("planescapeprovider: close checkpoint directory: %w", closeErr)
	}
	return nil
}

func validateCheckpointContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("planescapeprovider: checkpoint context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("planescapeprovider: checkpoint operation cancelled: %w", err)
	}
	return nil
}

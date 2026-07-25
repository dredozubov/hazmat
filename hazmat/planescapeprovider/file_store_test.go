package planescapeprovider

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTripUsesPrivateAtomicFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "provider-checkpoints")
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("checkpoint root exists before first save: %v", err)
	}
	value := []byte(`{"schema":"checkpoint-v1"}`)
	if err := store.Save(context.Background(), "session-1", value); err != nil {
		t.Fatal(err)
	}
	value[0] = '!'

	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := rootInfo.Mode().Perm(); got != checkpointDirectoryMode {
		t.Fatalf("checkpoint root mode = %o, want %o", got, checkpointDirectoryMode)
	}
	filename, err := checkpointFilename("session-1")
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Lstat(filepath.Join(root, filename))
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != checkpointFileMode {
		t.Fatalf("checkpoint mode = %o, want %o", got, checkpointFileMode)
	}
	if matches, err := filepath.Glob(filepath.Join(root, ".checkpoint-*")); err != nil {
		t.Fatal(err)
	} else if len(matches) != 0 {
		t.Fatalf("temporary checkpoints remain after save: %v", matches)
	}

	got, ok, err := store.Load(context.Background(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !bytes.Equal(got, []byte(`{"schema":"checkpoint-v1"}`)) {
		t.Fatalf("Load = %q, %v", got, ok)
	}
	got[0] = '!'
	reloaded, ok, err := store.Load(context.Background(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !bytes.Equal(reloaded, []byte(`{"schema":"checkpoint-v1"}`)) {
		t.Fatalf("second Load = %q, %v", reloaded, ok)
	}
	if _, ok, err := store.Load(context.Background(), "session-missing"); err != nil || ok {
		t.Fatalf("missing Load = %v, %v", ok, err)
	}
}

func TestFileStoreRejectsInvalidWritesWithoutClobberingCheckpoint(t *testing.T) {
	root := filepath.Join(t.TempDir(), "provider-checkpoints")
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"schema":"checkpoint-v1"}`)
	if err := store.Save(context.Background(), "session-1", original); err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, test := range map[string]struct {
		key   string
		value []byte
		ctx   context.Context
	}{
		"empty key":       {key: "", value: []byte("{}"), ctx: context.Background()},
		"empty value":     {key: "session-1", value: nil, ctx: context.Background()},
		"oversized value": {key: "session-1", value: make([]byte, MaxRecordBytes+1), ctx: context.Background()},
		"cancelled":       {key: "session-1", value: []byte(`{"replacement":true}`), ctx: cancelled},
	} {
		t.Run(name, func(t *testing.T) {
			if err := store.Save(test.ctx, test.key, test.value); err == nil {
				t.Fatal("invalid checkpoint save succeeded")
			}
			got, ok, err := store.Load(context.Background(), "session-1")
			if err != nil {
				t.Fatal(err)
			}
			if !ok || !bytes.Equal(got, original) {
				t.Fatalf("checkpoint after failed save = %q, %v", got, ok)
			}
		})
	}
}

func TestFileStoreRejectsUnsafeRootsAndCheckpointFiles(t *testing.T) {
	if _, err := NewFileStore("relative/checkpoints"); err == nil {
		t.Fatal("relative checkpoint root succeeded")
	}

	parent := t.TempDir()
	if _, err := NewFileStore(filepath.Join(parent, "missing", "checkpoints")); err == nil {
		t.Fatal("checkpoint root with missing parent succeeded")
	}
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, checkpointDirectoryMode); err != nil {
		t.Fatal(err)
	}
	rootLink := filepath.Join(parent, "root-link")
	if err := os.Symlink(target, rootLink); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(rootLink); err == nil {
		t.Fatal("symlink checkpoint root succeeded")
	}

	root := filepath.Join(parent, "checkpoints")
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "session-1", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	filename, err := checkpointFilename("session-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, mustCheckpointFilename(t, "session-1")), filepath.Join(root, filename)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(context.Background(), "session-2"); err == nil {
		t.Fatal("symlink checkpoint file loaded")
	}

	if err := os.Chmod(filepath.Join(root, mustCheckpointFilename(t, "session-1")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(context.Background(), "session-1"); err == nil {
		t.Fatal("non-private checkpoint file loaded")
	}
}

func mustCheckpointFilename(t *testing.T, key string) string {
	t.Helper()
	filename, err := checkpointFilename(key)
	if err != nil {
		t.Fatal(err)
	}
	return filename
}

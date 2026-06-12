package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeSessionHomeCheckedWritebackEntriesCopiesAndReceipts(t *testing.T) {
	layout := newTestSessionHomeLayout(t)
	persistentHome := filepath.Join(t.TempDir(), "agent")
	entry := testCheckedWritebackEntry(layout, persistentHome, ".config/tool/state.json")
	writeSessionHomeTestFile(t, entry.PersistentPath, "original\n", 0o640)

	receipts, err := materializeSessionHomeCheckedWritebackEntries(layout, []sessionHomeAssemblyEntry{entry})
	if err != nil {
		t.Fatalf("materializeSessionHomeCheckedWritebackEntries: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("receipts len = %d, want 1", len(receipts))
	}
	receipt := receipts[0]
	if receipt.Outcome != sessionHomeWritebackCopiedIn ||
		receipt.RelPath != entry.RelPath ||
		receipt.PersistentPath != entry.PersistentPath ||
		receipt.RuntimePath != entry.RuntimePath ||
		!strings.HasPrefix(receipt.SourceFingerprint, "file:sha256:") {
		t.Fatalf("receipt = %+v", receipt)
	}
	assertTestFile(t, entry.RuntimePath, "original\n")
	if info, err := os.Stat(entry.RuntimePath); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("runtime mode err=%v info=%v", err, info)
	}
}

func TestMaterializeSessionHomeCheckedWritebackEntriesRecordsMissingSource(t *testing.T) {
	layout := newTestSessionHomeLayout(t)
	persistentHome := filepath.Join(t.TempDir(), "agent")
	entry := testCheckedWritebackEntry(layout, persistentHome, ".config/tool/missing.json")

	receipts, err := materializeSessionHomeCheckedWritebackEntries(layout, []sessionHomeAssemblyEntry{entry})
	if err != nil {
		t.Fatalf("materializeSessionHomeCheckedWritebackEntries: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("receipts len = %d, want 1", len(receipts))
	}
	if receipts[0].Outcome != sessionHomeWritebackSourceMissing ||
		receipts[0].SourceFingerprint != sessionHomePathFingerprintAbsent {
		t.Fatalf("receipt = %+v", receipts[0])
	}
	if _, err := os.Lstat(entry.RuntimePath); !os.IsNotExist(err) {
		t.Fatalf("runtime path should stay absent, err=%v", err)
	}
}

func TestWriteBackSessionHomeCheckedWritebackReceiptsWritesFileWhenSourceUnchanged(t *testing.T) {
	layout := newTestSessionHomeLayout(t)
	persistentHome := filepath.Join(t.TempDir(), "agent")
	entry := testCheckedWritebackEntry(layout, persistentHome, ".config/tool/state.json")
	writeSessionHomeTestFile(t, entry.PersistentPath, "original\n", 0o640)
	receipts, err := materializeSessionHomeCheckedWritebackEntries(layout, []sessionHomeAssemblyEntry{entry})
	if err != nil {
		t.Fatalf("materializeSessionHomeCheckedWritebackEntries: %v", err)
	}
	writeSessionHomeTestFile(t, entry.RuntimePath, "session change\n", 0o600)

	results, err := writeBackSessionHomeCheckedWritebackReceipts(layout, receipts)
	if err != nil {
		t.Fatalf("writeBackSessionHomeCheckedWritebackReceipts: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != sessionHomeWritebackWritten {
		t.Fatalf("results = %+v", results)
	}
	assertTestFile(t, entry.PersistentPath, "session change\n")
	if results[0].PersistentFingerprint != results[0].RuntimeFingerprint ||
		results[0].PersistentFingerprint == receipts[0].SourceFingerprint {
		t.Fatalf("fingerprints after writeback = %+v, source was %s", results[0], receipts[0].SourceFingerprint)
	}
}

func TestWriteBackSessionHomeCheckedWritebackReceiptsCreatesMissingPersistentSource(t *testing.T) {
	layout := newTestSessionHomeLayout(t)
	persistentHome := filepath.Join(t.TempDir(), "agent")
	entry := testCheckedWritebackEntry(layout, persistentHome, ".config/tool/generated.json")
	receipts, err := materializeSessionHomeCheckedWritebackEntries(layout, []sessionHomeAssemblyEntry{entry})
	if err != nil {
		t.Fatalf("materializeSessionHomeCheckedWritebackEntries: %v", err)
	}
	writeSessionHomeTestFile(t, entry.RuntimePath, "generated\n", 0o600)

	results, err := writeBackSessionHomeCheckedWritebackReceipts(layout, receipts)
	if err != nil {
		t.Fatalf("writeBackSessionHomeCheckedWritebackReceipts: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != sessionHomeWritebackWritten {
		t.Fatalf("results = %+v", results)
	}
	assertTestFile(t, entry.PersistentPath, "generated\n")
}

func TestWriteBackSessionHomeCheckedWritebackReceiptsWritesDirectoryWhenSourceUnchanged(t *testing.T) {
	layout := newTestSessionHomeLayout(t)
	persistentHome := filepath.Join(t.TempDir(), "agent")
	entry := testCheckedWritebackEntry(layout, persistentHome, ".config/tool")
	writeSessionHomeTestFile(t, filepath.Join(entry.PersistentPath, "state.json"), "original\n", 0o600)
	receipts, err := materializeSessionHomeCheckedWritebackEntries(layout, []sessionHomeAssemblyEntry{entry})
	if err != nil {
		t.Fatalf("materializeSessionHomeCheckedWritebackEntries: %v", err)
	}
	writeSessionHomeTestFile(t, filepath.Join(entry.RuntimePath, "state.json"), "session change\n", 0o600)
	writeSessionHomeTestFile(t, filepath.Join(entry.RuntimePath, "new.json"), "new\n", 0o600)

	results, err := writeBackSessionHomeCheckedWritebackReceipts(layout, receipts)
	if err != nil {
		t.Fatalf("writeBackSessionHomeCheckedWritebackReceipts: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != sessionHomeWritebackWritten {
		t.Fatalf("results = %+v", results)
	}
	assertTestFile(t, filepath.Join(entry.PersistentPath, "state.json"), "session change\n")
	assertTestFile(t, filepath.Join(entry.PersistentPath, "new.json"), "new\n")
}

func TestWriteBackSessionHomeCheckedWritebackReceiptsPreservesRecoveryOnConflict(t *testing.T) {
	layout := newTestSessionHomeLayout(t)
	persistentHome := filepath.Join(t.TempDir(), "agent")
	entry := testCheckedWritebackEntry(layout, persistentHome, ".config/tool/state.json")
	writeSessionHomeTestFile(t, entry.PersistentPath, "original\n", 0o600)
	receipts, err := materializeSessionHomeCheckedWritebackEntries(layout, []sessionHomeAssemblyEntry{entry})
	if err != nil {
		t.Fatalf("materializeSessionHomeCheckedWritebackEntries: %v", err)
	}
	writeSessionHomeTestFile(t, entry.PersistentPath, "host change\n", 0o600)
	writeSessionHomeTestFile(t, entry.RuntimePath, "session change\n", 0o600)

	results, err := writeBackSessionHomeCheckedWritebackReceipts(layout, receipts)
	if err != nil {
		t.Fatalf("writeBackSessionHomeCheckedWritebackReceipts: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != sessionHomeWritebackConflict || results[0].RecoveryPath == "" {
		t.Fatalf("results = %+v", results)
	}
	assertTestFile(t, entry.PersistentPath, "host change\n")
	assertTestFile(t, results[0].RecoveryPath, "session change\n")
}

func TestWriteBackSessionHomeCheckedWritebackReceiptsRejectsRuntimeSymlink(t *testing.T) {
	layout := newTestSessionHomeLayout(t)
	persistentHome := filepath.Join(t.TempDir(), "agent")
	entry := testCheckedWritebackEntry(layout, persistentHome, ".config/tool/state.json")
	writeSessionHomeTestFile(t, entry.PersistentPath, "original\n", 0o600)
	receipts, err := materializeSessionHomeCheckedWritebackEntries(layout, []sessionHomeAssemblyEntry{entry})
	if err != nil {
		t.Fatalf("materializeSessionHomeCheckedWritebackEntries: %v", err)
	}
	if err := os.Remove(entry.RuntimePath); err != nil {
		t.Fatalf("remove runtime path: %v", err)
	}
	if err := os.Symlink(entry.PersistentPath, entry.RuntimePath); err != nil {
		t.Fatalf("symlink runtime path: %v", err)
	}

	_, err = writeBackSessionHomeCheckedWritebackReceipts(layout, receipts)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("writeBackSessionHomeCheckedWritebackReceipts err = %v, want symlink rejection", err)
	}
	assertTestFile(t, entry.PersistentPath, "original\n")
}

func TestWriteBackSessionHomeCheckedWritebackReceiptsRejectsNestedRuntimeSymlink(t *testing.T) {
	layout := newTestSessionHomeLayout(t)
	persistentHome := filepath.Join(t.TempDir(), "agent")
	entry := testCheckedWritebackEntry(layout, persistentHome, ".config/tool")
	writeSessionHomeTestFile(t, filepath.Join(entry.PersistentPath, "state.json"), "original\n", 0o600)
	receipts, err := materializeSessionHomeCheckedWritebackEntries(layout, []sessionHomeAssemblyEntry{entry})
	if err != nil {
		t.Fatalf("materializeSessionHomeCheckedWritebackEntries: %v", err)
	}
	if err := os.Symlink(filepath.Join(entry.PersistentPath, "state.json"), filepath.Join(entry.RuntimePath, "link.json")); err != nil {
		t.Fatalf("symlink nested runtime path: %v", err)
	}

	_, err = writeBackSessionHomeCheckedWritebackReceipts(layout, receipts)
	if err == nil || !strings.Contains(err.Error(), "nested symlink") {
		t.Fatalf("writeBackSessionHomeCheckedWritebackReceipts err = %v, want nested symlink rejection", err)
	}
	assertTestFile(t, filepath.Join(entry.PersistentPath, "state.json"), "original\n")
}

func newTestSessionHomeLayout(t *testing.T) sessionHomeLayout {
	t.Helper()
	layout, err := newSessionHomeLayout(filepath.Join(t.TempDir(), "hazmat-home"), "session-123")
	if err != nil {
		t.Fatal(err)
	}
	if err := createSessionHomeLayout(layout); err != nil {
		t.Fatalf("createSessionHomeLayout: %v", err)
	}
	return layout
}

func testCheckedWritebackEntry(layout sessionHomeLayout, persistentHome, rel string) sessionHomeAssemblyEntry {
	return sessionHomeAssemblyEntry{
		RelPath:        rel,
		RuntimePolicy:  sessionHomePolicyCheckedWriteback,
		PersistentPath: filepath.Join(persistentHome, filepath.FromSlash(rel)),
		RuntimePath:    filepath.Join(layout.Home, filepath.FromSlash(rel)),
	}
}

func writeSessionHomeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func assertTestFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

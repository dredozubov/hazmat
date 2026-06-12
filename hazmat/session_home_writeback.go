package hazmat

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const sessionHomePathFingerprintAbsent = "absent"

type sessionHomeWritebackOutcome string

const (
	sessionHomeWritebackCopiedIn       sessionHomeWritebackOutcome = "copied-in"
	sessionHomeWritebackSourceMissing  sessionHomeWritebackOutcome = "source-missing"
	sessionHomeWritebackUnchanged      sessionHomeWritebackOutcome = "unchanged"
	sessionHomeWritebackWritten        sessionHomeWritebackOutcome = "written"
	sessionHomeWritebackConflict       sessionHomeWritebackOutcome = "conflict"
	sessionHomeWritebackRuntimeMissing sessionHomeWritebackOutcome = "runtime-missing"
)

type sessionHomeCheckedWritebackReceipt struct {
	RelPath               string
	PersistentPath        string
	RuntimePath           string
	SourceFingerprint     string
	PersistentFingerprint string
	RuntimeFingerprint    string
	Outcome               sessionHomeWritebackOutcome
	RecoveryPath          string
}

func materializeSessionHomeCheckedWritebackEntries(layout sessionHomeLayout, assembly []sessionHomeAssemblyEntry) ([]sessionHomeCheckedWritebackReceipt, error) {
	var receipts []sessionHomeCheckedWritebackReceipt
	for _, entry := range assembly {
		if entry.RuntimePolicy != sessionHomePolicyCheckedWriteback {
			continue
		}
		if err := validateSessionHomeAssemblyEntry(layout, entry); err != nil {
			return nil, err
		}
		fingerprint, err := fingerprintSessionHomeWritebackPath(entry.PersistentPath)
		if err != nil {
			return nil, fmt.Errorf("%s: fingerprint persistent source: %w", entry.RelPath, err)
		}
		receipt := sessionHomeCheckedWritebackReceipt{
			RelPath:           entry.RelPath,
			PersistentPath:    entry.PersistentPath,
			RuntimePath:       entry.RuntimePath,
			SourceFingerprint: fingerprint,
			Outcome:           sessionHomeWritebackCopiedIn,
		}
		if fingerprint == sessionHomePathFingerprintAbsent {
			receipt.Outcome = sessionHomeWritebackSourceMissing
			receipts = append(receipts, receipt)
			continue
		}
		info, err := os.Lstat(entry.PersistentPath)
		if err != nil {
			return nil, fmt.Errorf("%s: inspect checked-writeback source: %w", entry.RelPath, err)
		}
		if err := copySessionHomeSeedPath(layout, entry.PersistentPath, entry.RuntimePath, info); err != nil {
			return nil, err
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

func writeBackSessionHomeCheckedWritebackReceipts(layout sessionHomeLayout, receipts []sessionHomeCheckedWritebackReceipt) ([]sessionHomeCheckedWritebackReceipt, error) {
	results := make([]sessionHomeCheckedWritebackReceipt, 0, len(receipts))
	for _, receipt := range receipts {
		if err := validateSessionHomeWritebackReceipt(layout, receipt); err != nil {
			return nil, err
		}
		persistentFingerprint, err := fingerprintSessionHomeWritebackPath(receipt.PersistentPath)
		if err != nil {
			return nil, fmt.Errorf("%s: fingerprint persistent source before writeback: %w", receipt.RelPath, err)
		}
		runtimeFingerprint, err := fingerprintSessionHomeWritebackPath(receipt.RuntimePath)
		if err != nil {
			return nil, fmt.Errorf("%s: fingerprint runtime source before writeback: %w", receipt.RelPath, err)
		}
		updated := receipt
		updated.PersistentFingerprint = persistentFingerprint
		updated.RuntimeFingerprint = runtimeFingerprint

		sessionChanged := runtimeFingerprint != receipt.SourceFingerprint
		sourceChanged := persistentFingerprint != receipt.SourceFingerprint
		switch {
		case !sessionChanged:
			updated.Outcome = sessionHomeWritebackUnchanged
		case sourceChanged:
			recoveryPath, err := preserveSessionHomeWritebackRecovery(layout, updated)
			if err != nil {
				return nil, err
			}
			updated.Outcome = sessionHomeWritebackConflict
			updated.RecoveryPath = recoveryPath
		case runtimeFingerprint == sessionHomePathFingerprintAbsent:
			updated.Outcome = sessionHomeWritebackRuntimeMissing
		default:
			if err := replaceSessionHomePersistentPathFromRuntime(receipt.RuntimePath, receipt.PersistentPath); err != nil {
				return nil, fmt.Errorf("%s: write back session home path: %w", receipt.RelPath, err)
			}
			persistentFingerprint, err := fingerprintSessionHomeWritebackPath(receipt.PersistentPath)
			if err != nil {
				return nil, fmt.Errorf("%s: fingerprint persistent source after writeback: %w", receipt.RelPath, err)
			}
			updated.PersistentFingerprint = persistentFingerprint
			updated.Outcome = sessionHomeWritebackWritten
		}
		results = append(results, updated)
	}
	return results, nil
}

func validateSessionHomeWritebackReceipt(layout sessionHomeLayout, receipt sessionHomeCheckedWritebackReceipt) error {
	if strings.TrimSpace(receipt.SourceFingerprint) == "" {
		return fmt.Errorf("%s: checked-writeback receipt is missing source fingerprint", receipt.RelPath)
	}
	entry := sessionHomeAssemblyEntry{
		RelPath:        receipt.RelPath,
		PersistentPath: receipt.PersistentPath,
		RuntimePath:    receipt.RuntimePath,
	}
	if err := validateSessionHomeAssemblyEntry(layout, entry); err != nil {
		return err
	}
	if _, err := sessionHomeRecoveryPath(layout, receipt.RelPath); err != nil {
		return err
	}
	return nil
}

func fingerprintSessionHomeWritebackPath(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return sessionHomePathFingerprintAbsent, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s: checked-writeback symlinks are not supported", path)
	}
	hasher := sha256.New()
	switch {
	case info.Mode().IsRegular():
		if err := hashSessionHomeWritebackFile(hasher, path, ".", info); err != nil {
			return "", err
		}
		return "file:sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
	case info.IsDir():
		if err := hashSessionHomeWritebackDir(hasher, path, "."); err != nil {
			return "", err
		}
		return "dir:sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
	default:
		return "", fmt.Errorf("%s: unsupported checked-writeback source type %s", path, info.Mode().String())
	}
}

func hashSessionHomeWritebackDir(hasher hash.Hash, path, rel string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: checked-writeback nested symlink is not supported", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: checked-writeback source is not a directory", path)
	}
	if _, err := fmt.Fprintf(hasher, "dir\x00%s\x00%o\x00", filepath.ToSlash(rel), info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		childRel := filepath.Join(rel, entry.Name())
		childInfo, err := os.Lstat(child)
		if err != nil {
			return err
		}
		if childInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: checked-writeback nested symlink is not supported", child)
		}
		switch {
		case childInfo.Mode().IsRegular():
			if err := hashSessionHomeWritebackFile(hasher, child, childRel, childInfo); err != nil {
				return err
			}
		case childInfo.IsDir():
			if err := hashSessionHomeWritebackDir(hasher, child, childRel); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: unsupported checked-writeback source type %s", child, childInfo.Mode().String())
		}
	}
	return nil
}

func hashSessionHomeWritebackFile(hasher hash.Hash, path, rel string, info os.FileInfo) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(hasher, "file\x00%s\x00%o\x00", filepath.ToSlash(rel), info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := hasher.Write(raw); err != nil {
		return err
	}
	return nil
}

func preserveSessionHomeWritebackRecovery(layout sessionHomeLayout, receipt sessionHomeCheckedWritebackReceipt) (string, error) {
	if receipt.RuntimeFingerprint == sessionHomePathFingerprintAbsent {
		return "", nil
	}
	info, err := os.Lstat(receipt.RuntimePath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%s: inspect runtime recovery source: %w", receipt.RelPath, err)
	}
	recoveryPath, err := sessionHomeRecoveryPath(layout, receipt.RelPath)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(recoveryPath); err == nil {
		recoveryPath = fmt.Sprintf("%s.conflict-%d", recoveryPath, time.Now().UnixNano())
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("%s: inspect checked-writeback recovery path: %w", recoveryPath, err)
	}
	if err := copySessionHomePathOutsideSession(receipt.RuntimePath, recoveryPath, info); err != nil {
		return "", fmt.Errorf("%s: preserve checked-writeback recovery: %w", receipt.RelPath, err)
	}
	return recoveryPath, nil
}

func sessionHomeRecoveryPath(layout sessionHomeLayout, rel string) (string, error) {
	sessionDir := filepath.Clean(layout.SessionDir)
	if sessionDir == "" || !filepath.IsAbs(sessionDir) {
		return "", fmt.Errorf("session home directory %q must be absolute", layout.SessionDir)
	}
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if cleanRel == "." || filepath.IsAbs(cleanRel) || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%s: invalid checked-writeback recovery rel path", rel)
	}
	recoveryRoot := filepath.Join(sessionDir, "writeback-recovery")
	recoveryPath := filepath.Join(recoveryRoot, cleanRel)
	if !isWithinDir(recoveryRoot, recoveryPath) {
		return "", fmt.Errorf("%s: checked-writeback recovery path escapes %s", rel, recoveryRoot)
	}
	return recoveryPath, nil
}

func replaceSessionHomePersistentPathFromRuntime(runtimePath, persistentPath string) error {
	info, err := os.Lstat(runtimePath)
	if err != nil {
		return err
	}
	parent := filepath.Dir(persistentPath)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create checked-writeback parent %s: %w", parent, err)
	}
	tempPath, err := copySessionHomePathToPersistentTemp(runtimePath, parent, filepath.Base(persistentPath), info)
	if err != nil {
		return err
	}
	if err := replaceSessionHomePersistentPath(tempPath, persistentPath); err != nil {
		_ = removeSessionHomePath(tempPath, info)
		return err
	}
	return nil
}

func copySessionHomePathToPersistentTemp(src, parent, base string, info os.FileInfo) (string, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s: checked-writeback symlinks are not supported", src)
	}
	if info.Mode().IsRegular() {
		file, err := os.CreateTemp(parent, "."+base+".hazmat-writeback-*")
		if err != nil {
			return "", err
		}
		tempPath := file.Name()
		if err := file.Close(); err != nil {
			_ = os.Remove(tempPath)
			return "", err
		}
		if err := copySessionHomeFileOutsideSession(src, tempPath, info); err != nil {
			_ = os.Remove(tempPath)
			return "", err
		}
		return tempPath, nil
	}
	if info.IsDir() {
		tempPath, err := os.MkdirTemp(parent, "."+base+".hazmat-writeback-*")
		if err != nil {
			return "", err
		}
		if err := copySessionHomeDirOutsideSession(src, tempPath, info); err != nil {
			_ = os.RemoveAll(tempPath)
			return "", err
		}
		return tempPath, nil
	}
	return "", fmt.Errorf("%s: unsupported checked-writeback source type %s", src, info.Mode().String())
}

func replaceSessionHomePersistentPath(tempPath, destPath string) error {
	if info, err := os.Lstat(destPath); os.IsNotExist(err) {
		return os.Rename(tempPath, destPath)
	} else if err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: checked-writeback destination symlinks are not supported", destPath)
	}

	backupPath := filepath.Join(filepath.Dir(destPath), "."+filepath.Base(destPath)+".hazmat-writeback-old-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.Rename(destPath, backupPath); err != nil {
		return err
	}
	if err := os.Rename(tempPath, destPath); err != nil {
		_ = os.Rename(backupPath, destPath)
		return err
	}
	return os.RemoveAll(backupPath)
}

func copySessionHomePathOutsideSession(src, dest string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: checked-writeback symlinks are not supported", src)
	}
	switch {
	case info.Mode().IsRegular():
		return copySessionHomeFileOutsideSession(src, dest, info)
	case info.IsDir():
		return copySessionHomeDirOutsideSession(src, dest, info)
	default:
		return fmt.Errorf("%s: unsupported checked-writeback source type %s", src, info.Mode().String())
	}
}

func copySessionHomeFileOutsideSession(src, dest string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dest, data, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Chmod(dest, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Chtimes(dest, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	return nil
}

func copySessionHomeDirOutsideSession(src, dest string, info os.FileInfo) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		childSrc := filepath.Join(src, entry.Name())
		childDest := filepath.Join(dest, entry.Name())
		childInfo, err := os.Lstat(childSrc)
		if err != nil {
			return err
		}
		if err := copySessionHomePathOutsideSession(childSrc, childDest, childInfo); err != nil {
			return err
		}
	}
	if err := os.Chmod(dest, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Chtimes(dest, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	return nil
}

func removeSessionHomePath(path string, info os.FileInfo) error {
	if info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

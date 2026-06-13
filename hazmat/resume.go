package hazmat

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// sanitizePathForClaude replicates Claude Code's sanitizePath function.
// It replaces all non-alphanumeric characters with hyphens, matching:
//
//	src/utils/sessionStoragePortable.ts → sanitizePath()
//
// For paths exceeding 200 characters after sanitization, Claude Code
// truncates and appends a hash suffix. We handle that case via prefix
// matching in invokerSessionDir.
func sanitizePathForClaude(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]`)
	return re.ReplaceAllString(name, "-")
}

const maxSanitizedLength = 200

var resumeAgentEnsureDir = agentEnsureDir

// invokerHome returns the home directory of the user who invoked hazmat
// (the real user, not the agent).
func invokerHome() string {
	return os.Getenv("HOME")
}

// invokerSessionDir returns the invoking user's session directory for the
// given project, e.g. /Users/dr/.claude/projects/-Users-dr-workspace-foo.
// Returns "" if the directory does not exist.
func invokerSessionDir(projectDir string) string {
	home := invokerHome()
	if home == "" {
		return ""
	}
	claudeDir := filepath.Join(home, ".claude", "projects")
	sanitized := sanitizePathForClaude(projectDir)

	// Exact match for short paths.
	if len(sanitized) <= maxSanitizedLength {
		dir := filepath.Join(claudeDir, sanitized)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
		return ""
	}

	// Long paths: Claude Code appends a hash that differs between Bun and
	// Node.js runtimes. Match by the stable 200-char prefix.
	prefix := sanitized[:maxSanitizedLength]
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix+"-") {
			return filepath.Join(claudeDir, e.Name())
		}
	}
	return ""
}

func agentSessionDirPath(homeRoot, invokerDir string) (string, error) {
	homeRoot = filepath.Clean(homeRoot)
	if !filepath.IsAbs(homeRoot) {
		return "", fmt.Errorf("agent session home %q must be absolute", homeRoot)
	}
	dirName := filepath.Base(invokerDir)
	if dirName == "." || dirName == ".." || strings.Contains(dirName, string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid Claude session directory %q", invokerDir)
	}
	return filepath.Join(homeRoot, ".claude", "projects", dirName), nil
}

func agentSessionDirInHome(homeRoot, invokerDir string) (string, error) {
	dest, err := agentSessionDirPath(homeRoot, invokerDir)
	if err != nil {
		return "", err
	}
	if err := resumeAgentEnsureDir(dest, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w (%s)", dest, err, initDriftRepairAdvice)
	}
	return dest, nil
}

type resumeSessionFile struct {
	name    string
	path    string
	size    int64
	modTime time.Time
}

func listResumeSessionFiles(dir string) ([]resumeSessionFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list sessions in %s: %w", dir, err)
	}

	var files []resumeSessionFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("stat session %s: %w", filepath.Join(dir, name), err)
		}

		files = append(files, resumeSessionFile{
			name:    name,
			path:    filepath.Join(dir, name),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].name < files[j].name
	})
	return files, nil
}

func selectResumeSessionFiles(files []resumeSessionFile, resumeTarget string, wantsContinue bool) []resumeSessionFile {
	if resumeTarget != "" {
		target := resumeTarget + ".jsonl"
		for _, file := range files {
			if file.name == target {
				return []resumeSessionFile{file}
			}
		}
		return nil
	}

	if wantsContinue {
		var latest *resumeSessionFile
		for i := range files {
			file := &files[i]
			if latest == nil || file.modTime.After(latest.modTime) || (file.modTime.Equal(latest.modTime) && file.name > latest.name) {
				latest = file
			}
		}
		if latest == nil {
			return nil
		}
		return []resumeSessionFile{*latest}
	}

	return files
}

func copyResumeSessionFile(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	return copyResumeSessionFileWithMode(src, dest, 0o600, info.ModTime())
}

func copySharedResumeSessionFile(file resumeSessionFile, dest string) error {
	return copyResumeSessionFileWithMode(file.path, dest, 0o660, file.modTime)
}

func copyResumeSessionFileWithMode(src, dest string, mode os.FileMode, modTime time.Time) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", dest, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close() //nolint:errcheck // error-path close; copy error is more important
		return fmt.Errorf("copy %s to %s: %w", src, dest, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", dest, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Chtimes(tmpName, modTime, modTime); err != nil {
		return fmt.Errorf("preserve timestamp for %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, dest, err)
	}
	return nil
}

func resumeSessionContentsEqual(a, b string) bool {
	left, err := os.Open(a)
	if err != nil {
		return false
	}
	defer left.Close()
	right, err := os.Open(b)
	if err != nil {
		return false
	}
	defer right.Close()

	leftBuf := make([]byte, 32*1024)
	rightBuf := make([]byte, 32*1024)
	for {
		leftN, leftErr := left.Read(leftBuf)
		rightN, rightErr := right.Read(rightBuf)
		if leftN != rightN || !bytes.Equal(leftBuf[:leftN], rightBuf[:rightN]) {
			return false
		}
		if leftErr == io.EOF && rightErr == io.EOF {
			return true
		}
		if leftErr != nil || rightErr != nil {
			return false
		}
	}
}

func shouldRefreshResumeSessionFile(src resumeSessionFile, dest string, destInfo os.FileInfo) bool {
	if src.modTime.After(destInfo.ModTime()) {
		return true
	}
	needsSharedMode := destInfo.Mode().Perm()&0o060 != 0o060
	if !needsSharedMode && src.modTime.Equal(destInfo.ModTime()) {
		return false
	}
	return src.size == destInfo.Size() && resumeSessionContentsEqual(src.path, dest)
}

func repairExistingResumeSessionPermissions(dest string, info os.FileInfo) {
	if info.Mode().Perm()&0o060 == 0o060 {
		return
	}
	_ = os.Chmod(dest, 0o660)
}

func syncResumeSessionFiles(srcDir, destDir, resumeTarget string, wantsContinue bool) (int, error) {
	files, err := listResumeSessionFiles(srcDir)
	if err != nil {
		return 0, err
	}

	selected := selectResumeSessionFiles(files, resumeTarget, wantsContinue)
	synced := 0
	for _, file := range selected {
		dest := filepath.Join(destDir, file.name)

		info, err := os.Lstat(dest)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				if err := os.Remove(dest); err != nil {
					return synced, fmt.Errorf("remove stale symlink %s: %w", dest, err)
				}
			} else {
				// Regular files may be prior host syncs or agent-local
				// continuations. Refresh stale/equivalent host copies, but keep
				// newer divergent files intact.
				if !shouldRefreshResumeSessionFile(file, dest, info) {
					repairExistingResumeSessionPermissions(dest, info)
					continue
				}
			}
		}

		if err := copySharedResumeSessionFile(file, dest); err != nil {
			return synced, err
		}
		synced++
	}

	return synced, nil
}

type agentResumeSessionFileInfo struct {
	exists    bool
	symlink   bool
	regular   bool
	size      int64
	modTime   time.Time
	typeLabel string
}

var (
	agentResumeSessionFileInfoFunc = defaultAgentResumeSessionFileInfo
	agentRemoveResumeSessionFile   = defaultAgentRemoveResumeSessionFile
	agentWriteResumeSessionFile    = defaultAgentWriteResumeSessionFile
)

func defaultAgentResumeSessionFileInfo(path string) (agentResumeSessionFileInfo, error) {
	const script = `path=$1
if [ -L "$path" ]; then
  printf 'symlink\t0\t0\n'
elif [ -f "$path" ]; then
  /usr/bin/stat -f 'file\t%z\t%m\n' "$path"
elif [ -e "$path" ]; then
  printf 'other\t0\t0\n'
else
  printf 'missing\t0\t0\n'
fi`

	out, err := asAgentOutput("/bin/sh", "-c", script, "hazmat-agent-resume-stat", path)
	if err != nil {
		return agentResumeSessionFileInfo{}, fmt.Errorf("stat agent resume session %s: %w", path, err)
	}

	fields := strings.Split(strings.TrimSpace(out), "\t")
	if len(fields) != 3 {
		return agentResumeSessionFileInfo{}, fmt.Errorf("parse agent resume session stat for %s: %q", path, out)
	}

	info := agentResumeSessionFileInfo{typeLabel: fields[0]}
	switch fields[0] {
	case "missing":
		return info, nil
	case "symlink":
		info.exists = true
		info.symlink = true
		return info, nil
	case "other":
		info.exists = true
		return info, nil
	case "file":
		info.exists = true
		info.regular = true
	default:
		return agentResumeSessionFileInfo{}, fmt.Errorf("parse agent resume session stat for %s: unknown type %q", path, fields[0])
	}

	size, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return agentResumeSessionFileInfo{}, fmt.Errorf("parse agent resume session size for %s: %w", path, err)
	}
	sec, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return agentResumeSessionFileInfo{}, fmt.Errorf("parse agent resume session mtime for %s: %w", path, err)
	}
	info.size = size
	info.modTime = time.Unix(sec, 0)
	return info, nil
}

func defaultAgentRemoveResumeSessionFile(path string) error {
	if err := asAgentQuiet("/bin/rm", "-f", path); err != nil {
		return fmt.Errorf("remove stale agent resume session %s: %w", path, err)
	}
	return nil
}

func defaultAgentWriteResumeSessionFile(file resumeSessionFile, dest string) error {
	content, err := os.ReadFile(file.path)
	if err != nil {
		return fmt.Errorf("read %s: %w", file.path, err)
	}

	tmp := filepath.Join(
		filepath.Dir(dest),
		fmt.Sprintf(".%s.tmp-%d-%d", filepath.Base(dest), os.Getpid(), time.Now().UnixNano()),
	)
	defer asAgentQuiet("/bin/rm", "-f", tmp) //nolint:errcheck // best-effort temp cleanup after atomic move

	if err := agentWriteFile(tmp, content, 0o600); err != nil {
		return err
	}
	if err := asAgentQuiet("/usr/bin/touch", "-mt", file.modTime.Local().Format("200601021504.05"), tmp); err != nil {
		return fmt.Errorf("preserve timestamp for %s: %w", tmp, err)
	}
	if err := asAgentQuiet("/bin/mv", "-f", tmp, dest); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmp, dest, err)
	}
	return nil
}

func agentResumeSessionContentsEqual(srcPath, destPath string) bool {
	left, err := os.ReadFile(srcPath)
	if err != nil {
		return false
	}
	right, err := agentReadFileBytes(destPath)
	if err != nil {
		return false
	}
	return bytes.Equal(left, right)
}

func shouldRefreshAgentResumeSessionFile(src resumeSessionFile, dest string, destInfo agentResumeSessionFileInfo) bool {
	if !destInfo.exists {
		return true
	}
	if destInfo.symlink {
		return true
	}
	if !destInfo.regular {
		return false
	}
	if src.modTime.After(destInfo.modTime) {
		return true
	}
	if src.modTime.Equal(destInfo.modTime) {
		return false
	}
	return src.size == destInfo.size && agentResumeSessionContentsEqual(src.path, dest)
}

func syncResumeSessionFilesToAgent(srcDir, destDir, resumeTarget string, wantsContinue bool) (int, error) {
	files, err := listResumeSessionFiles(srcDir)
	if err != nil {
		return 0, err
	}

	selected := selectResumeSessionFiles(files, resumeTarget, wantsContinue)
	synced := 0
	for _, file := range selected {
		dest := filepath.Join(destDir, file.name)

		info, err := agentResumeSessionFileInfoFunc(dest)
		if err != nil {
			return synced, err
		}
		if info.exists && !info.symlink && !info.regular {
			return synced, fmt.Errorf("agent resume destination %s is %s, not a regular file", dest, info.typeLabel)
		}
		if !shouldRefreshAgentResumeSessionFile(file, dest, info) {
			continue
		}
		if info.symlink {
			if err := agentRemoveResumeSessionFile(dest); err != nil {
				return synced, err
			}
		}
		if err := agentWriteResumeSessionFile(file, dest); err != nil {
			return synced, err
		}
		synced++
	}

	return synced, nil
}

// syncResumeSession copies the invoking user's Claude Code sessions into the
// agent user's session directory. This lets --resume and --continue work
// without granting the seatbelt direct access to the host transcript store.
//
// Existing regular files are refreshed only when the host transcript is newer
// or the destination is an equivalent prior sync with bad metadata. Newer
// divergent files are kept intact so agent-local continuations are not
// overwritten. When --continue is used, only the most recent session is copied.
// A targeted --resume copies only the requested session. Bare --resume copies
// the available project sessions so Claude can offer its picker UI.
func syncResumeSession(projectDir string, resumeTarget string, wantsContinue bool) error {
	return syncResumeSessionIntoHome(agentHome, projectDir, resumeTarget, wantsContinue)
}

func syncResumeSessionIntoHome(homeRoot, projectDir string, resumeTarget string, wantsContinue bool) error {
	srcDir := invokerSessionDir(projectDir)
	if srcDir == "" {
		return nil // no sessions to sync — not an error
	}

	destDir, err := agentSessionDirInHome(homeRoot, srcDir)
	if err != nil {
		return err
	}

	synced, err := syncResumeSessionFilesToAgent(srcDir, destDir, resumeTarget, wantsContinue)
	if err != nil {
		return err
	}

	if synced > 0 {
		fmt.Fprintf(os.Stderr, "  Resume: synced %d session(s) from %s\n", synced, invokerHome())
	}

	return nil
}

// detectResumeFlags scans the forwarded Claude args for --resume/-r and
// --continue/-c. These flags stay in the forwarded list (Claude needs them);
// we just detect their presence and extract an optional session ID.
func detectResumeFlags(forwarded []string) (wantsResume bool, resumeTarget string, wantsContinue bool) {
	for i := 0; i < len(forwarded); i++ {
		arg := forwarded[i]
		switch {
		case arg == "--continue" || arg == "-c":
			wantsContinue = true
		case arg == "--resume" || arg == "-r":
			wantsResume = true
			// Check if next arg is a session ID (not a flag).
			if i+1 < len(forwarded) && !strings.HasPrefix(forwarded[i+1], "-") {
				resumeTarget = forwarded[i+1]
			}
		case strings.HasPrefix(arg, "--resume="):
			wantsResume = true
			resumeTarget = arg[len("--resume="):]
		}
	}
	return
}

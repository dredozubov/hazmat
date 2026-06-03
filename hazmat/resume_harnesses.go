package hazmat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	sharedResumeDirMode  os.FileMode = 0o2770
	sharedResumeFileMode os.FileMode = 0o660
)

type resumeDirEnsurer func(string) error

func localEnsureSharedResumeDir(path string) error {
	if err := os.MkdirAll(path, sharedResumeDirMode); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := os.Chmod(path, sharedResumeDirMode); err != nil {
		return fmt.Errorf("set mode on %s: %w", path, err)
	}
	return nil
}

func agentEnsureSharedResumeDir(path string) error {
	return agentEnsureSharedDir(path, sharedResumeDirMode)
}

func cachedResumeDirEnsurer(ensureDir resumeDirEnsurer) resumeDirEnsurer {
	if ensureDir == nil {
		ensureDir = localEnsureSharedResumeDir
	}
	seen := make(map[string]struct{})
	return func(path string) error {
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			return nil
		}
		if err := ensureDir(path); err != nil {
			return err
		}
		seen[path] = struct{}{}
		return nil
	}
}

func writeSharedResumeFile(path string, content []byte, mode os.FileMode, modTime time.Time, ensureDir resumeDirEnsurer) error {
	if ensureDir == nil {
		ensureDir = localEnsureSharedResumeDir
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}

	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale symlink %s: %w", path, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(content); err != nil {
		tmp.Close() //nolint:errcheck // write error is more useful
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if !modTime.IsZero() {
		if err := os.Chtimes(tmpName, modTime, modTime); err != nil {
			return fmt.Errorf("preserve timestamp for %s: %w", tmpName, err)
		}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

func syncSharedResumeFile(src, dest string, ensureDir resumeDirEnsurer) (bool, error) {
	info, err := os.Stat(src)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", src, err)
	}
	file := resumeSessionFile{
		name:    filepath.Base(src),
		path:    src,
		size:    info.Size(),
		modTime: info.ModTime(),
	}

	if ensureDir == nil {
		ensureDir = localEnsureSharedResumeDir
	}
	if err := ensureDir(filepath.Dir(dest)); err != nil {
		return false, err
	}

	destInfo, err := os.Lstat(dest)
	if err == nil {
		if destInfo.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(dest); err != nil {
				return false, fmt.Errorf("remove stale symlink %s: %w", dest, err)
			}
		} else if !shouldRefreshResumeSessionFile(file, dest, destInfo) {
			repairExistingResumeSessionPermissions(dest, destInfo)
			return false, nil
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", dest, err)
	}

	if err := copyResumeSessionFileWithMode(src, dest, sharedResumeFileMode, file.modTime); err != nil {
		return false, err
	}
	return true, nil
}

func syncResumeFileTree(srcRoot, destRoot string, suffixes []string, ensureDir resumeDirEnsurer) (int, error) {
	if ensureDir == nil {
		ensureDir = localEnsureSharedResumeDir
	}
	if info, err := os.Stat(srcRoot); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("stat %s: %w", srcRoot, err)
	} else if !info.IsDir() {
		return 0, nil
	}

	matches := func(name string) bool {
		for _, suffix := range suffixes {
			if strings.HasSuffix(name, suffix) {
				return true
			}
		}
		return len(suffixes) == 0
	}

	synced := 0
	err := filepath.WalkDir(srcRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(destRoot, rel)
		if entry.IsDir() {
			return ensureDir(dest)
		}
		if entry.Type()&os.ModeSymlink != 0 || !matches(entry.Name()) {
			return nil
		}
		copied, err := syncSharedResumeFile(path, dest, ensureDir)
		if err != nil {
			return err
		}
		if copied {
			synced++
		}
		return nil
	})
	if err != nil {
		return synced, fmt.Errorf("sync %s to %s: %w", srcRoot, destRoot, err)
	}
	return synced, nil
}

type codexSessionIndexEntry map[string]any

func readCodexSessionIndex(path string) ([]codexSessionIndexEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var entries []codexSessionIndexEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry codexSessionIndexEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func codexSessionIndexID(entry codexSessionIndexEntry) string {
	id, _ := entry["id"].(string)
	return id
}

func codexSessionIndexUpdatedAt(entry codexSessionIndexEntry) time.Time {
	updated, _ := entry["updated_at"].(string)
	if updated == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return time.Time{}
	}
	return t
}

func codexSessionIndexLine(entry codexSessionIndexEntry) ([]byte, error) {
	line, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	return append(line, '\n'), nil
}

func mergeCodexSessionIndexes(hostEntries, agentEntries []codexSessionIndexEntry) []codexSessionIndexEntry {
	byID := make(map[string]codexSessionIndexEntry)
	for _, entry := range agentEntries {
		if id := codexSessionIndexID(entry); id != "" {
			byID[id] = entry
		}
	}
	for _, entry := range hostEntries {
		id := codexSessionIndexID(entry)
		if id == "" {
			continue
		}
		current, ok := byID[id]
		if !ok || codexSessionIndexUpdatedAt(entry).After(codexSessionIndexUpdatedAt(current)) {
			byID[id] = entry
		}
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left := byID[ids[i]]
		right := byID[ids[j]]
		leftTime := codexSessionIndexUpdatedAt(left)
		rightTime := codexSessionIndexUpdatedAt(right)
		if !leftTime.Equal(rightTime) {
			return leftTime.Before(rightTime)
		}
		return ids[i] < ids[j]
	})

	merged := make([]codexSessionIndexEntry, 0, len(ids))
	for _, id := range ids {
		merged = append(merged, byID[id])
	}
	return merged
}

func writeCodexSessionIndex(path string, entries []codexSessionIndexEntry, ensureDir resumeDirEnsurer) error {
	var content bytes.Buffer
	for _, entry := range entries {
		line, err := codexSessionIndexLine(entry)
		if err != nil {
			return fmt.Errorf("encode %s: %w", path, err)
		}
		content.Write(line)
	}
	return writeSharedResumeFile(path, content.Bytes(), sharedResumeFileMode, time.Now(), ensureDir)
}

func syncCodexSessionIndex(hostPath, agentPath string, ensureDir resumeDirEnsurer) (bool, error) {
	hostEntries, err := readCodexSessionIndex(hostPath)
	if err != nil {
		return false, err
	}
	if len(hostEntries) == 0 {
		return false, nil
	}
	agentEntries, err := readCodexSessionIndex(agentPath)
	if err != nil {
		return false, err
	}
	merged := mergeCodexSessionIndexes(hostEntries, agentEntries)
	if len(merged) == len(agentEntries) {
		unchanged := true
		for i := range merged {
			left, err := codexSessionIndexLine(merged[i])
			if err != nil {
				return false, err
			}
			right, err := codexSessionIndexLine(agentEntries[i])
			if err != nil {
				return false, err
			}
			if !bytes.Equal(left, right) {
				unchanged = false
				break
			}
		}
		if unchanged {
			return false, nil
		}
	}
	if err := writeCodexSessionIndex(agentPath, merged, ensureDir); err != nil {
		return false, err
	}
	return true, nil
}

func syncCodexResumeStateFromDirs(hostCodexDir, agentCodexDir string, ensureDir resumeDirEnsurer) (int, error) {
	if info, err := os.Stat(hostCodexDir); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("stat %s: %w", hostCodexDir, err)
	} else if !info.IsDir() {
		return 0, nil
	}

	if ensureDir == nil {
		ensureDir = localEnsureSharedResumeDir
	}
	ensureDir = cachedResumeDirEnsurer(ensureDir)
	if err := ensureDir(agentCodexDir); err != nil {
		return 0, err
	}

	synced := 0
	indexSynced, err := syncCodexSessionIndex(
		filepath.Join(hostCodexDir, "session_index.jsonl"),
		filepath.Join(agentCodexDir, "session_index.jsonl"),
		ensureDir,
	)
	if err != nil {
		return synced, err
	}
	if indexSynced {
		synced++
	}

	for _, dirName := range []string{"sessions", "archived_sessions"} {
		count, err := syncResumeFileTree(
			filepath.Join(hostCodexDir, dirName),
			filepath.Join(agentCodexDir, dirName),
			[]string{".jsonl"},
			ensureDir,
		)
		if err != nil {
			return synced, err
		}
		synced += count
	}
	return synced, nil
}

func syncCodexResumeState() error {
	home := invokerHome()
	if home == "" {
		return nil
	}
	hostCodexDir := filepath.Join(home, ".codex")
	agentCodexDir := filepath.Join(agentHome, ".codex")

	synced, err := syncCodexResumeStateFromDirs(hostCodexDir, agentCodexDir, agentEnsureSharedResumeDir)
	if err != nil {
		return err
	}
	if synced > 0 {
		fmt.Fprintf(os.Stderr, "  Resume: synced %d Codex artifact(s) from %s\n", synced, home)
	}
	return nil
}

type geminiProjectsFile struct {
	Projects map[string]string `json:"projects"`
}

func readGeminiProjectsFile(path string) (geminiProjectsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return geminiProjectsFile{Projects: map[string]string{}}, nil
		}
		return geminiProjectsFile{}, fmt.Errorf("read %s: %w", path, err)
	}
	var projects geminiProjectsFile
	if err := json.Unmarshal(data, &projects); err != nil {
		return geminiProjectsFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if projects.Projects == nil {
		projects.Projects = map[string]string{}
	}
	return projects, nil
}

func writeGeminiProjectsFile(path string, projects geminiProjectsFile, ensureDir resumeDirEnsurer) error {
	data, err := json.Marshal(projects)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	return writeSharedResumeFile(path, data, sharedResumeFileMode, time.Now(), ensureDir)
}

func mergeGeminiProjectEntry(hostPath, agentPath, projectDir string, ensureDir resumeDirEnsurer) (projectID string, synced bool, err error) {
	hostProjects, err := readGeminiProjectsFile(hostPath)
	if err != nil {
		return "", false, err
	}
	projectID = hostProjects.Projects[projectDir]
	if projectID == "" {
		return "", false, nil
	}

	agentProjects, err := readGeminiProjectsFile(agentPath)
	if err != nil {
		return "", false, err
	}
	if agentProjects.Projects == nil {
		agentProjects.Projects = map[string]string{}
	}
	if agentProjects.Projects[projectDir] == projectID {
		return projectID, false, nil
	}
	agentProjects.Projects[projectDir] = projectID
	if err := writeGeminiProjectsFile(agentPath, agentProjects, ensureDir); err != nil {
		return "", false, err
	}
	return projectID, true, nil
}

func syncGeminiResumeStateFromDirs(hostGeminiDir, agentGeminiDir, projectDir string, ensureDir resumeDirEnsurer) (int, error) {
	if info, err := os.Stat(hostGeminiDir); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("stat %s: %w", hostGeminiDir, err)
	} else if !info.IsDir() {
		return 0, nil
	}

	if ensureDir == nil {
		ensureDir = localEnsureSharedResumeDir
	}
	ensureDir = cachedResumeDirEnsurer(ensureDir)
	if err := ensureDir(agentGeminiDir); err != nil {
		return 0, err
	}

	synced := 0
	projectID, projectsSynced, err := mergeGeminiProjectEntry(
		filepath.Join(hostGeminiDir, "projects.json"),
		filepath.Join(agentGeminiDir, "projects.json"),
		projectDir,
		ensureDir,
	)
	if err != nil {
		return synced, err
	}
	if projectID == "" {
		return synced, nil
	}
	if projectsSynced {
		synced++
	}

	hostProjectDir := filepath.Join(hostGeminiDir, "tmp", projectID)
	agentProjectDir := filepath.Join(agentGeminiDir, "tmp", projectID)
	if copied, err := syncSharedResumeFile(filepath.Join(hostProjectDir, "logs.json"), filepath.Join(agentProjectDir, "logs.json"), ensureDir); err != nil {
		if !os.IsNotExist(err) {
			return synced, err
		}
	} else if copied {
		synced++
	}

	count, err := syncResumeFileTree(
		filepath.Join(hostProjectDir, "chats"),
		filepath.Join(agentProjectDir, "chats"),
		[]string{".json", ".jsonl"},
		ensureDir,
	)
	if err != nil {
		return synced, err
	}
	synced += count
	return synced, nil
}

func syncGeminiResumeState(projectDir string) error {
	home := invokerHome()
	if home == "" {
		return nil
	}
	hostGeminiDir := filepath.Join(home, ".gemini")
	agentGeminiDir := filepath.Join(agentHome, ".gemini")

	synced, err := syncGeminiResumeStateFromDirs(hostGeminiDir, agentGeminiDir, projectDir, agentEnsureSharedResumeDir)
	if err != nil {
		return err
	}
	if synced > 0 {
		fmt.Fprintf(os.Stderr, "  Resume: synced %d Gemini artifact(s) for %s\n", synced, projectDir)
	}
	return nil
}

func codexResumeRequested(forwarded []string) bool {
	for _, arg := range forwarded {
		if arg == "resume" || arg == "fork" {
			return true
		}
	}
	return false
}

func geminiResumeRequested(forwarded []string) bool {
	for _, arg := range forwarded {
		switch {
		case arg == "--resume" || arg == "-r" || arg == "--list-sessions":
			return true
		case strings.HasPrefix(arg, "--resume="):
			return true
		}
	}
	return false
}

type openCodeResumeRequest struct {
	sessionID     string
	wantsContinue bool
	requested     bool
}

func detectOpenCodeResumeRequest(forwarded []string) openCodeResumeRequest {
	var req openCodeResumeRequest
	for i := 0; i < len(forwarded); i++ {
		arg := forwarded[i]
		switch {
		case arg == "--continue" || arg == "-c":
			req.requested = true
			req.wantsContinue = true
		case arg == "--session" || arg == "-s":
			req.requested = true
			if i+1 < len(forwarded) && !strings.HasPrefix(forwarded[i+1], "-") {
				req.sessionID = forwarded[i+1]
			}
		case strings.HasPrefix(arg, "--session="):
			req.requested = true
			req.sessionID = arg[len("--session="):]
		}
	}
	return req
}

type openCodeSessionSummary struct {
	ID string `json:"id"`
}

type openCodeResumeHooks struct {
	listLatestSessionID func(projectDir string) (string, error)
	exportSession       func(projectDir, sessionID, dest string) error
	importSession       func(path string) error
}

func defaultOpenCodeResumeHooks() openCodeResumeHooks {
	return openCodeResumeHooks{
		listLatestSessionID: listLatestHostOpenCodeSessionID,
		exportSession:       exportHostOpenCodeSession,
		importSession:       importAgentOpenCodeSession,
	}
}

func listLatestHostOpenCodeSessionID(projectDir string) (string, error) {
	cmd := exec.Command("opencode", "session", "list", "--format", "json", "--max-count", "1")
	cmd.Dir = projectDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("list OpenCode sessions: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var sessions []openCodeSessionSummary
	if err := json.Unmarshal(out, &sessions); err != nil {
		return "", fmt.Errorf("parse OpenCode session list: %w", err)
	}
	if len(sessions) == 0 {
		return "", nil
	}
	return sessions[0].ID, nil
}

func exportHostOpenCodeSession(projectDir, sessionID, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create OpenCode export file: %w", err)
	}
	defer out.Close()

	cmd := exec.Command("opencode", "export", sessionID)
	cmd.Dir = projectDir
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("export OpenCode session %s: %w: %s", sessionID, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func importAgentOpenCodeSession(path string) error {
	bin, ok := findInstalledOpenCodeBinary()
	if !ok {
		return errors.New(openCodeMissingHelp)
	}
	if err := asAgentQuiet(bin, "import", path); err != nil {
		return fmt.Errorf("import OpenCode session: %w", err)
	}
	return nil
}

func syncOpenCodeResumeStateWithHooks(projectDir string, forwarded []string, hooks openCodeResumeHooks) (string, error) {
	req := detectOpenCodeResumeRequest(forwarded)
	if !req.requested {
		return "", nil
	}
	sessionID := req.sessionID
	if sessionID == "" && req.wantsContinue {
		latest, err := hooks.listLatestSessionID(projectDir)
		if err != nil {
			return "", err
		}
		sessionID = latest
	}
	if sessionID == "" {
		return "", nil
	}

	tmp, err := os.CreateTemp("", "hazmat-opencode-session-*.json")
	if err != nil {
		return "", fmt.Errorf("create OpenCode session temp file: %w", err)
	}
	tmpName := tmp.Name()
	tmp.Close() //nolint:errcheck // export reopens the path
	defer os.Remove(tmpName)

	if err := hooks.exportSession(projectDir, sessionID, tmpName); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return "", fmt.Errorf("make OpenCode export readable by agent: %w", err)
	}
	if err := hooks.importSession(tmpName); err != nil {
		return "", err
	}
	return sessionID, nil
}

func syncOpenCodeResumeState(projectDir string, forwarded []string) error {
	if err := agentEnsureSharedResumeDir(filepath.Join(agentHome, ".local", "share", "opencode")); err != nil {
		return err
	}
	sessionID, err := syncOpenCodeResumeStateWithHooks(projectDir, forwarded, defaultOpenCodeResumeHooks())
	if err != nil {
		return err
	}
	if sessionID != "" {
		fmt.Fprintf(os.Stderr, "  Resume: imported OpenCode session %s\n", sessionID)
	}
	return nil
}

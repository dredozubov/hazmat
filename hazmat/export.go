package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export data from hazmat-managed tools",
	}
	cmd.AddCommand(newExportClaudeCmd())
	return cmd
}

func newExportClaudeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude",
		Short: "Export Claude Code data",
	}
	cmd.AddCommand(newExportClaudeSessionCmd())
	return cmd
}

func newExportClaudeSessionCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "session [session-id]",
		Short: "Export a hazmat Claude session to your user account",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			requestedID := ""
			if len(args) == 1 {
				var err error
				requestedID, err = normalizeClaudeSessionID(args[0])
				if err != nil {
					return err
				}
			}

			projectDir, err := resolveDir(project, true)
			if err != nil {
				return fmt.Errorf("project: %w", err)
			}

			exportedID, err := exportClaudeSession(projectDir, requestedID)
			if err != nil {
				return err
			}

			fmt.Fprintln(os.Stdout, exportedID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory (defaults to current directory)")
	return cmd
}

func exportClaudeSession(projectDir, requestedID string) (string, error) {
	sourceDir := claudeProjectDir(agentHome, projectDir)
	if sourceDir == "" {
		return "", fmt.Errorf("no hazmat Claude sessions found for %s", projectDir)
	}

	files, err := listAgentClaudeSessionFiles(sourceDir)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no hazmat Claude sessions found for %s", projectDir)
	}

	selected := selectResumeSessionFiles(files, requestedID, requestedID == "")
	if len(selected) == 0 {
		return "", fmt.Errorf("Claude session %q not found for %s", requestedID, projectDir)
	}

	sessionID := strings.TrimSuffix(selected[0].name, ".jsonl")
	destDir, err := ensureInvokerClaudeProjectDir(sourceDir)
	if err != nil {
		return "", err
	}

	stagingDir, err := os.MkdirTemp("", "hazmat-claude-export-*")
	if err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	if err := exportAgentClaudeSessionBundle(sourceDir, sessionID, stagingDir); err != nil {
		return "", err
	}
	if err := installStagedClaudeSessionBundle(stagingDir, destDir, sessionID); err != nil {
		return "", err
	}

	destTranscript := filepath.Join(destDir, sessionID+".jsonl")
	entry, originalPath, err := stagedClaudeSessionIndexEntry(
		stagingDir,
		sessionID,
		destTranscript,
		projectDir,
		selected[0].modTime,
	)
	if err != nil {
		return "", err
	}
	if err := upsertClaudeSessionsIndex(filepath.Join(destDir, "sessions-index.json"), originalPath, entry); err != nil {
		return "", err
	}

	fmt.Fprintf(os.Stderr, "  Export: staged Claude session %s from hazmat\n", sessionID)
	return sessionID, nil
}

func normalizeClaudeSessionID(id string) (string, error) {
	id = strings.TrimSpace(strings.TrimSuffix(id, ".jsonl"))
	switch {
	case id == "", id == ".", id == "..":
		return "", fmt.Errorf("session ID must not be empty")
	case strings.Contains(id, "/"), strings.Contains(id, `\`):
		return "", fmt.Errorf("session ID must not contain path separators")
	default:
		return id, nil
	}
}

func claudeProjectDir(homeDir, projectDir string) string {
	if homeDir == "" {
		return ""
	}

	claudeDir := filepath.Join(homeDir, ".claude", "projects")
	sanitized := sanitizePathForClaude(projectDir)
	if len(sanitized) <= maxSanitizedLength {
		dir := filepath.Join(claudeDir, sanitized)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
		return ""
	}

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

func ensureInvokerClaudeProjectDir(sourceDir string) (string, error) {
	home := invokerHome()
	if home == "" {
		return "", fmt.Errorf("cannot determine invoking user's home directory")
	}

	root := filepath.Join(home, ".claude", "projects")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", root, err)
	}

	dest := filepath.Join(root, filepath.Base(sourceDir))
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dest, err)
	}
	return dest, nil
}

func listAgentClaudeSessionFiles(sourceDir string) ([]resumeSessionFile, error) {
	script := `cd "$SANDBOX_PROJECT_DIR" || exit 1
setopt NULL_GLOB
for f in *.jsonl; do
  [[ -f "$f" ]] || continue
  printf '%s\t%s\n' "$(stat -f '%m' "$f")" "$f"
done`

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd, cleanup, err := newAgentSeatbeltCommand(sessionConfig{ProjectDir: sourceDir}, script)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("list hazmat Claude sessions: %s", msg)
		}
		return nil, fmt.Errorf("list hazmat Claude sessions: %w", err)
	}

	var files []resumeSessionFile
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			return nil, fmt.Errorf("parse hazmat Claude session listing: %q", line)
		}

		sec, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse mtime for %q: %w", fields[1], err)
		}

		files = append(files, resumeSessionFile{
			name:    fields[1],
			path:    filepath.Join(sourceDir, fields[1]),
			modTime: time.Unix(sec, 0),
		})
	}

	return files, nil
}

func exportAgentClaudeSessionBundle(sourceDir, sessionID, destDir string) error {
	manifest, err := agentClaudeBundleManifest(sourceDir, sessionID)
	if err != nil {
		return err
	}

	for _, entry := range manifest {
		target := filepath.Join(destDir, entry.path)
		if !isWithinDir(destDir, target) {
			return fmt.Errorf("reject bundle path outside destination: %q", entry.path)
		}

		if entry.isDir {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("create parent directory for %s: %w", target, err)
		}
		if err := copyAgentClaudeFile(sourceDir, entry.path, target); err != nil {
			return err
		}
	}

	return nil
}

type claudeBundleEntry struct {
	path  string
	isDir bool
}

func agentClaudeBundleManifest(sourceDir, sessionID string) ([]claudeBundleEntry, error) {
	script := `session_id="$1"
cd "$SANDBOX_PROJECT_DIR" || exit 1
jsonl="${session_id}.jsonl"
[[ -f "$jsonl" ]] || { echo "session not found: $session_id" >&2; exit 1; }
printf 'F\t%s\n' "$jsonl"
[[ -f "sessions-index.json" ]] && printf 'F\t%s\n' "sessions-index.json"
if [[ -d "$session_id" ]]; then
  /usr/bin/find "$session_id" -print | while IFS= read -r path; do
    if [[ -d "$path" ]]; then
      printf 'D\t%s\n' "$path"
    elif [[ -f "$path" ]]; then
      printf 'F\t%s\n' "$path"
    fi
  done
fi`

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd, cleanup, err := newAgentSeatbeltCommand(sessionConfig{ProjectDir: sourceDir}, script, sessionID)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("export hazmat Claude session: %s", msg)
		}
		return nil, fmt.Errorf("export hazmat Claude session: %w", err)
	}

	var manifest []claudeBundleEntry
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			return nil, fmt.Errorf("parse Claude bundle manifest: %q", line)
		}

		relPath, err := cleanClaudeBundleRelativePath(fields[1])
		if err != nil {
			return nil, err
		}
		if relPath == "" {
			continue
		}

		switch fields[0] {
		case "D":
			manifest = append(manifest, claudeBundleEntry{path: relPath, isDir: true})
		case "F":
			manifest = append(manifest, claudeBundleEntry{path: relPath})
		default:
			return nil, fmt.Errorf("parse Claude bundle manifest entry kind: %q", fields[0])
		}
	}

	return manifest, nil
}

func cleanClaudeBundleRelativePath(relPath string) (string, error) {
	relPath = filepath.Clean(relPath)
	switch {
	case relPath == ".":
		return "", nil
	case relPath == "", relPath == "..":
		return "", fmt.Errorf("reject invalid bundle path %q", relPath)
	case filepath.IsAbs(relPath):
		return "", fmt.Errorf("reject absolute bundle path %q", relPath)
	case strings.HasPrefix(relPath, ".."+string(os.PathSeparator)):
		return "", fmt.Errorf("reject bundle path escape %q", relPath)
	default:
		return relPath, nil
	}
}

func copyAgentClaudeFile(sourceDir, relPath, destPath string) error {
	script := `rel="$1"
cd "$SANDBOX_PROJECT_DIR" || exit 1
[[ -f "$rel" ]] || { echo "bundle file not found: $rel" >&2; exit 1; }
exec /bin/cat -- "$rel"`

	cmd, cleanup, err := newAgentSeatbeltCommand(sessionConfig{ProjectDir: sourceDir}, script, relPath)
	if err != nil {
		return err
	}
	defer cleanup()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open export pipe for %s: %w", relPath, err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start export of %s: %w", relPath, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(destPath), filepath.Base(destPath)+".tmp-*")
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("create temp file for %s: %w", destPath, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, stdout); err != nil {
		tmp.Close() //nolint:errcheck // error-path close; copy error is more important
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("copy %s to %s: %w", relPath, destPath, err)
	}
	if err := tmp.Close(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("close temp file for %s: %w", destPath, err)
	}
	if err := cmd.Wait(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("export %s: %s", relPath, msg)
		}
		return fmt.Errorf("export %s: %w", relPath, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, destPath, err)
	}
	return nil
}

func extractTarArchive(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar stream: %w", err)
		}

		name := filepath.Clean(hdr.Name)
		switch name {
		case ".", "":
			continue
		}
		target := filepath.Join(destDir, name)
		if !isWithinDir(destDir, target) {
			return fmt.Errorf("reject tar path outside destination: %q", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return fmt.Errorf("create parent directory for %s: %w", target, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close() //nolint:errcheck // error-path close; copy error is more important
				return fmt.Errorf("write %s: %w", target, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close %s: %w", target, err)
			}
		default:
			return fmt.Errorf("unsupported tar entry type %q for %s", string(rune(hdr.Typeflag)), hdr.Name)
		}
	}
}

func installStagedClaudeSessionBundle(stagingDir, destDir, sessionID string) error {
	srcTranscript := filepath.Join(stagingDir, sessionID+".jsonl")
	destTranscript := filepath.Join(destDir, sessionID+".jsonl")
	if _, err := os.Stat(srcTranscript); err != nil {
		return fmt.Errorf("staged session transcript missing: %w", err)
	}
	if err := copyResumeSessionFile(srcTranscript, destTranscript); err != nil {
		return err
	}

	srcBundleDir := filepath.Join(stagingDir, sessionID)
	destBundleDir := filepath.Join(destDir, sessionID)
	if info, err := os.Stat(srcBundleDir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("staged session bundle %s is not a directory", srcBundleDir)
		}
		if err := os.RemoveAll(destBundleDir); err != nil {
			return fmt.Errorf("remove existing %s: %w", destBundleDir, err)
		}
		if err := copyDirTree(srcBundleDir, destBundleDir); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		if err := os.RemoveAll(destBundleDir); err != nil {
			return fmt.Errorf("remove stale %s: %w", destBundleDir, err)
		}
	} else {
		return fmt.Errorf("stat staged bundle %s: %w", srcBundleDir, err)
	}

	return nil
}

func copyDirTree(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("rel path for %s: %w", path, err)
		}
		target := filepath.Join(dest, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyResumeSessionFile(path, target)
	})
}

type claudeSessionsIndex struct {
	Version      int              `json:"version"`
	Entries      []map[string]any `json:"entries"`
	OriginalPath string           `json:"originalPath,omitempty"`
}

func stagedClaudeSessionIndexEntry(stagingDir, sessionID, destPath, fallbackProjectDir string, fileMtime time.Time) (map[string]any, string, error) {
	synthesized := synthesizeClaudeSessionIndexEntry(sessionID, destPath, fallbackProjectDir, fileMtime)
	indexPath := filepath.Join(stagingDir, "sessions-index.json")

	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return synthesized, fallbackProjectDir, nil
		}
		return nil, "", fmt.Errorf("read staged sessions index: %w", err)
	}

	var index claudeSessionsIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, "", fmt.Errorf("parse staged sessions index: %w", err)
	}

	originalPath := index.OriginalPath
	if originalPath == "" {
		originalPath = fallbackProjectDir
	}

	for _, entry := range index.Entries {
		if sessionIDFromIndexEntry(entry) == sessionID {
			return rewriteClaudeSessionIndexEntry(entry, sessionID, destPath, fallbackProjectDir, fileMtime), originalPath, nil
		}
	}

	return synthesized, originalPath, nil
}

func synthesizeClaudeSessionIndexEntry(sessionID, destPath, fallbackProjectDir string, fileMtime time.Time) map[string]any {
	timestamp := fileMtime.UTC().Format(time.RFC3339Nano)
	return map[string]any{
		"sessionId":    sessionID,
		"fullPath":     destPath,
		"fileMtime":    fileMtime.UnixMilli(),
		"projectPath":  fallbackProjectDir,
		"created":      timestamp,
		"modified":     timestamp,
		"isSidechain":  false,
		"messageCount": 0,
	}
}

func rewriteClaudeSessionIndexEntry(entry map[string]any, sessionID, destPath, fallbackProjectDir string, fileMtime time.Time) map[string]any {
	cloned := make(map[string]any, len(entry)+2)
	for k, v := range entry {
		cloned[k] = v
	}
	cloned["sessionId"] = sessionID
	cloned["fullPath"] = destPath
	cloned["fileMtime"] = fileMtime.UnixMilli()
	cloned["modified"] = fileMtime.UTC().Format(time.RFC3339Nano)

	projectPath, _ := cloned["projectPath"].(string)
	if projectPath == "" {
		cloned["projectPath"] = fallbackProjectDir
	}
	if _, ok := cloned["created"]; !ok {
		cloned["created"] = fileMtime.UTC().Format(time.RFC3339Nano)
	}
	return cloned
}

func upsertClaudeSessionsIndex(indexPath, originalPath string, entry map[string]any) error {
	index := claudeSessionsIndex{
		Version:      1,
		Entries:      []map[string]any{},
		OriginalPath: originalPath,
	}

	if data, err := os.ReadFile(indexPath); err == nil {
		if err := json.Unmarshal(data, &index); err != nil {
			return fmt.Errorf("parse %s: %w", indexPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", indexPath, err)
	}

	if index.Version == 0 {
		index.Version = 1
	}
	if index.OriginalPath == "" {
		index.OriginalPath = originalPath
	}

	sessionID := sessionIDFromIndexEntry(entry)
	for i, existing := range index.Entries {
		if sessionIDFromIndexEntry(existing) == sessionID {
			index.Entries[i] = entry
			return writeClaudeSessionsIndex(indexPath, index)
		}
	}

	index.Entries = append(index.Entries, entry)
	return writeClaudeSessionsIndex(indexPath, index)
}

func writeClaudeSessionsIndex(indexPath string, index claudeSessionsIndex) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", indexPath, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(indexPath, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", indexPath, err)
	}
	return nil
}

func sessionIDFromIndexEntry(entry map[string]any) string {
	value, _ := entry["sessionId"].(string)
	return value
}

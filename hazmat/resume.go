package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// agentSessionDir returns (and ensures existence of) the agent user's
// session directory for the given project. The directory name must match
// the invoker's so that Claude Code's sanitizePath produces the same key
// for the same absolute project path.
//
// No sudo needed — init sets .claude/projects/ to agent:dev 2770 (setgid),
// so the host user can create subdirectories that inherit the dev group.
func agentSessionDir(invokerDir string) (string, error) {
	dirName := filepath.Base(invokerDir)
	dest := filepath.Join(agentHome, ".claude", "projects", dirName)

	if err := os.MkdirAll(dest, 0o770); err != nil {
		return "", fmt.Errorf("create %s: %w (run 'hazmat init' to fix permissions)", dest, err)
	}
	return dest, nil
}

type resumeSessionFile struct {
	name    string
	path    string
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
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, dest, err)
	}
	return nil
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
				// Regular file — agent has its own local copy, don't overwrite it.
				continue
			}
		}

		if err := copyResumeSessionFile(file.path, dest); err != nil {
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
// Existing regular files are left untouched so agent-local continuations are
// never overwritten. When --continue is used, only the most recent session is
// copied. A targeted --resume copies only the requested session. Bare --resume
// copies the available project sessions so Claude can offer its picker UI.
func syncResumeSession(projectDir string, resumeTarget string, wantsContinue bool) error {
	srcDir := invokerSessionDir(projectDir)
	if srcDir == "" {
		return nil // no sessions to sync — not an error
	}

	destDir, err := agentSessionDir(srcDir)
	if err != nil {
		return err
	}

	synced, err := syncResumeSessionFiles(srcDir, destDir, resumeTarget, wantsContinue)
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

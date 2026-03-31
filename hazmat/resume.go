package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// sanitizePathForClaude replicates Claude Code's sanitizePath function.
// It replaces all non-alphanumeric characters with hyphens, matching:
//   src/utils/sessionStoragePortable.ts → sanitizePath()
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

// syncResumeSession makes the invoking user's Claude Code sessions
// visible to the agent user via symbolic links. This enables --resume
// and --continue to find conversations started outside the sandbox.
//
// Symlinks point from the agent's session directory into the invoker's.
// The seatbelt policy is extended (via sessionConfig.ResumeDir) to allow
// read+write access to the invoker's session directory so Claude Code
// can follow the symlinks and append to the resumed transcript.
//
// Files that already exist in the agent's directory as regular files
// (not symlinks) are left untouched — they represent sessions the agent
// has continued independently.
//
// Returns the invoker's session directory path (for seatbelt policy) or
// "" if no sync was needed.
func syncResumeSession(projectDir string, resumeTarget string) (invokerDir string, err error) {
	srcDir := invokerSessionDir(projectDir)
	if srcDir == "" {
		return "", nil // no sessions to sync — not an error
	}

	destDir, err := agentSessionDir(srcDir)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return "", fmt.Errorf("list sessions in %s: %w", srcDir, err)
	}

	synced := 0

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		// When resuming a specific session, only link that file.
		if resumeTarget != "" {
			if name != resumeTarget+".jsonl" {
				continue
			}
		}

		src := filepath.Join(srcDir, name)
		dest := filepath.Join(destDir, name)

		// Check if dest already exists.
		info, err := os.Lstat(dest)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				// Existing symlink — update if target changed.
				existing, _ := os.Readlink(dest)
				if existing == src {
					continue // already correct
				}
				os.Remove(dest)
			} else {
				// Regular file — agent has its own copy, don't overwrite.
				continue
			}
		}

		if err := os.Symlink(src, dest); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not link %s: %v\n", name, err)
			continue
		}
		synced++
	}

	if synced > 0 {
		fmt.Fprintf(os.Stderr, "  Resume: synced %d session(s) from %s\n", synced, invokerHome())
	}

	return srcDir, nil
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

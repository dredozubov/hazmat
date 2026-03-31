package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// devGroupACLEntry returns the macOS ACL entry string that grants the dev
// group full collaborative access with file and directory inheritance.
func devGroupACLEntry() string {
	return "group:" + sharedGroup +
		" allow read,write,execute,append,delete,delete_child," +
		"readattr,writeattr,readextattr,writeextattr,readsecurity," +
		"file_inherit,directory_inherit"
}

// devGroupACLEntryNoInherit returns the same permissions as devGroupACLEntry
// but without file_inherit and directory_inherit.
func devGroupACLEntryNoInherit() string {
	return "group:" + sharedGroup +
		" allow read,write,execute,append,delete,delete_child," +
		"readattr,writeattr,readextattr,writeextattr,readsecurity"
}

// pathHasDevACL checks whether a path already has a dev group ACL entry.
func pathHasDevACL(path string) bool {
	out, err := exec.Command("ls", "-le", path).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "group:"+sharedGroup)
}

// ensureProjectWritable checks if the agent user can write to the project
// directory and applies the dev group ACL if not. Called as a pre-flight
// check before every session.
//
// No sudo needed — the file owner can modify ACLs on their own files.
// The inheritable ACL is set on the project root, then applied recursively
// to existing content so the agent can modify existing source files.
//
// This replaces the old workspace-wide ACL scan during init. Instead of
// fixing everything upfront, we fix per-project on first use.
//
// Returns true if a fix was applied (for UI messaging).
func ensureProjectWritable(projectDir string) bool {
	// Fast path: agent can already write — nothing to do.
	if asAgentQuiet("test", "-w", projectDir) == nil {
		return false
	}

	fmt.Fprintf(os.Stderr, "  Setting up project for agent access (one-time)...\n")

	// 1. Set inheritable ACL on the project root (covers new files).
	aclEntry := devGroupACLEntry()
	if err := exec.Command("chmod", "+a", aclEntry, projectDir).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: could not set project ACL: %v\n", err)
		return false
	}

	// 2. Apply non-inheritable ACL to existing content so the agent can
	// modify existing source files (not just newly created ones).
	// Skip .git internals for performance.
	noInherit := devGroupACLEntryNoInherit()
	var paths []string
	filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == projectDir {
			return nil // already handled above
		}
		if d.IsDir() && d.Name() == ".git" {
			// Set ACL on .git dir itself but skip internals.
			paths = append(paths, path)
			return filepath.SkipDir
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".venv" || d.Name() == "venv") {
			return filepath.SkipDir // skip large dependency dirs
		}
		paths = append(paths, path)
		return nil
	})

	// Apply inheritable ACL to directories, non-inheritable to files.
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			exec.Command("chmod", "+a", aclEntry, p).Run()
		} else {
			exec.Command("chmod", "+a", noInherit, p).Run()
		}
	}

	return true
}

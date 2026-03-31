package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

// writableByAgentMode reports whether Unix ownership + mode bits alone are
// enough for the agent to write to a path, without relying on an ACL.
func writableByAgentMode(mode os.FileMode, ownerUID, agentUID uint32, groupHasAgent bool) bool {
	perm := mode.Perm()
	needsExec := mode.IsDir()

	hasOtherWrite := perm&0o002 != 0 && (!needsExec || perm&0o001 != 0)
	if hasOtherWrite {
		return true
	}

	hasOwnerWrite := perm&0o200 != 0 && (!needsExec || perm&0o100 != 0)
	if hasOwnerWrite && ownerUID == agentUID {
		return true
	}

	hasGroupWrite := perm&0o020 != 0 && (!needsExec || perm&0o010 != 0)
	return hasGroupWrite && groupHasAgent
}

// projectRootWritableByAgent avoids a daily sudo probe by checking whether the
// project root is already writable through the dev ACL or ordinary mode bits.
func projectRootWritableByAgent(projectDir string) bool {
	if pathHasDevACL(projectDir) {
		return true
	}

	info, err := os.Stat(projectDir)
	if err != nil {
		return false
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}

	agent, err := user.Lookup(agentUser)
	if err != nil {
		return false
	}
	agentUID64, err := strconv.ParseUint(agent.Uid, 10, 32)
	if err != nil {
		return false
	}

	groupHasAgent := false
	if group, err := user.LookupGroupId(strconv.FormatUint(uint64(stat.Gid), 10)); err == nil {
		groupHasAgent, _ = groupMembershipContains(group.Name, agentUser)
	}

	return writableByAgentMode(info.Mode(), stat.Uid, uint32(agentUID64), groupHasAgent)
}

// collectACLTargets returns the existing project paths that should receive the
// collaborative dev-group ACL. Symlinks are skipped so chmod never follows a
// project link to a target outside the project tree.
func collectACLTargets(projectDir string) []string {
	var paths []string
	filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || path == projectDir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
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
	return paths
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
	// Fast path: agent can already write via ACL or mode bits — nothing to do.
	if projectRootWritableByAgent(projectDir) {
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
	paths := collectACLTargets(projectDir)

	// Apply inheritable ACL to directories, non-inheritable to files.
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
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

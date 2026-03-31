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
// but without file_inherit and directory_inherit. Used when applying the ACL
// to individual existing items (the root's inheritable ACL handles future items).
func devGroupACLEntryNoInherit() string {
	return "group:" + sharedGroup +
		" allow read,write,execute,append,delete,delete_child," +
		"readattr,writeattr,readextattr,writeextattr,readsecurity"
}

// aclTrackingFile records which paths had ACLs applied by hazmat init,
// so rollback can remove exactly what was added.
const aclTrackingFile = ".hazmat-acl-applied"

func aclTrackingPath() string {
	return filepath.Join(os.Getenv("HOME"), ".hazmat", aclTrackingFile)
}

// applyACLToExistingContents walks the workspace and applies the dev group
// ACL to all existing directories and files that don't already have it.
// Returns the number of items fixed and records them for rollback.
func applyACLToExistingContents(r *Runner, ui *UI, root string) (int, error) {
	// Count items first for progress display.
	var paths []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible items
		}
		if path == root {
			return nil // root already has the inheritable ACL
		}
		// Skip .git internals for performance — git manages its own perms.
		// The .git directory itself still gets the ACL so git operations work.
		if d.IsDir() && d.Name() == ".git" {
			paths = append(paths, path)
			return filepath.SkipDir
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("walk %s: %w", root, err)
	}

	if len(paths) == 0 {
		return 0, nil
	}

	// Filter to only items missing the dev group ACL.
	var needFix []string
	for _, p := range paths {
		if pathHasDevACL(p) {
			continue
		}
		needFix = append(needFix, p)
	}

	if len(needFix) == 0 {
		return 0, nil
	}

	fmt.Fprintf(os.Stderr, "  Applying workspace ACL to %d existing items (of %d scanned)...\n", len(needFix), len(paths))

	// Split into directories (need inheritable ACL) and files (non-inheritable).
	// Directories must inherit so that files created inside them by the agent
	// get the dev group ACE regardless of umask.
	var dirs, files []string
	for _, p := range needFix {
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			dirs = append(dirs, p)
		} else {
			files = append(files, p)
		}
	}

	// Apply in batches via xargs for performance (one sudo per batch, not per file).
	// macOS chmod +a doesn't support reading paths from stdin, so we batch manually.
	const batchSize = 200
	applied := 0

	applyBatch := func(aclEntry string, items []string) {
		for i := 0; i < len(items); i += batchSize {
			end := i + batchSize
			if end > len(items) {
				end = len(items)
			}
			batch := items[i:end]

			args := []string{"chmod", "+a", aclEntry}
			args = append(args, batch...)
			if err := r.Sudo("apply dev group ACL to existing workspace items", args...); err != nil {
				// Log but don't fail — some items may be immutable.
				ui.WarnMsg(fmt.Sprintf("ACL batch failed (items %d-%d): %v", i, end-1, err))
				continue
			}
			applied += len(batch)
		}
	}

	applyBatch(devGroupACLEntry(), dirs)
	applyBatch(devGroupACLEntryNoInherit(), files)

	// Record applied paths for rollback.
	if applied > 0 {
		recordACLPaths(needFix[:applied])
	}

	return applied, nil
}

// recordACLPaths appends paths to the ACL tracking file so rollback knows
// which items had ACLs added by hazmat.
func recordACLPaths(paths []string) {
	trackFile := aclTrackingPath()
	os.MkdirAll(filepath.Dir(trackFile), 0o700)

	f, err := os.OpenFile(trackFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()

	for _, p := range paths {
		fmt.Fprintln(f, p)
	}
}

// rollbackWorkspaceACLs removes dev group ACLs from items that were modified
// by hazmat init, using the tracking file as the source of truth.
func rollbackWorkspaceACLs(ui *UI, r *Runner) {
	ui.Step("Remove workspace ACLs from existing items")

	trackFile := aclTrackingPath()
	data, err := os.ReadFile(trackFile)
	if os.IsNotExist(err) {
		ui.SkipDone("No ACL tracking file — nothing to revert")
		return
	}
	if err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not read %s: %v", trackFile, err))
		return
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		ui.SkipDone("ACL tracking file is empty")
		os.Remove(trackFile)
		return
	}

	// Remove the dev group ACL from each tracked path.
	// Use -a to remove a specific ACL entry; failures are non-fatal
	// (the file may have been deleted or moved).
	// Try both inheritable and non-inheritable entries to handle items
	// applied by either the old or new ACL logic.
	removed := 0
	for _, path := range lines {
		if path == "" {
			continue
		}
		if _, err := os.Lstat(path); err != nil {
			continue // file gone — nothing to remove
		}
		// Try inheritable first (directories), then non-inheritable (files).
		if r.Sudo("remove dev group ACL", "chmod", "-a", devGroupACLEntry(), path) == nil {
			removed++
		} else if r.Sudo("remove dev group ACL", "chmod", "-a", devGroupACLEntryNoInherit(), path) == nil {
			removed++
		}
	}

	// Also remove the inheritable ACL from the workspace root.
	inheritableEntry := devGroupACLEntry()
	if workspaceHasDevACL() {
		if err := r.Sudo("remove workspace root ACL", "chmod", "-a", inheritableEntry, sharedWorkspace); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove workspace root ACL: %v", err))
		}
	}

	// Clean up tracking file.
	os.Remove(trackFile)

	if removed > 0 {
		ui.Ok(fmt.Sprintf("Removed dev group ACL from %d items", removed))
	} else {
		ui.Ok("Workspace ACLs cleaned up")
	}
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
// check before every session launch to catch projects created after init
// or moved into the workspace.
//
// Returns true if a fix was applied (for UI messaging).
func ensureProjectWritable(r *Runner, projectDir string) bool {
	if asAgentQuiet("test", "-w", projectDir) == nil {
		return false // agent can already write — nothing to do
	}

	// Apply the dev group ACL to the project tree. Use the inheritable
	// entry so newly created files inside the project also get the ACE.
	// chmod -R applies the same entry to files and directories; the
	// file_inherit/directory_inherit flags are harmless on files (ignored)
	// but essential on directories.
	aclEntry := devGroupACLEntry()
	if err := r.Sudo("fix project permissions for agent access",
		"chmod", "-R", "+a", aclEntry, projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: could not fix project permissions: %v\n", err)
		return false
	}

	// Record for rollback.
	recordACLPaths([]string{projectDir + " (recursive)"})
	return true
}

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

func aclOutputHasDevACL(output string, requireInherit bool) bool {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "group:"+sharedGroup) {
			continue
		}
		if !requireInherit || (strings.Contains(line, "file_inherit") && strings.Contains(line, "directory_inherit")) {
			return true
		}
	}
	return false
}

// pathHasDevACL checks whether a path already has a dev group ACL entry.
func pathHasDevACL(path string, requireInherit bool) bool {
	out, err := exec.Command("ls", "-le", path).CombinedOutput()
	if err != nil {
		return false
	}
	return aclOutputHasDevACL(string(out), requireInherit)
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
// project root already has the inheritable dev ACL needed for host/agent
// collaboration on future files as well as current ones.
func projectRootWritableByAgent(projectDir string) bool {
	return pathHasDevACL(projectDir, true)
}

// collectACLTargets returns the existing project paths that should receive the
// collaborative dev-group ACL. Symlinks are skipped so chmod never follows a
// project link to a target outside the project tree.
func collectACLTargets(projectDir string) []string {
	var paths []string
	filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error { //nolint:errcheck // errors handled in callback; partial walk is acceptable
		if err != nil || path == projectDir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() && shouldSkipACLWalkDir(path, d.Name()) {
			return filepath.SkipDir
		}
		paths = append(paths, path)
		return nil
	})
	return paths
}

func shouldSkipACLWalkDir(path, name string) bool {
	if name != "node_modules" {
		return false
	}
	for _, keepAncestor := range []string{
		string(os.PathSeparator) + ".next" + string(os.PathSeparator),
		string(os.PathSeparator) + "dist" + string(os.PathSeparator),
		string(os.PathSeparator) + "build" + string(os.PathSeparator),
		string(os.PathSeparator) + "target" + string(os.PathSeparator),
	} {
		if strings.Contains(path, keepAncestor) {
			return false
		}
	}
	return true
}

var aclRepairProbeDirNames = map[string]struct{}{
	".next":  {},
	".venv":  {},
	"build":  {},
	"dist":   {},
	"target": {},
	"venv":   {},
}

const aclRepairProbeMaxDepth = 4

func projectNeedsACLRepair(projectDir string) bool {
	if !projectRootWritableByAgent(projectDir) {
		return true
	}

	needsRepair := false
	filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error { //nolint:errcheck // best-effort probe
		if needsRepair || err != nil || path == projectDir {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if shouldSkipACLWalkDir(path, d.Name()) {
			return filepath.SkipDir
		}

		rel, relErr := filepath.Rel(projectDir, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if depth > aclRepairProbeMaxDepth {
			return filepath.SkipDir
		}
		if _, tracked := aclRepairProbeDirNames[d.Name()]; tracked && !pathHasDevACL(path, true) {
			needsRepair = true
			return filepath.SkipDir
		}
		return nil
	})
	return needsRepair
}

func collectAgentTraverseTargets(homeDir, projectDir string, dirs []string) []string {
	seen := make(map[string]struct{})
	var targets []string

	for _, dir := range dirs {
		if dir == "" || dir == homeDir {
			continue
		}
		if !isWithinDir(homeDir, dir) || isWithinDir(projectDir, dir) {
			continue
		}
		for path := dir; path != homeDir && path != "/" && path != "."; path = filepath.Dir(path) {
			if _, dup := seen[path]; dup {
				continue
			}
			seen[path] = struct{}{}
			targets = append(targets, path)
		}
	}

	sort.Slice(targets, func(i, j int) bool {
		depthI := strings.Count(targets[i], string(os.PathSeparator))
		depthJ := strings.Count(targets[j], string(os.PathSeparator))
		if depthI == depthJ {
			return targets[i] < targets[j]
		}
		return depthI < depthJ
	})

	return targets
}

func ensureAgentCanTraverseExposedDirs(projectDir string, dirs []string) (bool, []string) {
	var (
		fixed    bool
		failures []string
	)
	for _, path := range pendingAgentTraverseTargets(projectDir, dirs) {
		if homeAllowsAgentTraverse(path) {
			continue
		}
		if err := exec.Command("chmod", "+a", homeTraverseACLEntry(), path).Run(); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		fixed = true
	}

	return fixed, failures
}

func pendingAgentTraverseTargets(projectDir string, dirs []string) []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var pending []string
	for _, path := range collectAgentTraverseTargets(homeDir, projectDir, dirs) {
		if homeAllowsAgentTraverse(path) {
			continue
		}
		pending = append(pending, path)
	}
	return pending
}

func applyACLTree(root, dirACLEntry, fileACLEntry string) []string {
	var failures []string
	if !pathHasDevACL(root, true) {
		if err := exec.Command("chmod", "+a", dirACLEntry, root).Run(); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", root, err))
		}
	}

	for _, p := range collectACLTargets(root) {
		info, err := os.Lstat(p)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		aclEntry := fileACLEntry
		requireInherit := false
		if info.IsDir() {
			aclEntry = dirACLEntry
			requireInherit = true
		}
		if pathHasDevACL(p, requireInherit) {
			continue
		}
		if err := exec.Command("chmod", "+a", aclEntry, p).Run(); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", p, err))
		}
	}

	return failures
}

func applyDevACLTree(root string) []string {
	return applyACLTree(root, devGroupACLEntry(), devGroupACLEntryNoInherit())
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
// Returns true if a fix was applied.
func ensureProjectWritable(projectDir string) (bool, error) {
	// Fast path: project already has the inheritable dev ACL we need and
	// known mutable dependency/build directories are healthy.
	if !projectNeedsACLRepair(projectDir) {
		return false, nil
	}

	if failures := applyDevACLTree(projectDir); len(failures) > 0 {
		return false, fmt.Errorf("%s", failures[0])
	}

	return true, nil
}

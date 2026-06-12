package hazmat

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var currentUserHomeDir = os.UserHomeDir
var pathAllowsAgentTraverse = homeAllowsAgentTraverse

// pathHasDevACL reports whether a path carries the dev-group collaborative
// ACL. requireInherit selects devGroupInheritableGrant (directories) vs
// devGroupGrant (files or any entry regardless of inheritance).
func pathHasDevACL(path string, requireInherit bool) bool {
	grant := devGroupGrant
	if requireInherit {
		grant = devGroupInheritableGrant
	}
	return hasACLSatisfying(path, grant)
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
			paths = append(paths, path) // include dir for inheritable ACL
			return filepath.SkipDir     // skip contents
		}
		paths = append(paths, path)
		return nil
	})
	return paths
}

var aclWalkSkipDirs = map[string]struct{}{
	".git":         {},
	".venv":        {},
	"__pycache__":  {},
	"node_modules": {},
	"vendor":       {},
	"venv":         {},
}

func shouldSkipACLWalkDir(path, name string) bool {
	if _, skip := aclWalkSkipDirs[name]; !skip {
		return false
	}
	// Preserve node_modules inside build output dirs (e.g. .next/server/node_modules).
	if name == "node_modules" {
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
	}
	return true
}

const projectACLStartupMaxDepth = 3
const projectACLStartupMaxTargets = 1024
const projectACLStartupMaxEntries = 4096
const projectACLReadDirBatchSize = 128
const aclChmodBatchMaxPaths = 256
const aclChmodBatchMaxBytes = 96 * 1024

type aclTreeApplyResult struct {
	Targets        int
	Batches        int
	EntriesScanned int
	Truncated      bool
	Failures       []string
}

type aclBatchApplyResult struct {
	Targets  int
	Batches  int
	Failures []string
}

type projectACLRepairOutcome struct {
	Fixed          bool
	Targets        int
	Batches        int
	EntriesScanned int
	Truncated      bool
}

type projectACLTargetCollection struct {
	Dirs           []string
	Files          []string
	EntriesScanned int
	Truncated      bool
	Failures       []string
}

func projectNeedsACLRepair(projectDir string) bool {
	return !projectRootWritableByAgent(projectDir)
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
	inv := directACLInvoker{}
	for _, path := range pendingAgentTraverseTargets(projectDir, dirs) {
		if homeAllowsAgentTraverse(path) {
			continue
		}
		if err := ensureACL(inv, path, agentTraverseGrant); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		fixed = true
	}

	return fixed, failures
}

func pendingAgentTraverseTargets(projectDir string, dirs []string) []string {
	homeDir, err := currentUserHomeDir()
	if err != nil {
		return nil
	}

	var pending []string

	// Safety net: ensure home directory itself is still traversable.
	// init sets this ACL, but permissions can change (macOS updates,
	// privacy settings, manual chmod). Without home traversal the
	// agent cannot reach any project directory.
	if !pathAllowsAgentTraverse(homeDir) {
		pending = append(pending, homeDir)
	}

	for _, path := range collectAgentTraverseTargets(homeDir, projectDir, dirs) {
		if pathAllowsAgentTraverse(path) {
			continue
		}
		pending = append(pending, path)
	}
	return pending
}

func pendingLaunchHelperTraverseTargets(helperPath string) []string {
	homeDir, err := currentUserHomeDir()
	if err != nil || homeDir == "" || !isWithinDir(homeDir, helperPath) {
		return nil
	}

	var pending []string
	if !pathAllowsAgentTraverse(homeDir) {
		pending = append(pending, homeDir)
	}

	var ancestors []string
	for path := filepath.Dir(helperPath); path != homeDir && path != "/" && path != "."; path = filepath.Dir(path) {
		ancestors = append([]string{path}, ancestors...)
	}
	for _, path := range ancestors {
		if pathAllowsAgentTraverse(path) {
			continue
		}
		pending = append(pending, path)
	}

	return pending
}

func ensureAgentCanTraverseLaunchHelperPath(helperPath string) (bool, []string) {
	var (
		fixed    bool
		failures []string
	)
	inv := directACLInvoker{}
	for _, path := range pendingLaunchHelperTraverseTargets(helperPath) {
		if pathAllowsAgentTraverse(path) {
			continue
		}
		if err := ensureACL(inv, path, agentTraverseGrant); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		fixed = true
	}

	return fixed, failures
}

// collectProjectACLStartupTargets returns a bounded set of existing project
// paths to repair during session startup. It deliberately does not try to
// backfill every historical file in a large checkout.
func collectProjectACLStartupTargets(root string) projectACLTargetCollection {
	type dirToScan struct {
		path  string
		depth int
	}

	var result projectACLTargetCollection
	queue := []dirToScan{{path: root, depth: 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if result.Truncated {
			break
		}
		if current.depth >= projectACLStartupMaxDepth {
			continue
		}

		dir, err := os.Open(current.path)
		if err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", current.path, err))
			continue
		}
		for {
			entries, readErr := dir.ReadDir(projectACLReadDirBatchSize)
			for _, entry := range entries {
				result.EntriesScanned++
				if result.EntriesScanned > projectACLStartupMaxEntries ||
					len(result.Dirs)+len(result.Files) >= projectACLStartupMaxTargets {
					result.Truncated = true
					break
				}

				path := filepath.Join(current.path, entry.Name())
				if entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				if entry.IsDir() {
					result.Dirs = append(result.Dirs, path)
					if shouldSkipACLWalkDir(path, entry.Name()) {
						continue
					}
					queue = append(queue, dirToScan{path: path, depth: current.depth + 1})
					continue
				}
				result.Files = append(result.Files, path)
			}
			if result.Truncated || readErr != nil {
				if readErr != nil && !errors.Is(readErr, io.EOF) {
					result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", current.path, readErr))
				}
				break
			}
			if len(entries) == 0 {
				break
			}
		}
		if err := dir.Close(); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", current.path, err))
		}
	}

	return result
}

// collectProjectACLBackfillTargets returns every existing non-symlink regular
// file and directory under root for an explicit operator-invoked backfill.
func collectProjectACLBackfillTargets(root string) projectACLTargetCollection {
	var result projectACLTargetCollection
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if path == root {
			return nil
		}

		result.EntriesScanned++
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			result.Dirs = append(result.Dirs, path)
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", path, infoErr))
			return nil
		}
		if info.Mode().IsRegular() {
			result.Files = append(result.Files, path)
		}
		return nil
	})
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", root, err))
	}
	return result
}

func applyDevACLStartupRepairResult(root string) aclTreeApplyResult {
	return applyDevACLRepairTargetCollection(root, collectProjectACLStartupTargets(root))
}

func applyDevACLBackfillRepairResult(root string, targets projectACLTargetCollection) aclTreeApplyResult {
	return applyDevACLRepairTargetCollection(root, targets)
}

func applyDevACLRepairTargetCollection(root string, targets projectACLTargetCollection) aclTreeApplyResult {
	var result aclTreeApplyResult
	var failures []string
	inv := directACLInvoker{}

	if err := ensureACL(inv, root, devGroupInheritableGrant); err != nil {
		failures = append(failures, fmt.Sprintf("%s: %v", root, err))
	} else {
		result.Targets++
	}

	result.EntriesScanned = targets.EntriesScanned
	result.Truncated = targets.Truncated
	failures = append(failures, targets.Failures...)

	sort.Strings(targets.Dirs)
	sort.Strings(targets.Files)

	for _, batchResult := range []aclBatchApplyResult{
		applyACLGrantToPathBatches(inv, devGroupInheritableGrant, targets.Dirs),
		applyACLGrantToPathBatches(inv, devGroupGrant, targets.Files),
	} {
		result.Targets += batchResult.Targets
		result.Batches += batchResult.Batches
		failures = append(failures, batchResult.Failures...)
	}

	result.Failures = failures
	return result
}

func applyACLGrantToPathBatches(inv aclInvoker, grant ACLGrant, paths []string) aclBatchApplyResult {
	var result aclBatchApplyResult
	for _, batch := range aclPathBatches(grant, paths) {
		args := append([]string{"+a", grant.String()}, batch...)
		if err := inv.Chmod(args...); err == nil {
			result.Targets += len(batch)
			result.Batches++
			continue
		}

		// Fall back to the idempotent single-path path when a batch contains
		// a stale or otherwise invalid entry. The common path remains one
		// chmod per batch instead of one ls+chmod pair per filesystem entry.
		for _, path := range batch {
			if err := ensureACL(inv, path, grant); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			result.Targets++
			result.Batches++
		}
	}
	return result
}

func aclPathBatches(grant ACLGrant, paths []string) [][]string {
	var batches [][]string
	var current []string
	currentBytes := len("+a") + len(grant.String())
	flush := func() {
		if len(current) == 0 {
			return
		}
		batches = append(batches, append([]string(nil), current...))
		current = current[:0]
		currentBytes = len("+a") + len(grant.String())
	}

	for _, path := range paths {
		pathBytes := len(path) + 1
		if len(current) >= aclChmodBatchMaxPaths || currentBytes+pathBytes > aclChmodBatchMaxBytes {
			flush()
		}
		current = append(current, path)
		currentBytes += pathBytes
	}
	flush()
	return batches
}

// ensureProjectWritable checks if the agent user can write to the project
// directory and applies the bounded startup dev group ACL repair if not.
// Called as a pre-flight check before every session.
//
// No sudo needed — the file owner can modify ACLs on their own files.
// The inheritable ACL is set on the project root, then applied to a finite
// shallow set of existing paths. Full historical backfill is intentionally not
// done on the startup critical path.
//
// This replaces the old workspace-wide ACL scan during init. Instead of
// fixing everything upfront, we fix per-project on first use.
//
// Returns a repair outcome when a fix was applied.
func ensureProjectWritable(projectDir string) (projectACLRepairOutcome, error) {
	// Fast path: project already has the inheritable dev ACL we need and
	// new content will inherit collaboration permissions.
	if !projectNeedsACLRepair(projectDir) {
		return projectACLRepairOutcome{}, nil
	}

	result := applyDevACLStartupRepairResult(projectDir)
	if len(result.Failures) > 0 {
		return projectACLRepairOutcome{}, fmt.Errorf("%s", result.Failures[0])
	}

	return projectACLRepairOutcome{
		Fixed:          true,
		Targets:        result.Targets,
		Batches:        result.Batches,
		EntriesScanned: result.EntriesScanned,
		Truncated:      result.Truncated,
	}, nil
}

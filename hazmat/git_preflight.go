package hazmat

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type gitPathRequirement struct {
	path           string
	optional       bool
	requireInherit bool
}

func gitMetadataDir(projectDir string) string {
	gitDir := filepath.Join(projectDir, ".git")
	info, err := os.Lstat(gitDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	return gitDir
}

func gitPathRequirements(gitDir string) []gitPathRequirement {
	return []gitPathRequirement{
		{path: gitDir, requireInherit: true},
		{path: filepath.Join(gitDir, "HEAD")},
		{path: filepath.Join(gitDir, "index"), optional: true},
		{path: filepath.Join(gitDir, "objects"), requireInherit: true},
		{path: filepath.Join(gitDir, "refs"), requireInherit: true},
		{path: filepath.Join(gitDir, "logs"), optional: true, requireInherit: true},
	}
}

func gitMutableFileACLTargets(gitDir string) []string {
	return []string{
		filepath.Join(gitDir, "HEAD"),
		filepath.Join(gitDir, "index"),
		filepath.Join(gitDir, "packed-refs"),
		filepath.Join(gitDir, "config"),
		filepath.Join(gitDir, "FETCH_HEAD"),
		filepath.Join(gitDir, "ORIG_HEAD"),
		filepath.Join(gitDir, "MERGE_HEAD"),
	}
}

func gitMetadataACLTargets(gitDir string) (dirs []string, files []string, failures []string) {
	addDir := func(path string, optional bool) {
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) && optional {
				return
			}
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			return
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			failures = append(failures, fmt.Sprintf("%s: not a regular directory", path))
			return
		}
		dirs = append(dirs, path)
	}
	addFile := func(path string, optional bool) {
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) && optional {
				return
			}
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			return
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			failures = append(failures, fmt.Sprintf("%s: not a regular file", path))
			return
		}
		files = append(files, path)
	}

	addDir(gitDir, false)
	addDir(filepath.Join(gitDir, "objects"), false)
	addDir(filepath.Join(gitDir, "refs"), false)
	addDir(filepath.Join(gitDir, "logs"), true)
	for _, path := range gitMutableFileACLTargets(gitDir) {
		addFile(path, path != filepath.Join(gitDir, "HEAD"))
	}

	for _, path := range gitImmediateObjectDirs(gitDir) {
		addDir(path, true)
	}
	refDirs, refFiles, refFailures := gitTreeACLTargets(filepath.Join(gitDir, "refs"), true)
	dirs = append(dirs, refDirs...)
	files = append(files, refFiles...)
	failures = append(failures, refFailures...)
	logDirs, logFiles, logFailures := gitTreeACLTargets(filepath.Join(gitDir, "logs"), true)
	dirs = append(dirs, logDirs...)
	files = append(files, logFiles...)
	failures = append(failures, logFailures...)

	dirs = uniqueSortedPaths(dirs)
	files = uniqueSortedPaths(files)
	return dirs, files, failures
}

func gitImmediateObjectDirs(gitDir string) []string {
	objectsDir := filepath.Join(gitDir, "objects")
	entries, err := os.ReadDir(objectsDir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirs = append(dirs, filepath.Join(objectsDir, entry.Name()))
	}
	return dirs
}

func gitTreeACLTargets(root string, includeFiles bool) (dirs []string, files []string, failures []string) {
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, []string{fmt.Sprintf("%s: %v", root, err)}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, nil, []string{fmt.Sprintf("%s: not a regular directory", root)}
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, infoErr))
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			failures = append(failures, fmt.Sprintf("%s: symlink not repaired", path))
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		if includeFiles && info.Mode().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		failures = append(failures, fmt.Sprintf("%s: %v", root, err))
	}
	return dirs, files, failures
}

func uniqueSortedPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	out := paths[:0]
	var last string
	for i, path := range paths {
		if i > 0 && path == last {
			continue
		}
		out = append(out, path)
		last = path
	}
	return out
}

func applyGitMetadataACLs(gitDir string) []string {
	dirs, files, failures := gitMetadataACLTargets(gitDir)
	inv := directACLInvoker{}
	for _, result := range []aclBatchApplyResult{
		applyACLGrantToPathBatches(inv, devGroupInheritableGrant, dirs),
		applyACLGrantToPathBatches(inv, devGroupGrant, files),
	} {
		failures = append(failures, result.Failures...)
	}
	return failures
}

func currentUserCanReadPath(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	return f.Close() == nil
}

// pathWritableByCurrentUser reports whether the calling user has write
// access to path, considering Unix permissions and macOS ACLs.
func pathWritableByCurrentUser(path string) bool {
	return syscall.Access(path, 0x2) == nil // W_OK
}

type gitAgentWriteProbe struct {
	agentUIDLoaded bool
	agentUIDValid  bool
	agentUID       uint32

	groupMembership map[uint32]bool
}

func (p *gitAgentWriteProbe) pathWritable(path string, requireInherit bool) bool {
	if requireInherit {
		return pathHasDevACL(path, true)
	}
	if pathHasDevACL(path, false) {
		return true
	}

	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}

	agentUID, ok := p.lookupAgentUID()
	if !ok {
		return false
	}

	groupHasAgent := p.groupHasAgent(stat.Gid)
	return writableByAgentMode(info.Mode(), stat.Uid, agentUID, groupHasAgent)
}

func (p *gitAgentWriteProbe) lookupAgentUID() (uint32, bool) {
	if p.agentUIDLoaded {
		return p.agentUID, p.agentUIDValid
	}
	p.agentUIDLoaded = true
	agentInfo, err := user.Lookup(agentUser)
	if err != nil {
		return 0, false
	}
	agentUID64, err := strconv.ParseUint(agentInfo.Uid, 10, 32)
	if err != nil {
		return 0, false
	}
	p.agentUID = uint32(agentUID64)
	p.agentUIDValid = true
	return p.agentUID, true
}

func (p *gitAgentWriteProbe) groupHasAgent(gid uint32) bool {
	if p.groupMembership == nil {
		p.groupMembership = make(map[uint32]bool)
	}
	if member, ok := p.groupMembership[gid]; ok {
		return member
	}
	member := false
	if group, err := user.LookupGroupId(strconv.FormatUint(uint64(gid), 10)); err == nil {
		member, _ = groupMembershipContains(group.Name, agentUser)
	}
	p.groupMembership[gid] = member
	return member
}

func pathWritableByAgent(path string, requireInherit bool) bool {
	var probe gitAgentWriteProbe
	return probe.pathWritable(path, requireInherit)
}

func collectGitPermissionProblems(gitDir string) []string {
	var problems []string
	var agentWrite gitAgentWriteProbe
	for _, req := range gitPathRequirements(gitDir) {
		_, err := os.Stat(req.path)
		if err != nil {
			if os.IsNotExist(err) && req.optional {
				continue
			}
			problems = append(problems, fmt.Sprintf("missing %s", req.path))
			continue
		}
		if !currentUserCanReadPath(req.path) {
			problems = append(problems, fmt.Sprintf("host user cannot read %s", req.path))
		}
		if !pathWritableByCurrentUser(req.path) {
			problems = append(problems, fmt.Sprintf("host user cannot write %s", req.path))
		}
		if !agentWrite.pathWritable(req.path, req.requireInherit) {
			want := "write"
			if req.requireInherit {
				want = "write with inheritable dev ACL"
			}
			problems = append(problems, fmt.Sprintf("agent user cannot %s %s", want, req.path))
		}
	}
	return problems
}

func gitRepairCommand(gitDir string) string {
	quotedDir := shellQuote([]string{gitDir})[0]
	return strings.Join([]string{
		fmt.Sprintf("sudo chown -R \"$(id -un)\":staff %s", quotedDir),
		fmt.Sprintf("find %s -type d -exec chmod +a '%s' {} +", quotedDir, devGroupInheritableGrant.String()),
		fmt.Sprintf("find %s -type f -exec chmod +a '%s' {} +", quotedDir, devGroupGrant.String()),
	}, " && ")
}

// repairGitAfterSession re-checks .git/ permissions after an agent session
// and attempts to repair the small set of mutable Git metadata paths. Object
// files are intentionally not walked: Git writes through object directories,
// while existing objects are immutable and only need ordinary read access.
func repairGitAfterSession(projectDir string) {
	gitDir := gitMetadataDir(projectDir)
	if gitDir == "" {
		return
	}

	if len(collectGitPermissionProblems(gitDir)) == 0 {
		return
	}

	_ = applyGitMetadataACLs(gitDir)

	if problems := collectGitPermissionProblems(gitDir); len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "\nhazmat: .git metadata needs repair (agent-owned files)\n")
		fmt.Fprintf(os.Stderr, "  Run: %s\n", gitRepairCommand(gitDir))
	}
}

func ensureGitMetadataHealthy(projectDir string) (bool, error) {
	gitDir := gitMetadataDir(projectDir)
	if gitDir == "" {
		return false, nil
	}

	if len(collectGitPermissionProblems(gitDir)) == 0 {
		return false, nil
	}

	_ = applyGitMetadataACLs(gitDir)

	if problems := collectGitPermissionProblems(gitDir); len(problems) > 0 {
		return false, fmt.Errorf("git metadata permissions are still broken:\n  - %s\nRun:\n  %s",
			strings.Join(problems, "\n  - "),
			gitRepairCommand(gitDir),
		)
	}

	return true, nil
}

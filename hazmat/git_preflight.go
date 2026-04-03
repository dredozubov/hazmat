package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
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

func pathWritableByAgent(path string, requireInherit bool) bool {
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

	agentInfo, err := user.Lookup(agentUser)
	if err != nil {
		return false
	}
	agentUID64, err := strconv.ParseUint(agentInfo.Uid, 10, 32)
	if err != nil {
		return false
	}

	groupHasAgent := false
	if group, err := user.LookupGroupId(strconv.FormatUint(uint64(stat.Gid), 10)); err == nil {
		groupHasAgent, _ = groupMembershipContains(group.Name, agentUser)
	}

	return writableByAgentMode(info.Mode(), stat.Uid, uint32(agentUID64), groupHasAgent)
}

func collectGitPermissionProblems(gitDir string) []string {
	var problems []string
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
		if !pathWritableByAgent(req.path, req.requireInherit) {
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
		fmt.Sprintf("find %s -type d -exec chmod +a '%s' {} +", quotedDir, devGroupACLEntry()),
		fmt.Sprintf("find %s -type f -exec chmod +a '%s' {} +", quotedDir, devGroupACLEntryNoInherit()),
	}, " && ")
}

// repairGitAfterSession re-checks .git/ permissions after an agent session
// and attempts to repair any files that became agent-owned during the session.
// This is best-effort: if the inherited dev group ACL is intact, chmod +a
// succeeds silently. If it isn't, we print a manual repair command.
func repairGitAfterSession(projectDir string) {
	gitDir := gitMetadataDir(projectDir)
	if gitDir == "" {
		return
	}

	if len(collectGitPermissionProblems(gitDir)) == 0 {
		return
	}

	_ = applyDevACLTree(gitDir)

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

	_ = applyDevACLTree(gitDir)

	if problems := collectGitPermissionProblems(gitDir); len(problems) > 0 {
		return false, fmt.Errorf("git metadata permissions are still broken:\n  - %s\nRun:\n  %s",
			strings.Join(problems, "\n  - "),
			gitRepairCommand(gitDir),
		)
	}

	return true, nil
}

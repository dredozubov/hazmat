package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAclOutputHasDevACL(t *testing.T) {
	t.Parallel()

	// Directory rendering: macOS normalizes execute to search and splits
	// read/write into directory-specific verbs (list, add_file, add_subdirectory,
	// delete_child). Files render with the original tokens (read, execute).
	dirInherit := "0: group:dev allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity,file_inherit,directory_inherit"
	dirNoInherit := "0: group:dev allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity"
	fileRendered := "0: group:dev allow read,write,execute,append,delete,readattr,writeattr,readextattr,writeextattr,readsecurity"

	// macOS displays deny entries as "deny" (not "allow"). A deny entry on the
	// dev group with inherit flags must not be mistaken for our allow grant.
	dirDenyInherit := "0: group:dev deny list,add_file,search,file_inherit,directory_inherit"

	// Unrelated group with inherit flags must not match.
	otherGroup := "0: group:staff allow list,add_file,search,file_inherit,directory_inherit"

	// A user-principal entry with the agent's name must not match a dev-group
	// query (principal scoping).
	userPrincipal := "0: user:agent allow execute"

	// Empty ls -led output (no ACL block on the path).
	emptyOutput := "drwxr-xr-x  8 dr  staff  256 Apr 18 10:00 .\n"

	cases := []struct {
		name           string
		output         string
		requireInherit bool
		want           bool
	}{
		{"dir-inherit-required-present", dirInherit, true, true},
		{"dir-inherit-not-required-present", dirInherit, false, true},
		{"dir-no-inherit-required", dirNoInherit, true, false},
		{"dir-no-inherit-not-required", dirNoInherit, false, true},
		{"file-rendered-not-required", fileRendered, false, true},
		{"deny-entry-not-required", dirDenyInherit, false, false},
		{"deny-entry-required", dirDenyInherit, true, false},
		{"other-group-required", otherGroup, true, false},
		{"user-principal-not-dev-group", userPrincipal, false, false},
		{"empty-output", emptyOutput, false, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := aclOutputHasDevACL(tc.output, tc.requireInherit); got != tc.want {
				t.Fatalf("aclOutputHasDevACL(..., requireInherit=%t) = %t, want %t", tc.requireInherit, got, tc.want)
			}
		})
	}
}

func TestAclOutputHasAgentTraverse(t *testing.T) {
	t.Parallel()

	// Home directory renders the traverse ACL with "search" (directory
	// normalization). File rendering shows "execute". Both must match.
	homeRenderedSearch := "0: user:agent allow search,readattr,readextattr,readsecurity"
	fileRenderedExecute := "0: user:agent allow execute,readattr,readextattr,readsecurity"

	// Deny entry must not be mistaken for allow.
	denyEntry := "0: user:agent deny search"

	// Principal mismatch: traverse for a different user must not match.
	differentUser := "0: user:other allow search"

	// Group-principal entry with search must not match: the traverse ACL is
	// user-scoped, and homeAllowsAgentTraverse has a separate group-membership
	// fallback for mode bits.
	groupPrincipal := "0: group:dev allow search,file_inherit,directory_inherit"

	// Multi-line output: unrelated allow line must not bleed into the agent
	// principal check. Without line-scoping, substring matching the whole
	// blob for "user:agent" + " allow search" would return true even though
	// the agent line carries "deny" and the allow line is a different user.
	multiLineMasked := "0: user:agent deny search\n1: user:other allow search\n"

	// Empty output (no ACL).
	emptyOutput := "drwxr-xr-x  10 dr  staff  320 Apr 18 10:00 .\n"

	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"dir-rendered-search", homeRenderedSearch, true},
		{"file-rendered-execute", fileRenderedExecute, true},
		{"deny-entry", denyEntry, false},
		{"different-user", differentUser, false},
		{"group-principal-not-user", groupPrincipal, false},
		{"multi-line-deny-masks-allow", multiLineMasked, false},
		{"empty-output", emptyOutput, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := aclOutputHasAgentTraverse(tc.output); got != tc.want {
				t.Fatalf("aclOutputHasAgentTraverse(...) = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestWritableByAgentMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		mode          os.FileMode
		ownerUID      uint32
		agentUID      uint32
		groupHasAgent bool
		wantWritable  bool
	}{
		{
			name:         "directory owned by agent with owner write and execute",
			mode:         os.ModeDir | 0o700,
			ownerUID:     599,
			agentUID:     599,
			wantWritable: true,
		},
		{
			name:          "directory group writable for agent group",
			mode:          os.ModeDir | 0o770,
			ownerUID:      501,
			agentUID:      599,
			groupHasAgent: true,
			wantWritable:  true,
		},
		{
			name:          "directory group writable without execute is not writable",
			mode:          os.ModeDir | 0o760,
			ownerUID:      501,
			agentUID:      599,
			groupHasAgent: true,
			wantWritable:  false,
		},
		{
			name:         "world writable directory remains writable",
			mode:         os.ModeDir | 0o733,
			ownerUID:     501,
			agentUID:     599,
			wantWritable: true,
		},
		{
			name:          "group writable without agent membership is not writable",
			mode:          os.ModeDir | 0o770,
			ownerUID:      501,
			agentUID:      599,
			groupHasAgent: false,
			wantWritable:  false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := writableByAgentMode(tc.mode, tc.ownerUID, tc.agentUID, tc.groupHasAgent)
			if got != tc.wantWritable {
				t.Fatalf("writableByAgentMode(%#o, %d, %d, %t) = %t, want %t",
					tc.mode, tc.ownerUID, tc.agentUID, tc.groupHasAgent, got, tc.wantWritable)
			}
		})
	}
}

func TestCollectACLTargetsSkipsSymlinksAndTopLevelNodeModules(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	outsideTarget := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsideTarget, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside target: %v", err)
	}

	keepFile := filepath.Join(projectDir, "main.go")
	if err := os.WriteFile(keepFile, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write keep file: %v", err)
	}

	subdir := filepath.Join(projectDir, "pkg")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	nestedFile := filepath.Join(subdir, "lib.go")
	if err := os.WriteFile(nestedFile, []byte("package pkg"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	gitDir := filepath.Join(projectDir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	gitHead := filepath.Join(gitDir, "HEAD")
	if err := os.WriteFile(gitHead, []byte("ref"), 0o644); err != nil {
		t.Fatalf("write .git/HEAD: %v", err)
	}
	gitObjectDir := filepath.Join(gitDir, "objects", "aa")
	if err := os.MkdirAll(gitObjectDir, 0o755); err != nil {
		t.Fatalf("mkdir .git objects dir: %v", err)
	}
	gitObject := filepath.Join(gitObjectDir, "blob")
	if err := os.WriteFile(gitObject, []byte("blob"), 0o644); err != nil {
		t.Fatalf("write git object: %v", err)
	}

	nodeModulesDir := filepath.Join(projectDir, "node_modules", "pkg")
	if err := os.MkdirAll(nodeModulesDir, 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nodeModulesDir, "index.js"), []byte("module.exports = {}"), 0o644); err != nil {
		t.Fatalf("write node_modules file: %v", err)
	}

	nextNodeModulesDir := filepath.Join(projectDir, ".next", "standalone", "node_modules", "pkg")
	if err := os.MkdirAll(nextNodeModulesDir, 0o755); err != nil {
		t.Fatalf("mkdir nested node_modules: %v", err)
	}
	nextNodeModulesFile := filepath.Join(nextNodeModulesDir, "index.js")
	if err := os.WriteFile(nextNodeModulesFile, []byte("module.exports = {}"), 0o644); err != nil {
		t.Fatalf("write nested node_modules file: %v", err)
	}

	venvBinDir := filepath.Join(projectDir, ".venv", "bin")
	if err := os.MkdirAll(venvBinDir, 0o755); err != nil {
		t.Fatalf("mkdir .venv/bin: %v", err)
	}
	venvPython := filepath.Join(venvBinDir, "python")
	if err := os.WriteFile(venvPython, []byte("#!/usr/bin/env python3"), 0o755); err != nil {
		t.Fatalf("write .venv python: %v", err)
	}

	linkPath := filepath.Join(projectDir, "outside-link")
	if err := os.Symlink(outsideTarget, linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	targets := collectACLTargets(projectDir)
	got := make(map[string]bool, len(targets))
	for _, target := range targets {
		got[target] = true
	}

	for _, want := range []string{
		keepFile,
		subdir,
		nestedFile,
		// Skipped dirs are included (for inheritable ACL) but not their contents.
		gitDir,
		filepath.Join(projectDir, ".venv"),
		filepath.Join(projectDir, "node_modules"),
		// node_modules inside build output dirs is still recursed into.
		filepath.Join(projectDir, ".next"),
		filepath.Join(projectDir, ".next", "standalone"),
		filepath.Join(projectDir, ".next", "standalone", "node_modules"),
		nextNodeModulesFile,
	} {
		if !got[want] {
			t.Fatalf("collectACLTargets() missing %s", want)
		}
	}

	for _, forbidden := range []string{
		linkPath,
		// Top-level node_modules is skipped entirely (dir included, contents not).
		filepath.Join(nodeModulesDir, "index.js"),
		// Contents inside skipped dirs are not collected.
		gitHead,
		gitObjectDir,
		gitObject,
		venvBinDir,
		venvPython,
	} {
		if got[forbidden] {
			t.Fatalf("collectACLTargets() unexpectedly included %s", forbidden)
		}
	}
}

func TestCollectAgentTraverseTargets(t *testing.T) {
	t.Parallel()

	homeDir := filepath.Join(string(os.PathSeparator), "Users", "dr")
	projectDir := filepath.Join(homeDir, "workspace", "niche-sieve")

	got := collectAgentTraverseTargets(homeDir, projectDir, []string{
		filepath.Join(homeDir, ".local", "share", "uv"),
		filepath.Join(homeDir, "workspace", "niche-sieve", ".venv"),
		"/opt/homebrew",
		filepath.Join(homeDir, ".local", "share", "uv"),
	})

	want := []string{
		filepath.Join(homeDir, ".local"),
		filepath.Join(homeDir, ".local", "share"),
		filepath.Join(homeDir, ".local", "share", "uv"),
	}
	if len(got) != len(want) {
		t.Fatalf("collectAgentTraverseTargets() count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, path := range want {
		if got[i] != path {
			t.Fatalf("collectAgentTraverseTargets()[%d] = %q, want %q (all=%v)", i, got[i], path, got)
		}
	}
}

func TestCollectAgentTraverseTargetsIncludesProjectParent(t *testing.T) {
	t.Parallel()

	homeDir := filepath.Join(string(os.PathSeparator), "Users", "rv")
	projectDir := filepath.Join(homeDir, "workspace", "test-hz")

	// Simulate what buildNativeSessionMutationPlan does: add filepath.Dir(projectDir)
	parentDir := filepath.Dir(projectDir)
	got := collectAgentTraverseTargets(homeDir, projectDir, []string{parentDir})

	want := []string{
		filepath.Join(homeDir, "workspace"),
	}
	if len(got) != len(want) {
		t.Fatalf("collectAgentTraverseTargets() with project parent: count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, path := range want {
		if got[i] != path {
			t.Fatalf("collectAgentTraverseTargets()[%d] = %q, want %q (all=%v)", i, got[i], path, got)
		}
	}
}

func TestCollectAgentTraverseTargetsDeeplyNested(t *testing.T) {
	t.Parallel()

	homeDir := filepath.Join(string(os.PathSeparator), "Users", "rv")
	projectDir := filepath.Join(homeDir, "code", "work", "api", "myproject")

	parentDir := filepath.Dir(projectDir)
	got := collectAgentTraverseTargets(homeDir, projectDir, []string{parentDir})

	want := []string{
		filepath.Join(homeDir, "code"),
		filepath.Join(homeDir, "code", "work"),
		filepath.Join(homeDir, "code", "work", "api"),
	}
	if len(got) != len(want) {
		t.Fatalf("collectAgentTraverseTargets() deeply nested: count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, path := range want {
		if got[i] != path {
			t.Fatalf("collectAgentTraverseTargets()[%d] = %q, want %q (all=%v)", i, got[i], path, got)
		}
	}
}

func TestPendingAgentTraverseTargetsIncludesHomeSafetyNet(t *testing.T) {
	savedUserHomeDir := currentUserHomeDir
	savedPathAllows := pathAllowsAgentTraverse
	t.Cleanup(func() {
		currentUserHomeDir = savedUserHomeDir
		pathAllowsAgentTraverse = savedPathAllows
	})

	homeDir := filepath.Join(string(os.PathSeparator), "Users", "rv")
	projectDir := filepath.Join(homeDir, "workspace", "test-hz")
	parentDir := filepath.Dir(projectDir)

	currentUserHomeDir = func() (string, error) {
		return homeDir, nil
	}
	pathAllowsAgentTraverse = func(path string) bool {
		return false
	}

	got := pendingAgentTraverseTargets(projectDir, []string{parentDir})
	want := []string{
		homeDir,
		parentDir,
	}
	if len(got) != len(want) {
		t.Fatalf("pendingAgentTraverseTargets() count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, path := range want {
		if got[i] != path {
			t.Fatalf("pendingAgentTraverseTargets()[%d] = %q, want %q (all=%v)", i, got[i], path, got)
		}
	}
}

func TestPendingLaunchHelperTraverseTargetsIncludesHomeAndParents(t *testing.T) {
	savedUserHomeDir := currentUserHomeDir
	savedPathAllows := pathAllowsAgentTraverse
	t.Cleanup(func() {
		currentUserHomeDir = savedUserHomeDir
		pathAllowsAgentTraverse = savedPathAllows
	})

	homeDir := filepath.Join(string(os.PathSeparator), "Users", "rv")
	helperPath := filepath.Join(homeDir, ".local", "libexec", "hazmat-launch")

	currentUserHomeDir = func() (string, error) {
		return homeDir, nil
	}
	pathAllowsAgentTraverse = func(path string) bool {
		return false
	}

	got := pendingLaunchHelperTraverseTargets(helperPath)
	want := []string{
		homeDir,
		filepath.Join(homeDir, ".local"),
		filepath.Join(homeDir, ".local", "libexec"),
	}
	if len(got) != len(want) {
		t.Fatalf("pendingLaunchHelperTraverseTargets() count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, path := range want {
		if got[i] != path {
			t.Fatalf("pendingLaunchHelperTraverseTargets()[%d] = %q, want %q (all=%v)", i, got[i], path, got)
		}
	}
}

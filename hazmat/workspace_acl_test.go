package hazmat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rowFromLine is a test helper that parses a single ls -leOd row. Fails the
// test if the line does not parse — shared by all row-level tests below.
func rowFromLine(t *testing.T, line string) ACLRow {
	t.Helper()
	row, ok := parseACLRow(line)
	if !ok {
		t.Fatalf("parseACLRow(%q) returned ok=false", line)
	}
	return row
}

func rowForGrant(grant ACLGrant) ACLRow {
	return ACLRow{
		Principal: grant.Principal,
		Kind:      ACLAllow,
		Perms:     append([]string(nil), grant.Perms...),
		Inherit:   grant.Inherit,
	}
}

func TestACLRowSatisfiesDevGroupGrant(t *testing.T) {
	t.Parallel()

	// Directory rendering: macOS normalizes execute→search and splits
	// read/write into directory-specific verbs (list, add_file,
	// add_subdirectory, delete_child). Files render with the original
	// chmod-input tokens (read, execute).
	dirInherit := " 0: group:dev allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity,file_inherit,directory_inherit"
	dirNoInherit := " 0: group:dev allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity"
	fileRendered := " 0: group:dev allow read,write,execute,append,delete,readattr,writeattr,readextattr,writeextattr,readsecurity"
	dirDenyInherit := " 0: group:dev deny list,add_file,search,file_inherit,directory_inherit"
	otherGroup := " 0: group:staff allow list,add_file,search,file_inherit,directory_inherit"
	userPrincipal := " 0: user:agent allow execute"

	cases := []struct {
		name            string
		line            string
		wantInheritable bool // Satisfies(devGroupInheritableGrant)
		wantNonInherit  bool // Satisfies(devGroupGrant)
	}{
		{"dir-inherit", dirInherit, true, true},
		{"dir-no-inherit", dirNoInherit, false, true},
		{"file-rendered", fileRendered, false, true},
		{"deny-entry", dirDenyInherit, false, false},
		{"other-group", otherGroup, false, false},
		{"user-principal-not-dev-group", userPrincipal, false, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := rowFromLine(t, tc.line)
			if got := row.Satisfies(devGroupInheritableGrant); got != tc.wantInheritable {
				t.Errorf("Satisfies(inheritable) = %t, want %t", got, tc.wantInheritable)
			}
			if got := row.Satisfies(devGroupGrant); got != tc.wantNonInherit {
				t.Errorf("Satisfies(non-inherit) = %t, want %t", got, tc.wantNonInherit)
			}
		})
	}
}

func TestACLRowSatisfiesAgentTraverseGrant(t *testing.T) {
	t.Parallel()

	// Home directory renders traverse as "search" (directory normalization);
	// files render as "execute". Both must satisfy the grant.
	dirRendered := " 0: user:agent allow search,readattr,readextattr,readsecurity"
	fileRendered := " 0: user:agent allow execute,readattr,readextattr,readsecurity"
	denyEntry := " 0: user:agent deny search"
	differentUser := " 0: user:other allow search"
	groupPrincipal := " 0: group:dev allow search,file_inherit,directory_inherit"

	cases := []struct {
		name          string
		line          string
		wantSatisfies bool
		wantTraverse  bool // GrantsPerm("execute")
	}{
		{"dir-rendered-search", dirRendered, true, true},
		{"file-rendered-execute", fileRendered, true, true},
		{"deny-entry", denyEntry, false, true}, // GrantsPerm sees the token regardless of kind
		{"different-user", differentUser, false, true},
		{"group-principal-not-user", groupPrincipal, false, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := rowFromLine(t, tc.line)
			if got := row.Satisfies(agentTraverseGrant); got != tc.wantSatisfies {
				t.Errorf("Satisfies(agentTraverse) = %t, want %t", got, tc.wantSatisfies)
			}
			if got := row.GrantsPerm("execute"); got != tc.wantTraverse {
				t.Errorf("GrantsPerm(\"execute\") = %t, want %t", got, tc.wantTraverse)
			}
		})
	}
}

func TestParseACLRowRejectsNonACLLines(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"drwxr-xr-x  10 dr  staff  320 Apr 18 10:00 .",
		"total 0",
		"garbage line without colon",
		" X: group:dev allow search",  // non-numeric index
		" 0: group:dev unknownkind x", // unknown kind token
		" 0: group:dev allow",         // missing perms field
	}

	for _, line := range cases {
		line := line
		t.Run(strings.ReplaceAll(line, " ", "_"), func(t *testing.T) {
			t.Parallel()
			if _, ok := parseACLRow(line); ok {
				t.Fatalf("parseACLRow(%q) should have returned ok=false", line)
			}
		})
	}
}

func TestParseACLRowHandlesInheritedFlag(t *testing.T) {
	t.Parallel()

	// Entries propagated from a parent's inheritable ACL carry an
	// "inherited" token between principal and kind. It must not confuse
	// the parser or alter Satisfies.
	line := " 1: user:agent inherited allow search,readattr,readextattr,readsecurity"
	row := rowFromLine(t, line)
	if row.Kind != ACLAllow {
		t.Fatalf("Kind = %v, want ACLAllow", row.Kind)
	}
	if row.Principal != "user:agent" {
		t.Fatalf("Principal = %q, want user:agent", row.Principal)
	}
	if !row.Satisfies(agentTraverseGrant) {
		t.Fatal("inherited allow row should satisfy agent traverse grant")
	}
}

func TestHasACLSatisfyingMultiLine(t *testing.T) {
	t.Parallel()

	// A deny line for the agent principal followed by an unrelated allow
	// line must not be mistaken for an agent allow entry. This is the
	// line-scoping invariant that the pre-refactor substring parser lacked.
	output := "drwxr-xr-x+ 10 dr  staff  320 Apr 18 10:00 .\n" +
		" 0: user:agent deny search\n" +
		" 1: user:other allow search\n"

	var rows []ACLRow
	for _, line := range strings.Split(output, "\n") {
		if row, ok := parseACLRow(line); ok {
			rows = append(rows, row)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("parsed %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Satisfies(agentTraverseGrant) {
			t.Fatalf("row %+v must not satisfy agent traverse grant", r)
		}
	}
}

func TestACLGrantString(t *testing.T) {
	t.Parallel()

	want := "user:agent allow execute,readattr,readextattr,readsecurity"
	if got := agentTraverseGrant.String(); got != want {
		t.Errorf("agentTraverseGrant.String() = %q, want %q", got, want)
	}

	inheritSuffix := ",file_inherit,directory_inherit"
	if got := devGroupInheritableGrant.String(); !strings.HasSuffix(got, inheritSuffix) {
		t.Errorf("devGroupInheritableGrant.String() = %q, should end with %q", got, inheritSuffix)
	}
	if got := devGroupGrant.String(); strings.HasSuffix(got, inheritSuffix) {
		t.Errorf("devGroupGrant.String() = %q, must not end with inherit flags", got)
	}
}

func TestACLPermAliases(t *testing.T) {
	t.Parallel()

	// The normalization table is the single reviewable place for macOS
	// directory ACL rendering. Lock down the mappings that matter for
	// traverse and collaborative-write semantics.
	cases := []struct {
		input string
		want  []string
	}{
		{"execute", []string{"execute", "search"}},
		{"read", []string{"read", "list"}},
		{"write", []string{"write", "add_file", "add_subdirectory"}},
		{"append", []string{"append", "add_subdirectory"}},
		{"delete", []string{"delete"}},
		{"readattr", []string{"readattr"}},
		{"unknown_perm", []string{"unknown_perm"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := aclPermAliases(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("aclPermAliases(%q) = %v, want %v", tc.input, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("aclPermAliases(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
				}
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

type batchRecordingACLBackend struct {
	chmods [][]string
}

func (b *batchRecordingACLBackend) ReadACLs(string) ([]ACLRow, error) {
	return nil, nil
}

func (b *batchRecordingACLBackend) Chmod(args ...string) error {
	b.chmods = append(b.chmods, append([]string(nil), args...))
	return nil
}

func (b *batchRecordingACLBackend) SudoChmod(*Runner, string, ...string) error {
	return nil
}

type pathRecordingACLBackend struct {
	rows   map[string][]ACLRow
	reads  []string
	chmods [][]string
}

func (b *pathRecordingACLBackend) ReadACLs(path string) ([]ACLRow, error) {
	b.reads = append(b.reads, path)
	return b.rows[path], nil
}

func (b *pathRecordingACLBackend) Chmod(args ...string) error {
	b.chmods = append(b.chmods, append([]string(nil), args...))
	return nil
}

func (b *pathRecordingACLBackend) SudoChmod(*Runner, string, ...string) error {
	return nil
}

func TestProjectNeedsACLRepairOnlyChecksRoot(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".venv", "bin"), 0o755); err != nil {
		t.Fatalf("mkdir .venv: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "pkg", "deep"), 0o755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}

	backend := &pathRecordingACLBackend{
		rows: map[string][]ACLRow{
			projectDir: {rowForGrant(devGroupInheritableGrant)},
		},
	}
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return backend
	}
	t.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	if projectNeedsACLRepair(projectDir) {
		t.Fatalf("projectNeedsACLRepair() = true, want false for healthy root ACL")
	}
	if len(backend.reads) != 1 || backend.reads[0] != projectDir {
		t.Fatalf("projectNeedsACLRepair read ACLs at %v, want only %s", backend.reads, projectDir)
	}
}

func TestInspectCheckWorkspaceReadinessIsReadOnly(t *testing.T) {
	projectDir := t.TempDir()
	backend := &pathRecordingACLBackend{}
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return backend
	}
	t.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	readiness, err := inspectCheckWorkspaceReadiness(projectDir)
	if err != nil {
		t.Fatalf("inspectCheckWorkspaceReadiness: %v", err)
	}
	if !readiness.NeedsACLRepair {
		t.Fatal("NeedsACLRepair = false, want true when root has no dev ACL")
	}
	if readiness.ProjectDir != projectDir {
		t.Fatalf("ProjectDir = %q, want %q", readiness.ProjectDir, projectDir)
	}
	if len(backend.reads) != 1 || backend.reads[0] != projectDir {
		t.Fatalf("ACL reads = %v, want only %s", backend.reads, projectDir)
	}
	if len(backend.chmods) != 0 {
		t.Fatalf("ACL chmods = %v, want none for read-only check inspection", backend.chmods)
	}
}

func TestInspectCheckWorkspaceReadinessAcceptsHealthyRootACL(t *testing.T) {
	projectDir := t.TempDir()
	backend := &pathRecordingACLBackend{
		rows: map[string][]ACLRow{
			projectDir: {rowForGrant(devGroupInheritableGrant)},
		},
	}
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return backend
	}
	t.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	readiness, err := inspectCheckWorkspaceReadiness(projectDir)
	if err != nil {
		t.Fatalf("inspectCheckWorkspaceReadiness: %v", err)
	}
	if readiness.NeedsACLRepair {
		t.Fatal("NeedsACLRepair = true, want false for healthy root ACL")
	}
	if len(backend.chmods) != 0 {
		t.Fatalf("ACL chmods = %v, want none for read-only check inspection", backend.chmods)
	}
}

func TestApplyDevACLStartupRepairIsBounded(t *testing.T) {
	projectDir := t.TempDir()
	deepFile := filepath.Join(projectDir, "pkg", "deep", "deeper", "file.go")
	if err := os.MkdirAll(filepath.Dir(deepFile), 0o755); err != nil {
		t.Fatalf("mkdir deep tree: %v", err)
	}
	if err := os.WriteFile(deepFile, []byte("package pkg"), 0o644); err != nil {
		t.Fatalf("write deep file: %v", err)
	}
	for i := 0; i < projectACLStartupMaxTargets+100; i++ {
		path := filepath.Join(projectDir, fmt.Sprintf("file-%04d.go", i))
		if err := os.WriteFile(path, []byte("package main"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	backend := &batchRecordingACLBackend{}
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return backend
	}
	t.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	start := time.Now()
	result := applyDevACLStartupRepairResult(projectDir)
	elapsed := time.Since(start)

	if len(result.Failures) != 0 {
		t.Fatalf("Failures = %v, want none", result.Failures)
	}
	if !result.Truncated {
		t.Fatalf("Truncated = false, want true for target cap fixture")
	}
	if result.Targets > projectACLStartupMaxTargets+1 {
		t.Fatalf("Targets = %d, want <= root + cap (%d)", result.Targets, projectACLStartupMaxTargets+1)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("bounded startup ACL repair took %s on synthetic large tree, want <= 2s", elapsed)
	}
	for _, args := range backend.chmods {
		for _, arg := range args[2:] {
			if arg == deepFile {
				t.Fatalf("startup repair chmoded deep file %s; args=%v", deepFile, backend.chmods)
			}
		}
	}
}

func BenchmarkProjectACLStartupRepairLargeTree(b *testing.B) {
	projectDir := b.TempDir()
	for dirIdx := 0; dirIdx < 150; dirIdx++ {
		dir := filepath.Join(projectDir, fmt.Sprintf("pkg-%03d", dirIdx), "internal", "deep")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatalf("mkdir %s: %v", dir, err)
		}
		for fileIdx := 0; fileIdx < 50; fileIdx++ {
			path := filepath.Join(dir, fmt.Sprintf("file-%03d.go", fileIdx))
			if err := os.WriteFile(path, []byte("package pkg"), 0o644); err != nil {
				b.Fatalf("write %s: %v", path, err)
			}
		}
	}

	backend := &batchRecordingACLBackend{}
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return backend
	}
	b.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backend.chmods = nil
		result := applyDevACLStartupRepairResult(projectDir)
		if len(result.Failures) != 0 {
			b.Fatalf("Failures = %v, want none", result.Failures)
		}
		if result.Targets > projectACLStartupMaxTargets+1 {
			b.Fatalf("Targets = %d, want <= root + cap (%d)", result.Targets, projectACLStartupMaxTargets+1)
		}
	}
}

func TestApplyACLGrantToPathBatchesChunksChmodCalls(t *testing.T) {
	backend := &batchRecordingACLBackend{}
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return backend
	}
	t.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	var paths []string
	for i := 0; i < aclChmodBatchMaxPaths+3; i++ {
		paths = append(paths, filepath.Join("/tmp/project", fmt.Sprintf("file-%03d", i)))
	}

	result := applyACLGrantToPathBatches(directACLInvoker{}, devGroupGrant, paths)
	if result.Targets != len(paths) {
		t.Fatalf("Targets = %d, want %d", result.Targets, len(paths))
	}
	if len(result.Failures) != 0 {
		t.Fatalf("Failures = %v, want none", result.Failures)
	}
	if len(backend.chmods) != 2 {
		t.Fatalf("chmod call count = %d, want 2", len(backend.chmods))
	}
	first := backend.chmods[0]
	if len(first) != 2+aclChmodBatchMaxPaths {
		t.Fatalf("first chmod arg count = %d, want %d", len(first), 2+aclChmodBatchMaxPaths)
	}
	if first[0] != "+a" || first[1] != devGroupGrant.String() {
		t.Fatalf("first chmod prefix = %v, want +a dev grant", first[:2])
	}
	second := backend.chmods[1]
	if len(second) != 2+3 {
		t.Fatalf("second chmod arg count = %d, want %d", len(second), 2+3)
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestACLTagOutputHasDevACL(t *testing.T) {
	t.Parallel()

	withInherit := "0: group:dev allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity,file_inherit,directory_inherit"
	if !aclOutputHasDevACL(withInherit, true) {
		t.Fatal("expected inheritable ACL to match")
	}

	withoutInherit := "0: group:dev allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity"
	if aclOutputHasDevACL(withoutInherit, true) {
		t.Fatal("expected non-inheritable ACL to fail inherit check")
	}
	if !aclOutputHasDevACL(withoutInherit, false) {
		t.Fatal("expected non-inheritable ACL to match when inherit is not required")
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

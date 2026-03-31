package main

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestCollectACLTargetsSkipsSymlinksAndDependencyDirs(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref"), 0o644); err != nil {
		t.Fatalf("write .git/HEAD: %v", err)
	}

	nodeModulesDir := filepath.Join(projectDir, "node_modules", "pkg")
	if err := os.MkdirAll(nodeModulesDir, 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nodeModulesDir, "index.js"), []byte("module.exports = {}"), 0o644); err != nil {
		t.Fatalf("write node_modules file: %v", err)
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

	for _, want := range []string{keepFile, subdir, nestedFile, gitDir} {
		if !got[want] {
			t.Fatalf("collectACLTargets() missing %s", want)
		}
	}

	for _, forbidden := range []string{
		linkPath,
		filepath.Join(gitDir, "HEAD"),
		filepath.Join(nodeModulesDir, "index.js"),
		filepath.Join(projectDir, "node_modules"),
	} {
		if got[forbidden] {
			t.Fatalf("collectACLTargets() unexpectedly included %s", forbidden)
		}
	}
}

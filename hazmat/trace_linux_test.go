//go:build linux

package main

import (
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestLinuxTraceBackendSelection(t *testing.T) {
	backend := currentTraceBackend()
	if !backend.supported() {
		t.Fatal("linux trace backend should be supported")
	}
	if backend.name() != "linux" {
		t.Fatalf("backend = %q, want linux", backend.name())
	}
}

func TestLinuxStraceArgsWrapLaunchCommand(t *testing.T) {
	cmd := exec.Command("/tmp/hazmat", "codex", "--yes", "exec", "say ok")
	target := append([]string{cmd.Path}, cmd.Args[1:]...)
	got := linuxStraceCommandArgs(filepath.Join("/tmp", "trace", "strace.log"), target)
	want := []string{
		"-ff",
		"-ttt",
		"-T",
		"-s",
		"256",
		"-yy",
		"-o",
		filepath.Join("/tmp", "trace", "strace.log"),
		"--",
		"/tmp/hazmat",
		"codex",
		"--yes",
		"exec",
		"say ok",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("linuxStraceArgs = %v, want %v", got, want)
	}
}

func TestLinuxTraceIndicatorFilesIncludeStraceOutputs(t *testing.T) {
	files := linuxTraceBackend{}.indicatorFiles()
	if !slices.Contains(files, "strace.log") {
		t.Fatalf("indicator files missing strace.log: %v", files)
	}
	if !slices.Contains(files, "strace.log.*") {
		t.Fatalf("indicator files missing strace glob: %v", files)
	}
	if !slices.Contains(files, "before-proc-process-status.txt") {
		t.Fatalf("indicator files missing process proc snapshot: %v", files)
	}
}

func TestLinuxTracePIDFromPSLine(t *testing.T) {
	got, ok := linuxTracePIDFromPSLine("123 1 1 agent S 00:01 /home/agent/.codex/bin/codex")
	if !ok || got != "123" {
		t.Fatalf("linuxTracePIDFromPSLine valid = %q, %v; want 123, true", got, ok)
	}
	if got, ok := linuxTracePIDFromPSLine("PID PPID PGID USER STAT ELAPSED COMMAND"); ok || got != "" {
		t.Fatalf("linuxTracePIDFromPSLine header = %q, %v; want empty, false", got, ok)
	}
}

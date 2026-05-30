//go:build hazmat_debug

package main

import (
	"slices"
	"testing"
)

func TestPlanLinuxStraceUsesResolvedTool(t *testing.T) {
	plan := planLinuxStrace(func(name string) (string, bool) {
		if name != "strace" {
			t.Fatalf("resolver asked for %q, want strace", name)
		}
		return "/usr/bin/strace", true
	})
	if !plan.Enabled {
		t.Fatal("expected strace plan to be enabled")
	}
	if plan.ToolPath != "/usr/bin/strace" {
		t.Fatalf("ToolPath = %q, want /usr/bin/strace", plan.ToolPath)
	}
	if plan.MissingReason != "" {
		t.Fatalf("MissingReason = %q, want empty", plan.MissingReason)
	}
}

func TestPlanLinuxStraceFailsWhenMissing(t *testing.T) {
	plan := planLinuxStrace(func(string) (string, bool) {
		return "", false
	})
	if plan.Enabled {
		t.Fatal("expected missing strace to disable plan")
	}
	if plan.MissingReason == "" {
		t.Fatal("expected strict failure reason for missing strace")
	}
}

func TestLinuxStraceCommandArgs(t *testing.T) {
	got := linuxStraceCommandArgs("/tmp/trace/strace.log", []string{"/tmp/hazmat", "codex", "--yes", "exec", "say ok"})
	want := []string{
		"-ff",
		"-ttt",
		"-T",
		"-s", "256",
		"-yy",
		"-o", "/tmp/trace/strace.log",
		"--",
		"/tmp/hazmat", "codex", "--yes", "exec", "say ok",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("linuxStraceCommandArgs = %v, want %v", got, want)
	}
}

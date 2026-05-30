//go:build hazmat_debug

package main

import (
	"slices"
	"testing"
)

func TestPlanLinuxStraceUsesResolvedTool(t *testing.T) {
	plan := planLinuxStrace(traceOptions{Syscalls: true}, func(name string) (string, bool) {
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
	if plan.DegradedReason != "" {
		t.Fatalf("DegradedReason = %q, want empty", plan.DegradedReason)
	}
}

func TestPlanLinuxStraceDisablesWhenMissing(t *testing.T) {
	plan := planLinuxStrace(traceOptions{Syscalls: true}, func(string) (string, bool) {
		return "", false
	})
	if plan.Enabled {
		t.Fatal("expected missing strace to disable plan")
	}
	if plan.DegradedReason == "" {
		t.Fatal("expected strict failure reason for missing strace")
	}
}

func TestPlanLinuxStraceDisabledByOption(t *testing.T) {
	plan := planLinuxStrace(traceOptions{Syscalls: false}, func(string) (string, bool) {
		t.Fatal("resolver should not be called when syscall tracing is disabled")
		return "", false
	})
	if plan.Enabled || plan.ToolPath != "" || plan.DegradedReason != "" {
		t.Fatalf("plan = %+v, want zero plan", plan)
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

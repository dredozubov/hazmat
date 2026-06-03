package hookruntime

import (
	"slices"
	"testing"
)

func TestGitHookWrapperCommandRunsInjectedRunner(t *testing.T) {
	var gotProject string
	var gotArgs []string
	cmd := NewGitHookWrapperCommand(func(projectDir string, args []string) error {
		gotProject = projectDir
		gotArgs = append([]string(nil), args...)
		return nil
	})
	cmd.SetArgs([]string{"--project", "/work/project", "commit", "message"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if gotProject != "/work/project" || !slices.Equal(gotArgs, []string{"commit", "message"}) {
		t.Fatalf("runner got project=%q args=%v", gotProject, gotArgs)
	}
}

func TestGitHookDispatchCommandRunsInjectedRunner(t *testing.T) {
	var gotProject string
	var gotHook string
	var gotArgs []string
	cmd := NewGitHookDispatchCommand(func(projectDir, hookName string, args []string) error {
		gotProject = projectDir
		gotHook = hookName
		gotArgs = append([]string(nil), args...)
		return nil
	})
	cmd.SetArgs([]string{"--project", "/work/project", "--hook", "pre-commit", "payload"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if gotProject != "/work/project" || gotHook != "pre-commit" || !slices.Equal(gotArgs, []string{"payload"}) {
		t.Fatalf("runner got project=%q hook=%q args=%v", gotProject, gotHook, gotArgs)
	}
}

func TestGitHookFallbackCommandRunsInjectedRefusal(t *testing.T) {
	var gotProject string
	var gotHook string
	cmd := NewGitHookFallbackCommand(func(projectDir, hookName string) error {
		gotProject = projectDir
		gotHook = hookName
		return nil
	})
	cmd.SetArgs([]string{"--project", "/work/project", "--hook", "commit-msg", "ignored"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if gotProject != "/work/project" || gotHook != "commit-msg" {
		t.Fatalf("runner got project=%q hook=%q", gotProject, gotHook)
	}
}

//go:build darwin

package main

import (
	"reflect"
	"testing"
)

func TestDarwinNativeLaunchSudoArgsShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range terminalEnvPassthroughKeys {
		t.Setenv(key, "")
	}
	t.Setenv("TERMINFO", "")
	t.Setenv("TERMINFO_DIRS", "")

	cfg := sessionConfig{
		ProjectDir: "/Users/dr/workspace/project",
		ReadDirs:   []string{"/Users/dr/workspace/reference"},
		WriteDirs:  []string{"/Users/dr/.cache/project"},
	}
	policy := nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-test.sb"}
	script := `echo "$1"`

	got := nativeLaunchSudoArgs(cfg, policy, []string{"RUNTIME_ENV=1"}, script, "arg1")
	want := []string{
		"-u", agentUser,
		launchHelperPath(), policy.Path,
		"/usr/bin/env", "-i",
	}
	want = append(want, agentEnvPairs(cfg)...)
	want = append(want, "RUNTIME_ENV=1", "/bin/zsh", "-lc", script, "zsh", "arg1")

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native launch sudo args = %#v, want %#v", got, want)
	}
}

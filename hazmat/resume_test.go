package main

import (
	"testing"
)

func TestSanitizePathForClaude(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/Users/dr/workspace/sandboxing", "-Users-dr-workspace-sandboxing"},
		{"/Users/dr/workspace/my-project", "-Users-dr-workspace-my-project"},
		{"/tmp/foo", "-tmp-foo"},
		{"simple", "simple"},
		{"/a/b/c", "-a-b-c"},
	}
	for _, tt := range tests {
		got := sanitizePathForClaude(tt.input)
		if got != tt.want {
			t.Errorf("sanitizePathForClaude(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectResumeFlagsNone(t *testing.T) {
	r, target, c := detectResumeFlags([]string{"-p", "hello", "--model", "sonnet"})
	if r || c || target != "" {
		t.Fatalf("expected no resume/continue, got resume=%v target=%q continue=%v", r, target, c)
	}
}

func TestDetectResumeFlagsContinue(t *testing.T) {
	for _, flag := range []string{"--continue", "-c"} {
		r, _, c := detectResumeFlags([]string{flag})
		if r {
			t.Fatalf("%s: unexpected resume=true", flag)
		}
		if !c {
			t.Fatalf("%s: expected continue=true", flag)
		}
	}
}

func TestDetectResumeFlagsResumeNoTarget(t *testing.T) {
	for _, flag := range []string{"--resume", "-r"} {
		r, target, c := detectResumeFlags([]string{flag})
		if !r {
			t.Fatalf("%s: expected resume=true", flag)
		}
		if c {
			t.Fatalf("%s: unexpected continue=true", flag)
		}
		if target != "" {
			t.Fatalf("%s: expected empty target, got %q", flag, target)
		}
	}
}

func TestDetectResumeFlagsResumeWithUUID(t *testing.T) {
	uuid := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	r, target, _ := detectResumeFlags([]string{"--resume", uuid})
	if !r {
		t.Fatal("expected resume=true")
	}
	if target != uuid {
		t.Fatalf("target = %q, want %q", target, uuid)
	}
}

func TestDetectResumeFlagsResumeEquals(t *testing.T) {
	uuid := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	r, target, _ := detectResumeFlags([]string{"--resume=" + uuid})
	if !r {
		t.Fatal("expected resume=true")
	}
	if target != uuid {
		t.Fatalf("target = %q, want %q", target, uuid)
	}
}

func TestDetectResumeFlagsResumeSkipsFlags(t *testing.T) {
	// --resume followed by a flag (not a session ID) should not capture it.
	r, target, _ := detectResumeFlags([]string{"--resume", "--model", "sonnet"})
	if !r {
		t.Fatal("expected resume=true")
	}
	if target != "" {
		t.Fatalf("target should be empty when next arg is a flag, got %q", target)
	}
}

func TestDetectResumeFlagsMixedWithOtherArgs(t *testing.T) {
	r, target, c := detectResumeFlags([]string{
		"-p", "hello", "--continue", "--model", "sonnet",
	})
	if r {
		t.Fatal("unexpected resume=true")
	}
	if !c {
		t.Fatal("expected continue=true")
	}
	if target != "" {
		t.Fatalf("expected empty target, got %q", target)
	}
}

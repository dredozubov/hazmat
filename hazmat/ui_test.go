package main

import (
	"os"
	"testing"
)

func TestUIChooseBlankInputReturnsDefaultChoice(t *testing.T) {
	restoreTTY := stubTerminal(t, true)
	defer restoreTTY()

	restoreStdin := stubStdinFile(t, "\n")
	defer restoreStdin()

	got, err := (&UI{}).Choose(
		"How should Hazmat use this selection?",
		[]UIChoice{
			{Key: "use-now", Label: "Use selected now"},
			{Key: "always", Label: "Always use for this project"},
			{Key: "not-now", Label: "Not now"},
		},
		"always",
	)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got != "always" {
		t.Fatalf("Choose(blank input) = %q, want always", got)
	}
}

func stubStdinFile(t *testing.T, content string) func() {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatalf("create temp stdin: %v", err)
	}
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		t.Fatalf("write temp stdin: %v", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		t.Fatalf("seek temp stdin: %v", err)
	}

	saved := os.Stdin
	os.Stdin = file
	return func() {
		os.Stdin = saved
		if err := file.Close(); err != nil {
			t.Fatalf("close temp stdin: %v", err)
		}
	}
}

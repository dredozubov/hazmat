package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupZshCompletionsInstallsGeneratedScriptAndCleansLegacyBlock(t *testing.T) {
	tmp := t.TempDir()
	env := testCompletionEnv(t, tmp)
	runner := newFakeToolingRunner(t)

	if err := SetupZshCompletions(env, &fakeToolingUI{}, runner); err != nil {
		t.Fatalf("SetupZshCompletions: %v", err)
	}

	if got := runner.sudoWrites[env.CompletionFile]; got != "#compdef hazmat\n" {
		t.Fatalf("completion write = %q, want generated script", got)
	}
	if _, err := os.Stat(filepath.Join(env.LegacyCompletionDir, "_hazmat")); !os.IsNotExist(err) {
		t.Fatalf("legacy completion file still exists or stat failed: %v", err)
	}
	rcData, err := os.ReadFile(env.ShellProfiles[0].RCPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rcData), env.CompletionBlockStart) || !strings.Contains(string(rcData), "export KEEP=1") {
		t.Fatalf("legacy completion block cleanup = %q", string(rcData))
	}
}

func TestSetupZshCompletionsSkipsNonZshShell(t *testing.T) {
	tmp := t.TempDir()
	env := testCompletionEnv(t, tmp)
	env.ShellName = "bash"
	env.GenerateZshCompletion = func() (string, error) {
		t.Fatal("GenerateZshCompletion should not run for non-zsh shell")
		return "", nil
	}

	if err := SetupZshCompletions(env, &fakeToolingUI{}, newFakeToolingRunner(t)); err != nil {
		t.Fatalf("SetupZshCompletions: %v", err)
	}
}

func TestRollbackZshCompletionsRemovesSystemAndLegacyState(t *testing.T) {
	tmp := t.TempDir()
	env := testCompletionEnv(t, tmp)
	runner := newFakeToolingRunner(t)
	if err := os.MkdirAll(filepath.Dir(env.CompletionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.CompletionFile, []byte("#compdef hazmat\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	RollbackZshCompletions(env, &fakeToolingUI{}, runner)

	if _, err := os.Stat(filepath.Join(env.LegacyCompletionDir, "_hazmat")); !os.IsNotExist(err) {
		t.Fatalf("legacy completion file still exists or stat failed: %v", err)
	}
	rcData, err := os.ReadFile(env.ShellProfiles[0].RCPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rcData), env.CompletionBlockStart) {
		t.Fatalf("legacy completion block still present:\n%s", string(rcData))
	}
}

func testCompletionEnv(t *testing.T, tmp string) CompletionEnv {
	t.Helper()
	legacyDir := filepath.Join(tmp, "legacy")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "_hazmat"), []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rcPath := filepath.Join(tmp, ".zshrc")
	rc := UpsertManagedBlock("export KEEP=1\n", "# >>> hazmat completions >>>", "# <<< hazmat completions <<<", "fpath=(legacy $fpath)")
	if err := os.WriteFile(rcPath, []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	return CompletionEnv{
		ShellName:            "zsh",
		SystemCompletionDir:  filepath.Join(tmp, "system"),
		CompletionFile:       filepath.Join(tmp, "system", "_hazmat"),
		LegacyCompletionDir:  legacyDir,
		CompletionBlockStart: "# >>> hazmat completions >>>",
		CompletionBlockEnd:   "# <<< hazmat completions <<<",
		ShellProfiles: []ShellProfile{
			{Name: "zsh", RCPath: rcPath},
		},
		GenerateZshCompletion: func() (string, error) {
			return "#compdef hazmat\n", nil
		},
	}
}

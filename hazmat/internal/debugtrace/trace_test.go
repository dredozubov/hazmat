//go:build hazmat_debug

package debugtrace

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func testTraceEnv() Env {
	return Env{
		AgentHome:        "/Users/agent",
		AgentUser:        "agent",
		DefaultAgentPath: "/Users/agent/.local/bin:/usr/bin:/bin",
		HostLsPath:       "/bin/ls",
		HostLogPath:      "/usr/bin/log",
		HostScriptPath:   "/usr/bin/script",
		HostSudoPath:     "/usr/bin/sudo",
		HostUnamePath:    "/usr/bin/uname",
		ExpandTilde: func(path string) string {
			if strings.HasPrefix(path, "~/") {
				return filepath.Join("/Users/dr", strings.TrimPrefix(path, "~/"))
			}
			return path
		},
		RunSessionCommand: func(*exec.Cmd) error { return nil },
	}
}

func testTraceSpecs() []HarnessSpec {
	return []HarnessSpec{
		{
			ID:              "claude",
			DisplayName:     "Claude Code",
			CommandName:     "claude",
			ProcessFilters:  []string{"claude"},
			AgentStatePaths: []string{"/Users/agent/.claude"},
			HostStatePaths:  []string{"~/.hazmat/secrets/claude"},
			SampleArgs:      []string{"-p", "say ok"},
			Explain:         func([]string) (any, error) { return map[string]string{"ok": "true"}, nil },
		},
		{
			ID:              "codex",
			DisplayName:     "Codex",
			CommandName:     "codex",
			ProcessFilters:  []string{"codex"},
			AgentStatePaths: []string{"/Users/agent/.codex"},
			HostStatePaths:  []string{"~/.hazmat/secrets/codex"},
			SampleArgs:      []string{"exec", "say ok"},
			Explain:         func([]string) (any, error) { return map[string]string{"ok": "true"}, nil },
		},
		{
			ID:              "opencode",
			DisplayName:     "OpenCode",
			CommandName:     "opencode",
			ProcessFilters:  []string{"opencode"},
			AgentStatePaths: []string{"/Users/agent/.opencode"},
			HostStatePaths:  []string{"~/.hazmat/secrets/opencode"},
			SampleArgs:      []string{"run", "say ok"},
			Explain:         func([]string) (any, error) { return map[string]string{"ok": "true"}, nil },
		},
		{
			ID:              "gemini",
			DisplayName:     "Gemini",
			CommandName:     "gemini",
			ProcessFilters:  []string{"gemini"},
			AgentStatePaths: []string{"/Users/agent/.gemini"},
			HostStatePaths:  []string{"~/.hazmat/secrets/gemini"},
			SampleArgs:      []string{"-p", "say ok"},
			Explain:         func([]string) (any, error) { return map[string]string{"ok": "true"}, nil },
		},
	}
}

func TestTraceLaunchArgsAddsYesBeforeForwardedArgs(t *testing.T) {
	spec, ok := HarnessSpecByID(testTraceSpecs(), "codex")
	if !ok {
		t.Fatal("missing codex trace harness spec")
	}
	got := TraceLaunchArgs(spec, []string{"--no-backup", "exec", "say ok"})
	want := []string{"codex", "--yes", "--no-backup", "exec", "say ok"}
	if !slices.Equal(got, want) {
		t.Fatalf("TraceLaunchArgs = %v, want %v", got, want)
	}
}

func TestSanitizeTraceLabel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"Baseline Run", "baseline-run"},
		{"  sandbox/logout? probe!  ", "sandbox-logout-probe"},
		{"keep.ok_value-1", "keep.ok_value-1"},
	}
	for _, tc := range tests {
		if got := SanitizeLabel(tc.in); got != tc.want {
			t.Fatalf("SanitizeLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTraceHarnessSpecsAreComplete(t *testing.T) {
	for _, spec := range testTraceSpecs() {
		if spec.CommandName == "" {
			t.Fatalf("%s CommandName is empty", spec.ID)
		}
		if len(spec.ProcessFilters) == 0 {
			t.Fatalf("%s ProcessFilters is empty", spec.ID)
		}
		if len(spec.SampleArgs) == 0 {
			t.Fatalf("%s SampleArgs is empty", spec.ID)
		}
		if spec.Explain == nil {
			t.Fatalf("%s Explain is nil", spec.ID)
		}
	}
}

func TestCurrentTraceBackendSelection(t *testing.T) {
	backend := currentTraceBackend()
	if backend.name() == "" {
		t.Fatal("trace backend name is empty")
	}
	if runtime.GOOS == "darwin" {
		if !backend.supported() {
			t.Fatal("darwin trace backend should be supported")
		}
		if backend.name() != "darwin" {
			t.Fatalf("backend = %q, want darwin", backend.name())
		}
		return
	}
	if runtime.GOOS == "linux" {
		if !backend.supported() {
			t.Fatal("linux trace backend should be supported")
		}
		if backend.name() != "linux" {
			t.Fatalf("backend = %q, want linux", backend.name())
		}
		return
	}
	if backend.supported() {
		t.Fatalf("%s trace backend should be unsupported before platform implementation", runtime.GOOS)
	}
}

func TestPrepareTraceDirUsesPrivateTimestampedDirectory(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 28, 18, 30, 0, 0, time.UTC)

	dir, label, err := PrepareTraceDir(testTraceEnv(), testTraceSpecs(), Options{
		Harness: "claude",
		OutRoot: root,
		Name:    "Baseline Run",
	}, now)
	if err != nil {
		t.Fatalf("PrepareTraceDir: %v", err)
	}
	if label != "baseline-run" {
		t.Fatalf("label = %q, want baseline-run", label)
	}
	if !strings.HasPrefix(filepath.Base(dir), "20260528T183000Z-claude-baseline-run") {
		t.Fatalf("trace dir = %s", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat trace dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("trace dir mode = %04o, want 0700", info.Mode().Perm())
	}
}

func TestPrepareTraceDirUsesHarnessName(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 28, 18, 30, 0, 0, time.UTC)

	dir, label, err := PrepareTraceDir(testTraceEnv(), testTraceSpecs(), Options{
		Harness: "gemini",
		OutRoot: root,
		Name:    "Network None",
	}, now)
	if err != nil {
		t.Fatalf("PrepareTraceDir: %v", err)
	}
	if label != "network-none" {
		t.Fatalf("label = %q, want network-none", label)
	}
	if !strings.HasPrefix(filepath.Base(dir), "20260528T183000Z-gemini-network-none") {
		t.Fatalf("trace dir = %s", dir)
	}
}

func TestProcessSampleLineRelevantUsesHarnessFilters(t *testing.T) {
	env := testTraceEnv()
	spec, ok := HarnessSpecByID(testTraceSpecs(), "opencode")
	if !ok {
		t.Fatal("missing opencode trace harness spec")
	}
	if !processSampleLineRelevant(env, "123 1 1 agent S 00:00 /Users/agent/.opencode/bin/opencode", spec) {
		t.Fatal("expected opencode agent process to be relevant")
	}
	if !processSampleLineRelevant(env, "123 1 1 dr S 00:00 /Users/dr/.local/bin/hazmat opencode", spec) {
		t.Fatal("expected hazmat process to be relevant")
	}
	if processSampleLineRelevant(env, "123 1 1 dr S 00:00 /usr/bin/unrelated", spec) {
		t.Fatal("unexpected unrelated process relevance")
	}
}

func TestTraceIndicatorPathsExpandsSortedGlobs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"strace.log.22", "strace.log.11", "other.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("ok\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	got := traceIndicatorPaths(dir, "strace.log.*")
	want := []string{
		filepath.Join(dir, "strace.log.11"),
		filepath.Join(dir, "strace.log.22"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("traceIndicatorPaths = %v, want %v", got, want)
	}
}

func TestWriteTraceIndicatorsExpandsGlobFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "strace.log.123"), []byte("openat denied EACCES sandbox\n"), 0o600); err != nil {
		t.Fatalf("write strace fixture: %v", err)
	}

	if err := writeTraceIndicators(dir, []string{"strace.log.*"}); err != nil {
		t.Fatalf("write indicators: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "indicators.md"))
	if err != nil {
		t.Fatalf("read indicators: %v", err)
	}
	text := string(got)
	if !strings.Contains(text, "## strace.log.123") {
		t.Fatalf("indicators missing expanded file header: %s", text)
	}
	if !strings.Contains(text, "openat denied EACCES sandbox") {
		t.Fatalf("indicators missing matched line: %s", text)
	}
}

func TestPreflightTraceRuntimeRejectsPartialTraceOptions(t *testing.T) {
	if err := preflightTraceRuntime(testTraceEnv(), Options{Syscalls: false, Transcript: true}); err == nil || !strings.Contains(err.Error(), "syscall observer") {
		t.Fatalf("disabled syscalls error = %v, want syscall observer error", err)
	}
	if err := preflightTraceRuntime(testTraceEnv(), Options{Syscalls: true, Transcript: false}); err == nil || !strings.Contains(err.Error(), "PTY transcript") {
		t.Fatalf("disabled transcript error = %v, want PTY transcript error", err)
	}
}

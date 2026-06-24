package hazmat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestFileWithTime(t *testing.T, path string, content string, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if !modTime.IsZero() {
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
}

func readCodexIndexByID(t *testing.T, path string) map[string]codexSessionIndexEntry {
	t.Helper()
	entries, err := readCodexSessionIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]codexSessionIndexEntry)
	for _, entry := range entries {
		byID[codexSessionIndexID(entry)] = entry
	}
	return byID
}

func TestCodexResumeRequested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "resume", args: []string{"resume", "--last"}, want: true},
		{name: "fork", args: []string{"fork", "019e"}, want: true},
		{name: "exec resume", args: []string{"exec", "resume", "--last"}, want: true},
		{name: "plain prompt", args: []string{"explain this repo"}, want: false},
	}
	for _, tt := range tests {
		if got := codexResumeRequested(tt.args); got != tt.want {
			t.Fatalf("%s: codexResumeRequested(%v) = %v, want %v", tt.name, tt.args, got, tt.want)
		}
	}
}

func TestCodexResumeStateDirUsesExplicitHomeRoot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "session-home")
	got, err := codexResumeStateDir(home)
	if err != nil {
		t.Fatalf("codexResumeStateDir: %v", err)
	}
	want := filepath.Join(home, ".codex")
	if got != want {
		t.Fatalf("codexResumeStateDir = %s, want %s", got, want)
	}
	if _, err := codexResumeStateDir("relative-home"); err == nil {
		t.Fatal("codexResumeStateDir accepted relative home root")
	}
}

func TestOpenCodeResumeStateDirUsesExplicitHomeRoot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "session-home")
	got, err := openCodeResumeStateDir(home)
	if err != nil {
		t.Fatalf("openCodeResumeStateDir: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "opencode")
	if got != want {
		t.Fatalf("openCodeResumeStateDir = %s, want %s", got, want)
	}
	if _, err := openCodeResumeStateDir("relative-home"); err == nil {
		t.Fatal("openCodeResumeStateDir accepted relative home root")
	}
}

func TestSyncCodexResumeStateCopiesIndexAndSessions(t *testing.T) {
	hostCodex := filepath.Join(t.TempDir(), ".codex")
	agentCodex := filepath.Join(t.TempDir(), ".codex")

	hostSessionTime := time.Unix(30, 0)
	agentSessionTime := time.Unix(10, 0)
	writeTestFileWithTime(t,
		filepath.Join(hostCodex, "sessions", "2026", "05", "15", "rollout-host.jsonl"),
		"host transcript\n",
		hostSessionTime,
	)
	writeTestFileWithTime(t,
		filepath.Join(agentCodex, "sessions", "2026", "05", "15", "rollout-host.jsonl"),
		"stale transcript\n",
		agentSessionTime,
	)

	hostIndex := `{"id":"host","thread_name":"Host newer","updated_at":"2026-05-15T09:00:00Z"}` + "\n"
	agentIndex := `{"id":"host","thread_name":"Agent stale","updated_at":"2026-05-15T08:00:00Z"}` + "\n" +
		`{"id":"agent","thread_name":"Agent local","updated_at":"2026-05-15T08:30:00Z"}` + "\n"
	writeTestFileWithTime(t, filepath.Join(hostCodex, "session_index.jsonl"), hostIndex, time.Unix(20, 0))
	writeTestFileWithTime(t, filepath.Join(agentCodex, "session_index.jsonl"), agentIndex, time.Unix(15, 0))

	synced, err := syncCodexResumeStateFromDirs(hostCodex, agentCodex, localEnsureSharedResumeDir)
	if err != nil {
		t.Fatalf("syncCodexResumeStateFromDirs: %v", err)
	}
	if synced != 2 {
		t.Fatalf("synced = %d, want 2", synced)
	}

	destSession := filepath.Join(agentCodex, "sessions", "2026", "05", "15", "rollout-host.jsonl")
	data, err := os.ReadFile(destSession)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "host transcript\n" {
		t.Fatalf("session content = %q, want host transcript", data)
	}
	info, err := os.Stat(destSession)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != sharedResumeFileMode {
		t.Fatalf("session mode = %04o, want %04o", perm, sharedResumeFileMode)
	}
	if !info.ModTime().Equal(hostSessionTime) {
		t.Fatalf("session modtime = %s, want %s", info.ModTime(), hostSessionTime)
	}

	index := readCodexIndexByID(t, filepath.Join(agentCodex, "session_index.jsonl"))
	if got := index["host"]["thread_name"]; got != "Host newer" {
		t.Fatalf("host thread_name = %v, want Host newer", got)
	}
	if got := index["agent"]["thread_name"]; got != "Agent local" {
		t.Fatalf("agent thread_name = %v, want Agent local", got)
	}
}

func TestDetectOpenCodeResumeRequest(t *testing.T) {
	req := detectOpenCodeResumeRequest([]string{"run", "--session", "ses_123"})
	if !req.requested || req.sessionID != "ses_123" || req.wantsContinue {
		t.Fatalf("request = %+v, want explicit session", req)
	}

	req = detectOpenCodeResumeRequest([]string{"--continue"})
	if !req.requested || !req.wantsContinue || req.sessionID != "" {
		t.Fatalf("request = %+v, want continue", req)
	}

	req = detectOpenCodeResumeRequest([]string{"--session=ses_456"})
	if !req.requested || req.sessionID != "ses_456" {
		t.Fatalf("request = %+v, want session ses_456", req)
	}
}

func TestSyncOpenCodeResumeStateWithHooksExplicitSession(t *testing.T) {
	projectDir := "/Users/dr/workspace/personal-brand"
	var exportedSession string
	var importedPayload map[string]any

	hooks := openCodeResumeHooks{
		listLatestSessionID: func(string) (string, error) {
			t.Fatal("listLatestSessionID should not be called for explicit session")
			return "", nil
		},
		exportSession: func(project, sessionID, dest string) error {
			if project != projectDir {
				t.Fatalf("project = %q, want %q", project, projectDir)
			}
			exportedSession = sessionID
			return os.WriteFile(dest, []byte(`{"info":{"id":"`+sessionID+`"},"messages":[]}`), 0o600)
		},
		importSession: func(homeRoot, path string) error {
			if homeRoot != agentHome {
				t.Fatalf("homeRoot = %q, want %q", homeRoot, agentHome)
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			if perm := info.Mode().Perm(); perm != 0o644 {
				t.Fatalf("export temp mode = %04o, want 0644", perm)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return json.Unmarshal(data, &importedPayload)
		},
	}

	sessionID, err := syncOpenCodeResumeStateWithHooks(projectDir, []string{"--session", "ses_123"}, hooks)
	if err != nil {
		t.Fatalf("syncOpenCodeResumeStateWithHooks: %v", err)
	}
	if sessionID != "ses_123" || exportedSession != "ses_123" {
		t.Fatalf("sessionID=%q exported=%q, want ses_123", sessionID, exportedSession)
	}
	info, _ := importedPayload["info"].(map[string]any)
	if got := info["id"]; got != "ses_123" {
		t.Fatalf("imported session id = %v, want ses_123", got)
	}
}

func TestSyncOpenCodeResumeStateWithHooksContinueUsesLatest(t *testing.T) {
	projectDir := "/Users/dr/workspace/personal-brand"
	var listedProject string
	var imported bool
	hooks := openCodeResumeHooks{
		listLatestSessionID: func(project string) (string, error) {
			listedProject = project
			return "ses_latest", nil
		},
		exportSession: func(project, sessionID, dest string) error {
			if sessionID != "ses_latest" {
				t.Fatalf("export sessionID = %q, want ses_latest", sessionID)
			}
			return os.WriteFile(dest, []byte(`{"info":{"id":"ses_latest"},"messages":[]}`), 0o600)
		},
		importSession: func(homeRoot, path string) error {
			if homeRoot != agentHome {
				t.Fatalf("homeRoot = %q, want %q", homeRoot, agentHome)
			}
			imported = true
			return nil
		},
	}

	sessionID, err := syncOpenCodeResumeStateWithHooks(projectDir, []string{"--continue"}, hooks)
	if err != nil {
		t.Fatalf("syncOpenCodeResumeStateWithHooks: %v", err)
	}
	if listedProject != projectDir {
		t.Fatalf("listed project = %q, want %q", listedProject, projectDir)
	}
	if sessionID != "ses_latest" || !imported {
		t.Fatalf("sessionID=%q imported=%v, want ses_latest and imported", sessionID, imported)
	}
}

func TestSyncOpenCodeResumeStateWithHooksPassesExplicitHomeRoot(t *testing.T) {
	projectDir := "/Users/dr/workspace/personal-brand"
	homeRoot := filepath.Join(t.TempDir(), "session-home")
	var importedHome string
	hooks := openCodeResumeHooks{
		listLatestSessionID: func(string) (string, error) {
			t.Fatal("listLatestSessionID should not be called for explicit session")
			return "", nil
		},
		exportSession: func(_, _, dest string) error {
			return os.WriteFile(dest, []byte(`{"info":{"id":"ses_123"},"messages":[]}`), 0o600)
		},
		importSession: func(homeRoot, _ string) error {
			importedHome = homeRoot
			return nil
		},
	}

	sessionID, err := syncOpenCodeResumeStateWithHooksIntoHome(homeRoot, projectDir, []string{"--session", "ses_123"}, hooks)
	if err != nil {
		t.Fatalf("syncOpenCodeResumeStateWithHooksIntoHome: %v", err)
	}
	if sessionID != "ses_123" || importedHome != homeRoot {
		t.Fatalf("sessionID=%q importedHome=%q, want ses_123 and %q", sessionID, importedHome, homeRoot)
	}
}

func TestAgentResumeHomeEnvCommandArgsUsesSessionHomeXDG(t *testing.T) {
	homeRoot := filepath.Join(defaultSessionHomeRoot, "session-123", "home")
	bin := "/Users/agent/.opencode/bin/opencode"
	got, err := agentResumeHomeEnvCommandArgs(homeRoot, bin, "import", "/tmp/session.json")
	if err != nil {
		t.Fatalf("agentResumeHomeEnvCommandArgs: %v", err)
	}
	want := []string{
		"/usr/bin/env",
		"HOME=" + homeRoot,
		"XDG_CACHE_HOME=" + filepath.Join(homeRoot, ".cache"),
		"XDG_CONFIG_HOME=" + filepath.Join(homeRoot, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(homeRoot, ".local", "share"),
		bin,
		"import",
		"/tmp/session.json",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("agentResumeHomeEnvCommandArgs = %#v, want %#v", got, want)
	}
}

func TestImportAgentOpenCodeSessionIntoHomeRejectsUnsafeHome(t *testing.T) {
	homeRoot := filepath.Join(t.TempDir(), "session-home")
	err := importAgentOpenCodeSessionIntoHome(homeRoot, filepath.Join(t.TempDir(), "session.json"))
	if err == nil {
		t.Fatal("importAgentOpenCodeSessionIntoHome accepted unsafe home")
	}
	if !strings.Contains(err.Error(), "Hazmat session home") {
		t.Fatalf("error = %v, want Hazmat session home guidance", err)
	}
}

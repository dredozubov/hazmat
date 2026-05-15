package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestGeminiResumeRequested(t *testing.T) {
	for _, args := range [][]string{
		{"--resume", "latest"},
		{"-r", "0"},
		{"--resume=latest"},
		{"--list-sessions"},
	} {
		if !geminiResumeRequested(args) {
			t.Fatalf("geminiResumeRequested(%v) = false, want true", args)
		}
	}
	if geminiResumeRequested([]string{"-p", "hello"}) {
		t.Fatal("geminiResumeRequested for prompt args = true, want false")
	}
}

func TestSyncGeminiResumeStateCopiesOnlyCurrentProjectHistory(t *testing.T) {
	hostGemini := filepath.Join(t.TempDir(), ".gemini")
	agentGemini := filepath.Join(t.TempDir(), ".gemini")
	projectDir := "/Users/dr/workspace/personal-brand"

	hostProjects := `{"projects":{"/other/project":"other","` + projectDir + `":"personal-brand"}}` + "\n"
	agentProjects := `{"projects":{"/agent/project":"agent"}}` + "\n"
	writeTestFileWithTime(t, filepath.Join(hostGemini, "projects.json"), hostProjects, time.Unix(20, 0))
	writeTestFileWithTime(t, filepath.Join(agentGemini, "projects.json"), agentProjects, time.Unix(10, 0))
	writeTestFileWithTime(t, filepath.Join(hostGemini, "tmp", "personal-brand", "logs.json"), `[{"session":"one"}]`+"\n", time.Unix(30, 0))
	writeTestFileWithTime(t, filepath.Join(hostGemini, "tmp", "personal-brand", "chats", "session-one.json"), `{"ok":true}`+"\n", time.Unix(31, 0))
	writeTestFileWithTime(t, filepath.Join(hostGemini, "tmp", "personal-brand", "chats", "session-two.jsonl"), `{"ok":true}`+"\n", time.Unix(32, 0))
	writeTestFileWithTime(t, filepath.Join(hostGemini, "tmp", "personal-brand", "chats", "notes.txt"), "skip\n", time.Unix(33, 0))

	synced, err := syncGeminiResumeStateFromDirs(hostGemini, agentGemini, projectDir, localEnsureSharedResumeDir)
	if err != nil {
		t.Fatalf("syncGeminiResumeStateFromDirs: %v", err)
	}
	if synced != 4 {
		t.Fatalf("synced = %d, want 4", synced)
	}

	projects, err := readGeminiProjectsFile(filepath.Join(agentGemini, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := projects.Projects[projectDir]; got != "personal-brand" {
		t.Fatalf("project mapping = %q, want personal-brand", got)
	}
	if got := projects.Projects["/agent/project"]; got != "agent" {
		t.Fatalf("existing mapping = %q, want agent", got)
	}
	if _, ok := projects.Projects["/other/project"]; ok {
		t.Fatal("other project mapping should not be copied")
	}

	for _, rel := range []string{
		filepath.Join("tmp", "personal-brand", "logs.json"),
		filepath.Join("tmp", "personal-brand", "chats", "session-one.json"),
		filepath.Join("tmp", "personal-brand", "chats", "session-two.jsonl"),
	} {
		info, err := os.Stat(filepath.Join(agentGemini, rel))
		if err != nil {
			t.Fatalf("%s not copied: %v", rel, err)
		}
		if perm := info.Mode().Perm(); perm != sharedResumeFileMode {
			t.Fatalf("%s mode = %04o, want %04o", rel, perm, sharedResumeFileMode)
		}
	}
	if _, err := os.Stat(filepath.Join(agentGemini, "tmp", "personal-brand", "chats", "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("notes.txt should not be copied, got err=%v", err)
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
		importSession: func(path string) error {
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
		importSession: func(path string) error {
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

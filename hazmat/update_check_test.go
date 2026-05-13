package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolateUpdateCheck(t *testing.T) {
	t.Helper()
	savedPath := updateCheckStatePath
	savedURL := updateCheckURL
	savedClient := updateCheckClient
	savedNow := updateCheckNow
	savedTTY := updateCheckIsTTY
	savedGetenv := updateCheckGetenv
	savedVersion := version

	updateCheckStatePath = filepath.Join(t.TempDir(), "update-check.json")
	updateCheckURL = "http://127.0.0.1/unused"
	updateCheckClient = &http.Client{Timeout: updateCheckTimeout}
	updateCheckNow = func() time.Time { return time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC) }
	updateCheckIsTTY = func() bool { return true }
	updateCheckGetenv = func(string) string { return "" }
	version = "v0.7.0"

	t.Cleanup(func() {
		updateCheckStatePath = savedPath
		updateCheckURL = savedURL
		updateCheckClient = savedClient
		updateCheckNow = savedNow
		updateCheckIsTTY = savedTTY
		updateCheckGetenv = savedGetenv
		version = savedVersion
	})
}

func TestMaybeNotifyUpdateAvailableUsesTapMetadata(t *testing.T) {
	isolateUpdateCheck(t)

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"version": "v0.8.0",
			"formula": "dredozubov/tap/hazmat",
			"upgrade_command": "brew update && brew upgrade dredozubov/tap/hazmat",
			"release_url": "https://github.com/dredozubov/hazmat/releases/tag/v0.8.0"
		}`))
	}))
	defer server.Close()
	updateCheckURL = server.URL
	updateCheckClient = server.Client()

	var out bytes.Buffer
	maybeNotifyUpdateAvailable(&out)

	got := out.String()
	if hits != 1 {
		t.Fatalf("metadata hits = %d, want 1", hits)
	}
	for _, want := range []string{
		"hazmat: v0.8.0 is available in Homebrew (current v0.7.0)",
		"Update with: brew update && brew upgrade dredozubov/tap/hazmat",
		"Release: https://github.com/dredozubov/hazmat/releases/tag/v0.8.0",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("notification missing %q:\n%s", want, got)
		}
	}

	state, err := readUpdateCheckState()
	if err != nil {
		t.Fatalf("read update state: %v", err)
	}
	if state.Latest.Version != "v0.8.0" || state.LastNotifiedVersion != "v0.8.0" {
		t.Fatalf("state = %+v, want latest/notified v0.8.0", state)
	}
}

func TestMaybeNotifyUpdateAvailableDoesNotCallBrew(t *testing.T) {
	isolateUpdateCheck(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"v0.8.0"}`))
	}))
	defer server.Close()
	updateCheckURL = server.URL
	updateCheckClient = server.Client()

	home := t.TempDir()
	marker := filepath.Join(home, "brew-called")
	brewPath := filepath.Join(home, "brew")
	if err := os.WriteFile(brewPath, []byte("#!/bin/sh\nprintf called > \"$BREW_CALLED\"\nexit 99\n"), 0o755); err != nil {
		t.Fatalf("write fake brew: %v", err)
	}
	t.Setenv("PATH", home)
	t.Setenv("BREW_CALLED", marker)

	var out bytes.Buffer
	maybeNotifyUpdateAvailable(&out)
	if !strings.Contains(out.String(), "brew update && brew upgrade") {
		t.Fatalf("notification missing brew command:\n%s", out.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("fake brew marker err = %v, want not exist", err)
	}
}

func TestMaybeNotifyUpdateAvailableThrottlesChecksAndNotifications(t *testing.T) {
	isolateUpdateCheck(t)

	now := updateCheckNow()
	state := updateCheckState{
		CheckedAt:           now.Add(-time.Hour).Format(time.RFC3339),
		Latest:              updateReleaseMetadata{Version: "v0.8.0"},
		LastNotifiedVersion: "v0.8.0",
		LastNotifiedAt:      now.Add(-time.Hour).Format(time.RFC3339),
	}
	if err := writeUpdateCheckState(state); err != nil {
		t.Fatalf("write update state: %v", err)
	}

	var out bytes.Buffer
	maybeNotifyUpdateAvailable(&out)
	if out.Len() != 0 {
		t.Fatalf("notification = %q, want none", out.String())
	}
}

func TestMaybeNotifyUpdateAvailableSkipsNonInteractiveAndDevBuilds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		isTTY      bool
		envValue   string
		appVersion string
	}{
		{name: "not tty", isTTY: false, appVersion: "v0.7.0"},
		{name: "opt out", isTTY: true, envValue: "1", appVersion: "v0.7.0"},
		{name: "dev", isTTY: true, appVersion: "dev"},
		{name: "git describe", isTTY: true, appVersion: "v0.7.0-3-gabc1234"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUpdateCheck(t)
			updateCheckIsTTY = func() bool { return tc.isTTY }
			updateCheckGetenv = func(key string) string {
				if key == "HAZMAT_NO_UPDATE_NOTIFIER" {
					return tc.envValue
				}
				return ""
			}
			version = tc.appVersion

			var out bytes.Buffer
			maybeNotifyUpdateAvailable(&out)
			if out.Len() != 0 {
				t.Fatalf("notification = %q, want none", out.String())
			}
			if _, err := os.Stat(updateCheckStatePath); !os.IsNotExist(err) {
				t.Fatalf("state file err = %v, want not exist", err)
			}
		})
	}
}

func TestMaybeNotifyUpdateAvailableDoesNotNotifyForCurrentVersion(t *testing.T) {
	isolateUpdateCheck(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"v0.7.0"}`))
	}))
	defer server.Close()
	updateCheckURL = server.URL
	updateCheckClient = server.Client()

	var out bytes.Buffer
	maybeNotifyUpdateAvailable(&out)
	if out.Len() != 0 {
		t.Fatalf("notification = %q, want none", out.String())
	}
}

func TestWriteUpdateCheckStatePersistsSecureJSON(t *testing.T) {
	isolateUpdateCheck(t)
	state := updateCheckState{Latest: updateReleaseMetadata{Version: "v0.8.0"}}
	if err := writeUpdateCheckState(state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	info, err := os.Stat(updateCheckStatePath)
	if err != nil {
		t.Fatalf("stat state: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state mode = %o, want 600", got)
	}
	raw, err := os.ReadFile(updateCheckStatePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var decoded updateCheckState
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("state JSON is invalid: %v\n%s", err, string(raw))
	}
}

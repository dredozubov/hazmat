package hazmat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	defaultUpdateCheckURL = "https://raw.githubusercontent.com/dredozubov/homebrew-tap/master/metadata/hazmat-release.json"
	updateCheckInterval   = 24 * time.Hour
	updateCheckTimeout    = 800 * time.Millisecond
)

type updateReleaseMetadata struct {
	Version        string `json:"version"`
	ReleaseURL     string `json:"release_url,omitempty"`
	Formula        string `json:"formula,omitempty"`
	UpgradeCommand string `json:"upgrade_command,omitempty"`
	PublishedAt    string `json:"published_at,omitempty"`
	Source         string `json:"source,omitempty"`
}

type updateCheckState struct {
	CheckedAt string                `json:"checked_at,omitempty"`
	Latest    updateReleaseMetadata `json:"latest,omitempty"`
}

var (
	updateCheckStatePath = filepath.Join(os.Getenv("HOME"), ".hazmat", "update-check.json")
	updateCheckURL       = defaultUpdateCheckURL
	updateCheckClient    = &http.Client{Timeout: updateCheckTimeout}
	updateCheckNow       = time.Now
	updateCheckIsTTY     = func() bool { return term.IsTerminal(int(os.Stderr.Fd())) }
	updateCheckGetenv    = os.Getenv
	exactReleaseVersion  = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)
)

func maybeNotifyUpdateAvailable(w io.Writer) {
	if w == nil || !shouldRunUpdateNotifier() {
		return
	}
	current, ok := currentExactReleaseVersion()
	if !ok {
		return
	}

	now := updateCheckNow().UTC()
	state, _ := readUpdateCheckState()
	printedVersion := ""
	if updateMetadataNewerThan(state.Latest, current) {
		printUpdateNotification(w, state.Latest, current)
		printedVersion = canonicalUpdateVersion(state.Latest.Version)
	}

	if !updateCheckDue(state, now) {
		return
	}

	latest, err := fetchLatestUpdateMetadata()
	state.CheckedAt = now.Format(time.RFC3339)
	if err == nil && validUpdateMetadata(latest) {
		state.Latest = latest
		latestVersion := canonicalUpdateVersion(latest.Version)
		if updateMetadataNewerThan(latest, current) &&
			latestVersion != printedVersion {
			printUpdateNotification(w, latest, current)
		}
	}
	_ = writeUpdateCheckState(state)
}

func withUpdateNotifications(cmd *cobra.Command) *cobra.Command {
	runE := cmd.RunE
	run := cmd.Run
	if runE != nil {
		cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {
			if skipUpdateNotificationForCommand(cmd) || skipUpdateNotificationForArgs(args) {
				return runE(cmd, args)
			}
			maybeNotifyUpdateAvailable(os.Stderr)
			defer maybeNotifyUpdateAvailable(os.Stderr)
			return runE(cmd, args)
		}
		return cmd
	}
	if run != nil {
		cmd.Run = func(cmd *cobra.Command, args []string) {
			if skipUpdateNotificationForCommand(cmd) || skipUpdateNotificationForArgs(args) {
				run(cmd, args)
				return
			}
			maybeNotifyUpdateAvailable(os.Stderr)
			defer maybeNotifyUpdateAvailable(os.Stderr)
			run(cmd, args)
		}
	}
	return cmd
}

func skipUpdateNotificationForCommand(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	switch cmd.Name() {
	case "check", "doctor", "status":
		return true
	default:
		return false
	}
}

func skipUpdateNotificationForArgs(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func shouldRunUpdateNotifier() bool {
	if updateCheckGetenv("HAZMAT_NO_UPDATE_NOTIFIER") != "" {
		return false
	}
	if updateCheckGetenv("CI") != "" {
		return false
	}
	return updateCheckIsTTY()
}

func currentExactReleaseVersion() (string, bool) {
	if !exactReleaseVersion.MatchString(version) {
		return "", false
	}
	return canonicalUpdateVersion(version), true
}

func canonicalUpdateVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return ""
	}
	return "v" + v
}

func validUpdateMetadata(m updateReleaseMetadata) bool {
	return exactReleaseVersion.MatchString(strings.TrimSpace(m.Version))
}

func updateMetadataNewerThan(m updateReleaseMetadata, current string) bool {
	if !validUpdateMetadata(m) {
		return false
	}
	return semverCompare(m.Version, current) > 0
}

func updateCheckDue(state updateCheckState, now time.Time) bool {
	if state.CheckedAt == "" {
		return true
	}
	checkedAt, err := time.Parse(time.RFC3339, state.CheckedAt)
	if err != nil {
		return true
	}
	if checkedAt.After(now) {
		return false
	}
	return now.Sub(checkedAt) >= updateCheckInterval
}

func printUpdateNotification(w io.Writer, latest updateReleaseMetadata, current string) {
	latest.Version = canonicalUpdateVersion(latest.Version)
	if latest.Formula == "" {
		latest.Formula = "dredozubov/tap/hazmat"
	}
	if latest.UpgradeCommand == "" {
		latest.UpgradeCommand = "brew update && brew upgrade " + latest.Formula
	}
	fmt.Fprintf(w, "hazmat: %s is available in Homebrew (current %s)\n", latest.Version, current)
	fmt.Fprintf(w, "        Update with: %s\n", latest.UpgradeCommand)
	if latest.ReleaseURL != "" {
		fmt.Fprintf(w, "        Release: %s\n", latest.ReleaseURL)
	}
}

func fetchLatestUpdateMetadata() (updateReleaseMetadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateCheckURL, nil)
	if err != nil {
		return updateReleaseMetadata{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := updateCheckClient.Do(req)
	if err != nil {
		return updateReleaseMetadata{}, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return updateReleaseMetadata{}, fmt.Errorf("update metadata HTTP %d", resp.StatusCode)
	}

	var latest updateReleaseMetadata
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64*1024))
	if err := decoder.Decode(&latest); err != nil {
		return updateReleaseMetadata{}, err
	}
	return latest, nil
}

func readUpdateCheckState() (updateCheckState, error) {
	raw, err := os.ReadFile(updateCheckStatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return updateCheckState{}, nil
		}
		return updateCheckState{}, err
	}
	var state updateCheckState
	if err := json.Unmarshal(raw, &state); err != nil {
		return updateCheckState{}, err
	}
	return state, nil
}

func writeUpdateCheckState(state updateCheckState) error {
	dir := filepath.Dir(updateCheckStatePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	tmp := filepath.Join(dir, fmt.Sprintf(".update-check.%d.%d.tmp", os.Getpid(), updateCheckNow().UnixNano()))
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, updateCheckStatePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

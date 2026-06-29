package hazmat

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

func runPreparedCurrentUserSeatbeltScriptWithUI(prepared preparedSession, ui sessionLaunchUI, script string, args ...string) error {
	return runCurrentUserSeatbeltScriptWithPlan(prepared.Config, prepared.BackendPlan, ui, script, args...)
}

var runCurrentUserSeatbeltScriptWithPlan = defaultRunCurrentUserSeatbeltScriptWithPlan

func defaultRunCurrentUserSeatbeltScriptWithPlan(cfg sessionConfig, plan sessionBackendPlan, ui sessionLaunchUI, script string, args ...string) error {
	profile := newSessionPhaseProfile("hazmat: current-user native launch execution profile:", os.Stderr)
	defer profile.Done()

	start := time.Now()
	ui = applyLaunchStatusBarConfig(ui, loadConfig)
	profile.Record("load launch config", start)

	start = time.Now()
	dirs, cleanup, err := prepareCurrentUserSessionDirs()
	profile.Record("prepare current-user session dirs", start)
	if err != nil {
		return err
	}
	defer func() {
		start := time.Now()
		cleanup()
		profile.Record("cleanup current-user session dirs", start)
	}()
	cfg.CurrentUserSession = &dirs
	cfg.TempDir = dirs.TempDir

	start = time.Now()
	policy, err := prepareNativeLaunchPolicyWithPlan(cfg, plan)
	profile.Record("prepare native policy", start)
	if err != nil {
		return err
	}
	defer func() {
		start := time.Now()
		policy.Cleanup()
		profile.Record("cleanup native policy", start)
	}()

	metadataJSON := ""
	if cfg.EmitSessionMetadataJSON {
		start = time.Now()
		var err error
		metadataJSON, err = marshalSessionLaunchMetadataJSON(cfg, sessionModeNative)
		profile.Record("marshal session metadata", start)
		if err != nil {
			return fmt.Errorf("marshal session metadata: %w", err)
		}
	}

	start = time.Now()
	full := nativeLaunchCurrentUserArgsWithMetadataPlanAndRuntime(cfg, plan, policy, nil, metadataJSON, script, args...)
	profile.Record("build current-user command", start)
	if len(full) == 0 {
		return fmt.Errorf("current-user launch command is empty")
	}

	var (
		barOnce     sync.Once
		barTeardown = func() {}
	)
	startBar := func() {
		barOnce.Do(func() {
			bar := newStatusBar(cfg.ActiveIntegrations, cfg.ProjectDir)
			barTeardown = bar.Start()
		})
	}
	if ui.showStatusBar {
		start = time.Now()
		startBar()
		profile.Record("start status bar", start)
	}
	defer barTeardown()

	cmd := exec.Command(full[0], full[1:]...)
	cmd.Dir = "/"
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if ui.clearScreen {
		fmt.Fprint(os.Stderr, "\033[2J\033[H")
	}

	startTime := time.Now()
	err = runSessionCommand(cmd)
	profile.Record("run current-user command", startTime)
	return err
}

func prepareCurrentUserSessionDirs() (currentUserSessionDirs, func(), error) {
	root, err := os.MkdirTemp("", "hazmat-current-user-*")
	if err != nil {
		return currentUserSessionDirs{}, func() {}, fmt.Errorf("create current-user session root: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(root)
	}
	home := filepath.Join(root, "home")
	dirs := currentUserSessionDirs{
		Root:       root,
		Home:       home,
		CacheHome:  filepath.Join(home, ".cache"),
		ConfigHome: filepath.Join(home, ".config"),
		DataHome:   filepath.Join(home, ".local", "share"),
		TempDir:    filepath.Join(root, "tmp"),
	}
	for _, dir := range []string{dirs.Home, dirs.CacheHome, dirs.ConfigHome, dirs.DataHome, dirs.TempDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			cleanup()
			return currentUserSessionDirs{}, func() {}, fmt.Errorf("create current-user session dir %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			cleanup()
			return currentUserSessionDirs{}, func() {}, fmt.Errorf("set current-user session dir mode %s: %w", dir, err)
		}
	}
	return dirs, cleanup, nil
}

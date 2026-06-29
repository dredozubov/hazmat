//go:build linux

package hazmat

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	linuxsetup "hazmat/internal/setup/linux"
)

const (
	linuxAgentUserSetupSmokeEnv       = "HAZMAT_LINUX_AGENT_USER_SETUP_VM_SMOKE"
	linuxAgentUserRollbackSmokeEnv    = "HAZMAT_LINUX_AGENT_USER_ROLLBACK_VM_SMOKE"
	linuxAgentUserCleanupSmokeEnv     = "HAZMAT_LINUX_AGENT_USER_CLEANUP_VM_SMOKE"
	linuxAgentUserDestructiveSmokeEnv = "HAZMAT_LINUX_AGENT_USER_LIFECYCLE_DESTRUCTIVE"
)

func TestLinuxAgentUserSetupLiveSmoke(t *testing.T) {
	requireLinuxAgentUserLifecycleSmoke(t, linuxAgentUserSetupSmokeEnv)
	requireLinuxRootHelperSource(t)
	ensureDisposableLinuxAgentTargets(t)

	runLinuxAgentUserDestructiveRollback(t, "preclean")

	t.Log("A1 fresh setup: running modeled Linux setup graph")
	runLinuxAgentUserSetup(t)
	verifyLinuxAgentUserSetup(t)
	t.Log("A1 fresh setup: pass")

	t.Log("A2 idempotent setup: rerunning modeled Linux setup graph")
	runLinuxAgentUserSetup(t)
	verifyLinuxAgentUserSetup(t)
	t.Log("A2 idempotent setup: pass")

	t.Log("A3 drift repair: removing and repairing the narrow root-helper sudoers rule")
	rollbackBackend := linuxAgentUserLifecycleBackend(t, linuxDiagnosticSetupRollback)
	if err := rollbackBackend.rollbackSudoers(); err != nil {
		t.Fatalf("A3 remove sudoers drift fixture: %v", err)
	}
	verifyCallbacks := linuxAgentUserLifecycleBackend(t, linuxDiagnosticSetupVerify).callbacks()
	if err := linuxsetup.VerifyRepairAction("verify.linux-setup.sudoers", verifyCallbacks); err == nil {
		t.Fatal("A3 sudoers verification unexpectedly passed after drift fixture")
	}
	applyCallbacks := linuxAgentUserLifecycleBackend(t, linuxDiagnosticSetupApply).callbacks()
	if err := linuxsetup.RunRepairAction("repair.linux-setup.sudoers", applyCallbacks); err != nil {
		t.Fatalf("A3 repair sudoers: %v", err)
	}
	if err := linuxsetup.VerifyRepairAction("verify.linux-setup.sudoers", verifyCallbacks); err != nil {
		t.Fatalf("A3 verify repaired sudoers: %v", err)
	}
	t.Log("A3 drift repair: pass")
}

func TestLinuxAgentUserRollbackLiveSmoke(t *testing.T) {
	requireLinuxAgentUserLifecycleSmoke(t, linuxAgentUserRollbackSmokeEnv)
	requireLinuxRootHelperSource(t)
	ensureDisposableLinuxAgentTargets(t)
	verifyLinuxAgentUserSetup(t)

	t.Log("A9 default rollback: removing privilege/runtime resources while preserving identity resources")
	runLinuxAgentUserRollback(t, linuxsetup.RollbackOptions{
		DeleteToolHome:    false,
		DeleteAgentHome:   false,
		DeleteAgentUser:   false,
		DeleteSharedGroup: false,
	})
	assertLinuxAgentUserPresent(t)
	assertLinuxSharedGroupPresent(t)
	assertPathExists(t, linuxAgentHomePath)
	assertPathAbsent(t, linuxSudoersFile)
	assertPathAbsent(t, linuxRootHelperPath)
	assertPathAbsent(t, linuxCgroupRootPath)
	t.Log("A9 default rollback: pass")

	t.Log("A10 destructive rollback: recreating resources, then deleting identity resources under explicit flag")
	runLinuxAgentUserSetup(t)
	verifyLinuxAgentUserSetup(t)
	runLinuxAgentUserDestructiveRollback(t, "A10 destructive rollback")
	assertLinuxAgentUserAbsent(t)
	assertLinuxSharedGroupAbsent(t)
	assertPathAbsent(t, linuxAgentHomePath)
	assertPathAbsent(t, linuxSudoersFile)
	assertPathAbsent(t, linuxRootHelperPath)
	assertPathAbsent(t, linuxCgroupRootPath)
	t.Log("A10 destructive rollback: pass")
}

func TestLinuxAgentUserDestructiveCleanupLiveSmoke(t *testing.T) {
	if os.Getenv(linuxAgentUserCleanupSmokeEnv) != "1" {
		t.Skipf("set %s=1 inside a disposable Linux VM to clean Linux agent-user lifecycle smoke residue", linuxAgentUserCleanupSmokeEnv)
	}
	requireLinuxHost(t)
	requireNonPromptingSudo(t)
	ensureDisposableLinuxAgentTargets(t)
	runLinuxAgentUserDestructiveRollback(t, "cleanup")
}

func requireLinuxAgentUserLifecycleSmoke(t *testing.T, env string) {
	t.Helper()
	if os.Getenv(env) != "1" {
		t.Skipf("set %s=1 inside a disposable Linux VM to run Linux agent-user lifecycle smokes", env)
	}
	if os.Getenv(linuxAgentUserDestructiveSmokeEnv) != "1" {
		t.Fatalf("set %s=1 to acknowledge destructive Linux agent-user setup/rollback smoke", linuxAgentUserDestructiveSmokeEnv)
	}
	requireLinuxHost(t)
	requireNonPromptingSudo(t)
}

func requireLinuxHost(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Fatalf("Linux agent-user lifecycle smoke requires Linux, got %s", runtime.GOOS)
	}
}

func requireNonPromptingSudo(t *testing.T) {
	t.Helper()
	cmd := exec.Command("sudo", "-n", "true")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("passwordless sudo is required for disposable Linux lifecycle smoke: %v: %s", err, strings.TrimSpace(string(out)))
	}
}

func requireLinuxRootHelperSource(t *testing.T) {
	t.Helper()
	src := strings.TrimSpace(os.Getenv("HAZMAT_LINUX_ROOT_HELPER_SOURCE"))
	if src == "" {
		t.Fatal("HAZMAT_LINUX_ROOT_HELPER_SOURCE must point at a built hazmat-launch helper")
	}
	if !filepath.IsAbs(src) {
		t.Fatalf("HAZMAT_LINUX_ROOT_HELPER_SOURCE must be absolute, got %q", src)
	}
	if info, err := os.Stat(src); err != nil {
		t.Fatalf("stat HAZMAT_LINUX_ROOT_HELPER_SOURCE: %v", err)
	} else if info.Mode()&0o111 == 0 {
		t.Fatalf("HAZMAT_LINUX_ROOT_HELPER_SOURCE is not executable: %s", src)
	}
}

func ensureDisposableLinuxAgentTargets(t *testing.T) {
	t.Helper()
	if out, err := commandStdout("id", "-u", linuxAgentUserName); err == nil && strings.TrimSpace(out) != agentUID {
		t.Fatalf("refusing destructive Linux lifecycle smoke: %s uid is %s, want disposable uid %s", linuxAgentUserName, strings.TrimSpace(out), agentUID)
	}
	if out, err := commandStdout("getent", "group", linuxSharedGroupName); err == nil {
		fields := strings.Split(strings.TrimSpace(out), ":")
		if len(fields) >= 3 && fields[2] != sharedGID {
			t.Fatalf("refusing destructive Linux lifecycle smoke: %s gid is %s, want disposable gid %s", linuxSharedGroupName, fields[2], sharedGID)
		}
	}
}

func linuxAgentUserLifecycleBackend(t *testing.T, operation linuxDiagnosticSetupOperation) linuxDiagnosticSetupBackend {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatalf("lookup current user: %v", err)
	}
	projectDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	ui := &UI{YesAll: true}
	return linuxDiagnosticSetupBackend{
		runner:      NewRunner(ui, true, false),
		operation:   operation,
		currentUser: linuxCurrentUserName(current.Username),
		projectDir:  projectDir,
		inspectHost: linuxDiagnosticInspectHost,
		executable:  linuxDiagnosticExecutable,
	}
}

func runLinuxAgentUserSetup(t *testing.T) {
	t.Helper()
	if err := linuxsetup.RunSetupSteps(linuxAgentUserLifecycleBackend(t, linuxDiagnosticSetupApply).callbacks()); err != nil {
		t.Fatalf("run Linux setup graph: %v", err)
	}
}

func verifyLinuxAgentUserSetup(t *testing.T) {
	t.Helper()
	if err := linuxsetup.RunSetupSteps(linuxAgentUserLifecycleBackend(t, linuxDiagnosticSetupVerify).callbacks()); err != nil {
		t.Fatalf("verify Linux setup graph: %v", err)
	}
}

func runLinuxAgentUserRollback(t *testing.T, options linuxsetup.RollbackOptions) {
	t.Helper()
	if err := linuxsetup.RunRollbackSteps(linuxAgentUserLifecycleBackend(t, linuxDiagnosticSetupRollback).callbacks(), options); err != nil {
		t.Fatalf("run Linux rollback graph: %v", err)
	}
}

func runLinuxAgentUserDestructiveRollback(t *testing.T, label string) {
	t.Helper()
	t.Log(label + ": running destructive Linux rollback graph")
	runLinuxAgentUserRollback(t, linuxsetup.RollbackOptions{
		DeleteToolHome:    true,
		DeleteAgentHome:   true,
		DeleteAgentUser:   true,
		DeleteSharedGroup: true,
	})
}

func assertLinuxAgentUserPresent(t *testing.T) {
	t.Helper()
	out, err := commandStdout("id", "-u", linuxAgentUserName)
	if err != nil {
		t.Fatalf("expected %s user to exist: %v", linuxAgentUserName, err)
	}
	if got := strings.TrimSpace(out); got != agentUID {
		t.Fatalf("%s uid = %s, want %s", linuxAgentUserName, got, agentUID)
	}
}

func assertLinuxAgentUserAbsent(t *testing.T) {
	t.Helper()
	if out, err := commandStdout("id", "-u", linuxAgentUserName); err == nil {
		t.Fatalf("expected %s user to be absent, got uid %s", linuxAgentUserName, strings.TrimSpace(out))
	}
}

func assertLinuxSharedGroupPresent(t *testing.T) {
	t.Helper()
	out, err := commandStdout("getent", "group", linuxSharedGroupName)
	if err != nil {
		t.Fatalf("expected %s group to exist: %v", linuxSharedGroupName, err)
	}
	fields := strings.Split(strings.TrimSpace(out), ":")
	if len(fields) < 3 || fields[2] != sharedGID {
		t.Fatalf("%s group entry = %q, want gid %s", linuxSharedGroupName, strings.TrimSpace(out), sharedGID)
	}
}

func assertLinuxSharedGroupAbsent(t *testing.T) {
	t.Helper()
	if out, err := commandStdout("getent", "group", linuxSharedGroupName); err == nil {
		t.Fatalf("expected %s group to be absent, got %q", linuxSharedGroupName, strings.TrimSpace(out))
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if out, err := exec.Command("sudo", "-n", "test", "-e", path).CombinedOutput(); err != nil {
		t.Fatalf("expected %s to exist: %v: %s", path, err, strings.TrimSpace(string(out)))
	}
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	out, err := exec.Command("sudo", "-n", "test", "-e", path).CombinedOutput()
	if err == nil {
		t.Fatalf("expected %s to be absent", path)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return
	}
	t.Fatalf("check %s absence: %v: %s", path, err, strings.TrimSpace(string(out)))
}

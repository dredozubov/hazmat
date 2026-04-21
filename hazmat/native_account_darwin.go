//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

type darwinNativeAccountBackend struct{}

func newNativeAccountBackend() nativeAccountBackend {
	return darwinNativeAccountBackend{}
}

func (b darwinNativeAccountBackend) SetupAgentUser(ui *UI, r *Runner) error {
	ui.Step(fmt.Sprintf("Create '%s' user", agentUser))

	if u, err := user.Lookup(agentUser); err == nil {
		ui.SkipDone(fmt.Sprintf("User '%s' already exists (uid=%s)", agentUser, u.Uid))
		return nil
	}

	if taken, err := b.UIDTaken(agentUID); err != nil {
		return fmt.Errorf("check UID: %w", err)
	} else if taken {
		return fmt.Errorf("UID %s is already taken — use: hazmat init --agent-uid <different-uid>", agentUID)
	}

	record := "/Users/" + agentUser
	type reasonedCmd struct {
		reason string
		args   []string
	}
	for _, rc := range []reasonedCmd{
		{"create agent user record", []string{"dscl", ".", "-create", record}},
		{"set agent user shell", []string{"dscl", ".", "-create", record, "UserShell", "/bin/zsh"}},
		{"set agent user UID", []string{"dscl", ".", "-create", record, "UniqueID", agentUID}},
		{"set agent user primary group", []string{"dscl", ".", "-create", record, "PrimaryGroupID", "20"}},
		{"set agent user home directory", []string{"dscl", ".", "-create", record, "NFSHomeDirectory", agentHome}},
	} {
		if err := r.Sudo(rc.reason, rc.args...); err != nil {
			return fmt.Errorf("dscl %v: %w", rc.args[2:], err)
		}
	}
	ui.Ok("User record created")

	if err := r.Sudo("create agent home directory", "mkdir", "-p", agentHome); err != nil {
		return fmt.Errorf("mkdir %s: %w", agentHome, err)
	}
	if err := r.Sudo("set agent home directory ownership", "chown", agentUser+":staff", agentHome); err != nil {
		return fmt.Errorf("chown %s: %w", agentHome, err)
	}
	// createhomedir may exit non-zero even on success; ignore the error.
	r.Sudo("populate agent home directory", "createhomedir", "-c", "-u", agentUser) //nolint:errcheck
	ui.Ok(fmt.Sprintf("Home directory created at %s", agentHome))

	if err := r.Sudo("hide agent from login screen", "dscl", ".", "-create", record, "IsHidden", "1"); err != nil {
		return fmt.Errorf("hide user: %w", err)
	}
	ui.Ok("Hidden from login screen")

	// Auto-generate the agent user password. The account is hidden and only
	// accessed via sudo — the password exists solely because macOS requires
	// every account to have a password hash.
	{
		var password string
		if r.DryRun {
			password = "<random-192bit-base64>"
		} else {
			var err error
			password, err = generateRandomPassword(24) // 192 bits
			if err != nil {
				return fmt.Errorf("generate agent password: %w", err)
			}
		}
		if err := r.Sudo("set agent password", "dscl", ".", "-passwd", "/Users/"+agentUser, password); err != nil {
			return fmt.Errorf("set agent password: %w", err)
		}
		ui.Ok("Password set (auto-generated, login is disabled)")
	}

	if !r.DryRun {
		if _, err := user.Lookup(agentUser); err != nil {
			return fmt.Errorf("user '%s' not found after creation: %w", agentUser, err)
		}
	}
	return nil
}

func (b darwinNativeAccountBackend) SetupDevGroup(ui *UI, r *Runner, currentUser string) error {
	ui.Step(fmt.Sprintf("Create '%s' group", sharedGroup))

	if g, err := user.LookupGroup(sharedGroup); err == nil {
		ui.SkipDone(fmt.Sprintf("Group '%s' already exists (gid=%s)", sharedGroup, g.Gid))
	} else {
		if taken, err := b.GIDTaken(sharedGID); err != nil {
			return fmt.Errorf("check GID: %w", err)
		} else if taken {
			return fmt.Errorf("GID %s is already taken — use: hazmat init --group-gid <different-gid>", sharedGID)
		}

		record := "/Groups/" + sharedGroup
		type reasonedCmd struct {
			reason string
			args   []string
		}
		for _, rc := range []reasonedCmd{
			{"create dev group", []string{"dscl", ".", "-create", record}},
			{"set dev group GID", []string{"dscl", ".", "-create", record, "PrimaryGroupID", sharedGID}},
			{"set dev group description", []string{"dscl", ".", "-create", record, "RealName", "Shared dev workspace"}},
		} {
			if err := r.Sudo(rc.reason, rc.args...); err != nil {
				return fmt.Errorf("dscl %v: %w", rc.args[2:], err)
			}
		}
		ui.Ok(fmt.Sprintf("Group '%s' created (gid=%s)", sharedGroup, sharedGID))
	}

	for _, u := range []string{currentUser, agentUser} {
		member, err := b.GroupMembershipContains(sharedGroup, u)
		if err != nil {
			return err
		}
		if member {
			ui.SkipDone(fmt.Sprintf("%s is already a member of '%s'", u, sharedGroup))
		} else {
			if err := r.Sudo("add "+u+" to dev group", "dscl", ".", "-append",
				"/Groups/"+sharedGroup, "GroupMembership", u); err != nil {
				return fmt.Errorf("add %s to %s: %w", u, sharedGroup, err)
			}
			ui.Ok(fmt.Sprintf("Added %s to '%s'", u, sharedGroup))
		}
	}
	return nil
}

func (darwinNativeAccountBackend) RollbackAgentUser(ui *UI, r *Runner) {
	ui.Step(fmt.Sprintf("Delete '%s' user and home directory", agentUser))

	if _, err := user.Lookup(agentUser); err != nil {
		ui.SkipDone(fmt.Sprintf("User '%s' does not exist", agentUser))
	} else {
		if err := r.Sudo("delete agent user account", "dscl", ".", "-delete", "/Users/"+agentUser); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not delete user record for '%s': %v", agentUser, err))
		} else {
			ui.Ok(fmt.Sprintf("Deleted user record for '%s'", agentUser))
		}
	}

	if _, err := os.Stat(agentHome); err == nil {
		if err := r.Sudo("delete agent home directory", "rm", "-rf", agentHome); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove home directory %s: %v", agentHome, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed home directory %s", agentHome))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Home directory %s does not exist", agentHome))
	}
}

func (darwinNativeAccountBackend) RollbackDevGroup(ui *UI, r *Runner) {
	ui.Step(fmt.Sprintf("Delete '%s' group", sharedGroup))

	if _, err := user.LookupGroup(sharedGroup); err != nil {
		ui.SkipDone(fmt.Sprintf("Group '%s' does not exist", sharedGroup))
		return
	}

	if err := r.Sudo("delete dev group", "dscl", ".", "-delete", "/Groups/"+sharedGroup); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not delete group '%s': %v", sharedGroup, err))
	} else {
		ui.Ok(fmt.Sprintf("Deleted group '%s'", sharedGroup))
	}
}

func (darwinNativeAccountBackend) UIDTaken(uid string) (bool, error) {
	out, err := dscl("-list", "/Users", "UniqueID")
	if err != nil {
		return false, fmt.Errorf("dscl list UIDs: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[1] == uid {
			return true, nil
		}
	}
	return false, nil
}

func (darwinNativeAccountBackend) GIDTaken(gid string) (bool, error) {
	out, err := dscl("-list", "/Groups", "PrimaryGroupID")
	if err != nil {
		return false, fmt.Errorf("dscl list GIDs: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[1] == gid {
			return true, nil
		}
	}
	return false, nil
}

func (darwinNativeAccountBackend) GroupMembershipContains(group, username string) (bool, error) {
	out, err := dscl("-read", "/Groups/"+group, "GroupMembership")
	if err != nil {
		return false, nil // group exists but has no members yet
	}
	for _, field := range strings.Fields(out) {
		if field == username {
			return true, nil
		}
	}
	return false, nil
}

// dscl runs a read-only dscl query without sudo. Directory Service reads for
// UIDs, GIDs, and group membership are world-readable on macOS and do not
// require elevated privileges.
func dscl(args ...string) (string, error) {
	full := append([]string{"."}, args...)
	out, err := exec.Command(hostDsclPath, full...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

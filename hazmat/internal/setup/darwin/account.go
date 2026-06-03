//go:build darwin

package darwin

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

type AccountEnv struct {
	AgentUser        string
	AgentUID         string
	AgentHome        string
	SharedGroup      string
	SharedGID        string
	DsclPath         string
	GeneratePassword func(int) (string, error)
}

type StatusUI interface {
	Step(string)
	SkipDone(string)
	Ok(string)
	WarnMsg(string)
}

type Runner interface {
	Sudo(reason string, args ...string) error
}

type AccountBackend struct {
	env AccountEnv
}

func NewAccountBackend(env AccountEnv) AccountBackend {
	return AccountBackend{env: env}
}

func (b AccountBackend) SetupAgentUser(ui StatusUI, runner Runner, dryRun bool) error {
	env := b.env
	ui.Step(fmt.Sprintf("Create '%s' user", env.AgentUser))

	if u, err := user.Lookup(env.AgentUser); err == nil {
		ui.SkipDone(fmt.Sprintf("User '%s' already exists (uid=%s)", env.AgentUser, u.Uid))
		return nil
	}

	if taken, err := b.UIDTaken(env.AgentUID); err != nil {
		return fmt.Errorf("check UID: %w", err)
	} else if taken {
		return fmt.Errorf("UID %s is already taken — use: hazmat init --agent-uid <different-uid>", env.AgentUID)
	}

	record := "/Users/" + env.AgentUser
	type reasonedCmd struct {
		reason string
		args   []string
	}
	for _, rc := range []reasonedCmd{
		{"create agent user record", []string{"dscl", ".", "-create", record}},
		{"set agent user shell", []string{"dscl", ".", "-create", record, "UserShell", "/bin/zsh"}},
		{"set agent user UID", []string{"dscl", ".", "-create", record, "UniqueID", env.AgentUID}},
		{"set agent user primary group", []string{"dscl", ".", "-create", record, "PrimaryGroupID", "20"}},
		{"set agent user home directory", []string{"dscl", ".", "-create", record, "NFSHomeDirectory", env.AgentHome}},
	} {
		if err := runner.Sudo(rc.reason, rc.args...); err != nil {
			return fmt.Errorf("dscl %v: %w", rc.args[2:], err)
		}
	}
	ui.Ok("User record created")

	if err := runner.Sudo("create agent home directory", "mkdir", "-p", env.AgentHome); err != nil {
		return fmt.Errorf("mkdir %s: %w", env.AgentHome, err)
	}
	if err := runner.Sudo("set agent home directory ownership", "chown", env.AgentUser+":staff", env.AgentHome); err != nil {
		return fmt.Errorf("chown %s: %w", env.AgentHome, err)
	}
	// createhomedir may exit non-zero even on success; ignore the error.
	runner.Sudo("populate agent home directory", "createhomedir", "-c", "-u", env.AgentUser) //nolint:errcheck
	ui.Ok(fmt.Sprintf("Home directory created at %s", env.AgentHome))

	if err := runner.Sudo("hide agent from login screen", "dscl", ".", "-create", record, "IsHidden", "1"); err != nil {
		return fmt.Errorf("hide user: %w", err)
	}
	ui.Ok("Hidden from login screen")

	var password string
	if dryRun {
		password = "<random-192bit-base64>"
	} else {
		if env.GeneratePassword == nil {
			return fmt.Errorf("generate agent password: password generator is not configured")
		}
		var err error
		password, err = env.GeneratePassword(24) // 192 bits
		if err != nil {
			return fmt.Errorf("generate agent password: %w", err)
		}
	}
	if err := runner.Sudo("set agent password", "dscl", ".", "-passwd", "/Users/"+env.AgentUser, password); err != nil {
		return fmt.Errorf("set agent password: %w", err)
	}
	ui.Ok("Password set (auto-generated, login is disabled)")

	if !dryRun {
		if _, err := user.Lookup(env.AgentUser); err != nil {
			return fmt.Errorf("user '%s' not found after creation: %w", env.AgentUser, err)
		}
	}
	return nil
}

func (b AccountBackend) SetupDevGroup(ui StatusUI, runner Runner, currentUser string) error {
	env := b.env
	ui.Step(fmt.Sprintf("Create '%s' group", env.SharedGroup))

	if g, err := user.LookupGroup(env.SharedGroup); err == nil {
		ui.SkipDone(fmt.Sprintf("Group '%s' already exists (gid=%s)", env.SharedGroup, g.Gid))
	} else {
		if taken, err := b.GIDTaken(env.SharedGID); err != nil {
			return fmt.Errorf("check GID: %w", err)
		} else if taken {
			return fmt.Errorf("GID %s is already taken — use: hazmat init --group-gid <different-gid>", env.SharedGID)
		}

		record := "/Groups/" + env.SharedGroup
		type reasonedCmd struct {
			reason string
			args   []string
		}
		for _, rc := range []reasonedCmd{
			{"create dev group", []string{"dscl", ".", "-create", record}},
			{"set dev group GID", []string{"dscl", ".", "-create", record, "PrimaryGroupID", env.SharedGID}},
			{"set dev group description", []string{"dscl", ".", "-create", record, "RealName", "Shared dev workspace"}},
		} {
			if err := runner.Sudo(rc.reason, rc.args...); err != nil {
				return fmt.Errorf("dscl %v: %w", rc.args[2:], err)
			}
		}
		ui.Ok(fmt.Sprintf("Group '%s' created (gid=%s)", env.SharedGroup, env.SharedGID))
	}

	for _, account := range []string{currentUser, env.AgentUser} {
		member, err := b.GroupMembershipContains(env.SharedGroup, account)
		if err != nil {
			return err
		}
		if member {
			ui.SkipDone(fmt.Sprintf("%s is already a member of '%s'", account, env.SharedGroup))
			continue
		}
		if err := runner.Sudo("add "+account+" to dev group", "dscl", ".", "-append",
			"/Groups/"+env.SharedGroup, "GroupMembership", account); err != nil {
			return fmt.Errorf("add %s to %s: %w", account, env.SharedGroup, err)
		}
		ui.Ok(fmt.Sprintf("Added %s to '%s'", account, env.SharedGroup))
	}
	return nil
}

func (b AccountBackend) RollbackAgentUser(ui StatusUI, runner Runner) {
	env := b.env
	ui.Step(fmt.Sprintf("Delete '%s' user and home directory", env.AgentUser))

	if _, err := user.Lookup(env.AgentUser); err != nil {
		ui.SkipDone(fmt.Sprintf("User '%s' does not exist", env.AgentUser))
	} else if err := runner.Sudo("delete agent user account", "dscl", ".", "-delete", "/Users/"+env.AgentUser); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not delete user record for '%s': %v", env.AgentUser, err))
	} else {
		ui.Ok(fmt.Sprintf("Deleted user record for '%s'", env.AgentUser))
	}

	if _, err := os.Stat(env.AgentHome); err == nil {
		if err := runner.Sudo("delete agent home directory", "rm", "-rf", env.AgentHome); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove home directory %s: %v", env.AgentHome, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed home directory %s", env.AgentHome))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Home directory %s does not exist", env.AgentHome))
	}
}

func (b AccountBackend) RollbackDevGroup(ui StatusUI, runner Runner) {
	env := b.env
	ui.Step(fmt.Sprintf("Delete '%s' group", env.SharedGroup))

	if _, err := user.LookupGroup(env.SharedGroup); err != nil {
		ui.SkipDone(fmt.Sprintf("Group '%s' does not exist", env.SharedGroup))
		return
	}

	if err := runner.Sudo("delete dev group", "dscl", ".", "-delete", "/Groups/"+env.SharedGroup); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not delete group '%s': %v", env.SharedGroup, err))
	} else {
		ui.Ok(fmt.Sprintf("Deleted group '%s'", env.SharedGroup))
	}
}

func (b AccountBackend) UIDTaken(uid string) (bool, error) {
	out, err := b.dscl("-list", "/Users", "UniqueID")
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

func (b AccountBackend) GIDTaken(gid string) (bool, error) {
	out, err := b.dscl("-list", "/Groups", "PrimaryGroupID")
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

func (b AccountBackend) GroupMembershipContains(group, username string) (bool, error) {
	out, err := b.dscl("-read", "/Groups/"+group, "GroupMembership")
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
func (b AccountBackend) dscl(args ...string) (string, error) {
	full := append([]string{"."}, args...)
	out, err := exec.Command(b.env.DsclPath, full...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

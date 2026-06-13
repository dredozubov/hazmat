package hazmat

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"hazmat/internal/setup"
)

type diagnosticHostRepairBackend struct {
	ui          *UI
	currentUser string
	homeDir     string
	projectDir  string
}

func newDiagnosticHostRepairBackend(ui *UI, currentUser, homeDir string) diagnosticRepairBackend {
	if strings.TrimSpace(homeDir) == "" {
		homeDir = os.Getenv("HOME")
	}
	projectDir, _ := os.Getwd()
	return &diagnosticHostRepairBackend{
		ui:          ui,
		currentUser: currentUser,
		homeDir:     homeDir,
		projectDir:  projectDir,
	}
}

type diagnosticHostRepairApplyHandler func(*diagnosticHostRepairBackend, *Runner, diagnosticRepairActionDefinition, diagnosticRepairPlanItem) error
type diagnosticHostRepairVerifyHandler func(*diagnosticHostRepairBackend, diagnosticRepairActionDefinition, diagnosticRepairPlanItem) error

var diagnosticHostRepairApplyHandlers = map[diagnosticRepairActionID]diagnosticHostRepairApplyHandler{
	"repair.workspace.setgid": func(b *diagnosticHostRepairBackend, _ *Runner, _ diagnosticRepairActionDefinition, item diagnosticRepairPlanItem) error {
		return b.applyWorkspaceRepair(item)
	},
	"repair.workspace.access": func(b *diagnosticHostRepairBackend, _ *Runner, _ diagnosticRepairActionDefinition, item diagnosticRepairPlanItem) error {
		return b.applyWorkspaceRepair(item)
	},
	"repair.agent-shell.umask": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.applyAgentUmask(r)
	},
	"repair.setup.agent-user": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return setupAgentUser(b.ui, r)
	},
	"repair.setup.agent-home": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.applySetupAgentHome(r)
	},
	"repair.setup.dev-group": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return setupDevGroup(b.ui, r, b.currentUser)
	},
	"repair.setup.home-traverse": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return setupHomeDirTraverse(b.ui, r)
	},
	"repair.setup.sudoers": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return setupSudoers(b.ui, r, b.currentUser)
	},
	"repair.setup.seatbelt-wrapper": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return setupSeatbelt(b.ui, r)
	},
	"repair.setup.agent-env": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return setupUserExperience(b.ui, r)
	},
	"repair.setup.host-wrappers": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.applyHostWrappers(r)
	},
	"repair.network.pf": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.applyNetworkPF(r)
	},
	"repair.network.dns-blocklist": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.applyDNSBlocklist(r)
	},
	"repair.network.persistence": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.applyNetworkPersistence(r)
	},
	"repair.credential.claude-state": func(b *diagnosticHostRepairBackend, _ *Runner, action diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.applyCredentialMigration(action.ID)
	},
	"repair.credential.cloud-secret-key": func(b *diagnosticHostRepairBackend, _ *Runner, action diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.applyCredentialMigration(action.ID)
	},
	"repair.credential.residue": func(b *diagnosticHostRepairBackend, _ *Runner, action diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.applyCredentialMigration(action.ID)
	},
	"repair.claude.project-permissions": func(b *diagnosticHostRepairBackend, r *Runner, _ diagnosticRepairActionDefinition, item diagnosticRepairPlanItem) error {
		return b.applyClaudeProjectPermissions(r, item)
	},
}

var diagnosticHostRepairVerifyHandlers = map[diagnosticVerificationID]diagnosticHostRepairVerifyHandler{
	"verify.workspace.setgid": func(b *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, item diagnosticRepairPlanItem) error {
		return b.verifyWorkspaceRepair(item)
	},
	"verify.workspace.access": func(b *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, item diagnosticRepairPlanItem) error {
		return b.verifyWorkspaceRepair(item)
	},
	"verify.agent-shell.umask": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifyAgentUmask()
	},
	"verify.setup.agent-user": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifySetupAgentUser()
	},
	"verify.setup.agent-home": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifySetupAgentHome()
	},
	"verify.setup.dev-group": func(b *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifySetupDevGroup(b.currentUser)
	},
	"verify.setup.home-traverse": func(b *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifySetupHomeTraverse(b.homeDir)
	},
	"verify.setup.sudoers": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifySetupSudoers()
	},
	"verify.setup.seatbelt-wrapper": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifySetupSeatbeltWrapper()
	},
	"verify.setup.agent-env": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifySetupAgentEnv()
	},
	"verify.setup.host-wrappers": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifySetupHostWrappers()
	},
	"verify.network.pf": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifyNetworkPF()
	},
	"verify.network.dns-blocklist": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifyDNSBlocklist()
	},
	"verify.network.persistence": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return verifyNetworkPersistence()
	},
	"verify.credential.claude-state": func(b *diagnosticHostRepairBackend, action diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.verifyCredentialMigration(action.ID)
	},
	"verify.credential.cloud-secret-key": func(b *diagnosticHostRepairBackend, action diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.verifyCredentialMigration(action.ID)
	},
	"verify.credential.residue": func(b *diagnosticHostRepairBackend, action diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
		return b.verifyCredentialMigration(action.ID)
	},
	"verify.claude.project-permissions": func(_ *diagnosticHostRepairBackend, _ diagnosticRepairActionDefinition, item diagnosticRepairPlanItem) error {
		return verifyClaudeProjectPermissions(item)
	},
}

func diagnosticHostRepairBackendSupportsAction(id diagnosticRepairActionID) bool {
	_, ok := diagnosticHostRepairApplyHandlers[id]
	return ok
}

func diagnosticHostRepairBackendSupportsVerification(id diagnosticVerificationID) bool {
	_, ok := diagnosticHostRepairVerifyHandlers[id]
	return ok
}

func (b *diagnosticHostRepairBackend) ApplyDiagnosticRepair(action diagnosticRepairActionDefinition, item diagnosticRepairPlanItem) diagnosticRepairStepResult {
	r := NewRunner(b.ui, flagVerbose, flagDryRun)
	evidence := []string{fmt.Sprintf("applying typed repair action %s", action.ID)}

	handler, ok := diagnosticHostRepairApplyHandlers[action.ID]
	if !ok {
		return diagnosticRepairStepResult{Evidence: evidence, Err: fmt.Errorf("no host repair backend for action %s", action.ID)}
	}

	if err := handler(b, r, action, item); err != nil {
		return diagnosticRepairStepResult{Evidence: evidence, Err: err}
	}
	return diagnosticRepairStepResult{Evidence: append(evidence, "repair action completed")}
}

func (b *diagnosticHostRepairBackend) VerifyDiagnosticRepair(action diagnosticRepairActionDefinition, item diagnosticRepairPlanItem) diagnosticRepairStepResult {
	evidence := []string{fmt.Sprintf("verifying %s", action.Verification)}

	handler, ok := diagnosticHostRepairVerifyHandlers[action.Verification]
	if !ok {
		return diagnosticRepairStepResult{Evidence: evidence, Err: fmt.Errorf("no host verification backend for %s", action.Verification)}
	}

	if err := handler(b, action, item); err != nil {
		return diagnosticRepairStepResult{Evidence: evidence, Err: err}
	}
	return diagnosticRepairStepResult{Evidence: append(evidence, "desired state verified")}
}

func (b *diagnosticHostRepairBackend) applyWorkspaceRepair(item diagnosticRepairPlanItem) error {
	targets := append([]string{}, b.projectDir)
	targets = append(targets, diagnosticRepairEvidencePaths(item, "workspace:", "path:")...)
	targets = uniqueStrings(targets)
	for _, target := range targets {
		if target == "" {
			continue
		}
		info, err := os.Stat(target)
		if err != nil {
			return fmt.Errorf("inspect workspace repair target %s: %w", target, err)
		}
		if !info.IsDir() {
			target = filepath.Dir(target)
		}
		if _, err := ensureProjectWritable(target); err != nil {
			return fmt.Errorf("repair workspace ACL for %s: %w", target, err)
		}
	}
	return nil
}

func (b *diagnosticHostRepairBackend) verifyWorkspaceRepair(item diagnosticRepairPlanItem) error {
	targets := append([]string{}, b.projectDir)
	targets = append(targets, diagnosticRepairEvidencePaths(item, "workspace:", "path:")...)
	targets = uniqueStrings(targets)
	for _, target := range targets {
		if target == "" {
			continue
		}
		info, err := os.Stat(target)
		if err != nil {
			return fmt.Errorf("inspect workspace verification target %s: %w", target, err)
		}
		if !info.IsDir() {
			target = filepath.Dir(target)
		}
		if projectNeedsACLRepair(target) {
			return fmt.Errorf("workspace ACL still missing on %s", target)
		}
	}
	return nil
}

func (b *diagnosticHostRepairBackend) applyAgentUmask(r *Runner) error {
	agentZshrc := filepath.Join(agentHome, ".zshrc")
	data, _ := r.AgentOutput("cat", agentZshrc)
	updated := setup.UpsertManagedBlock(data, umaskBlockStart, umaskBlockEnd, "umask 007")
	if err := r.SudoWriteFile("write agent umask to .zshrc", agentZshrc, updated); err != nil {
		return fmt.Errorf("set umask in agent .zshrc: %w", err)
	}
	if err := r.Sudo("set agent .zshrc ownership", "chown", agentUser+":staff", agentZshrc); err != nil {
		return fmt.Errorf("chown agent .zshrc: %w", err)
	}
	return nil
}

func (b *diagnosticHostRepairBackend) applySetupAgentHome(r *Runner) error {
	if _, err := user.Lookup(agentUser); err != nil {
		return fmt.Errorf("agent user must exist before repairing home: %w", err)
	}
	if err := r.Sudo("create agent home directory", "mkdir", "-p", agentHome); err != nil {
		return fmt.Errorf("mkdir agent home: %w", err)
	}
	if err := r.Sudo("set agent home directory ownership", "chown", agentUser+":staff", agentHome); err != nil {
		return fmt.Errorf("chown agent home: %w", err)
	}
	_ = r.Sudo("populate agent home directory", "createhomedir", "-c", "-u", agentUser)
	return nil
}

func (b *diagnosticHostRepairBackend) applyNetworkPF(r *Runner) error {
	_ = r.Sudo("remove stale Hazmat pf anchor before repair", "rm", "-f", pfAnchorFile)
	return setupPfFirewall(b.ui, r)
}

func (b *diagnosticHostRepairBackend) applyHostWrappers(r *Runner) error {
	env := setupToolingEnv()
	if err := r.MkdirAll(env.HostWrapperDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", env.HostWrapperDir, err)
	}
	hazmatBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve hazmat binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(hazmatBin); err == nil {
		hazmatBin = resolved
	}
	for _, wrapper := range []struct {
		name       string
		subcommand string
	}{
		{name: env.HostClaudeWrapperName, subcommand: "claude"},
		{name: env.HostExecWrapperName, subcommand: "exec"},
		{name: env.HostShellWrapperName, subcommand: "shell"},
	} {
		path := filepath.Join(env.HostWrapperDir, wrapper.name)
		if err := r.UserWriteFile(path, setup.HostWrapperContent(hazmatBin, wrapper.subcommand)); err != nil {
			return fmt.Errorf("write wrapper %s: %w", path, err)
		}
		if err := r.Chmod(path, 0o755); err != nil {
			return fmt.Errorf("chmod %s: %w", path, err)
		}
	}
	profile, ok := setup.CurrentUserShellProfile(env)
	if !ok {
		return nil
	}
	userRCData, _ := os.ReadFile(profile.RCPath)
	if strings.Contains(string(userRCData), env.UserPathBlockStart) {
		return nil
	}
	updatedUserRC := setup.UpsertManagedBlock(string(userRCData), env.UserPathBlockStart, env.UserPathBlockEnd, profile.PathBlockLines...)
	if err := r.MkdirAll(filepath.Dir(profile.RCPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(profile.RCPath), err)
	}
	if err := r.UserWriteFile(profile.RCPath, updatedUserRC); err != nil {
		return fmt.Errorf("update %s: %w", profile.RCPath, err)
	}
	return nil
}

func (b *diagnosticHostRepairBackend) applyDNSBlocklist(r *Runner) error {
	if !dnsBlocklistFullyApplied() {
		rollbackDNSBlocklist(b.ui, r)
	}
	return setupDNSBlocklist(b.ui, r)
}

func (b *diagnosticHostRepairBackend) applyNetworkPersistence(r *Runner) error {
	if err := setupPfFirewall(b.ui, r); err != nil {
		return err
	}
	if _, err := os.Stat(pfDaemonPlist); os.IsNotExist(err) {
		return setupLaunchDaemon(b.ui, r)
	}
	if !launchctlLoaded(pfDaemonLabel) {
		if err := r.LaunchctlBootstrap("start firewall persistence daemon", pfDaemonPlist); err != nil {
			return fmt.Errorf("launchctl bootstrap: %w", err)
		}
	}
	return nil
}

// launchctlLoaded is intentionally scoped to doctor repair execution. Read-only
// check/status paths must not run launchctl probes.
func launchctlLoaded(label string) bool {
	_, err := commandStdout(hostLaunchctlPath, "print", "system/"+label)
	return err == nil
}

func (b *diagnosticHostRepairBackend) applyCredentialMigration(actionID diagnosticRepairActionID) error {
	var out bytes.Buffer
	if err := runMigrateCredentials(migrateCredentialsOptions{
		Home:   b.homeDir,
		DryRun: flagDryRun,
		Writer: &out,
		Scope:  credentialMigrationScopeForRepairAction(actionID),
	}); err != nil {
		return err
	}
	return nil
}

func (b *diagnosticHostRepairBackend) applyClaudeProjectPermissions(r *Runner, item diagnosticRepairPlanItem) error {
	paths, err := claudeProjectPermissionRepairPaths(item)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := r.Sudo("set Claude project owner/group", "chown", agentUser+":"+sharedGroup, path); err != nil {
			return fmt.Errorf("chown Claude project dir %s: %w", path, err)
		}
		if err := r.Sudo("set Claude project group-writable setgid permissions", "chmod", "2770", path); err != nil {
			return fmt.Errorf("chmod Claude project dir %s: %w", path, err)
		}
	}
	return nil
}

func verifyAgentUmask() error {
	out, err := asAgentOutput("cat", filepath.Join(agentHome, ".zshrc"))
	if err != nil {
		return fmt.Errorf("read agent .zshrc: %w", err)
	}
	if !strings.Contains(out, "umask 007") {
		return fmt.Errorf("agent .zshrc still does not contain umask 007")
	}
	return nil
}

func verifySetupAgentUser() error {
	u, err := user.Lookup(agentUser)
	if err != nil {
		return fmt.Errorf("agent user missing: %w", err)
	}
	if u.Uid != agentUID {
		return fmt.Errorf("agent user UID is %s, want %s", u.Uid, agentUID)
	}
	if out, err := sudoOutput("dscl", ".", "-read", "/Users/"+agentUser, "IsHidden"); err == nil && !strings.Contains(out, "1") {
		return fmt.Errorf("agent user is not hidden from login screen")
	}
	return nil
}

func verifySetupAgentHome() error {
	info, err := os.Stat(agentHome)
	if err != nil {
		return fmt.Errorf("agent home missing: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("agent home is not a directory")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("agent home owner metadata unavailable")
	}
	ownerUID := strconv.FormatUint(uint64(st.Uid), 10)
	if ownerUID != agentUID {
		return fmt.Errorf("agent home owner uid is %s, want %s", ownerUID, agentUID)
	}
	return nil
}

func verifySetupDevGroup(currentUser string) error {
	if _, err := user.LookupGroup(sharedGroup); err != nil {
		return fmt.Errorf("shared group missing: %w", err)
	}
	for _, account := range []string{currentUser, agentUser} {
		member, err := groupMembershipContains(sharedGroup, account)
		if err != nil {
			return err
		}
		if !member {
			return fmt.Errorf("%s is not a member of %s", account, sharedGroup)
		}
	}
	return nil
}

func verifySetupHomeTraverse(homeDir string) error {
	if !homeAllowsAgentTraverse(homeDir) {
		return fmt.Errorf("agent cannot traverse host home %s", homeDir)
	}
	return nil
}

func verifySetupSudoers() error {
	if !launchSudoersInstalled() {
		return fmt.Errorf("launch-helper sudoers file missing: %s", sudoersFile)
	}
	helperPath := launchHelperPath()
	out, err := newSudoNoPromptCommand("-u", agentUser, helperPath, "exec", "/usr/bin/id", "-un").CombinedOutput()
	if err != nil {
		return fmt.Errorf("launch-helper sudoers verification failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) != agentUser {
		return fmt.Errorf("launch-helper sudoers verification returned %q, want %q", strings.TrimSpace(string(out)), agentUser)
	}
	return nil
}

func verifySetupSeatbeltWrapper() error {
	exists, err := agentPathExists(seatbeltWrapperPath)
	if err != nil {
		return fmt.Errorf("inspect seatbelt wrapper as agent: %w", err)
	}
	if !exists {
		return fmt.Errorf("seatbelt wrapper missing: %s", seatbeltWrapperPath)
	}
	executable, err := agentPathIsExecutable(seatbeltWrapperPath)
	if err != nil {
		return fmt.Errorf("inspect seatbelt wrapper executable bit as agent: %w", err)
	}
	if !executable {
		return fmt.Errorf("seatbelt wrapper is not executable: %s", seatbeltWrapperPath)
	}
	return nil
}

func verifySetupAgentEnv() error {
	if err := asAgentQuiet("test", "-f", agentEnvPath); err != nil {
		return fmt.Errorf("agent env file missing: %w", err)
	}
	out, _ := asAgentOutput("cat", filepath.Join(agentHome, ".zshrc"))
	if !strings.Contains(out, "agent-env.zsh") {
		return fmt.Errorf("agent .zshrc still does not source the Hazmat env file")
	}
	return nil
}

func verifySetupHostWrappers() error {
	for _, wrapper := range []string{hostClaudeWrapperName, hostExecWrapperName, hostShellWrapperName} {
		path := hostWrapperPath(wrapper)
		if err := validateHostWrapper(path); err != nil {
			return err
		}
	}
	return nil
}

func verifyNetworkPF() error {
	if _, err := os.Stat(pfAnchorFile); err != nil {
		return fmt.Errorf("pf anchor file missing: %w", err)
	}
	rules, err := pfAnchorRules()
	if err != nil {
		return fmt.Errorf("read pf anchor rules: %w", err)
	}
	if !strings.Contains(rules, "block") {
		return fmt.Errorf("pf anchor %s has no block rules", pfAnchorName)
	}
	if out, err := sudoOutput(hostPfctlPath, "-si"); err != nil || !strings.Contains(out, "Status: Enabled") {
		return fmt.Errorf("pf is not enabled")
	}
	return nil
}

// pfAnchorRules returns the loaded rules for the agent anchor. This is used by
// doctor repair verification, not by read-only hazmat check.
func pfAnchorRules() (string, error) {
	return sudoOutput(hostPfctlPath, "-a", pfAnchorName, "-sr")
}

func verifyDNSBlocklist() error {
	if !dnsBlocklistFullyApplied() {
		return fmt.Errorf("DNS blocklist is still incomplete")
	}
	return nil
}

func verifyNetworkPersistence() error {
	if _, err := os.Stat(pfDaemonPlist); err != nil {
		return fmt.Errorf("LaunchDaemon plist missing: %w", err)
	}
	if !launchctlLoadedPrivileged(pfDaemonLabel) {
		return fmt.Errorf("LaunchDaemon %s is not loaded", pfDaemonLabel)
	}
	data, err := os.ReadFile("/etc/pf.conf")
	if err != nil {
		return fmt.Errorf("read /etc/pf.conf: %w", err)
	}
	if !strings.Contains(string(data), `anchor "agent"`) {
		return fmt.Errorf("/etc/pf.conf does not reference anchor %s", pfAnchorName)
	}
	return nil
}

func launchctlLoadedPrivileged(label string) bool {
	return sudoNoPrompt(hostLaunchctlPath, "list", label) == nil
}

func (b *diagnosticHostRepairBackend) verifyCredentialMigration(actionID diagnosticRepairActionID) error {
	entries, err := inspectCredentialInventory(b.homeDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Status() != credentialInventoryNeedsRepair {
			continue
		}
		switch actionID {
		case "repair.credential.claude-state":
			if entry.ID == credentialHarnessClaudeState {
				return fmt.Errorf("Claude state residue still needs repair")
			}
		case "repair.credential.cloud-secret-key":
			if entry.ID == credentialCloudS3SecretKey {
				return fmt.Errorf("legacy cloud secret key still needs repair")
			}
		case "repair.credential.residue":
			if entry.ID != credentialHarnessClaudeState && entry.ID != credentialCloudS3SecretKey {
				return fmt.Errorf("credential residue still needs repair: %s", entry.ID)
			}
		}
	}
	return nil
}

func verifyClaudeProjectPermissions(item diagnosticRepairPlanItem) error {
	paths, err := claudeProjectPermissionRepairPaths(item)
	if err != nil {
		return err
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("inspect Claude project dir %s: %w", path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("Claude project path is not a directory: %s", path)
		}
		if !pathHasDevACL(path, true) && info.Mode().Perm()&0o020 == 0 {
			return fmt.Errorf("Claude project dir is still not group-writable: %s", path)
		}
	}
	return nil
}

func dnsBlocklistFullyApplied() bool {
	hosts, err := os.ReadFile("/etc/hosts")
	if err != nil || !strings.Contains(string(hosts), "AI Agent Blocklist") {
		return false
	}
	for _, domain := range []string{"ngrok.io", "pastebin.com", "webhook.site", "transfer.sh"} {
		if !checkBlockedDomain(domain) {
			return false
		}
	}
	return true
}

func claudeProjectPermissionRepairPaths(item diagnosticRepairPlanItem) ([]string, error) {
	root := filepath.Join(agentHome, ".claude", "projects")
	return boundedRepairDirs(root, diagnosticRepairEvidencePaths(item, "path:"))
}

func boundedRepairDirs(root string, candidates []string) ([]string, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("empty repair root")
	}
	root = filepath.Clean(root)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("repair item did not include bounded path evidence")
	}
	var paths []string
	for _, candidate := range candidates {
		path := filepath.Clean(candidate)
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("repair path is not absolute: %s", candidate)
		}
		if path == root || !isWithinDir(root, path) {
			return nil, fmt.Errorf("repair path %s is outside allowed root %s", path, root)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect repair path %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("repair path is a symlink: %s", path)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("repair path is not a directory: %s", path)
		}
		paths = append(paths, path)
	}
	return uniqueStrings(paths), nil
}

func diagnosticRepairEvidencePaths(item diagnosticRepairPlanItem, prefixes ...string) []string {
	var paths []string
	for _, detail := range item.Details {
		value := strings.TrimSpace(detail)
		if value == "" {
			continue
		}
		if len(prefixes) > 0 {
			matched := false
			for _, prefix := range prefixes {
				if rest, ok := strings.CutPrefix(value, prefix); ok {
					value = strings.TrimSpace(rest)
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if filepath.IsAbs(value) {
			paths = append(paths, filepath.Clean(value))
		}
	}
	return uniqueStrings(paths)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

package hazmat

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"hazmat/internal/setup"
	linuxsetup "hazmat/internal/setup/linux"
	platformlinux "hazmat/platform/linux"
)

type linuxDiagnosticSetupOperation string

const (
	linuxDiagnosticSetupApply    linuxDiagnosticSetupOperation = "apply"
	linuxDiagnosticSetupVerify   linuxDiagnosticSetupOperation = "verify"
	linuxDiagnosticSetupRollback linuxDiagnosticSetupOperation = "rollback"
)

type linuxSetupDiagnosticInfo struct {
	ResourceID      diagnosticResourceID
	FindingID       diagnosticFindingID
	Owner           string
	DesiredState    string
	Title           string
	Action          string
	SecurityImpact  string
	Authority       diagnosticRepairAuthority
	Privileged      bool
	Reversibility   diagnosticRepairReversibility
	Preconditions   []string
	TestObligations []string
	ProofNotes      string
}

var linuxDiagnosticRuntimeOS = func() string {
	return runtime.GOOS
}

type linuxSetupCommandRunner interface {
	Sudo(reason string, args ...string) error
	SudoOutput(args ...string) (string, error)
	SudoWriteFile(reason, path, content string) error
}

const (
	linuxSetupStateDir           = "/var/lib/hazmat/linux"
	linuxDistroProfilePath       = linuxSetupStateDir + "/distro-profile.json"
	linuxFirewallPolicyRoot      = "/etc/hazmat/linux/firewall.d"
	linuxResolverPolicyRoot      = "/etc/hazmat/linux/resolver.d"
	linuxCgroupRootPath          = "/sys/fs/cgroup/hazmat-agent"
	linuxAgentUserName           = "agent"
	linuxAgentHomePath           = "/home/agent"
	linuxSharedGroupName         = "dev"
	linuxAgentShell              = "/usr/sbin/nologin"
	linuxRootHelperPath          = "/usr/local/libexec/hazmat-launch"
	linuxSudoersFile             = "/etc/sudoers.d/agent"
	linuxRootHelperSudoersReason = "write Linux root-helper sudoers entry"
)

var linuxDiagnosticSetupRunner = func(b *diagnosticHostRepairBackend) linuxSetupCommandRunner {
	return NewRunner(b.ui, flagVerbose, flagDryRun)
}

var linuxDiagnosticInspectHost = platformlinux.InspectHost

var linuxDiagnosticExecutable = os.Executable

var linuxDiagnosticSetupCallbacks = func(b *diagnosticHostRepairBackend, operation linuxDiagnosticSetupOperation) linuxsetup.Callbacks {
	backend := linuxDiagnosticSetupBackend{
		runner:      linuxDiagnosticSetupRunner(b),
		operation:   operation,
		currentUser: linuxCurrentUserName(b.currentUser),
		projectDir:  b.projectDir,
		inspectHost: linuxDiagnosticInspectHost,
		executable:  linuxDiagnosticExecutable,
	}
	return backend.callbacks()
}

type linuxDiagnosticSetupBackend struct {
	runner      linuxSetupCommandRunner
	operation   linuxDiagnosticSetupOperation
	currentUser string
	projectDir  string
	inspectHost func() platformlinux.Report
	executable  func() (string, error)
}

func (b linuxDiagnosticSetupBackend) callbacks() linuxsetup.Callbacks {
	return linuxsetup.Callbacks{
		DistroProfile:   b.callback(b.applyDistroProfile, b.verifyDistroProfile, b.rollbackDistroProfile),
		AgentUser:       b.callback(b.applyAgentUser, b.verifyAgentUser, b.rollbackAgentUser),
		SharedGroup:     b.callback(b.applySharedGroup, b.verifySharedGroup, b.rollbackSharedGroup),
		AgentHome:       b.callback(b.applyAgentHome, b.verifyAgentHome, b.rollbackAgentHome),
		WorkspaceAccess: b.callback(b.applyWorkspaceAccess, b.verifyWorkspaceAccess, b.rollbackWorkspaceAccess),
		ToolHome:        b.callback(b.applyToolHome, b.verifyToolHome, b.rollbackToolHome),
		FirewallPolicy:  b.callback(b.applyFirewallPolicy, b.verifyFirewallPolicy, b.rollbackFirewallPolicy),
		ResolverPolicy:  b.callback(b.applyResolverPolicy, b.verifyResolverPolicy, b.rollbackResolverPolicy),
		CgroupRoot:      b.callback(b.applyCgroupRoot, b.verifyCgroupRoot, b.rollbackCgroupRoot),
		LaunchHelper:    b.callback(b.applyLaunchHelper, b.verifyLaunchHelper, b.rollbackLaunchHelper),
		Sudoers:         b.callback(b.applySudoers, b.verifySudoers, b.rollbackSudoers),
	}
}

func (b linuxDiagnosticSetupBackend) callback(apply, verify, rollback func() error) linuxsetup.Callback {
	return func() error {
		switch b.operation {
		case linuxDiagnosticSetupApply:
			return apply()
		case linuxDiagnosticSetupVerify:
			return verify()
		case linuxDiagnosticSetupRollback:
			return rollback()
		default:
			return fmt.Errorf("unknown Linux setup operation %q", b.operation)
		}
	}
}

func (b linuxDiagnosticSetupBackend) applyDistroProfile() error {
	report := b.inspectHost()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal Linux distro profile: %w", err)
	}
	if err := b.runner.Sudo("create Linux setup profile state dir", "mkdir", "-p", linuxSetupStateDir); err != nil {
		return fmt.Errorf("create Linux setup profile state dir: %w", err)
	}
	if err := b.runner.SudoWriteFile("write Linux distro profile", linuxDistroProfilePath, string(data)+"\n"); err != nil {
		return fmt.Errorf("write Linux distro profile: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) verifyDistroProfile() error {
	data, err := b.runner.SudoOutput("cat", linuxDistroProfilePath)
	if err != nil {
		return fmt.Errorf("read Linux distro profile: %w", err)
	}
	var report platformlinux.Report
	if err := json.Unmarshal([]byte(data), &report); err != nil {
		return fmt.Errorf("parse Linux distro profile: %w", err)
	}
	if strings.TrimSpace(report.RuntimeOS) != "linux" {
		return fmt.Errorf("Linux distro profile runtime_os = %q, want linux", report.RuntimeOS)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) applyAgentUser() error {
	if out, err := b.runner.SudoOutput("id", "-u", linuxAgentUserName); err == nil && strings.TrimSpace(out) != "" {
		return nil
	}
	if err := b.runner.Sudo("create Linux agent user",
		"useradd", "--create-home", "--home-dir", linuxAgentHomePath, "--shell", linuxAgentShell, "--uid", agentUID, linuxAgentUserName,
	); err != nil {
		return fmt.Errorf("create Linux agent user: %w", err)
	}
	if err := b.runner.Sudo("lock Linux agent user password", "passwd", "-l", linuxAgentUserName); err != nil {
		return fmt.Errorf("lock Linux agent user password: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) verifyAgentUser() error {
	out, err := b.runner.SudoOutput("id", "-u", linuxAgentUserName)
	if err != nil {
		return fmt.Errorf("Linux agent user missing: %w", err)
	}
	if got := strings.TrimSpace(out); got != agentUID {
		return fmt.Errorf("Linux agent user UID is %s, want %s", got, agentUID)
	}
	passwd, err := b.runner.SudoOutput("getent", "passwd", linuxAgentUserName)
	if err != nil {
		return fmt.Errorf("read Linux agent passwd entry: %w", err)
	}
	fields := strings.Split(strings.TrimSpace(passwd), ":")
	if len(fields) < 7 {
		return fmt.Errorf("Linux agent passwd entry is malformed")
	}
	if fields[5] != linuxAgentHomePath {
		return fmt.Errorf("Linux agent home is %s, want %s", fields[5], linuxAgentHomePath)
	}
	if fields[6] != linuxAgentShell {
		return fmt.Errorf("Linux agent shell is %s, want %s", fields[6], linuxAgentShell)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) applySharedGroup() error {
	if _, err := b.runner.SudoOutput("getent", "group", linuxSharedGroupName); err != nil {
		if err := b.runner.Sudo("create Linux shared group", "groupadd", "--gid", sharedGID, linuxSharedGroupName); err != nil {
			return fmt.Errorf("create Linux shared group: %w", err)
		}
	}
	for _, account := range []string{b.currentUser, linuxAgentUserName} {
		if strings.TrimSpace(account) == "" {
			return fmt.Errorf("Linux shared-group repair requires account name")
		}
		if err := b.runner.Sudo("add "+account+" to Linux shared group", "usermod", "-a", "-G", linuxSharedGroupName, account); err != nil {
			return fmt.Errorf("add %s to Linux shared group: %w", account, err)
		}
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) verifySharedGroup() error {
	group, err := b.runner.SudoOutput("getent", "group", linuxSharedGroupName)
	if err != nil {
		return fmt.Errorf("Linux shared group missing: %w", err)
	}
	fields := strings.Split(strings.TrimSpace(group), ":")
	if len(fields) < 4 {
		return fmt.Errorf("Linux shared group entry is malformed")
	}
	if fields[2] != sharedGID {
		return fmt.Errorf("Linux shared group GID is %s, want %s", fields[2], sharedGID)
	}
	members := "," + fields[3] + ","
	for _, account := range []string{b.currentUser, linuxAgentUserName} {
		if !strings.Contains(members, ","+account+",") {
			return fmt.Errorf("%s is not a member of Linux shared group %s", account, linuxSharedGroupName)
		}
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) applyAgentHome() error {
	if err := b.runner.Sudo("create Linux agent home", "install", "-d", "-o", linuxAgentUserName, "-g", linuxSharedGroupName, "-m", "0700", linuxAgentHomePath); err != nil {
		return fmt.Errorf("create Linux agent home: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) verifyAgentHome() error {
	out, err := b.runner.SudoOutput("stat", "-c", "%U:%G:%a", linuxAgentHomePath)
	if err != nil {
		return fmt.Errorf("stat Linux agent home: %w", err)
	}
	want := linuxAgentUserName + ":" + linuxSharedGroupName + ":700"
	if got := strings.TrimSpace(out); got != want {
		return fmt.Errorf("Linux agent home metadata = %s, want %s", got, want)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) applyWorkspaceAccess() error {
	if strings.TrimSpace(b.projectDir) == "" {
		return fmt.Errorf("Linux workspace access repair requires project directory")
	}
	if err := b.runner.Sudo("grant Linux agent workspace ACL", "setfacl", "-m", "u:"+linuxAgentUserName+":rwx", "-m", "g:"+linuxSharedGroupName+":rwx", b.projectDir); err != nil {
		return fmt.Errorf("grant Linux workspace ACL: %w", err)
	}
	if err := b.runner.Sudo("grant Linux agent default workspace ACL", "setfacl", "-d", "-m", "u:"+linuxAgentUserName+":rwx", "-m", "g:"+linuxSharedGroupName+":rwx", b.projectDir); err != nil {
		return fmt.Errorf("grant Linux default workspace ACL: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) verifyWorkspaceAccess() error {
	if strings.TrimSpace(b.projectDir) == "" {
		return fmt.Errorf("Linux workspace access verification requires project directory")
	}
	for _, flag := range []string{"-r", "-w", "-x"} {
		if err := b.runner.Sudo("verify Linux agent workspace access", "-u", linuxAgentUserName, "test", flag, b.projectDir); err != nil {
			return fmt.Errorf("verify Linux agent workspace access %s: %w", flag, err)
		}
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) applyToolHome() error {
	for _, dir := range []string{
		filepath.Join(linuxAgentHomePath, ".cache"),
		filepath.Join(linuxAgentHomePath, ".config"),
		filepath.Join(linuxAgentHomePath, ".local", "share"),
		filepath.Join(linuxAgentHomePath, ".local", "state"),
		filepath.Join(linuxAgentHomePath, ".local", "bin"),
	} {
		if err := b.runner.Sudo("create Linux agent tool dir", "install", "-d", "-o", linuxAgentUserName, "-g", linuxSharedGroupName, "-m", "0700", dir); err != nil {
			return fmt.Errorf("create Linux agent tool dir %s: %w", dir, err)
		}
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) verifyToolHome() error {
	for _, dir := range []string{
		filepath.Join(linuxAgentHomePath, ".cache"),
		filepath.Join(linuxAgentHomePath, ".config"),
		filepath.Join(linuxAgentHomePath, ".local", "share"),
		filepath.Join(linuxAgentHomePath, ".local", "state"),
	} {
		if err := b.runner.Sudo("verify Linux agent tool dir", "test", "-d", dir); err != nil {
			return fmt.Errorf("verify Linux agent tool dir %s: %w", dir, err)
		}
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) applyFirewallPolicy() error {
	return b.runner.Sudo("create Linux firewall policy root", "install", "-d", "-o", "root", "-g", "root", "-m", "0755", linuxFirewallPolicyRoot)
}

func (b linuxDiagnosticSetupBackend) verifyFirewallPolicy() error {
	return b.runner.Sudo("verify Linux firewall policy root", "test", "-d", linuxFirewallPolicyRoot)
}

func (b linuxDiagnosticSetupBackend) applyResolverPolicy() error {
	return b.runner.Sudo("create Linux resolver policy root", "install", "-d", "-o", "root", "-g", "root", "-m", "0755", linuxResolverPolicyRoot)
}

func (b linuxDiagnosticSetupBackend) verifyResolverPolicy() error {
	return b.runner.Sudo("verify Linux resolver policy root", "test", "-d", linuxResolverPolicyRoot)
}

func (b linuxDiagnosticSetupBackend) applyCgroupRoot() error {
	if err := b.runner.Sudo("verify Linux cgroup v2 is mounted", "test", "-f", "/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return fmt.Errorf("Linux cgroup v2 unavailable: %w", err)
	}
	if err := b.runner.Sudo("create Linux cgroup root", "mkdir", "-p", linuxCgroupRootPath); err != nil {
		return fmt.Errorf("create Linux cgroup root: %w", err)
	}
	if err := b.runner.Sudo("set Linux cgroup root group", "chgrp", linuxSharedGroupName, linuxCgroupRootPath); err != nil {
		return fmt.Errorf("chgrp Linux cgroup root: %w", err)
	}
	if err := b.runner.Sudo("set Linux cgroup root mode", "chmod", "0750", linuxCgroupRootPath); err != nil {
		return fmt.Errorf("chmod Linux cgroup root: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) verifyCgroupRoot() error {
	if err := b.runner.Sudo("verify Linux cgroup v2 is mounted", "test", "-f", "/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return fmt.Errorf("Linux cgroup v2 unavailable: %w", err)
	}
	return b.runner.Sudo("verify Linux cgroup root", "test", "-d", linuxCgroupRootPath)
}

func (b linuxDiagnosticSetupBackend) applyLaunchHelper() error {
	src, err := b.launchHelperSource()
	if err != nil {
		return err
	}
	if err := b.runner.Sudo("create Linux launch helper directory", "mkdir", "-p", filepath.Dir(linuxRootHelperPath)); err != nil {
		return fmt.Errorf("create Linux launch helper directory: %w", err)
	}
	if err := b.runner.Sudo("install Linux root helper", "install", "-o", "root", "-g", "root", "-m", "0755", src, linuxRootHelperPath); err != nil {
		return fmt.Errorf("install Linux root helper: %w", err)
	}
	return b.verifyLaunchHelper()
}

func (b linuxDiagnosticSetupBackend) verifyLaunchHelper() error {
	if err := b.runner.Sudo("verify Linux root helper executable", "test", "-x", linuxRootHelperPath); err != nil {
		return fmt.Errorf("Linux root helper missing: %w", err)
	}
	if _, err := b.runner.SudoOutput(linuxRootHelperPath, "run-agent", "--help"); err != nil {
		return fmt.Errorf("Linux root helper does not support run-agent: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) applySudoers() error {
	for _, verify := range []func() error{b.verifyAgentUser, b.verifyDistroProfile, b.verifyFirewallPolicy, b.verifyResolverPolicy, b.verifyCgroupRoot, b.verifyLaunchHelper} {
		if err := verify(); err != nil {
			return fmt.Errorf("refuse Linux sudoers before prerequisites verify: %w", err)
		}
	}
	entry := linuxRootHelperSudoersEntry(b.currentUser)
	if data, err := b.runner.SudoOutput("cat", linuxSudoersFile); err == nil && strings.Contains(data, strings.TrimSpace(entry)) {
		return nil
	}
	if err := b.runner.SudoWriteFile(linuxRootHelperSudoersReason, linuxSudoersFile, entry); err != nil {
		return fmt.Errorf("write Linux sudoers: %w", err)
	}
	if err := b.runner.Sudo("set Linux sudoers permissions", "chmod", "440", linuxSudoersFile); err != nil {
		return fmt.Errorf("chmod Linux sudoers: %w", err)
	}
	if err := b.runner.Sudo("validate Linux sudoers", "visudo", "-c", "-f", linuxSudoersFile); err != nil {
		return fmt.Errorf("validate Linux sudoers: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) verifySudoers() error {
	data, err := b.runner.SudoOutput("cat", linuxSudoersFile)
	if err != nil {
		return fmt.Errorf("read Linux sudoers: %w", err)
	}
	entry := strings.TrimSpace(linuxRootHelperSudoersEntry(b.currentUser))
	if !strings.Contains(data, entry) {
		return fmt.Errorf("Linux sudoers does not contain narrow root-helper rule")
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackDistroProfile() error {
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackAgentUser() error {
	if _, err := b.runner.SudoOutput("id", "-u", linuxAgentUserName); err != nil {
		return nil
	}
	if err := b.runner.Sudo("delete Linux agent user", "userdel", linuxAgentUserName); err != nil {
		return fmt.Errorf("delete Linux agent user: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackSharedGroup() error {
	if _, err := b.runner.SudoOutput("getent", "group", linuxSharedGroupName); err != nil {
		return nil
	}
	if err := b.runner.Sudo("delete Linux shared group", "groupdel", linuxSharedGroupName); err != nil {
		return fmt.Errorf("delete Linux shared group: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackAgentHome() error {
	if err := b.runner.Sudo("remove Linux agent home", "rm", "-rf", linuxAgentHomePath); err != nil {
		return fmt.Errorf("remove Linux agent home: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackWorkspaceAccess() error {
	if strings.TrimSpace(b.projectDir) == "" {
		return fmt.Errorf("Linux workspace access rollback requires project directory")
	}
	acl, err := b.runner.SudoOutput("getfacl", "-cp", b.projectDir)
	if err != nil {
		return fmt.Errorf("inspect Linux workspace ACL: %w", err)
	}
	if linuxACLContains(acl, "user", linuxAgentUserName) || linuxACLContains(acl, "group", linuxSharedGroupName) {
		if err := b.runner.Sudo("remove Linux agent workspace ACL", "setfacl", "-x", "u:"+linuxAgentUserName, "-x", "g:"+linuxSharedGroupName, b.projectDir); err != nil {
			return fmt.Errorf("remove Linux workspace ACL: %w", err)
		}
	}
	if linuxACLContains(acl, "default:user", linuxAgentUserName) || linuxACLContains(acl, "default:group", linuxSharedGroupName) {
		if err := b.runner.Sudo("remove Linux agent default workspace ACL", "setfacl", "-d", "-x", "u:"+linuxAgentUserName, "-x", "g:"+linuxSharedGroupName, b.projectDir); err != nil {
			return fmt.Errorf("remove Linux default workspace ACL: %w", err)
		}
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackToolHome() error {
	for _, dir := range []string{
		filepath.Join(linuxAgentHomePath, ".cache"),
		filepath.Join(linuxAgentHomePath, ".config"),
		filepath.Join(linuxAgentHomePath, ".local", "share"),
		filepath.Join(linuxAgentHomePath, ".local", "state"),
		filepath.Join(linuxAgentHomePath, ".local", "bin"),
	} {
		if err := b.runner.Sudo("remove Linux agent tool dir", "rm", "-rf", dir); err != nil {
			return fmt.Errorf("remove Linux agent tool dir %s: %w", dir, err)
		}
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackFirewallPolicy() error {
	if err := b.runner.Sudo("remove Linux firewall policy root", "rm", "-rf", linuxFirewallPolicyRoot); err != nil {
		return fmt.Errorf("remove Linux firewall policy root: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackResolverPolicy() error {
	if err := b.runner.Sudo("remove Linux resolver policy root", "rm", "-rf", linuxResolverPolicyRoot); err != nil {
		return fmt.Errorf("remove Linux resolver policy root: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackCgroupRoot() error {
	if err := b.runner.Sudo("check Linux cgroup root", "test", "-d", linuxCgroupRootPath); err != nil {
		return nil
	}
	if err := b.runner.Sudo("remove Linux cgroup root", "rmdir", linuxCgroupRootPath); err != nil {
		return fmt.Errorf("remove Linux cgroup root: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackLaunchHelper() error {
	if err := b.runner.Sudo("remove Linux root helper", "rm", "-f", linuxRootHelperPath); err != nil {
		return fmt.Errorf("remove Linux root helper: %w", err)
	}
	return nil
}

func (b linuxDiagnosticSetupBackend) rollbackSudoers() error {
	if err := b.runner.Sudo("remove Linux root-helper sudoers entry", "rm", "-f", linuxSudoersFile); err != nil {
		return fmt.Errorf("remove Linux sudoers: %w", err)
	}
	return nil
}

func linuxACLContains(acl, kind, name string) bool {
	prefix := kind + ":" + name + ":"
	for _, line := range strings.Split(acl, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}

func (b linuxDiagnosticSetupBackend) launchHelperSource() (string, error) {
	if override := strings.TrimSpace(os.Getenv("HAZMAT_LINUX_ROOT_HELPER_SOURCE")); override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("HAZMAT_LINUX_ROOT_HELPER_SOURCE must be absolute")
		}
		return override, nil
	}
	exe, err := b.executable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable for Linux helper source: %w", err)
	}
	candidates := []string{
		filepath.Join(filepath.Dir(exe), "hazmat-linux-root-helper"),
		filepath.Join(filepath.Dir(exe), "hazmat-launch"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Linux root helper source not found; build hazmat-linux-root-helper or set HAZMAT_LINUX_ROOT_HELPER_SOURCE")
}

func linuxRootHelperSudoersEntry(currentUser string) string {
	return fmt.Sprintf("%s ALL=(root) NOPASSWD: %s run-agent *\n", currentUser, linuxRootHelperPath)
}

func linuxCurrentUserName(configured string) string {
	if name := strings.TrimSpace(configured); name != "" {
		return name
	}
	if u, err := user.Current(); err == nil && strings.TrimSpace(u.Username) != "" {
		return u.Username
	}
	return strings.TrimSpace(os.Getenv("USER"))
}

func init() {
	registerLinuxSetupDiagnostics()
}

func registerLinuxSetupDiagnostics() {
	for _, spec := range linuxsetup.RepairSpecs() {
		info, ok := linuxSetupDiagnosticInfos[spec.Resource]
		if !ok {
			panic(fmt.Sprintf("missing Linux setup diagnostic metadata for %s", spec.Resource))
		}
		diagnosticResourceDefinitions[info.ResourceID] = diagnosticResourceDefinition{
			ID:           info.ResourceID,
			Owner:        info.Owner,
			DesiredState: info.DesiredState,
		}
		diagnosticFindingDefinitions[info.FindingID] = mustDiagnosticFinding(diagnosticFindingDefinition{
			ID:               info.FindingID,
			Resource:         info.ResourceID,
			Title:            info.Title,
			Repairability:    diagnosticRepairConsent,
			Action:           info.Action,
			RepairAction:     diagnosticRepairActionID(spec.ActionID),
			RepairReceipt:    diagnosticRepairReceiptID(spec.ReceiptID),
			Verification:     diagnosticVerificationID(spec.VerificationID),
			SecurityImpact:   info.SecurityImpact,
			RollbackBoundary: spec.RollbackBoundary,
			GroupKey:         "linux.setup",
		})
		diagnosticRepairActionDefinitions[diagnosticRepairActionID(spec.ActionID)] = diagnosticRepairActionDefinition{
			ID:               diagnosticRepairActionID(spec.ActionID),
			Repairability:    diagnosticRepairConsent,
			Authority:        info.Authority,
			Privileged:       info.Privileged,
			Reversibility:    info.Reversibility,
			Preconditions:    info.Preconditions,
			Receipt:          diagnosticRepairReceiptID(spec.ReceiptID),
			Verification:     diagnosticVerificationID(spec.VerificationID),
			RollbackBoundary: spec.RollbackBoundary,
			TestObligations:  info.TestObligations,
			ProofLanes: []diagnosticRepairProofLane{
				diagnosticRepairProofTLASetupRollback,
				diagnosticRepairProofUnitTests,
				diagnosticRepairProofDirtyStateConvergence,
				diagnosticRepairProofVerifyAfterAction,
			},
			ProofNotes: info.ProofNotes,
		}
		diagnosticHostRepairApplyHandlers[diagnosticRepairActionID(spec.ActionID)] = applyLinuxSetupRepair
		diagnosticHostRepairVerifyHandlers[diagnosticVerificationID(spec.VerificationID)] = verifyLinuxSetupRepair
	}
}

func applyLinuxSetupRepair(b *diagnosticHostRepairBackend, _ *Runner, action diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
	if err := requireLinuxDiagnosticHost(action.ID); err != nil {
		return err
	}
	return linuxsetup.RunRepairAction(string(action.ID), linuxDiagnosticSetupCallbacks(b, linuxDiagnosticSetupApply))
}

func verifyLinuxSetupRepair(b *diagnosticHostRepairBackend, action diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
	if err := requireLinuxDiagnosticHost(action.ID); err != nil {
		return err
	}
	return linuxsetup.VerifyRepairAction(string(action.Verification), linuxDiagnosticSetupCallbacks(b, linuxDiagnosticSetupVerify))
}

func requireLinuxDiagnosticHost(actionID diagnosticRepairActionID) error {
	if goos := linuxDiagnosticRuntimeOS(); goos != "linux" {
		return fmt.Errorf("%s requires Linux host diagnostics, got %s", actionID, goos)
	}
	return nil
}

var linuxSetupDiagnosticInfos = map[setup.Resource]linuxSetupDiagnosticInfo{
	setup.ResourceLinuxDistroProfile: {
		ResourceID:      "linux.setup.distro-profile",
		FindingID:       "linux.setup.distro-profile",
		Owner:           "linux.setup.profile",
		DesiredState:    "Linux distro, kernel, namespace, Landlock, seccomp, cgroup, service-manager, and helper-strategy facts are recorded as diagnostic evidence",
		Title:           "Refresh the Linux distro profile",
		Action:          "Refresh the Linux distro profile through Hazmat's modeled Linux setup backend, then verify the recorded facts before enabling agent-user setup.",
		SecurityImpact:  "Stale host capability facts can make Linux setup choose an unsupported helper or containment profile.",
		Authority:       diagnosticRepairAuthorityCurrentUser,
		Privileged:      false,
		Reversibility:   diagnosticRepairReversibleByReceipt,
		Preconditions:   []string{"Linux host facts are readable", "selected helper strategy is explicit", "profile path is managed by Hazmat"},
		TestObligations: []string{"Linux profile unit tests", "read-only diagnostic fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux profile repair is setup-owned evidence, not launch authority; MC_SetupRollback governs its position before account, cgroup, and sudoers setup.",
	},
	setup.ResourceLinuxAgentUser: {
		ResourceID:      "linux.setup.agent-user",
		FindingID:       "linux.setup.agent-user",
		Owner:           "linux.setup.identity",
		DesiredState:    "dedicated locked Linux agent account exists with expected home, shell, and ownership policy",
		Title:           "Restore the Linux agent account",
		Action:          "Restore the dedicated Linux agent account through Hazmat's modeled Linux setup backend, then verify identity state before privilege setup continues.",
		SecurityImpact:  "Missing or malformed agent identity removes the host-user isolation boundary for the Linux agent-user lane.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"caller approved account repair", "uid and home policy are selected", "sudoers privilege is not installed before containment resources verify"},
		TestObligations: []string{"Linux account graph tests", "dirty setup convergence fixture", "destructive rollback fixture"},
		ProofNotes:      "Linux agent-user repair is identity setup state; MC_SetupRollback proves privilege is added only after required containment resources.",
	},
	setup.ResourceLinuxSharedGroup: {
		ResourceID:      "linux.setup.shared-group",
		FindingID:       "linux.setup.shared-group",
		Owner:           "linux.setup.identity",
		DesiredState:    "controlled Linux shared group exists with only the memberships needed for workspace collaboration",
		Title:           "Restore the Linux shared group",
		Action:          "Restore the Linux shared group through Hazmat's modeled Linux setup backend, then verify host and agent membership.",
		SecurityImpact:  "Incorrect group state can either block collaboration or grant broader workspace access than the contract allows.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"caller approved group repair", "agent account policy is known", "workspace access repair is bounded to managed paths"},
		TestObligations: []string{"Linux shared-group graph tests", "membership convergence fixture", "destructive rollback fixture"},
		ProofNotes:      "Linux shared-group repair can outlive the agent user only as unprivileged residue; MC_SetupRollback owns that preservation boundary.",
	},
	setup.ResourceLinuxAgentHome: {
		ResourceID:      "linux.setup.agent-home",
		FindingID:       "linux.setup.agent-home",
		Owner:           "linux.setup.identity",
		DesiredState:    "Linux agent HOME and required XDG parents exist with agent ownership and restrictive modes",
		Title:           "Restore the Linux agent home",
		Action:          "Restore the Linux agent home through Hazmat's modeled Linux setup backend, then verify ownership and mode before launch.",
		SecurityImpact:  "Wrong home ownership can leak agent state or prevent credential and harness materialization.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"agent user exists or will be repaired first", "home path is within Hazmat's Linux setup boundary", "caller approved home repair"},
		TestObligations: []string{"Linux home graph tests", "ownership convergence fixture", "destructive rollback fixture"},
		ProofNotes:      "Linux agent-home repair is setup identity state and must be removed only under the explicit destructive home flag.",
	},
	setup.ResourceLinuxWorkspaceAccess: {
		ResourceID:      "linux.setup.workspace-access",
		FindingID:       "linux.setup.workspace-access",
		Owner:           "linux.setup.workspace",
		DesiredState:    "selected project workspace has only the Hazmat-managed traversal and ACL/group access required by the launch contract",
		Title:           "Restore Linux workspace access",
		Action:          "Restore Linux workspace access through Hazmat's modeled Linux setup backend, then verify the selected project path is reachable by the agent.",
		SecurityImpact:  "Broken workspace access blocks launches; overly broad access weakens host-user file isolation.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"project path is canonical", "agent account and shared group exist", "repair is scoped to the selected workspace"},
		TestObligations: []string{"Linux workspace graph tests", "bounded ACL fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux workspace repair is project-scoped setup state; MC_SetupRollback requires it to be removed before destructive identity deletion.",
	},
	setup.ResourceLinuxToolHome: {
		ResourceID:      "linux.setup.tool-home",
		FindingID:       "linux.setup.tool-home",
		Owner:           "linux.setup.identity",
		DesiredState:    "optional Linux agent tool/cache root is agent-owned and not writable by the host user unless explicitly modeled",
		Title:           "Restore the Linux tool home",
		Action:          "Restore the Linux tool home through Hazmat's modeled Linux setup backend, then verify ownership and mode.",
		SecurityImpact:  "Host-owned or broadly writable tool cache state can bypass the intended agent identity boundary.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"agent home exists", "tool-home path is managed by Hazmat", "caller approved tool-home repair"},
		TestObligations: []string{"Linux tool-home graph tests", "ownership convergence fixture", "destructive rollback fixture"},
		ProofNotes:      "Linux tool-home repair is preserved by default and deleted only under the explicit destructive tool-home flag modeled by MC_SetupRollback.",
	},
	setup.ResourceLinuxFirewallPolicy: {
		ResourceID:      "linux.setup.firewall-policy",
		FindingID:       "linux.setup.firewall-policy",
		Owner:           "linux.setup.network",
		DesiredState:    "Hazmat-owned Linux firewall policy root exists for supported egress modes without unmanaged policy takeover",
		Title:           "Restore the Linux firewall policy",
		Action:          "Restore the Linux firewall policy through Hazmat's modeled Linux setup backend, then verify managed policy ownership.",
		SecurityImpact:  "Missing firewall setup can leave requested Linux egress modes unenforced.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"caller approved network repair", "policy root is Hazmat-managed", "unsupported host policy conflicts are absent"},
		TestObligations: []string{"Linux firewall graph tests", "policy ownership fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux firewall repair is setup-owned network state and must be removed after sudoers/helper privilege is revoked.",
	},
	setup.ResourceLinuxResolverPolicy: {
		ResourceID:      "linux.setup.resolver-policy",
		FindingID:       "linux.setup.resolver-policy",
		Owner:           "linux.setup.network",
		DesiredState:    "Hazmat-owned Linux resolver policy root exists for supported DNS modes without unmanaged resolver takeover",
		Title:           "Restore the Linux resolver policy",
		Action:          "Restore the Linux resolver policy through Hazmat's modeled Linux setup backend, then verify managed resolver ownership.",
		SecurityImpact:  "Missing resolver setup can leave requested DNS restrictions unenforced.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"caller approved resolver repair", "resolver policy root is Hazmat-managed", "unsupported resolver conflicts are absent"},
		TestObligations: []string{"Linux resolver graph tests", "resolver ownership fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux resolver repair is setup-owned network state and must be removed after sudoers/helper privilege is revoked.",
	},
	setup.ResourceLinuxCgroupRoot: {
		ResourceID:      "linux.setup.cgroup-root",
		FindingID:       "linux.setup.cgroup-root",
		Owner:           "linux.setup.resources",
		DesiredState:    "Linux cgroup v2 subtree or delegation exists for the selected agent-user resource profile",
		Title:           "Restore the Linux cgroup root",
		Action:          "Restore the Linux cgroup root through Hazmat's modeled Linux setup backend, then verify cgroup v2 delegation before sudoers setup.",
		SecurityImpact:  "Missing cgroup setup prevents resource controls and weakens the agent-user runtime boundary.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"cgroup v2 is available", "service-manager strategy is supported", "caller approved cgroup repair"},
		TestObligations: []string{"Linux cgroup graph tests", "capability-gap fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux cgroup repair is a containment prerequisite for sudoers; MC_SetupRollback proves sudoers is created after it and removed before it.",
	},
	setup.ResourceLinuxLaunchHelper: {
		ResourceID:      "linux.setup.launch-helper",
		FindingID:       "linux.setup.launch-helper",
		Owner:           "linux.setup.privilege",
		DesiredState:    "Linux launch helper is installed at the fixed managed path with expected owner, mode, and digest",
		Title:           "Restore the Linux launch helper",
		Action:          "Restore the Linux launch helper through Hazmat's modeled Linux setup backend, then verify owner, mode, digest, and command boundary.",
		SecurityImpact:  "A stale or unmanaged helper can turn the agent-user lane into an unbounded privileged command surface.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"fixed helper path is selected", "caller approved helper repair", "helper digest is from the installed Hazmat binary"},
		TestObligations: []string{"Linux helper graph tests", "fixed-path digest fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux launch-helper repair is privileged setup state; MC_SetupRollback requires helper access to be disabled before weaker resources are removed.",
	},
	setup.ResourceLinuxSudoers: {
		ResourceID:      "linux.setup.sudoers",
		FindingID:       "linux.setup.sudoers",
		Owner:           "linux.setup.privilege",
		DesiredState:    "narrow Linux sudoers rule allows only the fixed Hazmat helper path after containment prerequisites verify",
		Title:           "Restore the Linux sudoers rule",
		Action:          "Restore the Linux sudoers rule through Hazmat's modeled Linux setup backend only after helper, cgroup, and containment prerequisites verify.",
		SecurityImpact:  "Broad or premature sudoers grants are the highest-risk Linux setup failure mode.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"helper verifies", "cgroup and containment resources verify", "caller approved sudoers repair"},
		TestObligations: []string{"Linux sudoers graph tests", "privilege-last regression", "verify-after-action fixture"},
		ProofNotes:      "Linux sudoers repair is the privilege boundary; MC_SetupRollback proves it is installed last and removed first.",
	},
}

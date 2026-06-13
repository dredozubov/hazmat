package hazmat

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kopia/kopia/fs/localfs"
	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/blob/filesystem"
	"github.com/kopia/kopia/snapshot"
	"github.com/kopia/kopia/snapshot/restore"
	"github.com/kopia/kopia/snapshot/snapshotfs"
	"github.com/kopia/kopia/snapshot/upload"

	"hazmat/internal/diagnostics"
)

func runTest(options diagnostics.CheckOptions) error {
	ui := &UI{Quick: options.Quick, JSON: options.JSON, DryRun: flagDryRun, YesAll: flagYesAll}
	ui.RepairExecution = diagnosticRepairExecutionRequest{
		Command:     options.Command,
		Fix:         options.Fix,
		YesAll:      flagYesAll,
		Interactive: ui.IsInteractive() && !ui.JSON,
		DryRun:      flagDryRun,
	}
	return diagnostics.RunCheck(options.Quick, diagnostics.CheckSuite{
		Begin: func(quick bool) (diagnostics.CheckContext, error) {
			if !ui.JSON {
				fmt.Println()
				cBold.Println("  ┌──────────────────────────────────────────────┐")
				cBold.Println("  │  Hazmat verification suite                   │")
				cBold.Println("  └──────────────────────────────────────────────┘")
				fmt.Println()
				fmt.Println("  Modes:")
				for _, line := range diagnosticModeGuidanceLines() {
					fmt.Println(line)
				}
				fmt.Println()
			}

			cu, err := user.Current()
			if err != nil {
				return diagnostics.CheckContext{}, fmt.Errorf("cannot determine current user: %w", err)
			}
			ui.RepairBackend = newDiagnosticHostRepairBackend(ui, cu.Username, cu.HomeDir)
			if !ui.JSON {
				fmt.Printf("  Running as: %s\n", cu.Username)
				fmt.Printf("  Agent user: %s\n", agentUser)
				if quick {
					cYellow.Println("  Quick mode: helper-backed, backup, and cloud live validation skipped")
				}
				fmt.Println()
			}

			selfPath, _ := os.Executable()
			return diagnostics.CheckContext{
				CurrentUser: cu.Username,
				SelfPath:    selfPath,
				AgentProbes: inspectAgentProbeGate(),
			}, nil
		},
		AgentUser:            func() { testAgentUser(ui) },
		DevGroupAndWorkspace: func(currentUser string) { testDevGroupAndWorkspace(ui, currentUser) },
		AgentProbesSkipped:   func(reason string) { testAgentProbesSkipped(ui, reason) },
		UserIsolation:        func(currentUser string) { testUserIsolation(ui, currentUser) },
		HardeningGaps:        func() { testHardeningGaps(ui) },
		PasswordlessSudo:     func() { testPasswordlessSudo(ui) },
		PFFirewallStatic:     func() { testPfFirewallStatic(ui) },
		PFFirewallLive:       func(quick bool, selfPath string) { testPfFirewallLive(ui, quick, selfPath) },
		DNSBlocklist:         func() { testDNSBlocklist(ui) },
		Persistence:          func() { testPersistence(ui) },
		CredentialInventory:  func() { testCredentialInventory(ui) },
		AgentTools:           func() { testAgentTools(ui) },
		CommandSurface:       func() { testCommandSurface(ui) },
		Seatbelt:             func() { testSeatbelt(ui) },
		ProjectToolchain:     func() { testProjectToolchain(ui) },
		LocalSnapshot:        func() { testLocalSnapshot(ui) },
		LocalSnapshotSkipped: func(reason string) { testLocalSnapshotSkipped(ui, reason) },
		CloudBackup:          func() { testCloudBackup(ui) },
		CloudBackupSkipped:   func(reason string) { testCloudBackupSkipped(ui, reason) },
		CloudRestore:         func() { testCloudRestore(ui) },
		CloudRestoreSkipped:  func(reason string) { testCloudRestoreSkipped(ui, reason) },
		Decommission:         func() { testDecommission(ui) },
		Finish:               ui.Summary,
		Exit:                 os.Exit,
	})
}

func diagnosticModeGuidanceLines() []string {
	return []string{
		"    hazmat check          Quick health report (no helper-backed probes, backup smokes, or external traffic)",
		"    hazmat check --full   Helper-backed, backup, and cloud live probes (sudo-adjacent)",
		"    hazmat doctor --fix   Apply approved executable typed repairs",
		"    hazmat doctor --dry-run",
		"                          Preview the typed repair plan",
	}
}

func inspectAgentProbeGate() diagnostics.AgentProbeGate {
	var blockers []string
	if _, err := user.Lookup(agentUser); err != nil {
		blockers = append(blockers, fmt.Sprintf("agent user %q is missing", agentUser))
	}
	if !launchSudoersInstalled() {
		blockers = append(blockers, fmt.Sprintf("launch-helper sudoers file is missing: %s", sudoersFile))
	}
	helperPath := launchHelperPath()
	if info, err := os.Stat(helperPath); err != nil {
		blockers = append(blockers, fmt.Sprintf("launch helper is missing: %s", helperPath))
	} else if info.Mode()&0o111 == 0 {
		blockers = append(blockers, fmt.Sprintf("launch helper is not executable: %s", helperPath))
	}
	if len(blockers) == 0 {
		return diagnostics.AllowAgentProbes()
	}
	return diagnostics.BlockAgentProbes(fmt.Sprintf(
		"%s. Skipping helper-backed probes so hazmat check stays read-only and non-prompting; run hazmat doctor --fix or preview with hazmat doctor --dry-run.",
		strings.Join(blockers, "; "),
	))
}

func testAgentProbesSkipped(ui *UI, reason string) {
	ui.Step("Agent-backed probes")
	ui.TestSkip(reason)
}

// ── Step 1: Agent user ────────────────────────────────────────────────────────

func testAgentUser(ui *UI) {
	ui.Step("Agent user")

	u, err := user.Lookup(agentUser)
	if err != nil {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupAgentUser),
			missingAgentUserRepairAdvice(),
			fmt.Sprintf("lookup error: %v", err),
		)
		return
	}
	ui.TestPass(fmt.Sprintf("User '%s' exists", agentUser))

	if u.Uid == agentUID {
		ui.TestPass(fmt.Sprintf("UID is %s", agentUID))
	} else {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupAgentUser),
			fmt.Sprintf("UID is '%s', expected %s", u.Uid, agentUID),
		)
	}

	if _, err := os.Stat(agentHome); err == nil {
		ui.TestPass(fmt.Sprintf("Home directory exists: %s", agentHome))
	} else {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupAgentHome),
			fmt.Sprintf("Home directory missing: %s", agentHome),
			fmt.Sprintf("stat error: %v", err),
		)
	}

	if info, err := os.Stat(agentHome); err == nil {
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			ownerUID := strconv.FormatUint(uint64(st.Uid), 10)
			if ownerUID == agentUID {
				ui.TestPass(fmt.Sprintf("Home directory owned by %s", agentUser))
			} else {
				ui.TestFailFinding(
					diagnosticFinding(findingSetupAgentHome),
					fmt.Sprintf("Home directory owned by uid=%s, expected %s", ownerUID, agentUID),
				)
			}
		}
	}

	if out, err := commandStdout(hostDsclPath, ".", "-read", "/Users/"+agentUser, "IsHidden"); err == nil {
		if strings.Contains(out, "1") {
			ui.TestPass("User is hidden from login screen")
		} else {
			ui.TestWarnFinding(
				diagnosticFinding(findingSetupAgentUser),
				fmt.Sprintf("User is NOT hidden from login screen (IsHidden=%s)", strings.TrimSpace(out)),
			)
		}
	}
}

func missingAgentUserRepairAdvice() string {
	return fmt.Sprintf("User '%s' does not exist — baseline setup is missing; preview setup repairs with hazmat doctor --dry-run or apply approved executable repairs with hazmat doctor --fix", agentUser)
}

// ── Step 2: Dev group and home traverse ──────────────────────────────────────

type checkWorkspaceReadiness struct {
	ProjectDir     string
	NeedsACLRepair bool
}

func inspectCheckWorkspaceReadiness(projectDir string) (checkWorkspaceReadiness, error) {
	info, err := os.Stat(projectDir)
	if err != nil {
		return checkWorkspaceReadiness{}, fmt.Errorf("inspect project workspace %s: %w", projectDir, err)
	}
	if !info.IsDir() {
		return checkWorkspaceReadiness{}, fmt.Errorf("project workspace is not a directory: %s", projectDir)
	}
	return checkWorkspaceReadiness{
		ProjectDir:     projectDir,
		NeedsACLRepair: projectNeedsACLRepair(projectDir),
	}, nil
}

func testDevGroupAndWorkspace(ui *UI, currentUser string) {
	ui.Step("Dev group and home traverse")

	if _, err := user.LookupGroup(sharedGroup); err == nil {
		ui.TestPass(fmt.Sprintf("Group '%s' exists", sharedGroup))
	} else {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupDevGroup),
			fmt.Sprintf("Group '%s' does not exist", sharedGroup),
			fmt.Sprintf("lookup error: %v", err),
		)
	}

	for _, u := range []string{currentUser, agentUser} {
		if ok, _ := groupMembershipContains(sharedGroup, u); ok {
			ui.TestPass(fmt.Sprintf("%s is a member of '%s'", u, sharedGroup))
		} else {
			ui.TestFailFinding(
				diagnosticFinding(findingSetupDevGroup),
				fmt.Sprintf("%s is NOT a member of '%s'", u, sharedGroup),
			)
		}
	}

	if homeAllowsAgentTraverse(os.Getenv("HOME")) {
		ui.TestPass(fmt.Sprintf("Home directory ACL lets '%s' traverse to project directories", agentUser))
	} else {
		ui.TestWarnFinding(
			diagnosticFinding(findingSetupHomeTraverse),
			fmt.Sprintf("Home directory access for '%s' not detected — project directories may be unreachable", agentUser),
			fmt.Sprintf("home: %s", os.Getenv("HOME")),
		)
	}

	projectDir, err := os.Getwd()
	if err != nil {
		ui.TestFailFinding(
			diagnosticFinding(findingWorkspaceAccess),
			fmt.Sprintf("could not resolve current project workspace: %v", err),
		)
		return
	}

	readiness, err := inspectCheckWorkspaceReadiness(projectDir)
	if err != nil {
		ui.TestFailFinding(
			diagnosticFinding(findingWorkspaceAccess),
			err.Error(),
			fmt.Sprintf("workspace: %s", projectDir),
		)
		return
	}
	if readiness.NeedsACLRepair {
		ui.TestWarnFinding(
			diagnosticFinding(findingWorkspaceAccess),
			"Project workspace is missing the inheritable dev-group ACL required for host/agent collaboration",
			fmt.Sprintf("workspace: %s", readiness.ProjectDir),
		)
	} else {
		ui.TestPass("Project workspace ACL is ready for host/agent collaboration")
	}
}

// ── Step 3: User isolation ────────────────────────────────────────────────────

func testUserIsolation(ui *UI, currentUser string) {
	ui.Step("User isolation")

	sensitiveDirs := []string{
		os.Getenv("HOME") + "/.ssh",
		os.Getenv("HOME") + "/.aws",
		os.Getenv("HOME") + "/.gnupg",
		os.Getenv("HOME") + "/.config/gh",
		os.Getenv("HOME") + "/Library",
	}

	for _, dir := range sensitiveDirs {
		name := dir[strings.LastIndex(dir, "/")+1:]
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			ui.TestSkip(fmt.Sprintf("%s doesn't exist on this system", name))
			continue
		}
		// asAgentQuiet("ls", dir): exit 0 = agent can read = isolation breach
		if err := asAgentQuiet("ls", dir); err == nil {
			ui.TestFail(fmt.Sprintf("ISOLATION BREACH: %s can read %s", agentUser, dir))
		} else {
			ui.TestPass(fmt.Sprintf("%s cannot read %s", agentUser, dir))
		}
	}

	// Check if current user can read files inside agent's home
	if f, err := os.Open(agentHome + "/.zshrc"); err == nil {
		f.Close()
		ui.TestWarnFinding(
			diagnosticFinding(findingAgentHomeReadable),
			fmt.Sprintf("%s can read %s's .zshrc; agent home privacy needs a modeled repair boundary", currentUser, agentUser),
		)
	} else {
		ui.TestPass(fmt.Sprintf("%s cannot read files inside %s's home", currentUser, agentUser))
	}
}

// ── Step 4: Hardening gaps ────────────────────────────────────────────────────

func testHardeningGaps(ui *UI) {
	ui.Step("Hardening gaps")

	dockerSock := os.Getenv("HOME") + "/.docker/run/docker.sock"
	if info, err := os.Stat(dockerSock); err == nil && info.Mode()&os.ModeSocket != 0 {
		if info.Mode().Perm() == 0o700 {
			ui.TestPass("Docker socket restricted to owner only (700)")
		} else {
			ui.TestFailFinding(
				diagnosticFinding(findingDockerSocketPermissions),
				fmt.Sprintf("Docker socket permissions: %04o (expected 700 — agent could escape via Docker)", info.Mode().Perm()),
			)
		}
	} else {
		ui.TestSkip("Docker socket not present")
	}

	if out, _ := asAgentOutput("cat", agentHome+"/.zshrc"); strings.Contains(out, "umask 007") {
		ui.TestPass("umask 007 set in agent's .zshrc")
	} else {
		ui.TestWarnFinding(
			diagnosticFinding(findingAgentUmask),
			"umask 007 not found in agent's .zshrc — new files will have permissive defaults",
		)
	}
}

// ── Step 5: Passwordless sudo ─────────────────────────────────────────────────

func testPasswordlessSudo(ui *UI) {
	ui.Step("Passwordless sudoers")

	if launchSudoersInstalled() {
		ui.TestPass(fmt.Sprintf("Launch-helper sudoers file exists: %s", sudoersFile))
		helperPath := launchHelperPath()
		if info, err := os.Stat(helperPath); err == nil && info.Mode()&0o111 != 0 {
			ui.TestPass(fmt.Sprintf("Hazmat helper is installed and executable: %s", helperPath))
		} else {
			ui.TestFailFinding(
				diagnosticFinding(findingSetupSudoers),
				fmt.Sprintf("Hazmat helper missing or not executable: %s", helperPath),
			)
		}
	} else {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupSudoers),
			fmt.Sprintf("Launch-helper sudoers file missing: %s", sudoersFile),
		)
	}

	if agentMaintenanceSudoersInstalled() {
		ui.TestPass(fmt.Sprintf("Optional agent-maintenance sudoers file exists: %s", agentMaintenanceSudoersFile))
		ui.TestSkip("Optional agent-maintenance sudoers runtime execution is not probed by read-only check")
	} else {
		ui.TestSkip("Optional agent-maintenance passwordless sudo is disabled")
	}
}

// ── Step 6: pf firewall (static) ─────────────────────────────────────────────

func testPfFirewallStatic(ui *UI) {
	ui.Step("pf firewall (static)")

	if enabled, observed := pfRuntimeEnabledUnprivileged(); observed {
		if enabled {
			ui.TestPass("pf is enabled")
		} else {
			ui.TestFailFinding(diagnosticFinding(findingPFFirewall), "pf is not enabled")
		}
	} else {
		ui.TestSkip("pf runtime status is not available to read-only check without privileged inspection")
	}

	if _, err := os.Stat(pfAnchorFile); err == nil {
		ui.TestPass(fmt.Sprintf("pf anchor file exists: %s", pfAnchorFile))
	} else {
		ui.TestFailFinding(diagnosticFinding(findingPFFirewall), fmt.Sprintf("pf anchor file missing: %s", pfAnchorFile))
	}

	rules, err := readPfAnchorFileRules()
	if err == nil && strings.Contains(rules, "block") {
		n := len(strings.Split(strings.TrimSpace(rules), "\n"))
		ui.TestPass(fmt.Sprintf("pf anchor file contains %d rules", n))
	} else {
		ui.TestFailFinding(diagnosticFinding(findingPFFirewall), fmt.Sprintf("pf anchor '%s' file has no block rules", pfAnchorName))
	}

	for _, p := range []struct{ port, label string }{
		{"25", "SMTP"}, {"6667", "IRC"}, {"21", "FTP"}, {"9050", "Tor"},
	} {
		if portInAnchor(rules, p.port) {
			ui.TestPass(fmt.Sprintf("pf anchor file blocks port %s (%s)", p.port, p.label))
		} else {
			ui.TestWarnFinding(
				diagnosticFinding(findingPFFirewall),
				fmt.Sprintf("pf anchor file may not block port %s (%s)", p.port, p.label),
			)
		}
	}
}

// ── Step 7: pf firewall (live) ────────────────────────────────────────────────

func testPfFirewallLive(ui *UI, quick bool, selfPath string) {
	ui.Step("pf firewall (live — as agent user)")

	if quick {
		ui.TestSkip("Live network tests (--quick mode)")
		return
	}
	if _, err := user.Lookup(agentUser); err != nil {
		ui.TestSkip("Agent user doesn't exist — can't run as-agent network tests")
		return
	}

	probes := []struct {
		host, port, label string
		wantAllow         bool
	}{
		{"1.1.1.1", "443", "HTTPS", true},
		{"1.1.1.1", "25", "SMTP", false},
		{"1.1.1.1", "6667", "IRC", false},
		{"127.0.0.1", "9050", "Tor", false},
	}

	for _, p := range probes {
		if !ui.JSON {
			fmt.Printf("    Testing %s (port %s, should be %s)...\n",
				p.label, p.port, map[bool]string{true: "ALLOWED", false: "BLOCKED"}[p.wantAllow])
		}

		got := agentTCPConnect(selfPath, p.host, p.port)
		switch {
		case p.wantAllow && got:
			ui.TestPass(fmt.Sprintf("%s can connect on port %s (%s allowed)", agentUser, p.port, p.label))
		case p.wantAllow && !got:
			ui.TestWarn(fmt.Sprintf("%s could not connect on port %s — network may be down, or pf is too restrictive",
				agentUser, p.port))
		case !p.wantAllow && !got:
			ui.TestPass(fmt.Sprintf("Port %s (%s) is BLOCKED for %s", p.port, p.label, agentUser))
		case !p.wantAllow && got:
			ui.TestFailFinding(
				diagnosticFinding(findingPFFirewall),
				fmt.Sprintf("BLOCK FAILURE: %s connected to port %s (%s not blocked)", agentUser, p.port, p.label),
			)
		}
	}

	// Verify rules are scoped to agent only — current user should still reach 443
	if !ui.JSON {
		fmt.Println("    Testing that pf rules are scoped to agent only...")
	}
	conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 3*time.Second)
	if err != nil {
		ui.TestWarn("Current user cannot connect on port 443 either — general network issue, not a sandbox problem")
	} else {
		conn.Close()
		ui.TestPass(fmt.Sprintf("pf rules are scoped: current user can reach port 443 (rules only restrict %s)", agentUser))
	}
}

// ── Step 8: DNS blocklist ─────────────────────────────────────────────────────

func testDNSBlocklist(ui *UI) {
	ui.Step("DNS blocklist")

	hosts, err := readDNSBlocklistHosts()
	if err != nil || !strings.Contains(string(hosts), "AI Agent Blocklist") {
		ui.TestFailFinding(
			diagnosticFinding(findingDNSBlocklist),
			"DNS blocklist not found in /etc/hosts — Hazmat-managed DNS blocklist is absent",
		)
		return
	}
	n := strings.Count(string(hosts), "0.0.0.0 ")
	ui.TestPass(fmt.Sprintf("DNS blocklist present in /etc/hosts (%d entries)", n))

	if ui.Quick {
		ui.TestSkip("Live DNS resolver probes skipped in quick mode")
		return
	}

	for _, domain := range dnsBlocklistProbeDomains {
		if dnsBlocklistDomainBlocked(domain) {
			ui.TestPass(fmt.Sprintf("%s is blocked (resolves to 0.0.0.0 or fails)", domain))
		} else {
			ui.TestFailFinding(
				diagnosticFinding(findingDNSBlocklist),
				fmt.Sprintf("%s resolved to a real IP — blocklist not working for this domain", domain),
			)
		}
	}
}

var dnsBlocklistProbeDomains = []string{"ngrok.io", "pastebin.com", "webhook.site", "transfer.sh"}

var readDNSBlocklistHosts = func() ([]byte, error) {
	return os.ReadFile("/etc/hosts")
}

var dnsBlocklistDomainBlocked = checkBlockedDomain

// ── Step 9: Persistence ───────────────────────────────────────────────────────

func testPersistence(ui *UI) {
	ui.Step("Persistence across reboots")

	if _, err := os.Stat(pfDaemonPlist); err == nil {
		ui.TestPass(fmt.Sprintf("LaunchDaemon plist exists: %s", pfDaemonPlist))
	} else {
		ui.TestFailFinding(
			diagnosticFinding(findingLaunchdPersistence),
			fmt.Sprintf("LaunchDaemon plist missing: %s — pf rules will not reload on reboot", pfDaemonPlist),
		)
	}

	ui.TestSkip(fmt.Sprintf("LaunchDaemon '%s' loaded state is not probed by read-only check", pfDaemonLabel))

	if data, err := os.ReadFile("/etc/pf.conf"); err == nil &&
		strings.Contains(string(data), `anchor "agent"`) {
		ui.TestPass(fmt.Sprintf("/etc/pf.conf references anchor '%s'", pfAnchorName))
	} else {
		ui.TestFailFinding(
			diagnosticFinding(findingLaunchdPersistence),
			fmt.Sprintf("/etc/pf.conf does not reference anchor '%s'", pfAnchorName),
		)
	}
}

// ── Credential inventory ─────────────────────────────────────────────────────

func testCredentialInventory(ui *UI) {
	ui.Step("Credential inventory")

	summary := summarizeCredentialRegistry(builtinCredentialDescriptors())
	ui.TestPass(fmt.Sprintf("Credential registry declares %d managed host secret-store entries", summary.ManagedHostSecretStore))
	if len(summary.ExternalBoundaries) > 0 {
		ui.TestPass(fmt.Sprintf("External credential boundaries: %s", strings.Join(summary.ExternalBoundaries, ", ")))
	}
	if len(summary.AdapterRequired) > 0 {
		ui.TestPass(fmt.Sprintf("Credential backend adapters still required: %s", strings.Join(summary.AdapterRequired, ", ")))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		ui.TestFail(fmt.Sprintf("Cannot determine home directory for credential inventory: %v", err))
		return
	}
	entries, err := inspectCredentialInventory(home)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Credential inventory failed: %v", err))
		return
	}
	inventorySummary := summarizeCredentialInventory(entries)
	ui.TestPass(fmt.Sprintf(
		"Credential inventory covers %d redacted surfaces (%d configured, %d not configured, %d external, %d adapter-required)",
		inventorySummary.Total,
		inventorySummary.Configured,
		inventorySummary.NotConfigured,
		inventorySummary.External,
		inventorySummary.AdapterRequired,
	))
	for _, entry := range entries {
		reportCredentialInventoryEntry(ui, entry)
	}
}

func reportCredentialInventoryEntry(ui *UI, entry credentialInventoryEntry) {
	line := formatCredentialInventoryEntry(entry)
	switch entry.Status() {
	case credentialInventoryConfigured, credentialInventoryExternal:
		ui.TestPass(line)
	case credentialInventoryNotConfigured:
		ui.TestSkip(line)
	case credentialInventoryAdapterRequired:
		ui.TestWarnFinding(diagnosticCredentialFinding(entry), line)
	case credentialInventoryNeedsRepair:
		details := append([]string{line}, credentialInventoryEntryDetails(entry)...)
		ui.TestWarnFinding(diagnosticCredentialFinding(entry), line, details...)
		if !ui.JSON {
			for _, detail := range details[1:] {
				cDim.Printf("    %s\n", detail)
			}
		}
	case credentialInventoryError:
		ui.TestFail(line)
		if !ui.JSON {
			for _, errText := range entry.Errors {
				cDim.Printf("    %s\n", errText)
			}
			for _, finding := range entry.AgentResidue {
				cDim.Printf("    %s\n", formatCredentialInventoryFinding("agent-home residue", finding))
			}
			for _, finding := range entry.LegacyResidue {
				cDim.Printf("    %s\n", formatCredentialInventoryFinding("legacy residue", finding))
			}
		}
	}
}

func credentialInventoryEntryDetails(entry credentialInventoryEntry) []string {
	var details []string
	for _, finding := range entry.AgentResidue {
		details = append(details, formatCredentialInventoryFinding("agent-home residue", finding))
	}
	for _, finding := range entry.LegacyResidue {
		details = append(details, formatCredentialInventoryFinding("legacy residue", finding))
	}
	return details
}

// ── Step 10: Agent user tools ─────────────────────────────────────────────────

func testAgentTools(ui *UI) {
	ui.Step("Agent user tools")

	if len(installedManagedHarnesses()) == 0 {
		ui.TestSkip(noManagedHarnessesInstalledMessage())
	} else {
		var installed []string
		for _, harness := range installedManagedHarnesses() {
			installed = append(installed, harness.Spec.DisplayName)
		}
		ui.TestPass(fmt.Sprintf("Installed harnesses: %s", strings.Join(installed, ", ")))
	}

	// Git identity
	name, _ := asAgentOutput("git", "config", "--global", "user.name")
	email, _ := asAgentOutput("git", "config", "--global", "user.email")
	if name != "" && email != "" {
		ui.TestPass(fmt.Sprintf("Git identity configured: %s <%s>", name, email))
	} else {
		ui.TestWarnFinding(
			diagnosticFinding(findingAgentGitIdentity),
			fmt.Sprintf("Git identity not fully configured for agent (name=%q, email=%q)", name, email),
		)
	}

	// SSH key
	if asAgentQuiet("test", "-f", agentHome+"/.ssh/id_ed25519.pub") == nil {
		ui.TestPass("SSH key exists (ed25519)")
	} else if asAgentQuiet("test", "-d", agentHome+"/.ssh") == nil {
		ui.TestWarnFinding(
			diagnosticFinding(findingAgentSSHKey),
			"~/.ssh exists but no id_ed25519.pub — optional Git SSH workflows may need a key",
		)
	} else {
		ui.TestWarnFinding(
			diagnosticFinding(findingAgentSSHKey),
			"No SSH key found for agent user — optional unless this project needs Git SSH access",
		)
	}

	if isManagedHarnessInstalled(HarnessClaude) {
		// Claude Code
		if path, ok := findInstalledClaudeBinary(); ok {
			ui.TestPass(fmt.Sprintf("Claude Code installed: %s", path))
		} else if _, out, _ := func() (bool, string, error) {
			out, err := asAgentOutput("bash", "-c", "command -v claude 2>/dev/null")
			return err == nil && out != "", out, err
		}(); out != "" {
			ui.TestPass(fmt.Sprintf("Claude Code is in agent's PATH: %s", out))
		} else {
			ui.TestWarn(fmt.Sprintf("Claude Code expected but not found for agent user — run 'hazmat harness update claude' or verify %s", claudeInstallerURL))
		}

		if value, source, err := lookupConfiguredAPIKey(harnessAPIKeyPrompts[0]); err == nil && value != "" {
			if source == configuredAPIKeySourceLegacy {
				ui.TestWarnFinding(
					diagnosticFinding(findingAnthropicAPIKey),
					fmt.Sprintf("ANTHROPIC_API_KEY still lives in %s; Hazmat credential repair can migrate it into ~/.hazmat/secrets when Claude API-key sessions are needed", agentZshrcPath),
				)
			} else {
				ui.TestPass("ANTHROPIC_API_KEY is stored in Hazmat's host-owned secret store")
			}
		} else {
			ui.TestWarnFinding(
				diagnosticFinding(findingAnthropicAPIKey),
				"ANTHROPIC_API_KEY not configured for Hazmat sessions — optional unless Claude API-key sessions are required",
			)
		}

		if asAgentQuiet("test", "-f", agentHome+"/.claude/settings.json") == nil {
			ui.TestPass("~/.claude/settings.json exists for agent user")
		} else {
			ui.TestWarn("No ~/.claude/settings.json for agent user — Claude deny rules are not configured")
		}

		claudeDir := agentHome + "/.claude"
		projectsDir := filepath.Join(claudeDir, "projects")
		if exists, err := agentPathIsDir(claudeDir); err != nil {
			ui.TestWarn(fmt.Sprintf("Could not verify Claude helper access to %s: %v", claudeDir, err))
		} else if exists {
			ui.TestPass(fmt.Sprintf("Claude state directory is helper-readable as agent: %s", claudeDir))
		} else {
			ui.TestSkip(fmt.Sprintf("%s does not exist for agent user yet", claudeDir))
		}

		if exists, err := agentPathIsDir(projectsDir); err != nil {
			ui.TestWarn(fmt.Sprintf("Could not verify Claude helper access to %s: %v", projectsDir, err))
		} else if !exists {
			ui.TestSkip(fmt.Sprintf("%s does not exist for agent user yet", projectsDir))
		} else if projects, err := agentReadDirNames(projectsDir); err != nil {
			ui.TestWarn(fmt.Sprintf("Could not list Claude session projects via helper: %v", err))
		} else {
			ui.TestPass(fmt.Sprintf("Claude export/resume session store is helper-backed (%d project dirs)", len(projects)))
		}
	} else {
		ui.TestSkip(managedHarnessNotInstalledMessage("Claude Code", HarnessClaude))
	}

	// OpenCode
	if path, ok := findInstalledOpenCodeBinary(); ok {
		ui.TestPass(fmt.Sprintf("OpenCode installed: %s", path))
	} else if _, out, _ := func() (bool, string, error) {
		out, err := asAgentOutput("bash", "-c", "command -v opencode 2>/dev/null")
		return err == nil && out != "", out, err
	}(); out != "" {
		ui.TestPass(fmt.Sprintf("OpenCode is in agent's PATH: %s", out))
	} else {
		ui.TestSkip(managedHarnessNotInstalledMessage("OpenCode", HarnessOpenCode))
	}

	// Codex
	if path, ok := findInstalledCodexBinary(); ok {
		ui.TestPass(fmt.Sprintf("Codex installed: %s", path))
	} else if _, out, _ := func() (bool, string, error) {
		out, err := asAgentOutput("bash", "-c", "command -v codex 2>/dev/null")
		return err == nil && out != "", out, err
	}(); out != "" {
		ui.TestPass(fmt.Sprintf("Codex is in agent's PATH: %s", out))
	} else {
		ui.TestSkip(managedHarnessNotInstalledMessage("Codex", HarnessCodex))
	}
}

func noManagedHarnessesInstalledMessage() string {
	return "No AI coding agent harness installed yet (optional; inspect choices with 'hazmat harness status' and install only needed harnesses with 'hazmat harness update <harness>')"
}

func managedHarnessNotInstalledMessage(displayName string, harness HarnessID) string {
	return fmt.Sprintf("%s not installed for agent user (optional; run 'hazmat harness update %s' only if this workflow needs it)", displayName, harness)
}

// ── Step 11: Command surface ─────────────────────────────────────────────────

func testCommandSurface(ui *UI) {
	ui.Step("Command surface")

	if asAgentQuiet("test", "-f", agentEnvPath) == nil {
		ui.TestPass(fmt.Sprintf("Agent env file exists: %s", agentEnvPath))
	} else {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupAgentEnv),
			fmt.Sprintf("Agent env file missing: %s", agentEnvPath),
		)
	}

	if out, _ := asAgentOutput("cat", agentHome+"/.zshrc"); strings.Contains(out, "agent-env.zsh") {
		ui.TestPass("Agent .zshrc sources the hazmat env file")
	} else {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupAgentEnv),
			"Agent .zshrc does not source the hazmat env file",
		)
	}

	for _, wrapper := range []string{hostClaudeWrapperName, hostExecWrapperName, hostShellWrapperName} {
		path := hostWrapperPath(wrapper)
		if err := validateHostWrapper(path); err != nil {
			ui.TestFailFinding(
				diagnosticFinding(findingSetupHostWrappers),
				fmt.Sprintf("Host wrapper drift: %v", err),
				path,
			)
		} else {
			ui.TestPass(fmt.Sprintf("Host wrapper is installed with an executable Hazmat target: %s", path))
		}
	}

	userZshrc := userZshrcPath()
	switch {
	case strings.Contains(os.Getenv("PATH"), hostWrapperDir()):
		ui.TestPass(fmt.Sprintf("Current shell PATH includes %s", hostWrapperDir()))
	case func() bool {
		data, err := os.ReadFile(userZshrc)
		return err == nil && strings.Contains(string(data), "/.local/bin")
	}():
		ui.TestPass(fmt.Sprintf("%s configures ~/.local/bin in PATH", userZshrc))
	default:
		ui.TestWarnFinding(
			diagnosticFinding(findingSetupHostWrappers),
			fmt.Sprintf("%s does not appear to expose ~/.local/bin yet — open a new shell after setup", userZshrc),
			userZshrc,
		)
	}
}

// ── Step 12: Seatbelt confinement ─────────────────────────────────────────────

func testSeatbelt(ui *UI) {
	ui.Step("Seatbelt confinement")

	if exists, err := agentPathExists(seatbeltWrapperPath); err != nil {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupSeatbeltWrapper),
			fmt.Sprintf("Could not inspect seatbelt wrapper as agent: %s — %v", seatbeltWrapperPath, err),
			seatbeltWrapperPath,
		)
	} else if !exists {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupSeatbeltWrapper),
			fmt.Sprintf("Seatbelt wrapper missing: %s — baseline setup is incomplete", seatbeltWrapperPath),
			seatbeltWrapperPath,
		)
	} else if executable, err := agentPathIsExecutable(seatbeltWrapperPath); err != nil {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupSeatbeltWrapper),
			fmt.Sprintf("Could not verify seatbelt wrapper executable bit as agent: %s — %v", seatbeltWrapperPath, err),
			seatbeltWrapperPath,
		)
	} else if !executable {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupSeatbeltWrapper),
			fmt.Sprintf("Seatbelt wrapper not executable: %s", seatbeltWrapperPath),
			seatbeltWrapperPath,
		)
	} else {
		ui.TestPass(fmt.Sprintf("Seatbelt wrapper is executable: %s", seatbeltWrapperPath))
	}

	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		ui.TestFail("sandbox-exec not found at /usr/bin/sandbox-exec")
		return
	}
	ui.TestPass("sandbox-exec available at /usr/bin/sandbox-exec")

	if _, err := user.Lookup(agentUser); err != nil {
		ui.TestSkip(fmt.Sprintf("Agent user '%s' not found — skipping confinement tests", agentUser))
		return
	}

	// Create isolated test directories. They are filesystem-writable by the
	// agent so failures below exercise the seatbelt policy rather than Unix
	// ownership of the caller's private TMPDIR.
	seatbeltFixtureRoot := os.Getenv("HOME")
	projectDir := fmt.Sprintf("%s/.seatbelt-project-%d", seatbeltFixtureRoot, os.Getpid())
	readDir := fmt.Sprintf("%s/.seatbelt-read-%d", seatbeltFixtureRoot, os.Getpid())
	if err := os.MkdirAll(projectDir, 0o777); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not create seatbelt project dir: %v", err))
		return
	}
	if err := os.Chmod(projectDir, 0o777); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not make seatbelt project dir agent-accessible: %v", err))
		return
	}
	if err := os.MkdirAll(readDir, 0o777); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not create seatbelt read dir: %v", err))
		return
	}
	if err := os.Chmod(readDir, 0o777); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not make seatbelt read dir agent-accessible: %v", err))
		return
	}
	defer os.RemoveAll(projectDir)
	defer os.RemoveAll(readDir)

	// Generate a per-session policy with the test dirs embedded as literals.
	cfg := sessionConfig{
		ProjectDir: projectDir,
		ReadDirs:   []string{readDir},
	}
	policy, err := buildNativeSessionPolicy(cfg)
	if err != nil {
		ui.TestWarn(fmt.Sprintf("Could not build test seatbelt policy: %v", err))
		return
	}
	policyContent, err := compileDarwinSBPLChecked(policy)
	if err != nil {
		ui.TestWarn(fmt.Sprintf("Could not compile test seatbelt policy: %v", err))
		return
	}
	if strings.Contains(policyContent, `(allow file-read* file-write* (subpath "`+agentHome+`"))`) ||
		strings.Contains(policyContent, `(allow process-exec (subpath "`+agentHome+`"))`) {
		ui.TestFail("CONFINEMENT BREACH: Seatbelt policy contains a blanket agent-home allow")
	} else {
		ui.TestPass("Seatbelt omits blanket agent-home allow")
	}
	policyFile := fmt.Sprintf("/private/tmp/hazmat-%d.sb", os.Getpid())
	if err := os.WriteFile(policyFile, []byte(policyContent), 0o644); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not write test seatbelt policy: %v", err))
		return
	}
	defer os.Remove(policyFile)

	runSandboxed := func(args ...string) error {
		all := []string{"/usr/bin/sandbox-exec", "-f", policyFile}
		all = append(all, args...)
		return asAgentQuiet(all...)
	}

	// Allowed: write inside the active project directory.
	testWritePath := fmt.Sprintf("%s/.seatbelt-write-%d", projectDir, os.Getpid())
	if err := runSandboxed("/usr/bin/touch", testWritePath); err == nil {
		asAgentQuiet("/bin/rm", "-f", testWritePath) //nolint:errcheck
		ui.TestPass("Seatbelt allows writes inside PROJECT_DIR")
	} else {
		ui.TestFail(fmt.Sprintf("Seatbelt unexpectedly denied write inside PROJECT_DIR: %v", err))
	}

	// Denied: write to a read-only directory.
	testReadWritePath := fmt.Sprintf("%s/.seatbelt-read-write-%d", readDir, os.Getpid())
	if err := runSandboxed("/usr/bin/touch", testReadWritePath); err != nil {
		ui.TestPass("Seatbelt denies writes to read-only directories")
	} else {
		asAgentQuiet("/bin/rm", "-f", testReadWritePath) //nolint:errcheck
		ui.TestFail("CONFINEMENT BREACH: Seatbelt allowed write to a read-only directory")
	}

	// Denied: read/write inside credential subdirs. The current policy allows
	// explicit agent HOME runtime state, but credential directories remain
	// deny zones.
	credentialProbeDir := agentHome + "/.aws"
	createdCredentialProbeDir := false
	credentialProbeReady := true
	if isLink, err := agentPathIsSymlink(credentialProbeDir); err == nil && isLink {
		ui.TestWarn(fmt.Sprintf("Credential deny probe dir is a symlink; skipping credential read/write probe: %s", credentialProbeDir))
		credentialProbeReady = false
	} else if exists, err := agentPathExists(credentialProbeDir); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not inspect credential deny probe dir: %v", err))
		credentialProbeReady = false
	} else if !exists {
		if err := agentEnsureDir(credentialProbeDir, 0o700); err != nil {
			ui.TestWarn(fmt.Sprintf("Could not create credential deny probe dir: %v", err))
			credentialProbeReady = false
		} else {
			createdCredentialProbeDir = true
			defer asAgentQuiet("/bin/rmdir", credentialProbeDir) //nolint:errcheck
		}
	}
	if credentialProbeReady {
		credentialProbePath := fmt.Sprintf("%s/hazmat-seatbelt-probe-%d", credentialProbeDir, os.Getpid())
		if err := asAgentQuiet("/usr/bin/install", "-m", "600", "/dev/null", credentialProbePath); err == nil {
			defer asAgentQuiet("/bin/rm", "-f", credentialProbePath) //nolint:errcheck
			if err := runSandboxed("/bin/cat", credentialProbePath); err != nil {
				ui.TestPass("Seatbelt denies reads inside credential directories")
			} else {
				ui.TestFail("CONFINEMENT BREACH: Seatbelt allowed read inside credential directory")
			}
		} else if !createdCredentialProbeDir {
			ui.TestWarn(fmt.Sprintf("Could not create credential read probe: %v", err))
		}
		credentialWritePath := fmt.Sprintf("%s/hazmat-seatbelt-write-%d", credentialProbeDir, os.Getpid())
		if err := runSandboxed("/usr/bin/touch", credentialWritePath); err != nil {
			ui.TestPass("Seatbelt denies writes inside credential directories")
		} else {
			asAgentQuiet("/bin/rm", "-f", credentialWritePath) //nolint:errcheck
			ui.TestFail("CONFINEMENT BREACH: Seatbelt allowed write inside credential directory")
		}
	}

	// Allowed: read from a directory passed as a read-only dir.
	probeReadPath := fmt.Sprintf("%s/.seatbelt-readprobe-%d", readDir, os.Getpid())
	if f, err := os.Create(probeReadPath); err == nil {
		f.Close()
		defer os.Remove(probeReadPath)
		if err := runSandboxed("/bin/cat", probeReadPath); err == nil {
			ui.TestPass("Seatbelt allows reads inside read-only directories")
		} else {
			ui.TestFail(fmt.Sprintf("Seatbelt unexpectedly denied read inside read-only directory: %v", err))
		}
	} else {
		ui.TestWarn(fmt.Sprintf("Could not create read probe in read-only directory: %v", err))
	}

	// Allowed: read ~/.claude (durable Claude state must be accessible).
	claudeDir := agentHome + "/.claude"
	if _, err := os.Stat(claudeDir); err == nil {
		if err := runSandboxed("/bin/ls", claudeDir); err == nil {
			ui.TestPass("Seatbelt allows reads inside ~/.claude (durable Claude state accessible)")
		} else {
			ui.TestFail("Seatbelt denies reads of ~/.claude — durable Claude state will fail under confinement")
		}
	} else {
		ui.TestSkip("~/.claude does not exist for agent — skipping Claude auth read test")
	}
}

// ── Step 12b: Project toolchain ──────────────────────────────────────────────

func testProjectToolchain(ui *UI) {
	ui.Step("Project development toolchain")

	projectDir, err := os.Getwd()
	if err != nil {
		ui.TestSkip("Cannot determine working directory")
		return
	}

	integrations, err := resolveActiveIntegrations(nil, projectDir)
	if err != nil {
		ui.TestSkip(fmt.Sprintf("Cannot resolve integrations: %v", err))
		return
	}

	if len(integrations) == 0 {
		ui.TestSkip("No integrations detected for project directory")
		return
	}

	names := make([]string, len(integrations))
	for i, spec := range integrations {
		names[i] = spec.Meta.Name
	}
	ui.TestPass(fmt.Sprintf("Active integrations: %s", strings.Join(names, ", ")))

	// Run integration resolution to inspect toolchain paths. Check mode is
	// read-only, so any returned session mutations are only reported here.
	resolved, _, err := resolveRuntimeIntegrations(projectDir, integrations)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Integration resolution failed: %v", err))
		return
	}

	for _, r := range resolved {
		for _, detail := range r.Details {
			if strings.Contains(detail, "cannot execute") || strings.Contains(detail, "not executable") {
				ui.TestWarnFinding(diagnosticFinding(findingAgentToolPath), detail)
			}
		}
		if len(r.AdditionalReadDirs) > 0 {
			ui.TestPass(fmt.Sprintf("%s: resolved toolchain at %s", r.Spec.Meta.Name, strings.Join(r.AdditionalReadDirs, ", ")))
		} else if r.Source == "" {
			ui.TestWarnFinding(
				diagnosticFinding(findingIntegrationToolchain),
				fmt.Sprintf("%s: no toolchain path resolved — agent may not be able to use this tool", r.Spec.Meta.Name),
			)
		}
	}

	// Check specific tools by running them as agent to verify end-to-end access.
	if _, err := user.Lookup(agentUser); err != nil {
		ui.TestSkip("Agent user not found — skipping agent-level tool checks")
		return
	}

	for _, spec := range integrations {
		for cmdName, cmdLine := range spec.Commands {
			tool := strings.Fields(cmdLine)[0]
			if out, err := asAgentOutput("bash", "-c", "command -v "+tool+" 2>/dev/null"); err == nil && out != "" {
				ui.TestPass(fmt.Sprintf("%s/%s: %s found at %s", spec.Meta.Name, cmdName, tool, out))
			} else {
				ui.TestFailFinding(
					diagnosticFinding(findingAgentToolPath),
					fmt.Sprintf("%s/%s: %s not found in agent PATH", spec.Meta.Name, cmdName, tool),
				)
			}
		}
	}

	// Check golangci-lint if a Go project (Makefile lint target).
	for _, spec := range integrations {
		if spec.Meta.Name != "go" {
			continue
		}
		if out, err := asAgentOutput("bash", "-c", "command -v golangci-lint 2>/dev/null"); err == nil && out != "" {
			ui.TestPass(fmt.Sprintf("golangci-lint: found at %s", out))
		} else {
			// Try a read-only Homebrew opt/ lookup before reporting drift.
			lintPath := resolveHomebrewToolForAgent("golangci-lint")
			if lintPath != "" {
				ui.TestPass(fmt.Sprintf("golangci-lint: found via Homebrew and executable by agent at %s", lintPath))
			} else {
				ui.TestWarnFinding(
					diagnosticFinding(findingGolangCILintAccess),
					"golangci-lint: not accessible by agent (doctor can plan Homebrew permission repair or installation)",
				)
			}
		}
	}

	// Check CGO compilation prerequisites.
	for _, spec := range integrations {
		if spec.Meta.Name != "go" {
			continue
		}
		for _, key := range spec.Session.EnvPassthrough {
			if key != "CGO_ENABLED" {
				continue
			}
			sdkPath := "/Library/Developer/CommandLineTools/SDKs"
			if _, err := os.Stat(sdkPath); err == nil {
				ui.TestPass(fmt.Sprintf("CGO SDK: Command Line Tools found at %s", sdkPath))
			} else {
				ui.TestFail("CGO SDK: /Library/Developer/CommandLineTools not found — install Xcode Command Line Tools: xcode-select --install")
			}
		}
	}

	// Check tla2tools.jar accessibility for TLA+ integration.
	for _, spec := range integrations {
		if spec.Meta.Name != "tla-java" {
			continue
		}
		jarPath := os.Getenv("TLA2TOOLS_JAR")
		if jarPath == "" {
			home, _ := os.UserHomeDir()
			jarPath = filepath.Join(home, "workspace", "tla2tools.jar")
		}
		if _, err := os.Stat(jarPath); err == nil {
			ui.TestPass(fmt.Sprintf("tla2tools.jar: found at %s", jarPath))
		} else {
			ui.TestWarnFinding(
				diagnosticFinding(findingTLA2ToolsJar),
				fmt.Sprintf("tla2tools.jar: not found at %s — set TLA2TOOLS_JAR env var", jarPath),
			)
		}
	}
}

// resolveHomebrewToolForAgent finds a Homebrew opt/ tool that is already
// executable by the agent. It never repairs permissions; check mode is read-only.
func resolveHomebrewToolForAgent(tool string) string {
	return resolveHomebrewToolForAgentInPrefixes(tool, []string{"/opt/homebrew", "/usr/local"}, pathExecutableByAgent)
}

func resolveHomebrewToolForAgentInPrefixes(tool string, prefixes []string, executableByAgent func(string) bool) string {
	if tool == "" || strings.ContainsRune(tool, os.PathSeparator) || executableByAgent == nil {
		return ""
	}
	for _, prefix := range prefixes {
		optPath := filepath.Join(prefix, "opt", tool)
		resolved, err := filepath.EvalSymlinks(optPath)
		if err != nil {
			continue
		}
		binPath := filepath.Join(resolved, "bin", tool)
		if _, err := os.Stat(binPath); err != nil {
			continue
		}
		if executableByAgent(binPath) {
			return binPath
		}
	}
	return ""
}

// ── Step 13: Local Snapshot ──────────────────────────────────────────────────

func testLocalSnapshot(ui *UI) {
	ui.Step("Local Snapshot (Kopia)")

	// Test with a throwaway repo to avoid touching the real one.
	tmpRepoDir := fmt.Sprintf("/tmp/haztest-local-repo-%d", os.Getpid())
	tmpConfigFile := tmpRepoDir + "/repo.config"
	tmpSourceDir := fmt.Sprintf("/tmp/haztest-local-src-%d", os.Getpid())
	defer os.RemoveAll(tmpRepoDir)
	defer os.RemoveAll(tmpSourceDir)

	if err := os.MkdirAll(tmpRepoDir, 0o700); err != nil {
		ui.TestFail(fmt.Sprintf("could not create temp repo dir: %v", err))
		return
	}
	if err := os.MkdirAll(tmpSourceDir, 0o700); err != nil {
		ui.TestFail(fmt.Sprintf("could not create temp source dir: %v", err))
		return
	}

	// Write fixture files.
	os.WriteFile(filepath.Join(tmpSourceDir, "main.go"), []byte("package main\n"), 0o644)
	os.MkdirAll(filepath.Join(tmpSourceDir, "pkg"), 0o755)
	os.WriteFile(filepath.Join(tmpSourceDir, "pkg/lib.go"), []byte("package pkg\n"), 0o644)

	// 1. Initialize repo
	ctx := context.Background()
	st, err := filesystem.New(ctx, &filesystem.Options{Path: tmpRepoDir}, false)
	if err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: could not create storage: %v", err))
		return
	}
	if err := repo.Initialize(ctx, st, &repo.NewRepositoryOptions{}, "test-pass"); err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: could not initialize repo: %v", err))
		return
	}
	if err := repo.Connect(ctx, tmpConfigFile, st, "test-pass", &repo.ConnectOptions{}); err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: could not connect: %v", err))
		return
	}
	ui.TestPass("local snapshot: repository initialization successful")

	// 2. First snapshot
	r, err := repo.Open(ctx, tmpConfigFile, "test-pass", &repo.Options{})
	if err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: could not open repo: %v", err))
		return
	}
	defer r.Close(ctx)
	directRepo, err := requireDirectKopiaRepository(r)
	if err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: repository does not support direct writes: %v", err))
		return
	}

	if err := snapshotDir(ctx, directRepo, tmpSourceDir, "pre-session (test)"); err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: first snapshot failed: %v", err))
		return
	}
	ui.TestPass("local snapshot: first snapshot successful")

	// 3. Modify source, take incremental snapshot
	os.WriteFile(filepath.Join(tmpSourceDir, "new.go"), []byte("package main\n"), 0o644)
	if err := snapshotDir(ctx, directRepo, tmpSourceDir, "pre-session (test-2)"); err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: incremental snapshot failed: %v", err))
		return
	}
	ui.TestPass("local snapshot: incremental snapshot successful")

	// 4. List snapshots
	snaps, err := listSnapshots(ctx, r, tmpSourceDir)
	if err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: could not list snapshots: %v", err))
		return
	}
	if len(snaps) == 2 {
		ui.TestPass(fmt.Sprintf("local snapshot: snapshot count correct (%d)", len(snaps)))
	} else {
		ui.TestFail(fmt.Sprintf("local snapshot: expected 2 snapshots, got %d", len(snaps)))
		if len(snaps) == 0 {
			return
		}
	}

	// 5. Restore first snapshot (before modification) and verify
	first := snaps[0]
	restoreDir := fmt.Sprintf("/tmp/haztest-local-restore-%d", os.Getpid())
	defer os.RemoveAll(restoreDir)

	stats, err := restoreSnapshotTo(ctx, r, first, restoreDir)
	if err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: restore failed: %v", err))
		return
	}
	if stats.RestoredFileCount == 2 {
		ui.TestPass(fmt.Sprintf("local snapshot: restored %d files from first snapshot", stats.RestoredFileCount))
	} else {
		ui.TestWarn(fmt.Sprintf("local snapshot: restore reported %d files from first snapshot; verifying restored content directly", stats.RestoredFileCount))
	}

	// new.go should NOT exist in first snapshot
	if _, err := os.Stat(filepath.Join(restoreDir, "new.go")); os.IsNotExist(err) {
		ui.TestPass("local snapshot: first snapshot correctly does not contain new.go")
	} else {
		ui.TestFail("local snapshot: first snapshot unexpectedly contains new.go")
	}

	// main.go and pkg/lib.go should exist with correct content
	if data, err := os.ReadFile(filepath.Join(restoreDir, "main.go")); err == nil && string(data) == "package main\n" {
		if data, err := os.ReadFile(filepath.Join(restoreDir, "pkg", "lib.go")); err == nil && string(data) == "package pkg\n" {
			ui.TestPass("local snapshot: round-trip content verification passed")
		} else {
			ui.TestFail("local snapshot: nested file content mismatch after restore")
		}
	} else {
		ui.TestFail("local snapshot: content mismatch after restore")
	}

	// 6. Check that real local repo exists if hazmat init was run
	if _, err := os.Stat(localConfigFile); err == nil {
		ui.TestPass(fmt.Sprintf("local snapshot repo configured at %s", localRepoDir))
	} else {
		ui.TestWarn(fmt.Sprintf("local snapshot repo not found at %s — local backup setup is absent", localRepoDir))
	}
}

func testLocalSnapshotSkipped(ui *UI, reason string) {
	ui.Step("Local Snapshot (Kopia)")
	ui.TestSkip(reason)
}

// ── Step 15: Decommission coverage ────────────────────────────────────────────

// testDecommission exercises the rollback helper functions with representative
// fixtures so that future refactors cannot quietly break the decommission path.
func testDecommission(ui *UI) {
	ui.Step("Decommission (rollback) coverage")

	// ── umask managed block ───────────────────────────────────────────────────
	// Verify that removeManagedBlock removes only the managed umask block and
	// leaves surrounding content intact.
	fixture := "# shell config\n" +
		managedBlock(umaskBlockStart, umaskBlockEnd, "umask 007") +
		"export PATH=$HOME/.local/bin:$PATH\n"
	cleaned := removeManagedBlock(fixture, umaskBlockStart, umaskBlockEnd)
	if strings.Contains(cleaned, "umask 007") {
		ui.TestFail("umask rollback: 'umask 007' still present after managed block removal")
	} else if strings.Contains(cleaned, umaskBlockStart) {
		ui.TestFail("umask rollback: block start marker still present after removal")
	} else if !strings.Contains(cleaned, "export PATH") {
		ui.TestFail("umask rollback: removed too much — surrounding lines missing")
	} else {
		ui.TestPass("umask rollback removes managed block without disturbing surrounding content")
	}

	// ── managed block helpers ────────────────────────────────────────────────
	fixture = "export FOO=1\n" +
		managedBlock(userPathBlockStart, userPathBlockEnd, `export PATH="$HOME/.local/bin:$PATH"`) +
		"export BAR=2\n"
	cleaned = removeManagedBlock(fixture, userPathBlockStart, userPathBlockEnd)
	switch {
	case strings.Contains(cleaned, userPathBlockStart):
		ui.TestFail("removeManagedBlock: managed block start marker still present after removal")
	case !strings.Contains(cleaned, "export FOO=1") || !strings.Contains(cleaned, "export BAR=2"):
		ui.TestFail("removeManagedBlock: removed too much — surrounding shell lines missing")
	default:
		ui.TestPass("removeManagedBlock strips the managed block while preserving surrounding lines")
	}

	// ── pf anchor line stripping ──────────────────────────────────────────────
	// Mirror the logic in stripPfAnchorLines to verify it removes agent anchor
	// lines without touching unrelated pf rules.
	pfData := "# Default pf rules\nset skip on lo\npass all\n# Claude Code sandbox user blocklist\nanchor \"agent\"\nload anchor \"agent\" from \"/etc/pf.anchors/agent\"\n"
	var keptPf []string
	for _, line := range strings.Split(pfData, "\n") {
		if strings.Contains(line, `anchor "agent"`) ||
			strings.Contains(line, `load anchor "agent"`) ||
			strings.TrimSpace(line) == "# Claude Code sandbox user blocklist" {
			continue
		}
		keptPf = append(keptPf, line)
	}
	strippedPf := strings.Join(keptPf, "\n")
	if strings.Contains(strippedPf, `anchor "agent"`) {
		ui.TestFail("pf anchor line stripping: anchor lines still present after removal")
	} else if !strings.Contains(strippedPf, "pass all") {
		ui.TestFail("pf anchor line stripping: removed too much — non-anchor rules missing")
	} else {
		ui.TestPass("pf anchor line stripping removes agent anchor without disturbing other rules")
	}

	// ── DNS blocklist stripping ───────────────────────────────────────────────
	// Mirror the logic in rollbackDNSBlocklist to verify the block is excised
	// while unrelated /etc/hosts entries are preserved.
	hostsData := "127.0.0.1 localhost\n" +
		hostsMarker + "\n" +
		"0.0.0.0 ngrok.io\n" +
		"0.0.0.0 pastebin.com\n" +
		"# === End AI Agent Blocklist ===\n" +
		"255.255.255.255 broadcasthost\n"
	const endMarker = "# === End AI Agent Blocklist ==="
	var hostsKept []string
	inside := false
	for _, line := range strings.Split(hostsData, "\n") {
		if strings.TrimSpace(line) == hostsMarker {
			inside = true
			continue
		}
		if inside {
			if strings.TrimSpace(line) == endMarker {
				inside = false
			}
			continue
		}
		hostsKept = append(hostsKept, line)
	}
	strippedHosts := strings.Join(hostsKept, "\n")
	switch {
	case strings.Contains(strippedHosts, "ngrok.io"):
		ui.TestFail("DNS blocklist stripping: ngrok.io still present after removal")
	case strings.Contains(strippedHosts, "pastebin.com"):
		ui.TestFail("DNS blocklist stripping: pastebin.com still present after removal")
	case !strings.Contains(strippedHosts, "127.0.0.1 localhost"):
		ui.TestFail("DNS blocklist stripping: system entry '127.0.0.1 localhost' was removed")
	case !strings.Contains(strippedHosts, "broadcasthost"):
		ui.TestFail("DNS blocklist stripping: entry after blocklist block was removed")
	default:
		ui.TestPass("DNS blocklist stripping removes agent block without touching surrounding /etc/hosts entries")
	}

	// ── Local snapshot repo removal ───────────────────────────────────────────
	// Verify that rollbackLocalRepo can clean up a repo directory.
	tmpRepoDecom := fmt.Sprintf("/tmp/haztest-decom-repo-%d", os.Getpid())
	if err := os.MkdirAll(tmpRepoDecom, 0o700); err == nil {
		if err := os.RemoveAll(tmpRepoDecom); err != nil {
			ui.TestFail(fmt.Sprintf("Local repo removal failed: %v", err))
		} else if _, err := os.Stat(tmpRepoDecom); os.IsNotExist(err) {
			ui.TestPass("Local repo removal: directory no longer exists after os.RemoveAll")
		} else {
			ui.TestFail("Local repo removal: directory still exists after os.RemoveAll")
		}
	} else {
		ui.TestWarn(fmt.Sprintf("Could not create temp repo dir for decommission test: %v", err))
	}
}

// kopiaTest holds state shared between testCloudBackup (Step 15) and
// testCloudRestore (Step 16). Step 15 populates these; Step 16 consumes
// and cleans up.
var kopiaTest struct {
	repoDir    string
	configFile string
	sourceDir  string
	password   string
}

// ── Step 15: Cloud Backup ───────────────────────────────────────────────────

func testCloudBackup(ui *UI) {
	ui.Step("Cloud Backup (Go-native Kopia)")

	ctx := context.Background()
	kopiaTest.repoDir = fmt.Sprintf("/tmp/haztest-kopia-repo-%d", os.Getpid())
	kopiaTest.sourceDir = fmt.Sprintf("/tmp/haztest-kopia-src-%d", os.Getpid())
	kopiaTest.password = "test-password-T3st!"

	if err := os.MkdirAll(kopiaTest.repoDir, 0o700); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not create temp repo dir: %v", err))
		return
	}
	if err := os.MkdirAll(kopiaTest.sourceDir, 0o700); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not create temp source dir: %v", err))
		return
	}

	// Create first test file
	if err := os.WriteFile(filepath.Join(kopiaTest.sourceDir, "hello.txt"), []byte("hello kopia"), 0o644); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not create test file: %v", err))
		return
	}

	// 1. Initialize local filesystem storage
	st, err := filesystem.New(ctx, &filesystem.Options{Path: kopiaTest.repoDir}, false)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not initialize storage: %v", err))
		return
	}
	ui.TestPass("Kopia: storage initialization successful")

	// 2. Initialize repository (encrypted with password)
	if err := repo.Initialize(ctx, st, &repo.NewRepositoryOptions{}, kopiaTest.password); err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not initialize repository: %v", err))
		return
	}
	ui.TestPass("Kopia: repository initialization successful")

	// 3. Connect and Open
	kopiaTest.configFile = filepath.Join(kopiaTest.repoDir, "kopia.config")
	if err := repo.Connect(ctx, kopiaTest.configFile, st, kopiaTest.password, &repo.ConnectOptions{}); err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not connect: %v", err))
		return
	}
	r, err := repo.Open(ctx, kopiaTest.configFile, kopiaTest.password, &repo.Options{})
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not open: %v", err))
		return
	}
	defer r.Close(ctx)
	ui.TestPass("Kopia: repository open successful")
	directRepo, err := requireDirectKopiaRepository(r)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: repository does not support direct writes: %v", err))
		return
	}

	// 4. First backup — single file
	ctx, wr, err := directRepo.NewDirectWriter(ctx, repo.WriteSessionOptions{Purpose: "Test"})
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not create writer: %v", err))
		return
	}

	localEntry, err := localfs.Directory(kopiaTest.sourceDir)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not open local directory: %v", err))
		return
	}

	sourceInfo := snapshot.SourceInfo{
		Host:     "test-host",
		UserName: "test-user",
		Path:     kopiaTest.sourceDir,
	}

	uploader1 := upload.NewUploader(wr)
	snap1, err := uploader1.Upload(ctx, localEntry, nil, sourceInfo)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: first upload failed: %v", err))
		wr.Close(ctx)
		return
	}
	ui.TestPass(fmt.Sprintf("Kopia: first upload successful (root: %v)", snap1.RootObjectID()))

	// Save snapshot manifest (mirrors production code)
	if _, err := snapshot.SaveSnapshot(ctx, wr, snap1); err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not save first snapshot: %v", err))
		wr.Close(ctx)
		return
	}
	if err := wr.Flush(ctx); err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: first flush failed: %v", err))
		return
	}
	ui.TestPass("Kopia: first snapshot manifest saved")

	// 5. Incrementality — add a second file, re-upload with previous snapshots
	if err := os.WriteFile(filepath.Join(kopiaTest.sourceDir, "world.txt"), []byte("world kopia"), 0o644); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not create second test file: %v", err))
		return
	}

	ctx2, wr2, err := directRepo.NewDirectWriter(ctx, repo.WriteSessionOptions{Purpose: "Test-Incr"})
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not create second writer: %v", err))
		return
	}

	localEntry2, err := localfs.Directory(kopiaTest.sourceDir)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not reopen local directory: %v", err))
		wr2.Close(ctx2)
		return
	}

	// Pass previous snapshots so kopia can skip unchanged files
	previous, err := snapshot.ListSnapshots(ctx2, wr2, sourceInfo)
	if err != nil {
		previous = nil
	}

	uploader2 := upload.NewUploader(wr2)
	snap2, err := uploader2.Upload(ctx2, localEntry2, nil, sourceInfo, previous...)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: incremental upload failed: %v", err))
		wr2.Close(ctx2)
		return
	}

	cached := atomic.LoadInt32(&snap2.Stats.CachedFiles)
	nonCached := atomic.LoadInt32(&snap2.Stats.NonCachedFiles)
	if cached >= 1 {
		ui.TestPass(fmt.Sprintf("Kopia: incremental upload — %d cached, %d new files", cached, nonCached))
	} else {
		ui.TestFail(fmt.Sprintf("Kopia: incremental upload expected cached files >0, got cached=%d non-cached=%d", cached, nonCached))
	}

	if _, err := snapshot.SaveSnapshot(ctx2, wr2, snap2); err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not save second snapshot: %v", err))
		wr2.Close(ctx2)
		return
	}
	if err := wr2.Flush(ctx2); err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: second flush failed: %v", err))
		return
	}

	// Verify we now have exactly 2 snapshots
	allSnaps, err := snapshot.ListSnapshots(ctx, r, sourceInfo)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not list snapshots: %v", err))
		return
	}
	if len(allSnaps) == 2 {
		ui.TestPass(fmt.Sprintf("Kopia: snapshot count correct (%d)", len(allSnaps)))
	} else {
		ui.TestFail(fmt.Sprintf("Kopia: expected 2 snapshots, got %d", len(allSnaps)))
	}

	// 6. Encryption at rest — verify no plaintext in blob storage
	plaintext := [][]byte{[]byte("hello kopia"), []byte("world kopia")}
	foundPlaintext := false

	_ = filepath.WalkDir(kopiaTest.repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, pt := range plaintext {
			if bytes.Contains(data, pt) {
				foundPlaintext = true
				return filepath.SkipAll
			}
		}
		return nil
	})

	if !foundPlaintext {
		ui.TestPass("Kopia: encryption verified — no plaintext found in blob storage")
	} else {
		ui.TestFail("Kopia: PLAINTEXT content found in blob storage — encryption may be broken")
	}
}

func testCloudBackupSkipped(ui *UI, reason string) {
	ui.Step("Cloud Backup (Go-native Kopia)")
	ui.TestSkip(reason)
}

// ── Step 16: Cloud Restore ──────────────────────────────────────────────────

func testCloudRestore(ui *UI) {
	ui.Step("Cloud Restore (Go-native Kopia)")

	// Clean up shared state when done, regardless of outcome
	defer func() {
		if kopiaTest.repoDir != "" {
			os.RemoveAll(kopiaTest.repoDir)
		}
		if kopiaTest.sourceDir != "" {
			os.RemoveAll(kopiaTest.sourceDir)
		}
	}()

	if kopiaTest.configFile == "" {
		ui.TestSkip("Kopia: skipping restore — backup step did not complete")
		return
	}

	ctx := context.Background()
	r, err := repo.Open(ctx, kopiaTest.configFile, kopiaTest.password, &repo.Options{})
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not reopen repository: %v", err))
		return
	}
	defer r.Close(ctx)

	sourceInfo := snapshot.SourceInfo{
		Host:     "test-host",
		UserName: "test-user",
		Path:     kopiaTest.sourceDir,
	}

	snapshots, err := snapshot.ListSnapshots(ctx, r, sourceInfo)
	if err != nil || len(snapshots) == 0 {
		ui.TestFail(fmt.Sprintf("Kopia: no snapshots found for restore (err=%v)", err))
		return
	}

	// Use latest snapshot (should be the 2-file snapshot)
	latest := snapshots[len(snapshots)-1]
	ui.TestPass(fmt.Sprintf("Kopia: found %d snapshot(s), restoring latest", len(snapshots)))

	rootEntry, err := snapshotfs.SnapshotRoot(r, latest)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not get snapshot root: %v", err))
		return
	}

	restoreDir := fmt.Sprintf("/tmp/haztest-kopia-restore-%d", os.Getpid())
	if err := os.MkdirAll(restoreDir, 0o700); err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not create restore dir: %v", err))
		return
	}
	defer os.RemoveAll(restoreDir)

	output := &restore.FilesystemOutput{
		TargetPath:     restoreDir,
		OverwriteFiles: true,
	}
	if err := output.Init(ctx); err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: could not initialize restore output: %v", err))
		return
	}
	// MinSizeForPlaceholder must be > any file size to avoid shallow .kopia-entry placeholders.
	stats, err := restore.Entry(ctx, r, output, rootEntry, restore.Options{
		Parallel:              4,
		MinSizeForPlaceholder: 1 << 30, // 1 GiB: larger than any test file
	})
	if err != nil {
		ui.TestFail(fmt.Sprintf("Kopia: restore.Entry failed: %v", err))
		return
	}

	if stats.RestoredFileCount == 2 {
		ui.TestPass(fmt.Sprintf("Kopia: restored %d files (%d bytes)", stats.RestoredFileCount, stats.RestoredTotalFileSize))
	} else {
		ui.TestFail(fmt.Sprintf("Kopia: expected 2 restored files, got %d", stats.RestoredFileCount))
	}

	// Round-trip content verification
	wantFiles := map[string]string{
		"hello.txt": "hello kopia",
		"world.txt": "world kopia",
	}
	allMatch := true
	for name, want := range wantFiles {
		got, err := os.ReadFile(filepath.Join(restoreDir, name))
		if err != nil {
			ui.TestFail(fmt.Sprintf("Kopia: restored file %q not found: %v", name, err))
			allMatch = false
			continue
		}
		if string(got) != want {
			ui.TestFail(fmt.Sprintf("Kopia: %q content mismatch: got %q, want %q", name, got, want))
			allMatch = false
		}
	}
	if allMatch {
		ui.TestPass("Kopia: round-trip content verification passed — all files match")
	}
}

func testCloudRestoreSkipped(ui *UI, reason string) {
	ui.Step("Cloud Restore (Go-native Kopia)")
	ui.TestSkip(reason)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// portInAnchor returns true if the anchor rules reference the given port
// using a word-boundary regex, preventing e.g. port "25" matching "250".
func portInAnchor(rules, port string) bool {
	re := regexp.MustCompile(`port = ` + regexp.QuoteMeta(port) + `\b`)
	return re.MatchString(rules)
}

// checkBlockedDomain returns true if domain resolves to 0.0.0.0 or fails
// to resolve.  Uses the system resolver (CGO on macOS) which respects /etc/hosts.
// Build with CGO_ENABLED=1 (the default) to ensure /etc/hosts is consulted.
func checkBlockedDomain(domain string) bool {
	//nolint:gosec // Test helper resolves operator-controlled domains to verify local DNS blocking behavior.
	addrs, err := net.LookupHost(domain)
	if err != nil {
		return true // NXDOMAIN or error = blocked
	}
	for _, a := range addrs {
		if a == "0.0.0.0" {
			return true // resolves to null route = blocked
		}
	}
	return false
}

func pfRuntimeEnabledUnprivileged() (bool, bool) {
	return false, false
}

func readPfAnchorFileRules() (string, error) {
	data, err := os.ReadFile(pfAnchorFile)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

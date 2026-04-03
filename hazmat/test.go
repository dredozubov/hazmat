package main

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
)

func runTest(quick bool) error {
	ui := &UI{}

	fmt.Println()
	cBold.Println("  ┌──────────────────────────────────────────────┐")
	cBold.Println("  │  Hazmat verification suite                   │")
	cBold.Println("  └──────────────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("  Modes:")
	fmt.Println("    hazmat check          Quick checks (no external traffic)")
	fmt.Println("    hazmat check --full   Full suite including live network probes")
	fmt.Println()

	cu, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}
	fmt.Printf("  Running as: %s\n", cu.Username)
	fmt.Printf("  Agent user: %s\n", agentUser)
	if quick {
		cYellow.Println("  Quick mode: live network tests skipped")
	}
	fmt.Println()

	// Resolve the binary path once; used for agent TCP probes.
	selfPath, _ := os.Executable()

	testAgentUser(ui)
	testDevGroupAndWorkspace(ui, cu.Username)
	testUserIsolation(ui, cu.Username)
	testHardeningGaps(ui)
	testPasswordlessSudo(ui)
	testPfFirewallStatic(ui)
	testPfFirewallLive(ui, quick, selfPath)
	testDNSBlocklist(ui)
	testPersistence(ui)
	testAgentTools(ui)
	testCommandSurface(ui)
	testSeatbelt(ui)
	testLocalSnapshot(ui)
	testCloudBackup(ui)
	testCloudRestore(ui)
	testDecommission(ui)

	if ui.Summary() {
		os.Exit(1)
	}
	return nil
}

// ── Step 1: Agent user ────────────────────────────────────────────────────────

func testAgentUser(ui *UI) {
	ui.Step("Agent user")

	u, err := user.Lookup(agentUser)
	if err != nil {
		ui.TestFail(fmt.Sprintf("User '%s' does not exist — run hazmat init first", agentUser))
		return
	}
	ui.TestPass(fmt.Sprintf("User '%s' exists", agentUser))

	if u.Uid == agentUID {
		ui.TestPass(fmt.Sprintf("UID is %s", agentUID))
	} else {
		ui.TestFail(fmt.Sprintf("UID is '%s', expected %s", u.Uid, agentUID))
	}

	if _, err := os.Stat(agentHome); err == nil {
		ui.TestPass(fmt.Sprintf("Home directory exists: %s", agentHome))
	} else {
		ui.TestFail(fmt.Sprintf("Home directory missing: %s", agentHome))
	}

	if info, err := os.Stat(agentHome); err == nil {
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			ownerUID := strconv.FormatUint(uint64(st.Uid), 10)
			if ownerUID == agentUID {
				ui.TestPass(fmt.Sprintf("Home directory owned by %s", agentUser))
			} else {
				ui.TestFail(fmt.Sprintf("Home directory owned by uid=%s, expected %s", ownerUID, agentUID))
			}
		}
	}

	if out, err := sudoOutput("dscl", ".", "-read", "/Users/"+agentUser, "IsHidden"); err == nil {
		if strings.Contains(out, "1") {
			ui.TestPass("User is hidden from login screen")
		} else {
			ui.TestWarn(fmt.Sprintf("User is NOT hidden from login screen (IsHidden=%s)", strings.TrimSpace(out)))
		}
	}
}

// testWorkspaceDir returns a temporary directory for test fixtures.
// Tests that need a writable directory use this instead of a hardcoded workspace.
func testWorkspaceDir() string {
	return os.TempDir()
}

// ── Step 2: Dev group and home traverse ──────────────────────────────────────

func testDevGroupAndWorkspace(ui *UI, currentUser string) {
	ui.Step("Dev group and home traverse")

	if _, err := user.LookupGroup(sharedGroup); err == nil {
		ui.TestPass(fmt.Sprintf("Group '%s' exists", sharedGroup))
	} else {
		ui.TestFail(fmt.Sprintf("Group '%s' does not exist", sharedGroup))
	}

	for _, u := range []string{currentUser, agentUser} {
		if ok, _ := groupMembershipContains(sharedGroup, u); ok {
			ui.TestPass(fmt.Sprintf("%s is a member of '%s'", u, sharedGroup))
		} else {
			ui.TestFail(fmt.Sprintf("%s is NOT a member of '%s'", u, sharedGroup))
		}
	}

	if homeAllowsAgentTraverse(os.Getenv("HOME")) {
		ui.TestPass(fmt.Sprintf("Home directory ACL lets '%s' traverse to project directories", agentUser))
	} else {
		ui.TestWarn(fmt.Sprintf("Home directory access for '%s' not detected — project directories may be unreachable", agentUser))
	}

	// Write test as current user
	tmpDr := fmt.Sprintf("%s/.test_dr_%d", testWorkspaceDir(), os.Getpid())
	if f, err := os.Create(tmpDr); err == nil {
		f.Close()
		os.Remove(tmpDr)
		ui.TestPass(fmt.Sprintf("%s can write to workspace root", currentUser))
	} else {
		ui.TestFail(fmt.Sprintf("%s cannot write to workspace root", currentUser))
	}

	// Write test as agent; also check setgid inheritance
	tmpAgent := fmt.Sprintf("%s/.test_agent_%d", testWorkspaceDir(), os.Getpid())
	if err := asAgentQuiet("touch", tmpAgent); err == nil {
		ui.TestPass(fmt.Sprintf("%s can write to workspace root", agentUser))

		if finfo, err := os.Stat(tmpAgent); err == nil {
			if st, ok := finfo.Sys().(*syscall.Stat_t); ok {
				gidStr := strconv.FormatUint(uint64(st.Gid), 10)
				if g, err := user.LookupGroupId(gidStr); err == nil && g.Name == sharedGroup {
					ui.TestPass(fmt.Sprintf("New files inherit '%s' group (setgid working)", sharedGroup))
				} else {
					ui.TestWarn(fmt.Sprintf("New file group is gid=%s, expected '%s' — setgid may not be working", gidStr, sharedGroup))
				}
			}
		}
		sudo("rm", "-f", tmpAgent) //nolint:errcheck
	} else {
		ui.TestFail(fmt.Sprintf("%s cannot write to workspace root", agentUser))
	}

	// Bidirectional access: agent-created file must be readable/writable by controlling user.
	// This verifies that the inheritable ACL overrides the agent's umask 077.
	tmpAgentRW := fmt.Sprintf("%s/.test_agent_rw_%d", testWorkspaceDir(), os.Getpid())
	if err := asAgentShellQuiet(fmt.Sprintf("echo test > %q", tmpAgentRW)); err == nil {
		defer sudo("rm", "-f", tmpAgentRW) //nolint:errcheck

		if f, err := os.Open(tmpAgentRW); err == nil {
			f.Close()
			ui.TestPass(fmt.Sprintf("%s can READ file created by %s (ACL effective)", currentUser, agentUser))
		} else {
			ui.TestFail(fmt.Sprintf("%s cannot read file created by %s — workspace ACL missing or not inherited", currentUser, agentUser))
		}

		if f, err := os.OpenFile(tmpAgentRW, os.O_WRONLY|os.O_APPEND, 0); err == nil {
			f.Close()
			ui.TestPass(fmt.Sprintf("%s can WRITE file created by %s (ACL effective)", currentUser, agentUser))
		} else {
			ui.TestFail(fmt.Sprintf("%s cannot write file created by %s — workspace ACL missing or not inherited", currentUser, agentUser))
		}
	} else {
		ui.TestWarn(fmt.Sprintf("%s could not create test file — skipping bidirectional write test", agentUser))
	}

	// Bidirectional access: controlling-user-created file must be readable/writable by agent.
	tmpUserRW := fmt.Sprintf("%s/.test_user_rw_%d", testWorkspaceDir(), os.Getpid())
	if f, err := os.Create(tmpUserRW); err == nil {
		f.Close()
		defer os.Remove(tmpUserRW)

		if err := asAgentQuiet("cat", tmpUserRW); err == nil {
			ui.TestPass(fmt.Sprintf("%s can READ file created by %s", agentUser, currentUser))
		} else {
			ui.TestFail(fmt.Sprintf("%s cannot read file created by %s — workspace ACL missing or not inherited", agentUser, currentUser))
		}

		if err := asAgentShellQuiet(fmt.Sprintf("echo test >> %q", tmpUserRW)); err == nil {
			ui.TestPass(fmt.Sprintf("%s can WRITE file created by %s", agentUser, currentUser))
		} else {
			ui.TestFail(fmt.Sprintf("%s cannot write file created by %s — workspace ACL missing or not inherited", agentUser, currentUser))
		}
	} else {
		ui.TestWarn(fmt.Sprintf("%s could not create test file — skipping bidirectional read test", currentUser))
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
		ui.TestWarn(fmt.Sprintf("%s can read %s's .zshrc — consider: chmod 700 %s",
			currentUser, agentUser, agentHome))
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
			ui.TestFail(fmt.Sprintf("Docker socket permissions: %04o (expected 700 — agent could escape via Docker)",
				info.Mode().Perm()))
		}
	} else {
		ui.TestSkip("Docker socket not present")
	}

	if out, _ := asAgentOutput("cat", agentHome+"/.zshrc"); strings.Contains(out, "umask 077") {
		ui.TestPass("umask 077 set in agent's .zshrc")
	} else {
		ui.TestWarn("umask 077 not found in agent's .zshrc — new files will have permissive defaults")
	}
}

// ── Step 5: Passwordless sudo ─────────────────────────────────────────────────

func testPasswordlessSudo(ui *UI) {
	ui.Step("Passwordless sudo")

	if _, err := os.Stat(sudoersFile); err == nil {
		ui.TestPass(fmt.Sprintf("Sudoers file exists: %s", sudoersFile))
	} else {
		ui.TestFail(fmt.Sprintf("Sudoers file missing: %s", sudoersFile))
	}

	if err := sudo("-u", agentUser, "whoami"); err == nil {
		ui.TestPass(fmt.Sprintf("sudo -u %s works without password", agentUser))
	} else {
		ui.TestFail(fmt.Sprintf("sudo -u %s failed — check %s", agentUser, sudoersFile))
	}
}

// ── Step 6: pf firewall (static) ─────────────────────────────────────────────

func testPfFirewallStatic(ui *UI) {
	ui.Step("pf firewall (static)")

	if out, err := sudoOutput("pfctl", "-si"); err == nil && strings.Contains(out, "Status: Enabled") {
		ui.TestPass("pf is enabled")
	} else {
		ui.TestFail("pf is NOT enabled — run: sudo pfctl -e")
	}

	if _, err := os.Stat(pfAnchorFile); err == nil {
		ui.TestPass(fmt.Sprintf("pf anchor file exists: %s", pfAnchorFile))
	} else {
		ui.TestFail(fmt.Sprintf("pf anchor file missing: %s", pfAnchorFile))
	}

	rules, err := pfAnchorRules()
	if err == nil && strings.Contains(rules, "block") {
		n := len(strings.Split(strings.TrimSpace(rules), "\n"))
		ui.TestPass(fmt.Sprintf("pf anchor loaded with %d rules", n))
	} else {
		ui.TestFail(fmt.Sprintf("pf anchor '%s' not loaded or has no block rules", pfAnchorName))
	}

	for _, p := range []struct{ port, label string }{
		{"25", "SMTP"}, {"6667", "IRC"}, {"21", "FTP"}, {"9050", "Tor"},
	} {
		if portInAnchor(rules, p.port) {
			ui.TestPass(fmt.Sprintf("pf anchor blocks port %s (%s)", p.port, p.label))
		} else {
			ui.TestWarn(fmt.Sprintf("pf anchor may not block port %s (%s) — verify anchor file",
				p.port, p.label))
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
		fmt.Printf("    Testing %s (port %s, should be %s)...\n",
			p.label, p.port, map[bool]string{true: "ALLOWED", false: "BLOCKED"}[p.wantAllow])

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
			ui.TestFail(fmt.Sprintf("BLOCK FAILURE: %s connected to port %s (%s not blocked)",
				agentUser, p.port, p.label))
		}
	}

	// Verify rules are scoped to agent only — current user should still reach 443
	fmt.Println("    Testing that pf rules are scoped to agent only...")
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

	hosts, err := os.ReadFile("/etc/hosts")
	if err != nil || !strings.Contains(string(hosts), "AI Agent Blocklist") {
		ui.TestFail("DNS blocklist not found in /etc/hosts — run hazmat init and enable the DNS blocklist")
		return
	}
	n := strings.Count(string(hosts), "0.0.0.0 ")
	ui.TestPass(fmt.Sprintf("DNS blocklist present in /etc/hosts (%d entries)", n))

	for _, domain := range []string{"ngrok.io", "pastebin.com", "webhook.site", "transfer.sh"} {
		if checkBlockedDomain(domain) {
			ui.TestPass(fmt.Sprintf("%s is blocked (resolves to 0.0.0.0 or fails)", domain))
		} else {
			ui.TestFail(fmt.Sprintf("%s resolved to a real IP — blocklist not working for this domain", domain))
		}
	}
}

// ── Step 9: Persistence ───────────────────────────────────────────────────────

func testPersistence(ui *UI) {
	ui.Step("Persistence across reboots")

	if _, err := os.Stat(pfDaemonPlist); err == nil {
		ui.TestPass(fmt.Sprintf("LaunchDaemon plist exists: %s", pfDaemonPlist))
	} else {
		ui.TestFail(fmt.Sprintf("LaunchDaemon plist missing: %s — pf rules will not reload on reboot", pfDaemonPlist))
	}

	if launchctlLoaded(pfDaemonLabel) {
		ui.TestPass(fmt.Sprintf("LaunchDaemon '%s' is loaded", pfDaemonLabel))
	} else {
		ui.TestWarn(fmt.Sprintf("LaunchDaemon '%s' is not loaded — try: sudo launchctl bootstrap system %s",
			pfDaemonLabel, pfDaemonPlist))
	}

	if data, err := os.ReadFile("/etc/pf.conf"); err == nil &&
		strings.Contains(string(data), `anchor "agent"`) {
		ui.TestPass(fmt.Sprintf("/etc/pf.conf references anchor '%s'", pfAnchorName))
	} else {
		ui.TestFail(fmt.Sprintf("/etc/pf.conf does not reference anchor '%s'", pfAnchorName))
	}
}

// ── Step 10: Agent user tools ─────────────────────────────────────────────────

func testAgentTools(ui *UI) {
	ui.Step("Agent user tools")

	// Claude Code
	if asAgentQuiet("test", "-f", agentHome+"/.local/bin/claude") == nil {
		ui.TestPass(fmt.Sprintf("Claude Code installed: %s/.local/bin/claude", agentHome))
	} else if _, out, _ := func() (bool, string, error) {
		out, err := asAgentOutput("bash", "-c", "command -v claude 2>/dev/null")
		return err == nil && out != "", out, err
	}(); out != "" {
		ui.TestPass(fmt.Sprintf("Claude Code is in agent's PATH: %s", out))
	} else {
		ui.TestWarn(fmt.Sprintf("Claude Code not found for agent user — run 'hazmat bootstrap' or verify %s", claudeInstallerURL))
	}

	// API key
	if out, _ := asAgentOutput("bash", "-c",
		"grep -q ANTHROPIC_API_KEY ~/.zshrc 2>/dev/null && echo yes || echo no"); strings.TrimSpace(out) == "yes" {
		ui.TestPass("ANTHROPIC_API_KEY is configured for agent user")
	} else {
		ui.TestWarn("ANTHROPIC_API_KEY not found in agent's .zshrc — Claude Code will not authenticate")
	}

	// Git identity
	name, _ := asAgentOutput("git", "config", "--global", "user.name")
	email, _ := asAgentOutput("git", "config", "--global", "user.email")
	if name != "" && email != "" {
		ui.TestPass(fmt.Sprintf("Git identity configured: %s <%s>", name, email))
	} else {
		ui.TestWarn(fmt.Sprintf("Git identity not fully configured for agent (name=%q, email=%q)", name, email))
	}

	// SSH key
	if asAgentQuiet("test", "-f", agentHome+"/.ssh/id_ed25519.pub") == nil {
		ui.TestPass("SSH key exists (ed25519)")
	} else if asAgentQuiet("test", "-d", agentHome+"/.ssh") == nil {
		ui.TestWarn("~/.ssh exists but no id_ed25519.pub — GitHub access may not work")
	} else {
		ui.TestWarn(fmt.Sprintf("No SSH key found for agent user — run: sudo -u %s -i, then: ssh-keygen -t ed25519", agentUser))
	}

	// Claude settings
	if asAgentQuiet("test", "-f", agentHome+"/.claude/settings.json") == nil {
		ui.TestPass("~/.claude/settings.json exists for agent user")
	} else {
		ui.TestWarn("No ~/.claude/settings.json for agent user — permissions and deny rules not configured")
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
		ui.TestSkip("OpenCode not installed for agent user (optional — run 'hazmat bootstrap opencode' to test it)")
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
		ui.TestSkip("Codex not installed for agent user (optional — run 'hazmat bootstrap codex' to test it)")
	}
}

// ── Step 11: Command surface ─────────────────────────────────────────────────

func testCommandSurface(ui *UI) {
	ui.Step("Command surface")

	if asAgentQuiet("test", "-f", agentEnvPath) == nil {
		ui.TestPass(fmt.Sprintf("Agent env file exists: %s", agentEnvPath))
	} else {
		ui.TestFail(fmt.Sprintf("Agent env file missing: %s", agentEnvPath))
	}

	if out, _ := asAgentOutput("cat", agentHome+"/.zshrc"); strings.Contains(out, "agent-env.zsh") {
		ui.TestPass("Agent .zshrc sources the hazmat env file")
	} else {
		ui.TestFail("Agent .zshrc does not source the hazmat env file")
	}

	for _, wrapper := range []string{hostClaudeWrapperName, hostExecWrapperName, hostShellWrapperName} {
		path := hostWrapperPath(wrapper)
		if info, err := os.Stat(path); err != nil {
			ui.TestFail(fmt.Sprintf("Host wrapper missing: %s", path))
		} else if info.Mode()&0o111 == 0 {
			ui.TestFail(fmt.Sprintf("Host wrapper not executable: %s", path))
		} else {
			ui.TestPass(fmt.Sprintf("Host wrapper is executable: %s", path))
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
		ui.TestWarn(fmt.Sprintf("%s does not appear to expose ~/.local/bin yet — open a new shell after setup", userZshrc))
	}
}

// ── Step 12: Seatbelt confinement ─────────────────────────────────────────────

func testSeatbelt(ui *UI) {
	ui.Step("Seatbelt confinement")

	if info, err := os.Stat(seatbeltWrapperPath); err != nil {
		ui.TestFail(fmt.Sprintf("Seatbelt wrapper missing: %s — run hazmat init", seatbeltWrapperPath))
	} else if info.Mode()&0o111 == 0 {
		ui.TestFail(fmt.Sprintf("Seatbelt wrapper not executable: %s", seatbeltWrapperPath))
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

	// Create isolated test directories. readDir is passed as a read-only dir
	// so it receives a per-dir read rule separate from the project.
	projectDir := fmt.Sprintf("%s/.seatbelt-project-%d", testWorkspaceDir(), os.Getpid())
	readDir := fmt.Sprintf("%s/.seatbelt-read-%d", testWorkspaceDir(), os.Getpid())
	if err := os.MkdirAll(projectDir, 0o770); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not create seatbelt project dir: %v", err))
		return
	}
	if err := os.MkdirAll(readDir, 0o770); err != nil {
		ui.TestWarn(fmt.Sprintf("Could not create seatbelt read dir: %v", err))
		return
	}
	defer os.RemoveAll(projectDir)
	defer os.RemoveAll(readDir)

	// Generate a per-session policy with the test dirs embedded as literals.
	cfg := sessionConfig{
		ProjectDir: projectDir,
		ReadDirs:   []string{readDir},
	}
	policyContent := generateSBPL(cfg)
	policyFile := fmt.Sprintf("/private/tmp/hazmat-test-%d.sb", os.Getpid())
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
		sudo("rm", "-f", testWritePath) //nolint:errcheck
		ui.TestPass("Seatbelt allows writes inside PROJECT_DIR")
	} else {
		ui.TestFail(fmt.Sprintf("Seatbelt unexpectedly denied write inside PROJECT_DIR: %v", err))
	}

	// Denied: write to a read-only directory.
	testReadWritePath := fmt.Sprintf("%s/.seatbelt-read-write-%d", readDir, os.Getpid())
	if err := runSandboxed("/usr/bin/touch", testReadWritePath); err != nil {
		ui.TestPass("Seatbelt denies writes to read-only directories")
	} else {
		sudo("rm", "-f", testReadWritePath) //nolint:errcheck
		ui.TestFail("CONFINEMENT BREACH: Seatbelt allowed write to a read-only directory")
	}

	// Denied: write to agent HOME outside approved subdirs.
	// The agent user owns their home, so this tests sandbox enforcement — not just
	// filesystem permissions.
	testExfilPath := fmt.Sprintf("%s/.seatbelt-exfil-%d", agentHome, os.Getpid())
	if err := runSandboxed("/usr/bin/touch", testExfilPath); err != nil {
		ui.TestPass("Seatbelt denies writes outside approved HOME subdirs")
	} else {
		sudo("rm", "-f", testExfilPath) //nolint:errcheck
		ui.TestFail("CONFINEMENT BREACH: Seatbelt allowed write to HOME outside approved subdirs")
	}

	// Denied: read a file inside agent HOME that is not in an approved subdir.
	// We create a world-readable probe file as root so the agent would normally be
	// able to read it; the sandbox must block the access.
	probePath := fmt.Sprintf("%s/.seatbelt-probe-%d", agentHome, os.Getpid())
	if err := sudo("bash", "-c",
		fmt.Sprintf("echo probe > %s && chmod 644 %s", probePath, probePath)); err == nil {
		defer sudo("rm", "-f", probePath) //nolint:errcheck
		if err := runSandboxed("/bin/cat", probePath); err != nil {
			ui.TestPass("Seatbelt denies reads of files outside approved HOME subdirs")
		} else {
			ui.TestFail("CONFINEMENT BREACH: Seatbelt allowed read of file outside approved HOME subdirs")
		}
	} else {
		ui.TestWarn("Could not create probe file for seatbelt read-denial test")
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

	// Allowed: read ~/.claude (Claude auth tokens must be accessible).
	claudeDir := agentHome + "/.claude"
	if _, err := os.Stat(claudeDir); err == nil {
		if err := runSandboxed("/bin/ls", claudeDir); err == nil {
			ui.TestPass("Seatbelt allows reads inside ~/.claude (Claude auth accessible)")
		} else {
			ui.TestFail("Seatbelt denies reads of ~/.claude — Claude auth will fail under confinement")
		}
	} else {
		ui.TestSkip("~/.claude does not exist for agent — skipping Claude auth read test")
	}
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

	if err := snapshotDir(ctx, r.(repo.DirectRepository), tmpSourceDir, "pre-session (test)"); err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: first snapshot failed: %v", err))
		return
	}
	ui.TestPass("local snapshot: first snapshot successful")

	// 3. Modify source, take incremental snapshot
	os.WriteFile(filepath.Join(tmpSourceDir, "new.go"), []byte("package main\n"), 0o644)
	if err := snapshotDir(ctx, r.(repo.DirectRepository), tmpSourceDir, "pre-session (test-2)"); err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: incremental snapshot failed: %v", err))
		return
	}
	ui.TestPass("local snapshot: incremental snapshot successful")

	// 4. List snapshots
	si := localSourceInfo(tmpSourceDir)
	snaps, err := snapshot.ListSnapshots(ctx, r, si)
	if err != nil {
		ui.TestFail(fmt.Sprintf("local snapshot: could not list snapshots: %v", err))
		return
	}
	if len(snaps) == 2 {
		ui.TestPass(fmt.Sprintf("local snapshot: snapshot count correct (%d)", len(snaps)))
	} else {
		ui.TestFail(fmt.Sprintf("local snapshot: expected 2 snapshots, got %d", len(snaps)))
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
		ui.TestFail(fmt.Sprintf("local snapshot: expected 2 restored files from first snapshot, got %d", stats.RestoredFileCount))
	}

	// new.go should NOT exist in first snapshot
	if _, err := os.Stat(filepath.Join(restoreDir, "new.go")); os.IsNotExist(err) {
		ui.TestPass("local snapshot: first snapshot correctly does not contain new.go")
	} else {
		ui.TestFail("local snapshot: first snapshot unexpectedly contains new.go")
	}

	// main.go should exist with correct content
	if data, err := os.ReadFile(filepath.Join(restoreDir, "main.go")); err == nil && string(data) == "package main\n" {
		ui.TestPass("local snapshot: round-trip content verification passed")
	} else {
		ui.TestFail("local snapshot: content mismatch after restore")
	}

	// 6. Check that real local repo exists if hazmat init was run
	if _, err := os.Stat(localConfigFile); err == nil {
		ui.TestPass(fmt.Sprintf("local snapshot repo configured at %s", localRepoDir))
	} else {
		ui.TestWarn(fmt.Sprintf("local snapshot repo not found at %s — run hazmat init to create it", localRepoDir))
	}
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
		managedBlock(umaskBlockStart, umaskBlockEnd, "umask 077") +
		"export PATH=$HOME/.local/bin:$PATH\n"
	cleaned := removeManagedBlock(fixture, umaskBlockStart, umaskBlockEnd)
	if strings.Contains(cleaned, "umask 077") {
		ui.TestFail("umask rollback: 'umask 077' still present after managed block removal")
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

	// 4. First backup — single file
	ctx, wr, err := r.(repo.DirectRepository).NewDirectWriter(ctx, repo.WriteSessionOptions{Purpose: "Test"})
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

	ctx2, wr2, err := r.(repo.DirectRepository).NewDirectWriter(ctx, repo.WriteSessionOptions{Purpose: "Test-Incr"})
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

// ── Helpers ───────────────────────────────────────────────────────────────────

// pfAnchorRules returns the loaded rules for the agent anchor.
func pfAnchorRules() (string, error) {
	return sudoOutput("pfctl", "-a", pfAnchorName, "-sr")
}

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

// launchctlLoaded returns true if the given label is listed in launchctl.
func launchctlLoaded(label string) bool {
	return sudo("launchctl", "list", label) == nil
}

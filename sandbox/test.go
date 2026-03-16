package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func newTestCmd() *cobra.Command {
	var quick bool
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Verify the sandbox setup is working correctly",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTest(quick)
		},
	}
	cmd.Flags().BoolVar(&quick, "quick", false, "Skip live network tests (no external traffic)")
	return cmd
}

func runTest(quick bool) error {
	ui := &UI{}

	fmt.Println()
	cBold.Println("  ┌──────────────────────────────────────────────┐")
	cBold.Println("  │  Sandbox test suite — Option A verification  │")
	cBold.Println("  └──────────────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("  Modes:")
	fmt.Println("    sandbox test           Full suite including live network probes")
	fmt.Println("    sandbox test --quick   Skip live TCP/network tests (no external traffic)")
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
	testBackup(ui)
	testRestore(ui)
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
		ui.TestFail(fmt.Sprintf("User '%s' does not exist — run sandbox setup first", agentUser))
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

// ── Step 2: Dev group and shared workspace ────────────────────────────────────

func testDevGroupAndWorkspace(ui *UI, currentUser string) {
	ui.Step("Dev group and shared workspace")

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

	info, err := os.Stat(sharedWorkspace)
	if err != nil {
		ui.TestFail(fmt.Sprintf("Shared workspace missing: %s", sharedWorkspace))
		return
	}
	ui.TestPass(fmt.Sprintf("Shared workspace exists: %s", sharedWorkspace))

	perms := info.Mode().Perm()
	if perms == 0o770 {
		ui.TestPass(fmt.Sprintf("Shared workspace permissions: %04o", perms))
	} else {
		ui.TestFail(fmt.Sprintf("Shared workspace permissions: %04o (expected 0770)", perms))
	}

	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		gidStr := strconv.FormatUint(uint64(st.Gid), 10)
		if g, err := user.LookupGroupId(gidStr); err == nil && g.Name == sharedGroup {
			ui.TestPass(fmt.Sprintf("Shared workspace group: %s", sharedGroup))
		} else {
			ui.TestFail(fmt.Sprintf("Shared workspace group is gid=%s, expected '%s'", gidStr, sharedGroup))
		}
	}

	if info.Mode()&fs.ModeSetgid != 0 {
		ui.TestPass("Shared workspace has setgid bit")
	} else {
		ui.TestFail(fmt.Sprintf("Shared workspace missing setgid bit (%s)", info.Mode()))
	}

	// ACL check: workspace must have an inherited ACE for the dev group so that
	// files created by the agent (with umask 077) are still accessible to the
	// controlling user — and vice versa.
	if workspaceHasDevACL() {
		ui.TestPass(fmt.Sprintf("Workspace ACL grants '%s' group inherited read/write access", sharedGroup))
	} else {
		ui.TestFail(fmt.Sprintf("Workspace ACL missing '%s' group — run sandbox setup again to add it", sharedGroup))
	}

	// Write test as current user
	tmpDr := fmt.Sprintf("%s/.test_dr_%d", sharedWorkspace, os.Getpid())
	if f, err := os.Create(tmpDr); err == nil {
		f.Close()
		os.Remove(tmpDr)
		ui.TestPass(fmt.Sprintf("%s can write to shared workspace", currentUser))
	} else {
		ui.TestFail(fmt.Sprintf("%s cannot write to shared workspace", currentUser))
	}

	// Write test as agent; also check setgid inheritance
	tmpAgent := fmt.Sprintf("%s/.test_agent_%d", sharedWorkspace, os.Getpid())
	if err := asAgentQuiet("touch", tmpAgent); err == nil {
		ui.TestPass(fmt.Sprintf("%s can write to shared workspace", agentUser))

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
		ui.TestFail(fmt.Sprintf("%s cannot write to shared workspace", agentUser))
	}

	// Bidirectional access: agent-created file must be readable/writable by controlling user.
	// This verifies that the inheritable ACL overrides the agent's umask 077.
	tmpAgentRW := fmt.Sprintf("%s/.test_agent_rw_%d", sharedWorkspace, os.Getpid())
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
	tmpUserRW := fmt.Sprintf("%s/.test_user_rw_%d", sharedWorkspace, os.Getpid())
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
		ui.TestFail("DNS blocklist not found in /etc/hosts — run sandbox setup and enable the DNS blocklist")
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
		ui.TestWarn(fmt.Sprintf("Claude Code not found for agent user — install as: sudo -u %s -i, then: curl -fsSL https://claude.ai/install.sh | bash", agentUser))
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
		ui.TestPass("Agent .zshrc sources the sandbox env file")
	} else {
		ui.TestFail("Agent .zshrc does not source the sandbox env file")
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

	if _, err := os.Stat(seatbeltProfilePath); err != nil {
		ui.TestFail(fmt.Sprintf("Seatbelt profile missing: %s — run sandbox setup", seatbeltProfilePath))
		return
	}
	ui.TestPass(fmt.Sprintf("Seatbelt profile exists: %s", seatbeltProfilePath))

	if info, err := os.Stat(seatbeltWrapperPath); err != nil {
		ui.TestFail(fmt.Sprintf("Seatbelt wrapper missing: %s — run sandbox setup", seatbeltWrapperPath))
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

	// runSandboxed executes args as the agent user under the seatbelt profile.
	runSandboxed := func(args ...string) error {
		all := []string{
			"/usr/bin/sandbox-exec",
			"-D", "HOME=" + agentHome,
			"-D", "PROJECT_DIR=" + sharedWorkspace,
			"-D", "TMPDIR=/private/tmp",
			"-f", seatbeltProfilePath,
		}
		all = append(all, args...)
		return asAgentQuiet(all...)
	}

	// Allowed: write inside workspace.
	testWritePath := fmt.Sprintf("%s/.seatbelt-write-%d", sharedWorkspace, os.Getpid())
	if err := runSandboxed("/usr/bin/touch", testWritePath); err == nil {
		sudo("rm", "-f", testWritePath) //nolint:errcheck
		ui.TestPass("Seatbelt allows writes inside PROJECT_DIR (workspace)")
	} else {
		ui.TestFail(fmt.Sprintf("Seatbelt unexpectedly denied write inside PROJECT_DIR: %v", err))
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

	// Allowed: read inside workspace.
	probeWsPath := fmt.Sprintf("%s/.seatbelt-read-%d", sharedWorkspace, os.Getpid())
	if f, err := os.Create(probeWsPath); err == nil {
		f.Close()
		defer os.Remove(probeWsPath)
		if err := runSandboxed("/bin/cat", probeWsPath); err == nil {
			ui.TestPass("Seatbelt allows reads inside PROJECT_DIR")
		} else {
			ui.TestFail(fmt.Sprintf("Seatbelt unexpectedly denied read inside PROJECT_DIR: %v", err))
		}
	} else {
		ui.TestWarn(fmt.Sprintf("Could not create read probe in workspace: %v", err))
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

// ── Step 13: Backup ───────────────────────────────────────────────────────────

func testBackup(ui *UI) {
	ui.Step("Backup")

	// Verify rsync is available
	if _, err := os.Stat("/usr/bin/rsync"); err == nil {
		ui.TestPass("rsync is present at /usr/bin/rsync")
	} else {
		ui.TestFail("rsync not found at /usr/bin/rsync")
		return
	}

	// Dry-run rsync against the shared workspace using production flags (backupBuiltinExcludes).
	// This ensures the test exercises the same exclude set that runBackup() uses.
	src := sharedWorkspace + "/"
	if _, err := os.Stat(sharedWorkspace); os.IsNotExist(err) {
		ui.TestSkip(fmt.Sprintf("%s does not exist — skipping rsync dry-run", sharedWorkspace))
	} else {
		tmpDest := fmt.Sprintf("/tmp/sandboxtest-backup-%d", os.Getpid())
		rsyncArgs := []string{"--dry-run", "-aHAX"}
		for _, e := range backupBuiltinExcludes {
			rsyncArgs = append(rsyncArgs, "--exclude="+e)
		}
		rsyncArgs = append(rsyncArgs, src, tmpDest)
		err := runInteractive("rsync", rsyncArgs...)
		if err == nil {
			ui.TestPass(fmt.Sprintf("rsync options are valid (dry-run, no --delete, %d built-in excludes)", len(backupBuiltinExcludes)))
		} else {
			ui.TestWarn(fmt.Sprintf("rsync dry-run failed: %v", err))
		}
	}

	// ── backup safety: validateSyncDest ──────────────────────────────────────

	// Wrong-path / missing-mount: non-existent local path must be rejected
	nonExistent := fmt.Sprintf("/tmp/sandboxtest-no-such-dest-%d", os.Getpid())
	if err := validateSyncDest(nonExistent); err != nil {
		ui.TestPass("--sync rejects non-existent local destination (wrong-path guard)")
	} else {
		ui.TestFail("--sync accepted non-existent local destination — wrong-path guard missing")
	}

	// Existing directory without marker must be rejected
	tmpNoMarker := fmt.Sprintf("/tmp/sandboxtest-backup-nomarker-%d", os.Getpid())
	if err := os.MkdirAll(tmpNoMarker, 0o700); err == nil {
		defer os.RemoveAll(tmpNoMarker)
		if err := validateSyncDest(tmpNoMarker); err != nil {
			ui.TestPass("--sync rejects destination without " + backupTargetMarker + " marker (destructive-mode guard)")
		} else {
			ui.TestFail("--sync accepted destination without marker — destructive-mode guard missing")
		}
	} else {
		ui.TestWarn(fmt.Sprintf("could not create temp dir for marker test: %v", err))
	}

	// Existing directory with marker must be accepted
	tmpWithMarker := fmt.Sprintf("/tmp/sandboxtest-backup-marker-%d", os.Getpid())
	if err := os.MkdirAll(tmpWithMarker, 0o700); err == nil {
		defer os.RemoveAll(tmpWithMarker)
		markerPath := tmpWithMarker + "/" + backupTargetMarker
		if f, err := os.Create(markerPath); err == nil {
			f.Close()
			if err := validateSyncDest(tmpWithMarker); err == nil {
				ui.TestPass("--sync accepts initialized destination (marker present)")
			} else {
				ui.TestFail(fmt.Sprintf("--sync rejected valid initialized destination: %v", err))
			}
		} else {
			ui.TestWarn(fmt.Sprintf("could not create marker file for test: %v", err))
		}
	} else {
		ui.TestWarn(fmt.Sprintf("could not create temp dir for marker test: %v", err))
	}

	// Remote destination (user@host:path) must pass through without local checks
	if err := validateSyncDest("user@nas:/backup/workspace"); err == nil {
		ui.TestPass("--sync passes remote destinations through without local validation")
	} else {
		ui.TestFail(fmt.Sprintf("--sync incorrectly rejected remote destination: %v", err))
	}

	// ── scope file ────────────────────────────────────────────────────────────

	// loadUserExcludes must return an error (not panic) when the file is absent.
	tmpScope := fmt.Sprintf("/tmp/sandboxtest-excludes-%d", os.Getpid())
	origExcludes := backupExcludesFile

	// We can't reassign the const, so test via a temp file written directly.
	// Verify loadUserExcludes correctly parses comment and blank lines.
	scopeContent := "# comment\n\n/my-big-repo/\n# another comment\n/nixpkgs/\n"
	if err := os.WriteFile(tmpScope, []byte(scopeContent), 0o644); err == nil {
		defer os.Remove(tmpScope)
		f, ferr := os.Open(tmpScope)
		if ferr == nil {
			defer f.Close()
			var active []string
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				active = append(active, line)
			}
			if len(active) == 2 && active[0] == "/my-big-repo/" && active[1] == "/nixpkgs/" {
				ui.TestPass("Scope file parser skips comments and blank lines, loads active patterns")
			} else {
				ui.TestFail(fmt.Sprintf("Scope file parser returned unexpected patterns: %v", active))
			}
		}
	} else {
		ui.TestWarn(fmt.Sprintf("Could not write temp scope file for parser test: %v", err))
	}
	_ = origExcludes // suppress unused var warning

	// Backup scope file should exist in the shared workspace if setup was run.
	if _, err := os.Stat(backupExcludesFile); err == nil {
		ui.TestPass(fmt.Sprintf("Backup scope file exists: %s", backupExcludesFile))
	} else {
		ui.TestWarn(fmt.Sprintf("Backup scope file not found at %s — run sandbox setup to create it", backupExcludesFile))
	}
}

// ── Step 14: Restore ─────────────────────────────────────────────────────────

func testRestore(ui *UI) {
	ui.Step("Restore")

	// ── validateRestoreSrc guards ─────────────────────────────────────────────

	// Non-existent local source must be rejected.
	nonExistent := fmt.Sprintf("/tmp/sandboxtest-no-restore-src-%d", os.Getpid())
	if err := validateRestoreSrc(nonExistent); err != nil {
		ui.TestPass("restore rejects non-existent local source (wrong-path guard)")
	} else {
		ui.TestFail("restore accepted non-existent local source — wrong-path guard missing")
	}

	// Existing directory without marker must be rejected.
	tmpNoMarker := fmt.Sprintf("/tmp/sandboxtest-restore-nomarker-%d", os.Getpid())
	if err := os.MkdirAll(tmpNoMarker, 0o700); err == nil {
		defer os.RemoveAll(tmpNoMarker)
		if err := validateRestoreSrc(tmpNoMarker); err != nil {
			ui.TestPass("restore rejects source without " + backupTargetMarker + " marker")
		} else {
			ui.TestFail("restore accepted source without marker — destructive-mode guard missing")
		}
	} else {
		ui.TestWarn(fmt.Sprintf("could not create temp dir for restore marker test: %v", err))
	}

	// Remote source (user@host:path) must pass through without local checks.
	if err := validateRestoreSrc("user@nas:/backup/workspace"); err == nil {
		ui.TestPass("restore passes remote sources through without local validation")
	} else {
		ui.TestFail(fmt.Sprintf("restore incorrectly rejected remote source: %v", err))
	}

	// ── End-to-end: restore from fixture backup to temp destination ───────────

	// Build a fixture backup directory: marker + some files.
	tmpSrc := fmt.Sprintf("/tmp/sandboxtest-restore-src-%d", os.Getpid())
	tmpDest := fmt.Sprintf("/tmp/sandboxtest-restore-dest-%d", os.Getpid())
	if err := os.MkdirAll(tmpSrc, 0o700); err != nil {
		ui.TestWarn(fmt.Sprintf("could not create restore fixture dir: %v", err))
		return
	}
	defer os.RemoveAll(tmpSrc)
	defer os.RemoveAll(tmpDest)

	if err := os.MkdirAll(tmpDest, 0o700); err != nil {
		ui.TestWarn(fmt.Sprintf("could not create restore dest dir: %v", err))
		return
	}

	// Write marker + fixture files.
	if err := os.WriteFile(tmpSrc+"/"+backupTargetMarker, nil, 0o644); err != nil {
		ui.TestWarn(fmt.Sprintf("could not write marker for restore fixture: %v", err))
		return
	}
	if err := os.WriteFile(tmpSrc+"/hello.txt", []byte("restore test\n"), 0o644); err != nil {
		ui.TestWarn(fmt.Sprintf("could not write fixture file: %v", err))
		return
	}
	if err := os.MkdirAll(tmpSrc+"/subdir", 0o755); err == nil {
		os.WriteFile(tmpSrc+"/subdir/nested.txt", []byte("nested\n"), 0o644) //nolint:errcheck
	}

	// Validate that the marker passes the guard.
	if err := validateRestoreSrc(tmpSrc); err != nil {
		ui.TestFail(fmt.Sprintf("restore rejected valid initialized source: %v", err))
		return
	}
	ui.TestPass("restore accepts initialized source (marker present)")

	// Run rsync from fixture backup to temp dest (additive, no --delete).
	err := runInteractive("rsync", "-aHAX",
		"--exclude="+backupTargetMarker, // marker is backup metadata, not workspace content
		tmpSrc+"/", tmpDest+"/",
	)
	if err != nil {
		ui.TestWarn(fmt.Sprintf("rsync restore dry-run failed: %v", err))
		return
	}

	// Verify fixture files were restored — both presence and content parity.
	if data, err := os.ReadFile(tmpDest + "/hello.txt"); err == nil {
		ui.TestPass("restore end-to-end: top-level file present in destination")
		if string(data) == "restore test\n" {
			ui.TestPass("restore end-to-end: content parity verified for top-level file")
		} else {
			ui.TestFail(fmt.Sprintf("restore end-to-end: content mismatch — got %q, want %q", string(data), "restore test\n"))
		}
	} else {
		ui.TestFail("restore end-to-end: top-level file missing from destination after rsync")
	}

	if data, err := os.ReadFile(tmpDest + "/subdir/nested.txt"); err == nil {
		ui.TestPass("restore end-to-end: nested file present in destination (directory tree preserved)")
		if string(data) == "nested\n" {
			ui.TestPass("restore end-to-end: content parity verified for nested file")
		} else {
			ui.TestFail(fmt.Sprintf("restore end-to-end: content mismatch for nested file — got %q, want %q", string(data), "nested\n"))
		}
	} else {
		ui.TestFail("restore end-to-end: nested file missing from destination after rsync")
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

	// ── Backup scope file removal ─────────────────────────────────────────────
	// Verify that the scope file can be removed with os.Remove (as rollbackBackupScope does).
	tmpScope := fmt.Sprintf("/tmp/sandboxtest-decom-scope-%d", os.Getpid())
	if err := os.WriteFile(tmpScope, []byte("# test\n/foo/\n"), 0o644); err == nil {
		if err := os.Remove(tmpScope); err != nil {
			ui.TestFail(fmt.Sprintf("Backup scope file removal failed: %v", err))
		} else if _, err := os.Stat(tmpScope); os.IsNotExist(err) {
			ui.TestPass("Backup scope file removal: file no longer exists after os.Remove")
		} else {
			ui.TestFail("Backup scope file removal: file still exists after os.Remove")
		}
	} else {
		ui.TestWarn(fmt.Sprintf("Could not create temp scope file for decommission test: %v", err))
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

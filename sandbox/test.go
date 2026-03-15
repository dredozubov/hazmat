package main

import (
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
	testBackup(ui)

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
		wantAllow          bool
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

// ── Step 11: Backup ───────────────────────────────────────────────────────────

func testBackup(ui *UI) {
	ui.Step("Backup")

	// Verify rsync is available
	if _, err := os.Stat("/usr/bin/rsync"); err == nil {
		ui.TestPass("rsync is present at /usr/bin/rsync")
	} else {
		ui.TestFail("rsync not found at /usr/bin/rsync")
		return
	}

	// Dry-run rsync if ~/workspace exists, to confirm flags are valid
	src := os.Getenv("HOME") + "/workspace/"
	if _, err := os.Stat(strings.TrimSuffix(src, "/")); os.IsNotExist(err) {
		ui.TestSkip("~/workspace does not exist — skipping rsync dry-run")
		return
	}

	tmpDest := fmt.Sprintf("/tmp/sandboxtest-backup-%d", os.Getpid())
	err := runInteractive("rsync", "--dry-run", "-aHAX",
		"--exclude=node_modules/", "--exclude=.venv/", "--exclude=__pycache__/",
		"--exclude=.next/", "--exclude=dist/", "--exclude=build/",
		src, tmpDest,
	)
	if err == nil {
		ui.TestPass("rsync options are valid (dry-run succeeded)")
	} else {
		ui.TestWarn(fmt.Sprintf("rsync dry-run failed: %v", err))
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

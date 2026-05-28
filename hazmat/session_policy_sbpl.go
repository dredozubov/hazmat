package main

import (
	"fmt"
	"strings"
)

// compileDarwinSBPL produces a per-session Seatbelt (SBPL) policy with all
// filesystem boundaries embedded as literal absolute paths. This makes --read
// an actual OS-level boundary rather than an advisory env var: only the listed
// directories receive read access beyond the project.
//
// Policy structure:
//   - PROJECT_DIR gets read+write
//   - Each ReadDirs entry gets read-only (skipped if covered by ProjectDir,
//     a WriteDirs entry, or another ReadDirs entry)
//   - Each WriteDirs entry gets read+write (skipped if covered by ProjectDir
//     or another WriteDirs entry)
//   - Agent home subtrees, system libraries, tmp, terminal, mach, and network
//     rules are identical to the former static profile
//   - Credential directories are denied last (last-match wins in SBPL)
func compileDarwinSBPL(policy nativeSessionPolicy) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	projectDir := policy.ProjectPath()
	readDirs := policy.ReadOnlyPaths()
	writeDirs := policy.ReadWritePaths()
	home := policy.AgentHome.Path
	tempDir := policy.Temp.Path

	w(";; Claude Code runtime seatbelt policy.\n")
	w(";; Generated per-session by hazmat — do not edit manually.\n\n")
	w("(version 1)\n(deny default)\n\n")

	w(";; ── Process execution ──────────────────────────────────────────────────────\n")
	for _, p := range []string{"/usr/bin", "/bin", "/usr/local", "/opt/homebrew", "/Library/Developer/CommandLineTools", home} {
		w("(allow process-exec (subpath %q))\n", p)
	}
	for _, dir := range readDirs {
		w("(allow process-exec (subpath %q))\n", dir)
	}
	for _, dir := range writeDirs {
		w("(allow process-exec (subpath %q))\n", dir)
	}
	w("(allow process-exec (subpath %q))\n", projectDir)
	w("(allow process-fork)\n")
	w("(allow process-info* (target same-sandbox))\n")
	w("(allow signal (target same-sandbox))\n\n")

	w(";; ── System info (V8 reads CPU/memory via sysctl at startup) ────────────\n")
	w("(allow sysctl-read)\n\n")

	w(";; ── System libraries (required by Node.js / dyld) ──────────────────────\n")
	w(";; Path traversal literals for realpath() and symlink resolution.\n")
	w(";; /var → /private/var (DNS resolv.conf), /tmp → /private/tmp.\n")
	w(";; /opt is the parent of /opt/homebrew — tools symlinked via /opt/homebrew/bin\n")
	w(";; that point to absolute paths elsewhere need realpath() to lstat /opt itself.\n")
	for _, p := range []string{"/", "/private", "/var", "/var/select", "/tmp", "/etc", "/usr", "/opt", "/System", "/Library", "/Library/Developer"} {
		w("(allow file-read* (literal %q))\n", p)
	}
	for _, p := range []string{"/usr/lib", "/usr/share", "/System/Library", "/Library/Frameworks", "/Library/Developer/CommandLineTools", "/private/etc", "/private/var/select", "/private/var/db/timezone"} {
		w("(allow file-read* (subpath %q))\n", p)
	}
	if policy.MacOSNativeTLS {
		// Rust + native-tls harnesses (codex) walk the macOS Security framework
		// trust store; the others ship their own CA bundle and don't need this.
		for _, p := range []string{"/System/Cryptexes", "/Library/Keychains"} {
			w("(allow file-read* (subpath %q))\n", p)
		}
		w("(allow file-read* (literal \"/Library/Preferences/com.apple.security.plist\"))\n")
	}
	for _, p := range []string{"/dev/urandom", "/dev/null", "/dev/zero"} {
		w("(allow file-read* (literal %q))\n", p)
	}
	w("(allow file-write* (literal \"/dev/null\"))\n")
	// /usr/bin and /bin: already in process-exec; file-read is needed so
	// exec.LookPath can scan the directory (e.g., CGO looking for "cc").
	for _, p := range []string{"/usr/bin", "/bin", "/usr/local", "/opt/homebrew"} {
		w("(allow file-read* (subpath %q))\n", p)
	}
	w("\n")

	if ancestors := policy.AncestorMetadataDirs(); len(ancestors) > 0 {
		w(";; ── Ancestor metadata (stat only, no content) ────────────────────────────\n")
		w(";; Required for path canonicalization by git, readlink, etc.\n")
		for _, p := range ancestors {
			w("(allow file-read-metadata (literal %q))\n", p)
		}
		w("\n")
	}

	if pending := policy.EffectiveReadOnlyDirs(); len(pending) > 0 {
		w(";; ── Read-only directories ──────────────────────────────────────────────────\n")
		for _, dir := range pending {
			w("(allow file-read* (subpath %q))\n", dir)
		}
		w("\n")
	}

	if pending := policy.EffectiveWritableDirs(); len(pending) > 0 {
		w(";; ── Read-write extensions ────────────────────────────────────────────────\n")
		for _, dir := range pending {
			w("(allow file-read* file-write* (subpath %q))\n", dir)
		}
		w("\n")
	}

	w(";; ── Active project — full read/write ──────────────────────────────────────\n")
	w("(allow file-read* (subpath %q))\n", projectDir)
	w("(allow file-write* (subpath %q))\n\n", projectDir)

	w(";; ── Agent home — broad read/write, credential dirs denied below ───────────\n")
	w(";; A single subpath rule replaces individual subdirectory allows.\n")
	w(";; Claude Code, Node.js, git, and shell rc files all live here.\n")
	w(";; Credential directories are denied at the end (last-match-wins).\n")
	w("(allow file-read* file-write* (subpath %q))\n\n", home)

	w(";; ── Session temp directory ───────────────────────────────────────────────────\n")
	w(";; Runtime TMPDIR points at this agent-owned per-session root. Do not grant\n")
	w(";; broad /private/tmp or /private/var/folders read/write/exec here.\n")
	w("(allow file-read* file-write* (subpath %q))\n", tempDir)
	w("(allow process-exec (subpath %q))\n\n", tempDir)

	w(";; ── DNS resolver + system state ───────────────────────────────────────────\n")
	w(";; resolv.conf is a symlink to /private/var/run/resolv.conf.\n")
	w("(allow file-read* (subpath \"/private/var/run\"))\n")
	w(";; xcode-select stores the active developer dir as a symlink here.\n")
	w(";; CGO and clang read it to locate the SDK.\n")
	w("(allow file-read* (literal \"/private/var/db/xcode_select_link\"))\n\n")

	w(";; Temp parent metadata only; session temp itself is agent-home scoped.\n")
	for _, p := range []string{"/private/tmp", "/private/var/folders"} {
		w("(allow file-read-metadata (literal %q))\n", p)
	}
	w("\n")

	w(";; ── Terminal support (Node.js requires these) ──────────────────────────────\n")
	w("(allow pseudo-tty)\n")
	w("(allow file-ioctl)\n")
	w("(allow file-read* file-write* (literal \"/dev/tty\"))\n")
	w("(allow file-read* file-write* (literal \"/dev/ptmx\"))\n")
	w("(allow file-read* file-write* (regex #\"/dev/ttys[0-9]+\"))\n\n")

	w(";; ── Mach services (base — needed by every harness) ─────────────────────────\n")
	baseMachServices := []string{
		"com.apple.system.logger",
		"com.apple.CoreServices.coreservicesd",
		"com.apple.system.notification_center",
		"com.apple.trustd",                                // TLS certificate verification (Go, curl, Python, etc.)
		"com.apple.system.opendirectoryd.api",             // user/group directory lookups
		"com.apple.system.opendirectoryd.libinfo",         // getpwuid/getgrnam via libinfo (needed by git, id, etc.)
		"com.apple.system.DirectoryService.libinfo_v1",    // getpwuid/getgrnam legacy path
		"com.apple.system.DirectoryService.membership_v1", // group membership checks
		"com.apple.pboard",                                // pasteboard (clipboard read/write — paste into Claude Code and copy out)
	}
	if policy.Network.Mode != sessionNetworkNone {
		baseMachServices = append(baseMachServices, "com.apple.mDNSResponder")
	}
	for _, svc := range baseMachServices {
		w("(allow mach-lookup (global-name %q))\n", svc)
	}
	if policy.MacOSNativeTLS {
		w(";; Mach services for harnesses that use macOS Security framework directly:\n")
		for _, svc := range []string{
			"com.apple.SystemConfiguration.configd", // SCDynamicStoreCreate (Rust reqwest proxy detection)
			"com.apple.trustd.agent",                // per-user trust agent (security-framework SecTrustEvaluate)
			"com.apple.SecurityServer",              // Security framework XPC engine — does the actual SecTrust* evaluation
		} {
			w("(allow mach-lookup (global-name %q))\n", svc)
		}
	}
	w("(allow mach-host*)\n\n")

	w(";; ── Pasteboard shared memory (clipboard copy out of session) ───────────────\n")
	w(";; mach-lookup for com.apple.pboard covers the IPC handshake; the actual\n")
	w(";; clipboard data is transferred via POSIX shared memory segments named\n")
	w(";; com.apple.pasteboard.<N>.  Without these rules pbcopy silently fails.\n")
	w("(allow ipc-posix-shm-read-data    (ipc-posix-name-regex #\"^com\\.apple\\.pasteboard\\.\"))\n")
	w("(allow ipc-posix-shm-write-data   (ipc-posix-name-regex #\"^com\\.apple\\.pasteboard\\.\"))\n")
	w("(allow ipc-posix-shm-write-create (ipc-posix-name-regex #\"^com\\.apple\\.pasteboard\\.\"))\n\n")

	if policy.MacOSNativeTLS {
		w(";; ── System notification center shared memory (Rust reqwest / Security ────\n")
		w(";; framework subscribes to libnotify events during TLS trust evaluation;\n")
		w(";; without apple.shm.notification_center the cert chain load hangs.)\n")
		w("(allow ipc-posix-shm-read-data (ipc-posix-name %q))\n\n", "apple.shm.notification_center")

		w(";; ── Kernel control socket (AF_SYSTEM / SYSPROTO_CONTROL) ──────────────────\n")
		w(";; SCDynamicStore's data channel (after the com.apple.SystemConfiguration\n")
		w(";; mach-lookup handshake) uses AF_SYSTEM sockets. Rust reqwest's proxy\n")
		w(";; detection blocks indefinitely without this; codex chat never round-trips.\n")
		w("(allow system-socket (require-all (socket-domain 32) (socket-protocol 2)))\n\n")
	}

	w(";; ── Network ───────────────────────────────────────────────────────────────\n")
	if policy.Network.Mode == sessionNetworkNone {
		w(";; Outbound IPv4, IPv6, and DNS are denied by default for this session.\n")
	} else {
		w("(allow network-outbound)\n")
	}
	w("(allow network-inbound (local tcp \"*:*\"))\n\n")

	w(";; ── Writable roots (re-assert after all read-only rules) ───────────────────\n")
	w(";; SBPL is last-match-wins. When a read-only -R directory is a parent of\n")
	w(";; a writable root (e.g. -R ~/workspace with project ~/workspace/foo),\n")
	w(";; the broad file-read* rule must not suppress explicit write access.\n")
	w(";; Re-asserting file-write* here guarantees it is the last matching allow\n")
	w(";; for any write operation targeting an explicit writable root.\n")
	w("(allow file-read* file-write* (subpath %q))\n\n", projectDir)
	for _, dir := range writeDirs {
		if isWithinDir(projectDir, dir) {
			continue
		}
		w("(allow file-read* file-write* (subpath %q))\n", dir)
	}
	if len(writeDirs) > 0 {
		w("\n")
	}

	w(";; ── DENY host temp capability sockets ─────────────────────────────────────\n")
	w(";; These override broad user-granted temp roots. They are local control\n")
	w(";; sockets, not ordinary build artifacts.\n")
	for _, p := range []string{"/tmp/codex-browser-use", "/tmp/codex-ipc", "/private/tmp/codex-browser-use", "/private/tmp/codex-ipc"} {
		w("(deny file-read* file-write* (subpath %q))\n", p)
		w("(deny process-exec (subpath %q))\n", p)
	}
	w("(deny file-read* file-write* (regex #\"^/private/var/folders/.*/T/codex-ipc(/|$)\"))\n")
	w("(deny process-exec (regex #\"^/private/var/folders/.*/T/codex-ipc(/|$)\"))\n\n")

	w(";; ── DENY sensitive credential directories ──────────────────────────────────\n")
	w(";; These appear last so they override the broad allows above (last match wins).\n")
	w(";; Both file-read* (exfiltration) and file-write* (planting) are denied.\n")
	for _, path := range policy.CredentialDenyPaths() {
		w("(deny file-read* file-write* (subpath %q))\n", path)
	}

	if policy.MacOSNativeTLS {
		w(";; ── Re-allow agent's empty login keychain (post-deny override) ────────────\n")
		w(";; The broader %s/Library/Keychains deny stays. macOS Security framework on\n", home)
		w(";; Sequoia+ refuses TLS trust evaluation when no user keychain is loadable\n")
		w(";; (errSecNoSuchKeychain -25291). Allowing read of the (empty) login keychain\n")
		w(";; lets Rust reqwest's native-tls path complete trust setup using system roots.\n")
		w(";; The directory metadata allow lets Security stat() the keychain dir before\n")
		w(";; opening the whitelisted keychain DB files inside it.\n")
		w("(allow file-read-metadata (literal %q))\n", home+"/Library/Keychains")
		w("(allow file-read* (literal %q))\n", home+"/Library/Keychains/login.keychain-db")
		w("(allow file-read* (literal %q))\n", home+"/Library/Keychains/login.keychain-db-shm")
		w("(allow file-read* (literal %q))\n", home+"/Library/Keychains/login.keychain-db-wal")
	}

	return b.String()
}

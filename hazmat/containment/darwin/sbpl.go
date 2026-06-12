// Package darwin compiles Hazmat's backend-neutral containment contract into a
// Darwin Seatbelt (SBPL) policy. It does not launch processes or manage policy
// files.
package darwin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hazmat/containment"
	"hazmat/sessionmeta"
)

// CompileOptions supplies Darwin-specific policy gates that are not part of the
// backend-neutral containment contract.
type CompileOptions struct {
	MacOSSecurityFramework   bool
	MacOSAgentKeychainAccess bool
	RuntimeTempDirs          []string
}

// Compile produces a per-session Seatbelt (SBPL) policy with all
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
//   - Explicit agent-home state/tooling subtrees, system libraries, tmp,
//     terminal, mach, and network rules are emitted by the static profile
//   - Credential directories are denied last (last-match wins in SBPL)
func Compile(contract containment.Contract, opts CompileOptions) (string, error) {
	if err := contract.Validate(); err != nil {
		return "", fmt.Errorf("invalid containment contract: %w", err)
	}

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	projectDir := contract.ProjectPath()
	readDirs := contract.ReadOnlyPaths()
	writeDirs := contract.ReadWritePaths()
	home := contract.AgentHome.Path
	tempDir := contract.Temp.Path

	w(";; Claude Code runtime seatbelt policy.\n")
	w(";; Generated per-session by hazmat — do not edit manually.\n\n")
	w("(version 1)\n(deny default)\n\n")

	w(";; ── Process execution ──────────────────────────────────────────────────────\n")
	for _, p := range []string{"/usr/bin", "/bin", "/usr/local", "/opt/homebrew", "/Library/Developer/CommandLineTools"} {
		w("(allow process-exec (subpath %q))\n", p)
	}
	for _, p := range agentHomeExecutableSubpaths(home) {
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
	if opts.MacOSSecurityFramework {
		// Some harnesses walk the macOS Security framework trust/keychain surface
		// during startup or TLS setup. Keep this gated so other harnesses stay on
		// the smaller base policy.
		for _, p := range []string{"/System/Cryptexes", "/System/Volumes/Preboot/Cryptexes", "/Library/Keychains", "/private/var/db/mds/messages"} {
			w("(allow file-read* (subpath %q))\n", p)
		}
		for _, p := range []string{
			"/Library/Preferences/com.apple.security.plist",
			"/Library/Preferences/.GlobalPreferences.plist",
			"/Library/Preferences/com.apple.networkd.plist",
		} {
			w("(allow file-read* (literal %q))\n", p)
		}
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

	if ancestors := contract.AncestorMetadataDirs(); len(ancestors) > 0 {
		w(";; ── Ancestor metadata (stat only, no content) ────────────────────────────\n")
		w(";; Required for path canonicalization by git, readlink, etc.\n")
		for _, p := range ancestors {
			w("(allow file-read-metadata (literal %q))\n", p)
		}
		w("\n")
	}

	if pending := contract.EffectiveReadOnlyDirs(); len(pending) > 0 {
		w(";; ── Read-only directories ──────────────────────────────────────────────────\n")
		for _, dir := range pending {
			w("(allow file-read* (subpath %q))\n", dir)
		}
		w("\n")
	}

	if pending := contract.EffectiveWritableDirs(); len(pending) > 0 {
		w(";; ── Read-write extensions ────────────────────────────────────────────────\n")
		for _, dir := range pending {
			w("(allow file-read* file-write* (subpath %q))\n", dir)
		}
		w("\n")
	}

	w(";; ── Active project — full read/write ──────────────────────────────────────\n")
	w("(allow file-read* (subpath %q))\n", projectDir)
	w("(allow file-write* (subpath %q))\n\n", projectDir)

	w(";; ── Agent home — explicit durable state/tooling paths ─────────────────────\n")
	w(";; HOME stays %s, but the policy does not grant the whole home.\n", home)
	w(";; Credential directories are denied at the end (last-match-wins).\n")
	for _, dir := range agentHomeWritableSubpaths(home) {
		w("(allow file-read* file-write* (subpath %q))\n", dir)
	}
	for _, file := range agentHomeWritableFiles(home) {
		w("(allow file-read* file-write* (literal %q))\n", file)
	}
	w("\n")

	w(";; ── Session temp directory ───────────────────────────────────────────────────\n")
	w(";; Runtime TMPDIR points at this agent-owned per-session root. Do not grant\n")
	w(";; broad /private/tmp or /private/var/folders read/write/exec here.\n")
	w("(allow file-read* file-write* (subpath %q))\n", tempDir)
	w("(allow process-exec (subpath %q))\n\n", tempDir)
	if len(opts.RuntimeTempDirs) > 0 {
		w(";; ── Harness runtime temp directories ──────────────────────────────────────\n")
		w(";; Narrow agent-owned runtime roots that packaged harnesses probe directly.\n")
		for _, dir := range opts.RuntimeTempDirs {
			w("(allow file-read* file-write* (subpath %q))\n", dir)
		}
		w("\n")
	}

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
	if contract.Network.Mode != sessionmeta.NetworkNone {
		baseMachServices = append(baseMachServices, "com.apple.mDNSResponder")
	}
	for _, svc := range baseMachServices {
		w("(allow mach-lookup (global-name %q))\n", svc)
	}
	if opts.MacOSSecurityFramework || opts.MacOSAgentKeychainAccess {
		w(";; Mach services for harnesses that use macOS Security framework directly:\n")
		services := []string{
			"com.apple.SecurityServer", // Security framework XPC engine — does the actual SecTrust* / Keychain work
			"com.apple.securityd",      // Keychain broker on current macOS releases
			"com.apple.securityd.xpc",  // security(1) keychain/trust settings helper path
		}
		if opts.MacOSSecurityFramework {
			services = append(services,
				"com.apple.SystemConfiguration.configd", // SCDynamicStoreCreate (Rust reqwest proxy detection)
				"com.apple.trustd.agent",                // per-user trust agent (security-framework SecTrustEvaluate)
			)
		}
		for _, svc := range services {
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

	if opts.MacOSSecurityFramework {
		w(";; ── System notification center shared memory (Security framework) ────────\n")
		w(";; framework subscribes to libnotify events during TLS trust evaluation;\n")
		w(";; without apple.shm.notification_center the cert chain load hangs.)\n")
		w("(allow ipc-posix-shm-read-data (ipc-posix-name %q))\n\n", "apple.shm.notification_center")
		w("(allow ipc-posix-shm-read-data    (ipc-posix-name %q))\n", "com.apple.AppleDatabaseChanged")
		w("(allow ipc-posix-shm-write-data   (ipc-posix-name %q))\n", "com.apple.AppleDatabaseChanged")
		w("(allow ipc-posix-shm-write-create (ipc-posix-name %q))\n\n", "com.apple.AppleDatabaseChanged")

		w(";; ── Kernel control socket (AF_SYSTEM / SYSPROTO_CONTROL) ──────────────────\n")
		w(";; SCDynamicStore's data channel (after the com.apple.SystemConfiguration\n")
		w(";; mach-lookup handshake) uses AF_SYSTEM sockets. Rust reqwest's proxy\n")
		w(";; detection blocks indefinitely without this; codex chat never round-trips.\n")
		w("(allow system-socket (require-all (socket-domain 32) (socket-protocol 2)))\n\n")
	}

	w(";; ── Network ───────────────────────────────────────────────────────────────\n")
	if contract.Network.Mode == sessionmeta.NetworkNone {
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
	w(";; These are the final broad credential boundary (last match wins).\n")
	w(";; Both file-read* (exfiltration) and file-write* (planting) are denied.\n")
	for _, path := range contract.CredentialDenyPaths() {
		w("(deny file-read* file-write* (subpath %q))\n", path)
	}

	if opts.MacOSSecurityFramework {
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
	if opts.MacOSAgentKeychainAccess {
		w(";; ── Re-allow Claude's agent login keychain (post-deny override) ──────────\n")
		w(";; Claude Code OAuth on newer releases may store/read auth via the agent\n")
		w(";; account login keychain. This grants only that managed agent keychain DB\n")
		w(";; and sidecar paths, after the broader Keychains credential deny.\n")
		w("(allow file-read-metadata (literal %q))\n", home+"/Library/Keychains")
		w("(allow file-read* file-write* (literal %q))\n", home+"/Library/Keychains/login.keychain-db")
		w("(allow file-read* file-write* (literal %q))\n", home+"/Library/Keychains/login.keychain-db-shm")
		w("(allow file-read* file-write* (literal %q))\n", home+"/Library/Keychains/login.keychain-db-wal")
	}

	return b.String(), nil
}

func isWithinDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func agentHomeWritableSubpaths(home string) []string {
	return agentHomeJoinAll(home, []string{
		".agents",
		".bun",
		".cache",
		".cargo",
		".claude",
		".codex",
		".config",
		".cursor",
		".deno",
		".gem",
		".gemini",
		".gradle",
		".hazmat",
		".ivy2",
		".local",
		".m2",
		".node-gyp",
		".npm",
		".opencode",
		".pub-cache",
		".qwen",
		".rustup",
		".sbt",
		".swiftpm",
		".terraform.d",
	})
}

func agentHomeWritableFiles(home string) []string {
	return agentHomeJoinAll(home, []string{
		".bash_profile",
		".bashrc",
		".gitconfig",
		".npmrc",
		".profile",
		".pypirc",
		".zprofile",
		".zshenv",
		".zshrc",
	})
}

func agentHomeExecutableSubpaths(home string) []string {
	return agentHomeJoinAll(home, []string{
		".bun/bin",
		".cargo/bin",
		".claude/hooks",
		".deno/bin",
		".gem",
		".local/bin",
		".local/lib",
		".opencode/bin",
		".pub-cache/bin",
	})
}

func agentHomeJoinAll(home string, rels []string) []string {
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		out = append(out, filepath.Join(home, rel))
	}
	return out
}

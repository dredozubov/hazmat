//go:build darwin

package main

func platformHostToolPaths() hostToolPaths {
	return hostToolPaths{
		// Absolute paths to macOS base-system utilities. Paths are stable
		// across every supported macOS version and match sudoers secure_path.
		sudo:      "/usr/bin/sudo",
		chmod:     "/bin/chmod",
		chown:     "/usr/sbin/chown",
		ls:        "/bin/ls",
		dscl:      "/usr/bin/dscl",
		pfctl:     "/sbin/pfctl",
		launchctl: "/bin/launchctl",
		uname:     "/usr/bin/uname",
		script:    "/usr/bin/script",
		diff:      "/usr/bin/diff",
		tee:       "/usr/bin/tee",

		// Homebrew installations win over Xcode Command Line Tools at
		// /usr/bin/git because the Xcode shim routes to Apple-shipped Git,
		// not to the user's Homebrew git. On macOS Sequoia, /usr/bin/git is
		// Apple Git ~2.50 while Homebrew git is ~2.53, so naive /usr/bin/git
		// substitution would silently downgrade functionality.
		gitAllowlistCandidates: []string{
			"/opt/homebrew/bin/git", // Apple Silicon Homebrew
			"/usr/local/bin/git",    // Intel Homebrew
			"/usr/bin/git",          // Xcode Command Line Tools shim (fallback)
		},
	}
}

//go:build !darwin

package main

func platformHostToolPaths() hostToolPaths {
	return hostToolPaths{
		sudo:  "/usr/bin/sudo",
		chmod: "/bin/chmod",
		chown: "/bin/chown",
		ls:    "/bin/ls",
		log:   "/usr/bin/log",

		// Darwin-only setup tools fail closed until a real Linux backend owns
		// account, firewall, and service-manager operations.
		dscl:      "/usr/bin/false",
		pfctl:     "/usr/bin/false",
		launchctl: "/usr/bin/false",

		uname:  "/usr/bin/uname",
		script: "/usr/bin/script",
		diff:   "/usr/bin/diff",
		tee:    "/usr/bin/tee",

		gitAllowlistCandidates: []string{
			"/usr/bin/git",
			"/usr/local/bin/git",
		},
	}
}

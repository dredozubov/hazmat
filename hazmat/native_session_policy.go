package main

import "hazmat/containment"

// nativeSessionPolicy is the backend-neutral containment contract for a native
// Hazmat session. Backends compile this contract into OS-specific enforcement:
// Darwin currently emits SBPL; Linux will compile the same shape into its own
// native primitives.
type nativeSessionPolicy struct {
	containment.Contract
	// MacOSNativeTLS is true when the harness running in this session uses the
	// macOS Security framework directly for TLS trust evaluation (Rust apps
	// linked against the security-framework crate, e.g. codex). Such harnesses
	// need a wider Security framework surface than Node-based harnesses that
	// ship their own CA bundle (claude, gemini) — see compileDarwinSBPL.
	MacOSNativeTLS bool

	// MacOSAgentKeychainAccess is true when a native session must talk to the
	// macOS Keychain for agent-owned harness auth. Keep this narrower than the
	// native TLS surface: it grants the Security framework broker plus the
	// agent login keychain files, not host keychains or trust roots.
	MacOSAgentKeychainAccess bool
}

// macOSNativeTLSHarnesses is the set of harness IDs that need the wider
// macOS Security framework surface (configd, /Library/Keychains, trustd.agent,
// SecurityServer, AF_SYSTEM kernel control sockets, etc.).
//
// As of 2026-04: only codex (Rust + reqwest with native-tls). Node-based
// harnesses (claude, gemini) and Bun-based ones (opencode) ship their own
// CA bundle and don't touch the Security framework, so they get the smaller
// base policy.
func harnessUsesMacOSNativeTLS(id HarnessID) bool {
	switch id {
	case HarnessCodex:
		return true
	default:
		return false
	}
}

func newNativeSessionPolicy(cfg sessionConfig) nativeSessionPolicy {
	return nativeSessionPolicy{
		Contract: containment.Contract{
			Project:       containment.PathGrant{Path: cfg.ProjectDir, Access: containment.PathReadWrite},
			ReadOnlyDirs:  containment.PathGrants(cfg.ReadDirs, containment.PathReadOnly),
			ReadWriteDirs: containment.PathGrants(cfg.WriteDirs, containment.PathReadWrite),
			AgentHome:     containment.AgentHomePolicy{Path: agentHome},
			Temp:          containment.TempPolicy{Path: sessionTempDirOrDefault(cfg.TempDir)},
			CredentialDenies: nativeCredentialDenies(
				agentHome,
				credentialDenySubs,
			),
			Network: containment.NetworkPolicy{Mode: normalizeSessionNetworkMode(cfg.NetworkMode)},
			Process: containment.ProcessPolicy{AllowFork: true},
		},
		MacOSNativeTLS:           harnessUsesMacOSNativeTLS(cfg.HarnessID),
		MacOSAgentKeychainAccess: cfg.ClaudeKeychainAccess,
	}
}

func sessionTempDirOrDefault(tempDir string) string {
	if tempDir != "" {
		return tempDir
	}
	return defaultAgentTmpDir
}

func nativeCredentialDenies(home string, subs []string) []containment.CredentialDeny {
	if len(subs) == 0 {
		return nil
	}
	denies := make([]containment.CredentialDeny, 0, len(subs))
	for _, sub := range subs {
		denies = append(denies, containment.CredentialDeny{Path: home + sub})
	}
	return denies
}

package hazmat

import "hazmat/containment"

// nativeSessionPolicy is the backend-neutral containment contract for a native
// Hazmat session. Backends compile this contract into OS-specific enforcement:
// Darwin currently emits SBPL; Linux will compile the same shape into its own
// native primitives.
type nativeSessionPolicy struct {
	containment.Contract
	// MacOSSecurityFramework is true when the harness touches Apple's Security
	// framework or the security(1) helper during startup or network setup. Such
	// harnesses need the wider trust/keychain/configd surface in compileDarwinSBPL.
	MacOSSecurityFramework bool
	// MacOSAgentKeychainAccess is true when a native session must write the
	// agent login keychain for harness auth. Keep this narrower than the general
	// Security framework surface: it grants only the agent login keychain files,
	// not host keychains or trust roots.
	MacOSAgentKeychainAccess bool
	// RuntimeTempDirs are narrow, harness-owned temp roots outside the normal
	// session TMPDIR that a packaged runtime still probes directly.
	RuntimeTempDirs []string
}

// macOSSecurityFrameworkHarnesses is the set of harness IDs that need the wider
// macOS Security framework surface (configd, /Library/Keychains, trustd.agent,
// SecurityServer, AF_SYSTEM kernel control sockets, etc.).
//
// Codex reaches this through Rust reqwest/native-tls. Claude Code 2.1.x also
// invokes Security framework helpers during print-mode startup, even though its
// main runtime is Node-based.
func harnessUsesMacOSSecurityFramework(id HarnessID) bool {
	switch id {
	case HarnessClaude, HarnessCodex:
		return true
	default:
		return false
	}
}

func runtimeTempDirsForHarness(id HarnessID) []string {
	switch id {
	case HarnessClaude:
		dir := claudeRuntimeTempDir()
		if dir == "" {
			return nil
		}
		return []string{dir}
	default:
		return nil
	}
}

func claudeRuntimeTempDir() string {
	agentInfo, err := lookupAgentUser()
	if err != nil || agentInfo == nil || agentInfo.Uid == "" {
		return ""
	}
	return "/private/tmp/claude-" + agentInfo.Uid
}

func newNativeSessionPolicy(cfg sessionConfig) nativeSessionPolicy {
	policy, err := buildNativeSessionPolicy(cfg)
	if err != nil {
		panic(err)
	}
	return policy
}

func buildNativeSessionPolicy(cfg sessionConfig) (nativeSessionPolicy, error) {
	floor, err := containment.NewCredentialFloor(agentHome, credentialDenySubs)
	if err != nil {
		return nativeSessionPolicy{}, err
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project:       containment.PathGrant{Path: cfg.ProjectDir, Access: containment.PathReadWrite},
		ReadOnlyDirs:  containment.PathGrants(cfg.ReadDirs, containment.PathReadOnly),
		ReadWriteDirs: containment.PathGrants(cfg.WriteDirs, containment.PathReadWrite),
		AgentHome:     containment.AgentHomePolicy{Path: agentHome},
		Temp:          containment.TempPolicy{Path: sessionTempDirOrDefault(cfg.TempDir)},
		Network:       containment.NetworkPolicy{Mode: normalizeSessionNetworkMode(cfg.NetworkMode)},
		Process:       containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		return nativeSessionPolicy{}, err
	}
	return nativeSessionPolicy{
		Contract:                 contract,
		MacOSSecurityFramework:   harnessUsesMacOSSecurityFramework(cfg.HarnessID),
		MacOSAgentKeychainAccess: cfg.ClaudeKeychainAccess,
		RuntimeTempDirs:          runtimeTempDirsForHarness(cfg.HarnessID),
	}, nil
}

func sessionTempDirOrDefault(tempDir string) string {
	if tempDir != "" {
		return tempDir
	}
	return defaultAgentTmpDir
}

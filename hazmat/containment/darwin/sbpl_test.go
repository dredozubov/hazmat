package darwin

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hazmat/containment"
	"hazmat/pathpolicy"
	"hazmat/sessionmeta"
)

var updateGoldenBaselines = flag.Bool("update-golden", false, "update golden baseline files")

func TestCompileBuildsSeatbeltPolicy(t *testing.T) {
	policy, err := Compile(testContract(t), CompileOptions{MacOSSecurityFramework: true})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, want := range []string{
		`(allow file-read* (subpath "/workspace/reference"))`,
		`(allow file-read* file-write* (subpath "/workspace/cache"))`,
		`(allow file-read* file-write* (subpath "/home/agent/.config"))`,
		`(allow file-read* file-write* (literal "/home/agent/.zshrc"))`,
		`(allow process-exec (subpath "/home/agent/.local/bin"))`,
		`(deny file-read* file-write* (subpath "/home/agent/.ssh"))`,
		`(allow mach-lookup (global-name "com.apple.SecurityServer"))`,
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("policy missing %q\n%s", want, policy)
		}
	}
	for _, forbidden := range []string{
		`(allow file-read* file-write* (subpath "/home/agent"))`,
		`(allow process-exec (subpath "/home/agent"))`,
	} {
		if strings.Contains(policy, forbidden) {
			t.Fatalf("policy should not contain broad agent-home grant %q\n%s", forbidden, policy)
		}
	}
}

func TestGoldenDarwinSBPLBaselines(t *testing.T) {
	agentHome := "/Users/agent"
	cases := map[string]struct {
		projectDir string
		readDirs   []string
		tempDir    string
		network    sessionmeta.NetworkMode
		options    CompileOptions
	}{
		"sbpl/default.sbpl": {
			projectDir: "/Users/dr/workspace/project",
			tempDir:    agentHome + "/.cache/hazmat/tmp/golden-default",
		},
		"sbpl/network-none.sbpl": {
			projectDir: "/Users/dr/workspace/project",
			tempDir:    agentHome + "/.cache/hazmat/tmp/golden-network-none",
			network:    sessionmeta.NetworkNone,
			options:    CompileOptions{MacOSSecurityFramework: true},
		},
		"sbpl/resume.sbpl": {
			projectDir: "/Users/dr/workspace/project",
			tempDir:    agentHome + "/.cache/hazmat/tmp/golden-resume",
			options: CompileOptions{
				MacOSSecurityFramework: true,
				RuntimeTempDirs:        []string{"/private/tmp/claude-777"},
			},
		},
		"sbpl/read-parent-reassert.sbpl": {
			projectDir: "/Users/dr/workspace/project",
			readDirs:   []string{"/Users/dr/workspace"},
			tempDir:    agentHome + "/.cache/hazmat/tmp/golden-read-parent",
		},
		"sbpl/integration-env.sbpl": {
			projectDir: "/Users/dr/workspace/project",
			readDirs:   []string{"/opt/homebrew/Cellar/go/1.2.3/libexec"},
			tempDir:    agentHome + "/.cache/hazmat/tmp/golden-integration-env",
		},
		"sbpl/codex-native-tls.sbpl": {
			projectDir: "/Users/dr/workspace/project",
			tempDir:    agentHome + "/.cache/hazmat/tmp/golden-codex-native-tls",
			options:    CompileOptions{MacOSSecurityFramework: true},
		},
		"sbpl/claude-keychain.sbpl": {
			projectDir: "/Users/dr/workspace/project",
			tempDir:    agentHome + "/.cache/hazmat/tmp/golden-claude-keychain",
			options: CompileOptions{
				MacOSSecurityFramework:   true,
				MacOSAgentKeychainAccess: true,
				RuntimeTempDirs:          []string{"/private/tmp/claude-777"},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			policy, err := Compile(goldenContract(t, tc.projectDir, tc.readDirs, tc.tempDir, tc.network), tc.options)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			assertGolden(t, name, policy)
		})
	}
}

func TestCompileSessionLocalHomePolicy(t *testing.T) {
	policy, err := Compile(testSessionLocalContract(t), CompileOptions{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, want := range []string{
		`(allow process-exec (subpath "/private/tmp/hazmat-home/session-123/home"))`,
		`(allow file-read* file-write* (subpath "/private/tmp/hazmat-home/session-123/home"))`,
		`(allow file-read* file-write* (subpath "/home/agent/.claude/projects"))`,
		`(deny file-read* file-write* (subpath "/home/agent/.ssh"))`,
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("session-local policy missing %q\n%s", want, policy)
		}
	}
	for _, forbidden := range []string{
		`(allow file-read* file-write* (subpath "/home/agent/.config"))`,
		`(allow file-read* file-write* (literal "/home/agent/.zshrc"))`,
		`(allow process-exec (subpath "/home/agent/.local/bin"))`,
		`(allow file-read* file-write* (subpath "/home/agent"))`,
	} {
		if strings.Contains(policy, forbidden) {
			t.Fatalf("session-local policy should not contain %q\n%s", forbidden, policy)
		}
	}
}

func TestCompileRejectsUnconstructedCredentialFloor(t *testing.T) {
	contract := containment.Contract{
		Project:          containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		AgentHome:        containment.AgentHomePolicy{Path: "/home/agent"},
		Temp:             containment.TempPolicy{Path: "/tmp/hazmat-session"},
		CredentialDenies: []containment.CredentialDeny{{Path: "/home/agent/.ssh"}},
		Network:          containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:          containment.ProcessPolicy{AllowFork: true},
	}
	_, err := Compile(contract, CompileOptions{})
	if err == nil {
		t.Fatal("Compile succeeded without structural credential floor")
	}
}

func testContract(t *testing.T) containment.Contract {
	t.Helper()
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{
		{Path: "/home/agent/.ssh"},
		{Path: "/home/agent/.aws"},
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project: containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		ReadOnlyDirs: containment.PathGrants([]string{
			"/workspace/reference",
		}, containment.PathReadOnly),
		ReadWriteDirs: containment.PathGrants([]string{
			"/workspace/cache",
		}, containment.PathReadWrite),
		AgentHome: containment.AgentHomePolicy{Path: "/home/agent"},
		Temp:      containment.TempPolicy{Path: "/tmp/hazmat-session"},
		Network:   containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:   containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func testSessionLocalContract(t *testing.T) containment.Contract {
	t.Helper()
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{
		{Path: "/home/agent/.ssh"},
		{Path: "/home/agent/.aws"},
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project: containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		AgentHome: containment.AgentHomePolicy{
			Path:               "/private/tmp/hazmat-home/session-123/home",
			Mode:               containment.AgentHomeModeSessionLocal,
			PersistentPath:     "/home/agent",
			DurableBridgeRoots: []string{"/home/agent/.claude/projects"},
		},
		Temp:    containment.TempPolicy{Path: "/tmp/hazmat-session"},
		Network: containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process: containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func goldenContract(t *testing.T, projectDir string, readDirs []string, tempDir string, network sessionmeta.NetworkMode) containment.Contract {
	t.Helper()
	agentHome := "/Users/agent"
	floor, err := containment.NewCredentialFloor(agentHome, pathpolicy.CredentialDenySubpaths())
	if err != nil {
		t.Fatal(err)
	}
	if network == "" {
		network = sessionmeta.NetworkDefault
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project:      containment.PathGrant{Path: projectDir, Access: containment.PathReadWrite},
		ReadOnlyDirs: containment.PathGrants(readDirs, containment.PathReadOnly),
		AgentHome:    containment.AgentHomePolicy{Path: agentHome},
		Temp:         containment.TempPolicy{Path: tempDir},
		Network:      containment.NetworkPolicy{Mode: network},
		Process:      containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", filepath.FromSlash(name))
	if *updateGoldenBaselines {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nRun `go test ./containment/darwin -update-golden` from hazmat/ to refresh baselines.", path, err)
	}
	if got != string(want) {
		t.Fatalf("%s changed; run `go test ./containment/darwin -update-golden` only after reviewing the diff.\n--- want\n%s\n--- got\n%s", name, string(want), got)
	}
}

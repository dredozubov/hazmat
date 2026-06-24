package hazmat

import (
	"errors"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessCommandExposesLifecycleSubcommands(t *testing.T) {
	cmd := newHarnessCmd()
	for _, name := range []string{"status", "update", "uninstall"} {
		found, _, err := cmd.Find([]string{name})
		if err != nil {
			t.Fatalf("Find(%s): %v", name, err)
		}
		if found == nil || found.Name() != name {
			t.Fatalf("expected harness %s subcommand, got %#v", name, found)
		}
	}
	statusCmd, _, err := cmd.Find([]string{"status"})
	if err != nil {
		t.Fatalf("Find(status): %v", err)
	}
	if flag := statusCmd.Flags().Lookup("json"); flag == nil {
		t.Fatal("expected harness status --json flag")
	}
}

func TestInspectManagedHarnessStatusIncludesProbeStateImportAndCredentials(t *testing.T) {
	restore := stubHarnessCredentialStatus(t, harnessCredentialStatus{
		Summary:    "2 configured; hazmat config agent",
		Configured: 2,
	})
	defer restore()

	harness, ok := managedHarnessByID(HarnessCodex)
	if !ok {
		t.Fatal("missing Codex harness")
	}
	read := fakeHarnessRead(
		map[string]harnessManagedArtifactKind{
			agentHome + codexBinRel: harnessArtifactFile,
		},
		map[string]string{
			agentHome + codexBinRel: "codex-cli 1.2.3\n",
		},
	)

	status := inspectManagedHarnessStatus(harness, HazmatState{
		Harnesses: map[HarnessID]HarnessState{
			HarnessCodex: {
				StateVersion:    codexHarnessStateVersion,
				LastImportRunAt: "2026-06-01T12:00:00Z",
			},
		},
	}, nil, read)

	if !status.Probe.Installed {
		t.Fatal("expected installed probe")
	}
	if status.Probe.BinaryPath != agentHome+codexBinRel {
		t.Fatalf("BinaryPath = %q", status.Probe.BinaryPath)
	}
	if status.Probe.Version != "codex-cli 1.2.3" {
		t.Fatalf("Version = %q", status.Probe.Version)
	}
	if status.StateStatus.Summary != "recorded v"+codexHarnessStateVersion {
		t.Fatalf("StateStatus.Summary = %q", status.StateStatus.Summary)
	}
	if status.ImportStatus.Summary != "2026-06-01T12:00:00Z" {
		t.Fatalf("ImportStatus.Summary = %q", status.ImportStatus.Summary)
	}
	if status.CredentialStatus.Summary != "2 configured; hazmat config agent" {
		t.Fatalf("CredentialStatus.Summary = %q", status.CredentialStatus.Summary)
	}
	if status.LifecycleStatus != harnessLifecycleOK {
		t.Fatalf("LifecycleStatus = %q", status.LifecycleStatus)
	}
	if status.NextAction != "hazmat harness update codex" {
		t.Fatalf("NextAction = %q", status.NextAction)
	}
}

func TestInspectManagedHarnessStatusClassifiesLifecycleStates(t *testing.T) {
	baseHarness := ManagedHarness{
		Spec: HarnessSpec{
			ID:           HarnessID("test"),
			DisplayName:  "Test",
			StateVersion: "2",
		},
		BootstrapCommand: "hazmat bootstrap test",
	}

	for _, tc := range []struct {
		name        string
		probe       harnessProbe
		state       HazmatState
		stateErr    error
		credentials harnessCredentialStatus
		want        harnessLifecycleStatus
		wantAction  string
	}{
		{
			name:       "not installed",
			probe:      harnessProbe{MissingReason: "missing"},
			want:       harnessLifecycleNotInstalled,
			wantAction: "hazmat harness update test",
		},
		{
			name:  "recorded missing binary",
			probe: harnessProbe{MissingReason: "missing"},
			state: HazmatState{Harnesses: map[HarnessID]HarnessState{
				HarnessID("test"): {StateVersion: "2"},
			}},
			want:       harnessLifecycleRecordedMissingBinary,
			wantAction: "hazmat harness update test",
		},
		{
			name:  "installed unrecorded",
			probe: harnessProbe{Installed: true, BinaryPath: agentHome + "/.local/bin/test"},
			want:  harnessLifecycleInstalledUnrecorded,
		},
		{
			name:  "state version stale",
			probe: harnessProbe{Installed: true, BinaryPath: agentHome + "/.local/bin/test"},
			state: HazmatState{Harnesses: map[HarnessID]HarnessState{
				HarnessID("test"): {StateVersion: "1"},
			}},
			want: harnessLifecycleStateVersionStale,
		},
		{
			name:  "probe failed",
			probe: harnessProbe{Installed: true, BinaryPath: agentHome + "/.local/bin/test", VersionErr: "exit status 1"},
			state: HazmatState{Harnesses: map[HarnessID]HarnessState{
				HarnessID("test"): {StateVersion: "2"},
			}},
			want: harnessLifecycleProbeFailed,
		},
		{
			name:  "credential repair needed",
			probe: harnessProbe{Installed: true, BinaryPath: agentHome + "/.local/bin/test", Version: "test 1.0.0"},
			state: HazmatState{Harnesses: map[HarnessID]HarnessState{
				HarnessID("test"): {StateVersion: "2"},
			}},
			credentials: harnessCredentialStatus{NeedsRepair: 1},
			want:        harnessLifecycleCredentialRepairNeeded,
		},
		{
			name:     "state unreadable",
			probe:    harnessProbe{Installed: true, BinaryPath: agentHome + "/.local/bin/test"},
			stateErr: errors.New("bad state"),
			want:     harnessLifecycleStateUnreadable,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restore := stubHarnessCredentialStatus(t, tc.credentials)
			defer restore()

			harness := baseHarness
			harness.Probe = func(func(args ...string) (string, error)) harnessProbe {
				return tc.probe
			}
			status := inspectManagedHarnessStatus(harness, tc.state, tc.stateErr, fakeHarnessRead(nil, nil))
			if status.LifecycleStatus != tc.want {
				t.Fatalf("LifecycleStatus = %q, want %q", status.LifecycleStatus, tc.want)
			}
			if tc.wantAction != "" && status.NextAction != tc.wantAction {
				t.Fatalf("NextAction = %q, want %q", status.NextAction, tc.wantAction)
			}
		})
	}
}

func TestInspectManagedHarnessStatusTreatsMissingOwnedExecutableAsNotInstalled(t *testing.T) {
	restore := stubHarnessCredentialStatus(t, harnessCredentialStatus{})
	defer restore()

	path := agentHome + "/.local/bin/test"
	harness := ManagedHarness{
		Spec: HarnessSpec{
			ID:           HarnessID("test"),
			DisplayName:  "Test",
			StateVersion: "2",
		},
		Probe: func(func(args ...string) (string, error)) harnessProbe {
			return harnessProbe{
				Installed:  true,
				BinaryPath: path,
				Version:    "test 1.0.0",
			}
		},
		ManagedCodeArtifacts: func() []harnessManagedArtifact {
			return []harnessManagedArtifact{
				harnessFileArtifact(path, "Test executable"),
			}
		},
	}

	status := inspectManagedHarnessStatus(harness, HazmatState{
		Harnesses: map[HarnessID]HarnessState{
			HarnessID("test"): {StateVersion: "2"},
		},
	}, nil, fakeHarnessRead(nil, nil))

	if status.Probe.Installed {
		t.Fatalf("Probe.Installed = true, want false when managed executable is missing")
	}
	if status.LifecycleStatus != harnessLifecycleRecordedMissingBinary {
		t.Fatalf("LifecycleStatus = %q, want %q", status.LifecycleStatus, harnessLifecycleRecordedMissingBinary)
	}
	if status.NextAction != "hazmat harness update test" {
		t.Fatalf("NextAction = %q", status.NextAction)
	}
}

func TestHarnessStatusJSONIncludesStructuredRedactedFields(t *testing.T) {
	harness, ok := managedHarnessByID(HarnessAntigravity)
	if !ok {
		t.Fatal("missing Antigravity harness")
	}
	restore := stubHarnessCredentialStatus(t, harnessCredentialStatus{
		Summary:         "1 configured, 1 adapter-required; hazmat config agent",
		Configured:      1,
		AdapterRequired: 1,
		Entries: []harnessCredentialEntryStatus{
			{
				ID:          credentialHarnessAntigravityKeychain,
				DisplayName: "Antigravity Keychain OAuth state",
				Status:      credentialInventoryAdapterRequired,
				Kind:        credentialKindExternalAuth,
				Backend:     credentialStorageKeychain,
				Delivery:    credentialDeliveryExternalReference,
				Support:     credentialSupportAdapterRequired,
			},
		},
	})
	defer restore()

	read := fakeHarnessRead(
		map[string]harnessManagedArtifactKind{
			agentHome + antigravityBinRel: harnessArtifactFile,
		},
		map[string]string{
			agentHome + antigravityBinRel: "agy 0.9.0\n",
		},
	)
	status := inspectManagedHarnessStatus(harness, HazmatState{
		Harnesses: map[HarnessID]HarnessState{
			HarnessAntigravity: {StateVersion: antigravityHarnessStateVersion},
		},
	}, nil, read)
	payload := harnessStatusForJSON(status)

	if payload.ID != HarnessAntigravity || payload.LifecycleStatus != harnessLifecycleOK {
		t.Fatalf("payload identity/status = %s/%s", payload.ID, payload.LifecycleStatus)
	}
	if payload.Import.Supported || !strings.Contains(payload.Import.Boundary, "adapter-required") {
		t.Fatalf("import boundary = %#v", payload.Import)
	}
	if payload.Credentials.AdapterRequired != 1 || len(payload.Credentials.Entries) != 1 {
		t.Fatalf("credential summary = %#v", payload.Credentials)
	}
	if strings.Contains(payload.Credentials.Summary, "antigravity-key") {
		t.Fatalf("credential summary contains secret-like material: %q", payload.Credentials.Summary)
	}
}

func TestManagedHarnessRegistryCarriesImportPolicy(t *testing.T) {
	for _, harness := range managedHarnesses() {
		switch harness.Spec.ID {
		case HarnessClaude, HarnessCodex, HarnessOpenCode:
			if !harness.ImportPolicy.Supported {
				t.Fatalf("%s should support curated basics import", harness.Spec.ID)
			}
		case HarnessAntigravity, HarnessHermes, HarnessQwen, HarnessCursorAgent, HarnessPi:
			if harness.ImportPolicy.Supported {
				t.Fatalf("%s should remain a no-import boundary in v1", harness.Spec.ID)
			}
			if !strings.Contains(harness.ImportPolicy.Boundary, "no curated import") {
				t.Fatalf("%s import boundary = %q", harness.Spec.ID, harness.ImportPolicy.Boundary)
			}
			if !strings.Contains(harness.ImportPolicy.Boundary, "contained-only") ||
				!strings.Contains(harness.ImportPolicy.Boundary, "not synced") {
				t.Fatalf("%s import boundary = %q", harness.Spec.ID, harness.ImportPolicy.Boundary)
			}
		default:
			t.Fatalf("unexpected harness %q", harness.Spec.ID)
		}
	}
}

func TestInspectHarnessArtifactDetectsDrift(t *testing.T) {
	path := agentHome + "/.local/lib/node_modules/@qwen-code/qwen-code"
	status := inspectHarnessArtifact(
		fakeHarnessRead(map[string]harnessManagedArtifactKind{
			path: harnessArtifactFile,
		}, nil),
		harnessDirArtifact(path, "Qwen Code npm package"),
	)
	if !status.Exists {
		t.Fatal("expected artifact to exist")
	}
	if !strings.Contains(status.Drift, "expected directory") {
		t.Fatalf("Drift = %q", status.Drift)
	}
}

func TestInspectHarnessArtifactChecksNpmPackageMetadata(t *testing.T) {
	path := agentHome + "/.local/lib/node_modules/@qwen-code/qwen-code"
	status := inspectHarnessArtifact(
		fakeHarnessRead(map[string]harnessManagedArtifactKind{
			path: harnessArtifactDir,
		}, map[string]string{
			filepath.Join(path, "package.json"): `{"name":"left-pad"}`,
		}),
		harnessNpmPackageDirArtifact(path, "@qwen-code/qwen-code", "Qwen Code npm package"),
	)
	if !status.Exists {
		t.Fatal("expected package directory to exist")
	}
	if !strings.Contains(status.Drift, "expected npm package @qwen-code/qwen-code, got left-pad") {
		t.Fatalf("Drift = %q", status.Drift)
	}
}

func TestInspectHarnessArtifactRejectsDisallowedSymlink(t *testing.T) {
	path := agentHome + "/.local/bin/codex"
	status := inspectHarnessArtifact(
		fakeHarnessRead(map[string]harnessManagedArtifactKind{
			path: harnessManagedArtifactKind("symlink"),
		}, nil),
		harnessRegularFileArtifact(path, "Codex executable"),
	)
	if !status.Exists || !status.Symlink {
		t.Fatalf("artifact status = %#v", status)
	}
	if !strings.Contains(status.Drift, "symlink not allowed") {
		t.Fatalf("Drift = %q", status.Drift)
	}
}

func TestInspectHarnessArtifactAllowsDeclaredSymlink(t *testing.T) {
	path := agentHome + "/.local/bin/opencode"
	status := inspectHarnessArtifact(
		fakeHarnessRead(map[string]harnessManagedArtifactKind{
			path: harnessManagedArtifactKind("symlink"),
		}, nil),
		harnessSymlinkArtifact(path, "OpenCode PATH shim"),
	)
	if status.Drift != "" {
		t.Fatalf("Drift = %q", status.Drift)
	}
}

func TestBuildHarnessUninstallPlanKeepsHermesManualBinaryOutOfManagedCode(t *testing.T) {
	restoreState := isolateStateFile(t)
	defer restoreState()

	if err := recordHarnessInstalled(harnessMetadata(HarnessHermes).Spec); err != nil {
		t.Fatalf("RecordInstalled: %v", err)
	}
	harness, ok := managedHarnessByID(HarnessHermes)
	if !ok {
		t.Fatal("missing Hermes harness")
	}

	plan := buildHarnessUninstallPlan(harness, fakeHarnessRead(nil, nil))
	if plan.StateErr != nil {
		t.Fatalf("StateErr: %v", plan.StateErr)
	}
	if !plan.MetadataPresent {
		t.Fatal("expected Hermes metadata to be present")
	}
	if len(plan.Artifacts) != 0 {
		t.Fatalf("Hermes managed artifacts = %d, want none", len(plan.Artifacts))
	}
	if !containsHarnessLifecycleString(plan.Preserved, agentHome+hermesBinRel+" manual Hermes executable") {
		t.Fatalf("preserved artifacts did not mention manual Hermes binary: %#v", plan.Preserved)
	}
}

func TestRunManagedHarnessUpdateCallsBootstrap(t *testing.T) {
	calls := 0
	harness := ManagedHarness{
		Spec: HarnessSpec{
			ID:          HarnessID("test"),
			DisplayName: "Test",
		},
		Bootstrap: func(*UI, *Runner) error {
			calls++
			return nil
		},
	}
	if err := runManagedHarnessUpdate(harness); err != nil {
		t.Fatalf("runManagedHarnessUpdate: %v", err)
	}
	if calls != 1 {
		t.Fatalf("Bootstrap calls = %d, want 1", calls)
	}
}

func TestRunManagedHarnessUpdateFailsWhenBootstrapLeavesOwnedExecutableMissing(t *testing.T) {
	path := agentHome + "/.local/bin/test"
	harness := ManagedHarness{
		Spec: HarnessSpec{
			ID:          HarnessID("test"),
			DisplayName: "Test",
		},
		Installed: func() bool { return true },
		ManagedCodeArtifacts: func() []harnessManagedArtifact {
			return []harnessManagedArtifact{
				harnessFileArtifact(path, "Test executable"),
			}
		},
		Bootstrap: func(*UI, *Runner) error {
			return nil
		},
	}

	savedRead := managedHarnessReadForLifecycle
	managedHarnessReadForLifecycle = fakeHarnessRead(nil, nil)
	t.Cleanup(func() { managedHarnessReadForLifecycle = savedRead })

	err := runManagedHarnessUpdate(harness)
	if err == nil || !strings.Contains(err.Error(), "still not installed after harness update") {
		t.Fatalf("runManagedHarnessUpdate error = %v, want still not installed", err)
	}
}

func TestBuildManagedHarnessLaunchUpdateMutationPlanSkipsInstalledHarness(t *testing.T) {
	harness := ManagedHarness{
		Spec: HarnessSpec{
			ID:          HarnessID("test"),
			DisplayName: "Test",
		},
		Installed: func() bool { return true },
	}

	plan := buildManagedHarnessLaunchUpdateMutationPlan(harness)
	if len(plan.Mutations) != 0 {
		t.Fatalf("mutations = %d, want 0", len(plan.Mutations))
	}
}

func TestBuildManagedHarnessLaunchUpdateMutationPlanRunsUpdateForMissingHarness(t *testing.T) {
	installed := false
	harness := ManagedHarness{
		Spec: HarnessSpec{
			ID:          HarnessID("test"),
			DisplayName: "Test",
		},
		Installed: func() bool { return installed },
	}

	calls := 0
	savedUpdate := managedHarnessUpdateForLaunch
	managedHarnessUpdateForLaunch = func(got ManagedHarness) error {
		calls++
		if got.Spec.ID != harness.Spec.ID {
			t.Fatalf("update harness ID = %q, want %q", got.Spec.ID, harness.Spec.ID)
		}
		installed = true
		return nil
	}
	t.Cleanup(func() { managedHarnessUpdateForLaunch = savedUpdate })

	plan := buildManagedHarnessLaunchUpdateMutationPlan(harness)
	if len(plan.Mutations) != 1 {
		t.Fatalf("mutations = %d, want 1", len(plan.Mutations))
	}
	if plan.Mutations[0].Metadata.Summary != "Test harness update" {
		t.Fatalf("summary = %q", plan.Mutations[0].Metadata.Summary)
	}
	exec, err := plan.Mutations[0].Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if calls != 1 {
		t.Fatalf("update calls = %d, want 1", calls)
	}
	if exec.AppliedMessage != "  Updated Test harness" {
		t.Fatalf("AppliedMessage = %q", exec.AppliedMessage)
	}
}

func TestBuildManagedHarnessLaunchUpdateMutationPlanFailsIfUpdateLeavesHarnessMissing(t *testing.T) {
	harness := ManagedHarness{
		Spec: HarnessSpec{
			ID:          HarnessID("test"),
			DisplayName: "Test",
		},
		Installed: func() bool { return false },
	}

	savedUpdate := managedHarnessUpdateForLaunch
	managedHarnessUpdateForLaunch = func(ManagedHarness) error { return nil }
	t.Cleanup(func() { managedHarnessUpdateForLaunch = savedUpdate })

	plan := buildManagedHarnessLaunchUpdateMutationPlan(harness)
	if len(plan.Mutations) != 1 {
		t.Fatalf("mutations = %d, want 1", len(plan.Mutations))
	}
	if _, err := plan.Mutations[0].Apply(); err == nil || !strings.Contains(err.Error(), "still not installed") {
		t.Fatalf("Apply error = %v, want still not installed", err)
	}
}

func TestBuildManagedHarnessLaunchUpdateMutationPlanRunsUpdateWhenOwnedExecutableMissing(t *testing.T) {
	path := agentHome + "/.local/bin/test"
	paths := map[string]harnessManagedArtifactKind{}
	harness := ManagedHarness{
		Spec: HarnessSpec{
			ID:          HarnessID("test"),
			DisplayName: "Test",
		},
		Installed: func() bool { return true },
		ManagedCodeArtifacts: func() []harnessManagedArtifact {
			return []harnessManagedArtifact{
				harnessFileArtifact(path, "Test executable"),
			}
		},
	}

	calls := 0
	savedUpdate := managedHarnessUpdateForLaunch
	managedHarnessUpdateForLaunch = func(got ManagedHarness) error {
		calls++
		if got.Spec.ID != harness.Spec.ID {
			t.Fatalf("update harness ID = %q, want %q", got.Spec.ID, harness.Spec.ID)
		}
		paths[path] = harnessArtifactFile
		return nil
	}
	t.Cleanup(func() { managedHarnessUpdateForLaunch = savedUpdate })

	plan := buildManagedHarnessLaunchUpdateMutationPlanWithRead(harness, fakeHarnessRead(paths, nil))
	if len(plan.Mutations) != 1 {
		t.Fatalf("mutations = %d, want 1", len(plan.Mutations))
	}
	exec, err := plan.Mutations[0].Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if calls != 1 {
		t.Fatalf("update calls = %d, want 1", calls)
	}
	if exec.AppliedMessage != "  Updated Test harness" {
		t.Fatalf("AppliedMessage = %q", exec.AppliedMessage)
	}
}

func TestBuildManagedHarnessLaunchUpdateMutationPlanForCommandIncludesPlanOnlyPreview(t *testing.T) {
	savedRegistry := managedHarnessRegistry
	managedHarnessRegistry = []ManagedHarness{
		{
			Spec: HarnessSpec{
				ID:          HarnessClaude,
				DisplayName: "Claude Code",
			},
			Installed: func() bool { return false },
		},
	}
	t.Cleanup(func() { managedHarnessRegistry = savedRegistry })

	plan := buildManagedHarnessLaunchUpdateMutationPlanForCommand("claude", harnessSessionOpts{planOnly: true})
	if len(plan.Mutations) != 1 {
		t.Fatalf("mutations = %d, want 1", len(plan.Mutations))
	}
	if plan.Mutations[0].Metadata.Summary != "Claude Code harness update" {
		t.Fatalf("summary = %q", plan.Mutations[0].Metadata.Summary)
	}

	shellPlan := buildManagedHarnessLaunchUpdateMutationPlanForCommand("shell", harnessSessionOpts{planOnly: true})
	if len(shellPlan.Mutations) != 0 {
		t.Fatalf("shell mutations = %d, want 0", len(shellPlan.Mutations))
	}
}

func TestHarnessUpdateDryRunDoesNotRequireAgentUser(t *testing.T) {
	restoreDryRun := enableHarnessLifecycleDryRun(t)
	defer restoreDryRun()

	lookups := 0
	restoreLookup := stubAgentUserLookup(func() (*user.User, error) {
		lookups++
		return nil, errors.New("missing agent user")
	})
	defer restoreLookup()

	for _, harness := range managedHarnesses() {
		if err := runManagedHarnessUpdate(harness); err != nil {
			t.Fatalf("runManagedHarnessUpdate(%s): %v", harness.Spec.ID, err)
		}
	}
	if lookups != 0 {
		t.Fatalf("dry-run update performed %d agent user lookups", lookups)
	}
}

func TestHarnessUninstallDryRunDoesNotRequireAgentUser(t *testing.T) {
	restoreDryRun := enableHarnessLifecycleDryRun(t)
	defer restoreDryRun()
	restoreState := isolateStateFile(t)
	defer restoreState()

	lookups := 0
	restoreLookup := stubAgentUserLookup(func() (*user.User, error) {
		lookups++
		return nil, errors.New("missing agent user")
	})
	defer restoreLookup()

	if err := runHarnessUninstall(string(HarnessClaude), false); err != nil {
		t.Fatalf("runHarnessUninstall dry-run: %v", err)
	}
	if lookups != 0 {
		t.Fatalf("dry-run uninstall performed %d agent user lookups", lookups)
	}
}

func enableHarnessLifecycleDryRun(t *testing.T) func() {
	t.Helper()
	oldDryRun := flagDryRun
	oldVerbose := flagVerbose
	flagDryRun = true
	flagVerbose = false
	return func() {
		flagDryRun = oldDryRun
		flagVerbose = oldVerbose
	}
}

func stubAgentUserLookup(fn func() (*user.User, error)) func() {
	oldLookup := lookupAgentUser
	lookupAgentUser = fn
	return func() {
		lookupAgentUser = oldLookup
	}
}

func stubHarnessCredentialStatus(t *testing.T, value harnessCredentialStatus) func() {
	t.Helper()
	old := inspectHarnessCredentialStatusForStatus
	inspectHarnessCredentialStatusForStatus = func(HarnessID) harnessCredentialStatus {
		return value
	}
	return func() {
		inspectHarnessCredentialStatusForStatus = old
	}
}

func fakeHarnessRead(paths map[string]harnessManagedArtifactKind, versions map[string]string) func(args ...string) (string, error) {
	if paths == nil {
		paths = map[string]harnessManagedArtifactKind{}
	}
	if versions == nil {
		versions = map[string]string{}
	}
	return func(args ...string) (string, error) {
		if len(args) >= 3 && (args[0] == "test" || args[0] == "/usr/bin/test") {
			kind, ok := paths[args[2]]
			if !ok {
				return "", errors.New("missing")
			}
			switch args[1] {
			case "-e", "-x":
				return "", nil
			case "-f":
				if kind == harnessArtifactFile || kind == harnessManagedArtifactKind("symlink") {
					return "", nil
				}
			case "-d":
				if kind == harnessArtifactDir {
					return "", nil
				}
			case "-L":
				if kind == harnessManagedArtifactKind("symlink") {
					return "", nil
				}
				return "", errors.New("not symlink")
			}
			return "", errors.New("wrong kind")
		}
		if len(args) == 2 && args[0] == "/bin/cat" {
			if value, ok := versions[args[1]]; ok {
				return value, nil
			}
			return "", errors.New("missing")
		}
		if len(args) > 0 {
			if version, ok := versions[args[0]]; ok {
				return version, nil
			}
		}
		return "", errors.New("unexpected command")
	}
}

func containsHarnessLifecycleString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

package diagnostics

import (
	"errors"
	"slices"
	"testing"
)

func TestRunCheckQuickSkipsHelperBackedAgentProbes(t *testing.T) {
	var got []string
	suite := CheckSuite{
		Begin: func(quick bool) (CheckContext, error) {
			if !quick {
				t.Fatal("quick = false, want true")
			}
			got = append(got, "begin")
			return CheckContext{CurrentUser: "dr", SelfPath: "/bin/hazmat"}, nil
		},
		AgentUser:            func() { got = append(got, "agent") },
		DevGroupAndWorkspace: func(user string) { got = append(got, "group:"+user) },
		AgentProbesSkipped:   func(reason string) { got = append(got, "skip:"+reason) },
		UserIsolation:        func(user string) { got = append(got, "isolation:"+user) },
		HardeningGaps:        func() { got = append(got, "hardening") },
		PasswordlessSudo:     func() { got = append(got, "sudo") },
		PFFirewallStatic:     func() { got = append(got, "pf-static") },
		PFFirewallLive:       func(bool, string) { got = append(got, "pf-live") },
		DNSBlocklist:         func() { got = append(got, "dns") },
		Persistence:          func() { got = append(got, "persistence") },
		CredentialInventory:  func() { got = append(got, "credentials") },
		AgentTools:           func() { got = append(got, "tools") },
		CommandSurface:       func() { got = append(got, "commands") },
		Seatbelt:             func() { got = append(got, "seatbelt") },
		ProjectToolchain:     func() { got = append(got, "toolchain") },
		LocalSnapshot:        func() { t.Fatal("LocalSnapshot called in quick mode") },
		LocalSnapshotSkipped: func(reason string) { got = append(got, "local-snapshot-skip:"+reason) },
		CloudBackup:          func() { t.Fatal("CloudBackup called in quick mode") },
		CloudBackupSkipped:   func(reason string) { got = append(got, "cloud-backup-skip:"+reason) },
		CloudRestore:         func() { t.Fatal("CloudRestore called in quick mode") },
		CloudRestoreSkipped:  func(reason string) { got = append(got, "cloud-restore-skip:"+reason) },
		Decommission:         func() { got = append(got, "decommission") },
		Finish: func() bool {
			got = append(got, "finish")
			return false
		},
	}

	if err := RunCheck(true, suite); err != nil {
		t.Fatalf("RunCheck(): %v", err)
	}
	want := []string{
		"begin", "agent", "group:dr", "sudo", "pf-static", "dns", "persistence",
		"skip:" + QuickAgentProbeSkipReason,
		"local-snapshot-skip:" + QuickLocalSnapshotSkipReason,
		"cloud-backup-skip:" + QuickCloudBackupSkipReason,
		"cloud-restore-skip:" + QuickCloudRestoreSkipReason,
		"decommission", "finish",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRunCheckFullRunsHelperBackedAgentProbesInOrder(t *testing.T) {
	var got []string
	suite := CheckSuite{
		Begin: func(quick bool) (CheckContext, error) {
			if quick {
				t.Fatal("quick = true, want false")
			}
			got = append(got, "begin")
			return CheckContext{CurrentUser: "dr", SelfPath: "/bin/hazmat"}, nil
		},
		AgentUser:            func() { got = append(got, "agent") },
		DevGroupAndWorkspace: func(user string) { got = append(got, "group:"+user) },
		AgentProbesSkipped:   func(reason string) { got = append(got, "skip:"+reason) },
		UserIsolation:        func(user string) { got = append(got, "isolation:"+user) },
		HardeningGaps:        func() { got = append(got, "hardening") },
		PasswordlessSudo:     func() { got = append(got, "sudo") },
		PFFirewallStatic:     func() { got = append(got, "pf-static") },
		PFFirewallLive: func(quick bool, selfPath string) {
			if quick || selfPath != "/bin/hazmat" {
				t.Fatalf("pf live args quick=%v selfPath=%q", quick, selfPath)
			}
			got = append(got, "pf-live")
		},
		DNSBlocklist:        func() { got = append(got, "dns") },
		Persistence:         func() { got = append(got, "persistence") },
		CredentialInventory: func() { got = append(got, "credentials") },
		AgentTools:          func() { got = append(got, "tools") },
		CommandSurface:      func() { got = append(got, "commands") },
		Seatbelt:            func() { got = append(got, "seatbelt") },
		ProjectToolchain:    func() { got = append(got, "toolchain") },
		LocalSnapshot:       func() { got = append(got, "local-snapshot") },
		LocalSnapshotSkipped: func(reason string) {
			got = append(got, "local-snapshot-skip:"+reason)
		},
		CloudBackup:         func() { got = append(got, "cloud-backup") },
		CloudBackupSkipped:  func(reason string) { got = append(got, "cloud-backup-skip:"+reason) },
		CloudRestore:        func() { got = append(got, "cloud-restore") },
		CloudRestoreSkipped: func(reason string) { got = append(got, "cloud-restore-skip:"+reason) },
		Decommission:        func() { got = append(got, "decommission") },
		Finish: func() bool {
			got = append(got, "finish")
			return false
		},
	}

	if err := RunCheck(false, suite); err != nil {
		t.Fatalf("RunCheck(): %v", err)
	}
	want := []string{
		"begin", "agent", "group:dr", "sudo", "pf-static", "dns", "persistence",
		"isolation:dr", "hardening", "pf-live", "credentials", "tools", "commands",
		"seatbelt", "toolchain", "local-snapshot", "cloud-backup", "cloud-restore",
		"decommission", "finish",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRunCheckFullSkipsAgentProbesWhenGateBlocked(t *testing.T) {
	var got []string
	suite := CheckSuite{
		Begin: func(quick bool) (CheckContext, error) {
			if quick {
				t.Fatal("quick = true, want false")
			}
			got = append(got, "begin")
			return CheckContext{
				CurrentUser: "dr",
				SelfPath:    "/bin/hazmat",
				AgentProbes: BlockAgentProbes("setup gate failed"),
			}, nil
		},
		AgentUser:            func() { got = append(got, "agent") },
		DevGroupAndWorkspace: func(user string) { got = append(got, "group:"+user) },
		AgentProbesSkipped:   func(reason string) { got = append(got, "skip:"+reason) },
		UserIsolation:        func(user string) { got = append(got, "isolation:"+user) },
		HardeningGaps:        func() { got = append(got, "hardening") },
		PasswordlessSudo:     func() { got = append(got, "sudo") },
		PFFirewallStatic:     func() { got = append(got, "pf-static") },
		PFFirewallLive:       func(bool, string) { got = append(got, "pf-live") },
		DNSBlocklist:         func() { got = append(got, "dns") },
		Persistence:          func() { got = append(got, "persistence") },
		CredentialInventory:  func() { got = append(got, "credentials") },
		AgentTools:           func() { got = append(got, "tools") },
		CommandSurface:       func() { got = append(got, "commands") },
		Seatbelt:             func() { got = append(got, "seatbelt") },
		ProjectToolchain:     func() { got = append(got, "toolchain") },
		LocalSnapshot:        func() { got = append(got, "local-snapshot") },
		LocalSnapshotSkipped: func(reason string) { got = append(got, "local-snapshot-skip:"+reason) },
		CloudBackup:          func() { got = append(got, "cloud-backup") },
		CloudBackupSkipped:   func(reason string) { got = append(got, "cloud-backup-skip:"+reason) },
		CloudRestore:         func() { got = append(got, "cloud-restore") },
		CloudRestoreSkipped:  func(reason string) { got = append(got, "cloud-restore-skip:"+reason) },
		Decommission:         func() { got = append(got, "decommission") },
		Finish:               func() bool { got = append(got, "finish"); return false },
	}

	if err := RunCheck(false, suite); err != nil {
		t.Fatalf("RunCheck(): %v", err)
	}
	want := []string{
		"begin", "agent", "group:dr", "sudo", "pf-static", "dns", "persistence",
		"skip:setup gate failed", "local-snapshot", "cloud-backup", "cloud-restore",
		"decommission", "finish",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRunCheckReturnsBeginError(t *testing.T) {
	errBoom := errors.New("boom")
	if err := RunCheck(false, CheckSuite{
		Begin: func(bool) (CheckContext, error) { return CheckContext{}, errBoom },
	}); !errors.Is(err, errBoom) {
		t.Fatalf("RunCheck() err = %v, want %v", err, errBoom)
	}
}

func TestRunCheckUsesExitOnFailure(t *testing.T) {
	var exitCode int
	err := RunCheck(false, CheckSuite{
		Finish: func() bool { return true },
		Exit:   func(code int) { exitCode = code },
	})
	if err != nil {
		t.Fatalf("RunCheck(): %v", err)
	}
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
}

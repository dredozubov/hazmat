package diagnostics

import "fmt"

type CheckContext struct {
	CurrentUser string
	SelfPath    string
	AgentProbes AgentProbeGate
}

type AgentProbeGate struct {
	blocked bool
	reason  string
}

func AllowAgentProbes() AgentProbeGate {
	return AgentProbeGate{}
}

func BlockAgentProbes(reason string) AgentProbeGate {
	if reason == "" {
		reason = "agent-backed probes are unavailable"
	}
	return AgentProbeGate{blocked: true, reason: reason}
}

func (g AgentProbeGate) Allowed() bool {
	return !g.blocked
}

func (g AgentProbeGate) Reason() string {
	return g.reason
}

type CheckSuite struct {
	Begin                func(quick bool) (CheckContext, error)
	AgentUser            func()
	DevGroupAndWorkspace func(currentUser string)
	AgentProbesSkipped   func(reason string)
	UserIsolation        func(currentUser string)
	HardeningGaps        func()
	PasswordlessSudo     func()
	PFFirewallStatic     func()
	PFFirewallLive       func(quick bool, selfPath string)
	DNSBlocklist         func()
	Persistence          func()
	CredentialInventory  func()
	AgentTools           func()
	CommandSurface       func()
	Seatbelt             func()
	ProjectToolchain     func()
	LocalSnapshot        func()
	LocalSnapshotSkipped func(reason string)
	CloudBackup          func()
	CloudBackupSkipped   func(reason string)
	CloudRestore         func()
	CloudRestoreSkipped  func(reason string)
	Decommission         func()
	Finish               func() bool
	Exit                 func(code int)
}

const QuickAgentProbeSkipReason = "helper-backed agent probes skipped in quick mode; use hazmat check --full for live helper-backed validation"
const QuickLocalSnapshotSkipReason = "local snapshot live validation skipped in quick mode; use hazmat check --full for local backup validation"
const QuickCloudBackupSkipReason = "cloud backup live validation skipped in quick mode; use hazmat check --full for cloud backup validation"
const QuickCloudRestoreSkipReason = "cloud restore live validation skipped in quick mode; use hazmat check --full for cloud restore validation"

func RunCheck(quick bool, suite CheckSuite) error {
	ctx := CheckContext{}
	if suite.Begin != nil {
		var err error
		ctx, err = suite.Begin(quick)
		if err != nil {
			return err
		}
	}

	call(suite.AgentUser)
	callString(suite.DevGroupAndWorkspace, ctx.CurrentUser)
	call(suite.PasswordlessSudo)
	call(suite.PFFirewallStatic)
	call(suite.DNSBlocklist)
	call(suite.Persistence)
	if quick {
		if suite.AgentProbesSkipped != nil {
			suite.AgentProbesSkipped(QuickAgentProbeSkipReason)
		}
	} else if ctx.AgentProbes.Allowed() {
		callString(suite.UserIsolation, ctx.CurrentUser)
		call(suite.HardeningGaps)
		if suite.PFFirewallLive != nil {
			suite.PFFirewallLive(quick, ctx.SelfPath)
		}
		call(suite.CredentialInventory)
		call(suite.AgentTools)
		call(suite.CommandSurface)
		call(suite.Seatbelt)
		call(suite.ProjectToolchain)
	} else if suite.AgentProbesSkipped != nil {
		suite.AgentProbesSkipped(ctx.AgentProbes.Reason())
	}
	if quick {
		callString(suite.LocalSnapshotSkipped, QuickLocalSnapshotSkipReason)
		callString(suite.CloudBackupSkipped, QuickCloudBackupSkipReason)
		callString(suite.CloudRestoreSkipped, QuickCloudRestoreSkipReason)
	} else {
		call(suite.LocalSnapshot)
		call(suite.CloudBackup)
		call(suite.CloudRestore)
	}
	call(suite.Decommission)

	if suite.Finish != nil && suite.Finish() {
		if suite.Exit != nil {
			suite.Exit(1)
			return nil
		}
		return fmt.Errorf("diagnostic checks failed")
	}
	return nil
}

func call(fn func()) {
	if fn != nil {
		fn()
	}
}

func callString(fn func(string), value string) {
	if fn != nil {
		fn(value)
	}
}

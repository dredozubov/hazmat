package diagnostics

import "fmt"

type CheckContext struct {
	CurrentUser string
	SelfPath    string
}

type CheckSuite struct {
	Begin                func(quick bool) (CheckContext, error)
	AgentUser            func()
	DevGroupAndWorkspace func(currentUser string)
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
	CloudBackup          func()
	CloudRestore         func()
	Decommission         func()
	Finish               func() bool
	Exit                 func(code int)
}

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
	callString(suite.UserIsolation, ctx.CurrentUser)
	call(suite.HardeningGaps)
	call(suite.PasswordlessSudo)
	call(suite.PFFirewallStatic)
	if suite.PFFirewallLive != nil {
		suite.PFFirewallLive(quick, ctx.SelfPath)
	}
	call(suite.DNSBlocklist)
	call(suite.Persistence)
	call(suite.CredentialInventory)
	call(suite.AgentTools)
	call(suite.CommandSurface)
	call(suite.Seatbelt)
	call(suite.ProjectToolchain)
	call(suite.LocalSnapshot)
	call(suite.CloudBackup)
	call(suite.CloudRestore)
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

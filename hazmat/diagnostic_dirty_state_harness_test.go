package hazmat

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type diagnosticFakeHostState struct {
	Users        map[string]diagnosticFakeUser
	Groups       map[string]diagnosticFakeGroup
	ACLs         map[string]diagnosticFakeACLState
	Files        map[string]diagnosticFakeFileState
	PF           diagnosticFakePFState
	DNS          diagnosticFakeDNSState
	Launchd      diagnosticFakeLaunchdState
	Credentials  map[credentialID]diagnosticFakeCredentialState
	Tools        map[string]diagnosticFakeToolState
	Optional     diagnosticFakeOptionalState
	Integrations map[string]diagnosticFakeIntegrationState
	projectPath  string
	agentHome    string
	claudeProj   string
	receiptCount int
}

type diagnosticFakeUser struct {
	UID  string
	Home string
}

type diagnosticFakeGroup struct {
	Members map[string]bool
}

type diagnosticFakeACLState struct {
	Setgid           bool
	GroupWritable    bool
	AgentCanTraverse bool
}

type diagnosticFakeFileState struct {
	Exists  bool
	Private bool
	Content string
}

type diagnosticFakePFState struct {
	Enabled      bool
	AnchorFile   bool
	AnchorLoaded bool
}

type diagnosticFakeDNSState struct {
	BlocklistPresent bool
	BlockedDomains   map[string]bool
}

type diagnosticFakeLaunchdState struct {
	PlistPresent bool
	Loaded       bool
	PFAnchored   bool
}

type diagnosticFakeCredentialState struct {
	HostStorePresent bool
	AgentResidue     bool
	LegacyResidue    bool
	AdapterRequired  bool
}

type diagnosticFakeToolState struct {
	AgentExecutable bool
	HomebrewBacked  bool
}

type diagnosticFakeOptionalState struct {
	SSHKeyConfigured      bool
	AnthropicAuthProvided bool
}

type diagnosticFakeIntegrationState struct {
	ToolchainResolved bool
}

type diagnosticFakeRepairResult struct {
	Action       diagnosticRepairActionID
	Receipt      diagnosticRepairReceipt
	Verification *diagnosticVerificationFailure
}

type diagnosticFakeRollbackStatus string

const (
	diagnosticFakeRollbackRemoved   diagnosticFakeRollbackStatus = "removed"
	diagnosticFakeRollbackPreserved diagnosticFakeRollbackStatus = "preserved"
	diagnosticFakeRollbackAbsent    diagnosticFakeRollbackStatus = "absent"
	diagnosticFakeRollbackManual    diagnosticFakeRollbackStatus = "manual"
)

type diagnosticFakeRollbackReceipt struct {
	ResourceID       string
	ResourceOwner    string
	RollbackBoundary string
	Status           diagnosticFakeRollbackStatus
	Details          []string
}

type diagnosticFakeRollbackReport struct {
	Receipts    []diagnosticFakeRollbackReceipt
	ManualItems []diagnosticRepairPlanItem
}

func TestDiagnosticFakeHostStateCoversRepairResourceFamilies(t *testing.T) {
	state := newDiagnosticDirtyHostState()

	if len(state.Users) == 0 || len(state.Groups) == 0 {
		t.Fatalf("fake host state missing users/groups: %+v %+v", state.Users, state.Groups)
	}
	if len(state.ACLs) == 0 || len(state.Files) == 0 {
		t.Fatalf("fake host state missing ACLs/files: %+v %+v", state.ACLs, state.Files)
	}
	if len(state.Credentials) == 0 || len(state.Tools) == 0 {
		t.Fatalf("fake host state missing credentials/tools: %+v %+v", state.Credentials, state.Tools)
	}
	if len(state.Integrations) == 0 {
		t.Fatalf("fake host state missing integrations: %+v", state.Integrations)
	}
	if state.DNS.BlockedDomains == nil {
		t.Fatal("fake host state missing DNS blocked-domain map")
	}
}

func TestDiagnosticFakeHostStatePlanningIsReadOnly(t *testing.T) {
	state := newDiagnosticDirtyHostState()
	before := state.clone()

	plan, findings := state.plan(diagnosticRepairExecutionRequest{Command: "check"})
	if !reflect.DeepEqual(before, state.clone()) {
		t.Fatal("planning mutated fake host state")
	}
	if plan.Mutating || plan.Execution.MutationAllowed {
		t.Fatalf("plan execution = %+v, want read-only non-mutating check", plan.Execution)
	}
	if len(plan.Items) == 0 || len(plan.ManualItems) == 0 {
		t.Fatalf("plan buckets = repair %d manual %d skipped %d, want repair and manual items", len(plan.Items), len(plan.ManualItems), len(plan.SkippedItems))
	}
	for _, id := range []diagnosticFindingID{
		findingWorkspaceSetgid,
		findingAgentUmask,
		findingDNSBlocklist,
		findingCredentialResidue,
		findingClaudeProjectPermissions,
		findingGolangCILintAccess,
	} {
		if !fakeFindingsContain(findings, id) {
			t.Fatalf("findings missing %s: %+v", id, findings)
		}
	}
}

func TestDiagnosticFakeHostStateCurrentCheckWarningScenarios(t *testing.T) {
	state := newDiagnosticDirtyHostState()
	plan, findings := state.plan(diagnosticRepairExecutionRequest{Command: "doctor"})
	scenarios := []struct {
		name   string
		id     diagnosticFindingID
		bucket string
		status string
	}{
		{"setgid inheritance drift", findingWorkspaceSetgid, "repair", "planned"},
		{"agent home permissions drift", findingAgentHomeReadable, "manual", string(diagnosticRepairUnsupported)},
		{"missing agent umask", findingAgentUmask, "repair", "planned"},
		{"stale Claude state", findingCredentialClaudeStateResidue, "repair", "planned"},
		{"legacy cloud secret key", findingCredentialCloudSecretKeyLegacy, "repair", "planned"},
		{"Gemini keychain adapter required", findingCredentialAdapterRequired, "manual", string(diagnosticRepairUnsupported)},
		{"Claude project permissions", findingClaudeProjectPermissions, "repair", "planned"},
		{"optional SSH key", findingAgentSSHKey, "skipped", string(diagnosticRepairOptional)},
		{"optional Anthropic auth", findingAnthropicAPIKey, "skipped", string(diagnosticRepairOptional)},
		{"beads toolchain resolution", findingIntegrationToolchain, "skipped", string(diagnosticRepairInformational)},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			if !fakeFindingsContain(findings, scenario.id) {
				t.Fatalf("findings missing %s: %+v", scenario.id, findings)
			}
			bucket, status, ok := fakePlanClassification(plan, scenario.id)
			if !ok {
				t.Fatalf("plan missing item for %s: %+v", scenario.id, plan)
			}
			if bucket != scenario.bucket || status != scenario.status {
				t.Fatalf("%s classified as %s/%s, want %s/%s", scenario.id, bucket, status, scenario.bucket, scenario.status)
			}
		})
	}
}

func TestDiagnosticFakeHostStateApplyPlanConvergesRepairableResources(t *testing.T) {
	state := newDiagnosticDirtyHostState()
	plan, _ := state.plan(diagnosticRepairExecutionRequest{Command: "doctor", Fix: true, YesAll: true})
	results := state.applyPlan(plan)
	if len(results) != len(plan.Items) {
		t.Fatalf("repair results = %d, want %d", len(results), len(plan.Items))
	}
	for _, result := range results {
		if result.Verification != nil {
			t.Fatalf("%s failed verification: %+v", result.Action, result.Verification)
		}
		if result.Receipt.ID == "" || !result.Receipt.Verified {
			t.Fatalf("%s receipt = %+v, want verified receipt", result.Action, result.Receipt)
		}
	}

	afterPlan, afterFindings := state.plan(diagnosticRepairExecutionRequest{Command: "check"})
	for _, finding := range afterFindings {
		if finding.Typed && finding.Definition.IsHazmatRepairable() {
			t.Fatalf("repairable finding still present after fake repair: %+v", finding)
		}
	}
	if len(afterPlan.Items) != 0 {
		t.Fatalf("repair plan still has executable items after fake repair: %+v", afterPlan.Items)
	}
}

func TestDiagnosticFakeRollbackPartialSetupReportsRemovedPreservedAndAbsent(t *testing.T) {
	state := newDiagnosticPartialSetupHostState()

	report := state.rollback(false, false)
	if state.PF.AnchorFile || state.PF.AnchorLoaded {
		t.Fatalf("pf state after rollback = %+v, want Hazmat anchor removed", state.PF)
	}
	if !state.PF.Enabled {
		t.Fatalf("pf state after rollback = %+v, want host pf enabled state preserved", state.PF)
	}
	if _, ok := state.Users[agentUser]; !ok {
		t.Fatalf("users after non-destructive rollback = %+v, want agent user preserved", state.Users)
	}
	if cred := state.Credentials[credentialHarnessClaudeState]; !cred.HostStorePresent || !cred.AgentResidue {
		t.Fatalf("Claude credential state after rollback = %+v, want host store and residue preserved for explicit cleanup", cred)
	}

	assertFakeRollbackReceipt(t, report, "network.pf", diagnosticFakeRollbackRemoved)
	assertFakeRollbackReceipt(t, report, "network.launchd-persistence", diagnosticFakeRollbackAbsent)
	assertFakeRollbackReceipt(t, report, "setup.agent-user", diagnosticFakeRollbackPreserved)
	assertFakeRollbackReceipt(t, report, "credential.claude-state", diagnosticFakeRollbackPreserved)
}

func TestDiagnosticFakeRollbackKeepsUnsupportedManualCleanupUnclaimed(t *testing.T) {
	state := newDiagnosticDirtyHostState()

	report := state.rollback(false, false)
	if !fakeRollbackManualContains(report, findingCredentialAdapterRequired) {
		t.Fatalf("manual rollback items = %+v, want credential adapter requirement left manual", report.ManualItems)
	}
	if cred := state.Credentials[credentialHarnessGeminiKeychain]; !cred.AdapterRequired {
		t.Fatalf("Gemini credential state after rollback = %+v, want adapter-required manual item still present", cred)
	}
	for _, receipt := range report.Receipts {
		if receipt.ResourceID == "credential.backend-adapter" && receipt.Status == diagnosticFakeRollbackRemoved {
			t.Fatalf("rollback receipt claims unsupported cleanup was removed: %+v", receipt)
		}
	}
}

func newDiagnosticDirtyHostState() *diagnosticFakeHostState {
	const project = "/fake/project"
	const claudeProject = "/Users/agent/.claude/projects/fake"
	state := &diagnosticFakeHostState{
		Users: map[string]diagnosticFakeUser{
			agentUser: {UID: agentUID, Home: agentHome},
		},
		Groups: map[string]diagnosticFakeGroup{
			sharedGroup: {Members: map[string]bool{agentUser: true}},
		},
		ACLs: map[string]diagnosticFakeACLState{
			project:       {Setgid: false, GroupWritable: false, AgentCanTraverse: false},
			claudeProject: {Setgid: false, GroupWritable: false, AgentCanTraverse: true},
		},
		Files: map[string]diagnosticFakeFileState{
			agentHome:             {Exists: true, Private: false},
			agentHome + "/.zshrc": {Exists: true, Content: "# missing hazmat umask block\n"},
		},
		PF: diagnosticFakePFState{
			Enabled:      false,
			AnchorFile:   false,
			AnchorLoaded: false,
		},
		DNS: diagnosticFakeDNSState{
			BlocklistPresent: false,
			BlockedDomains:   map[string]bool{"ngrok.io": false, "pastebin.com": false},
		},
		Launchd: diagnosticFakeLaunchdState{
			PlistPresent: false,
			Loaded:       false,
			PFAnchored:   false,
		},
		Credentials: map[credentialID]diagnosticFakeCredentialState{
			credentialHarnessClaudeState:    {HostStorePresent: true, AgentResidue: true},
			credentialCloudS3SecretKey:      {HostStorePresent: false, LegacyResidue: true},
			credentialProviderOpenAIAPIKey:  {HostStorePresent: true, AgentResidue: true},
			credentialHarnessGeminiKeychain: {AdapterRequired: true},
		},
		Tools: map[string]diagnosticFakeToolState{
			"golangci-lint": {AgentExecutable: false, HomebrewBacked: true},
			"tla2tools.jar": {AgentExecutable: false},
		},
		Optional: diagnosticFakeOptionalState{
			SSHKeyConfigured:      false,
			AnthropicAuthProvided: false,
		},
		Integrations: map[string]diagnosticFakeIntegrationState{
			"beads": {ToolchainResolved: false},
		},
		projectPath: project,
		agentHome:   agentHome,
		claudeProj:  claudeProject,
	}
	return state
}

func newDiagnosticPartialSetupHostState() *diagnosticFakeHostState {
	state := newDiagnosticDirtyHostState()
	state.ACLs[state.projectPath] = diagnosticFakeACLState{Setgid: true, GroupWritable: true, AgentCanTraverse: true}
	state.Files[state.agentHome] = diagnosticFakeFileState{Exists: true, Private: true}
	state.Files[state.agentHome+"/.zshrc"] = diagnosticFakeFileState{Exists: true, Content: "# hazmat managed\numask 007\n"}
	state.PF = diagnosticFakePFState{
		Enabled:      true,
		AnchorFile:   true,
		AnchorLoaded: true,
	}
	state.DNS = diagnosticFakeDNSState{
		BlocklistPresent: false,
		BlockedDomains:   map[string]bool{"ngrok.io": false, "pastebin.com": false},
	}
	state.Launchd = diagnosticFakeLaunchdState{}
	state.Credentials[credentialHarnessClaudeState] = diagnosticFakeCredentialState{HostStorePresent: true, AgentResidue: true}
	return state
}

func (s *diagnosticFakeHostState) clone() *diagnosticFakeHostState {
	cp := *s
	cp.Users = make(map[string]diagnosticFakeUser, len(s.Users))
	for k, v := range s.Users {
		cp.Users[k] = v
	}
	cp.Groups = make(map[string]diagnosticFakeGroup, len(s.Groups))
	for k, v := range s.Groups {
		members := make(map[string]bool, len(v.Members))
		for member, present := range v.Members {
			members[member] = present
		}
		cp.Groups[k] = diagnosticFakeGroup{Members: members}
	}
	cp.ACLs = make(map[string]diagnosticFakeACLState, len(s.ACLs))
	for k, v := range s.ACLs {
		cp.ACLs[k] = v
	}
	cp.Files = make(map[string]diagnosticFakeFileState, len(s.Files))
	for k, v := range s.Files {
		cp.Files[k] = v
	}
	cp.DNS.BlockedDomains = make(map[string]bool, len(s.DNS.BlockedDomains))
	for domain, blocked := range s.DNS.BlockedDomains {
		cp.DNS.BlockedDomains[domain] = blocked
	}
	cp.Credentials = make(map[credentialID]diagnosticFakeCredentialState, len(s.Credentials))
	for k, v := range s.Credentials {
		cp.Credentials[k] = v
	}
	cp.Tools = make(map[string]diagnosticFakeToolState, len(s.Tools))
	for k, v := range s.Tools {
		cp.Tools[k] = v
	}
	cp.Integrations = make(map[string]diagnosticFakeIntegrationState, len(s.Integrations))
	for k, v := range s.Integrations {
		cp.Integrations[k] = v
	}
	return &cp
}

func (s *diagnosticFakeHostState) rollback(deleteUser, deleteGroup bool) diagnosticFakeRollbackReport {
	plan, _ := s.plan(diagnosticRepairExecutionRequest{Command: "rollback"})
	report := diagnosticFakeRollbackReport{ManualItems: append([]diagnosticRepairPlanItem(nil), plan.ManualItems...)}
	for _, item := range plan.ManualItems {
		report.Receipts = append(report.Receipts, diagnosticFakeRollbackReceipt{
			ResourceID:       item.ResourceID,
			ResourceOwner:    diagnosticRepairResourceOwner(item.ResourceID),
			RollbackBoundary: item.RollbackBoundary,
			Status:           diagnosticFakeRollbackManual,
			Details:          append([]string(nil), item.Details...),
		})
	}

	if s.PF.AnchorFile || s.PF.AnchorLoaded {
		s.PF.AnchorFile = false
		s.PF.AnchorLoaded = false
		report.addReceipt("network.pf", "setup.network-pf", diagnosticFakeRollbackRemoved, "removed Hazmat pf anchor state")
	} else {
		report.addReceipt("network.pf", "setup.network-pf", diagnosticFakeRollbackAbsent, "Hazmat pf anchor state was not present")
	}
	if s.DNS.BlocklistPresent || fakeAnyDomainBlocked(s.DNS.BlockedDomains) {
		s.DNS.BlocklistPresent = false
		for domain := range s.DNS.BlockedDomains {
			s.DNS.BlockedDomains[domain] = false
		}
		report.addReceipt("network.dns-blocklist", "setup.network-dns-blocklist", diagnosticFakeRollbackRemoved, "removed Hazmat DNS blocklist state")
	} else {
		report.addReceipt("network.dns-blocklist", "setup.network-dns-blocklist", diagnosticFakeRollbackAbsent, "Hazmat DNS blocklist state was not present")
	}
	if s.Launchd.PlistPresent || s.Launchd.Loaded || s.Launchd.PFAnchored {
		s.Launchd = diagnosticFakeLaunchdState{}
		report.addReceipt("network.launchd-persistence", "setup.network-persistence", diagnosticFakeRollbackRemoved, "removed Hazmat launchd persistence")
	} else {
		report.addReceipt("network.launchd-persistence", "setup.network-persistence", diagnosticFakeRollbackAbsent, "Hazmat launchd persistence was not present")
	}
	if file := s.Files[s.agentHome+"/.zshrc"]; fakeFileContains(file, "umask 007") {
		file.Content = strings.ReplaceAll(file.Content, "umask 007\n", "")
		s.Files[s.agentHome+"/.zshrc"] = file
		report.addReceipt("agent-shell.umask", "setup.agent-shell", diagnosticFakeRollbackRemoved, "removed managed umask line")
	}
	if deleteUser {
		delete(s.Users, agentUser)
		delete(s.Files, s.agentHome)
		report.addReceipt("setup.agent-user", "setup.agent-account", diagnosticFakeRollbackRemoved, "deleted dedicated agent account")
	} else {
		report.addReceipt("setup.agent-user", "setup.agent-account", diagnosticFakeRollbackPreserved, "non-destructive rollback preserves the agent account")
	}
	if deleteGroup {
		delete(s.Groups, sharedGroup)
		report.addReceipt("workspace.group-inheritance", "setup.workspace-permissions", diagnosticFakeRollbackRemoved, "deleted shared development group")
	} else {
		report.addReceipt("workspace.group-inheritance", "setup.workspace-permissions", diagnosticFakeRollbackPreserved, "non-destructive rollback preserves the shared group")
	}
	for id, cred := range s.Credentials {
		if !cred.HostStorePresent && !cred.AgentResidue && !cred.LegacyResidue {
			continue
		}
		report.addReceipt(fakeCredentialRollbackResourceID(id), "credentials.host-secret-store", diagnosticFakeRollbackPreserved, "credential material requires explicit migration or cleanup outside rollback")
	}
	return report
}

func (r *diagnosticFakeRollbackReport) addReceipt(resourceID, boundary string, status diagnosticFakeRollbackStatus, details ...string) {
	r.Receipts = append(r.Receipts, diagnosticFakeRollbackReceipt{
		ResourceID:       resourceID,
		ResourceOwner:    diagnosticRepairResourceOwner(resourceID),
		RollbackBoundary: boundary,
		Status:           status,
		Details:          append([]string(nil), details...),
	})
}

func (s *diagnosticFakeHostState) plan(req diagnosticRepairExecutionRequest) (diagnosticRepairPlan, []uiFinding) {
	findings := s.probeFindings()
	ui := &UI{findings: findings}
	return planDiagnosticRepairs(findings, ui.recommendations(), req), findings
}

func (s *diagnosticFakeHostState) probeFindings() []uiFinding {
	var findings []uiFinding
	add := func(severity uiFindingSeverity, id diagnosticFindingID, message string, details ...string) {
		def := diagnosticFinding(id)
		if len(details) == 0 {
			details = []string{message}
		}
		findings = append(findings, uiFinding{
			Severity:   severity,
			Step:       "fake host state",
			Message:    message,
			Definition: def,
			Typed:      true,
			Details:    append([]string(nil), details...),
		})
	}

	projectACL := s.ACLs[s.projectPath]
	if !projectACL.Setgid {
		add(uiFindingWarning, findingWorkspaceSetgid, "fake project lacks setgid", s.projectPath)
	}
	if !projectACL.GroupWritable || !projectACL.AgentCanTraverse {
		add(uiFindingWarning, findingWorkspaceAccess, "fake project is not writable/traversable by the agent", s.projectPath)
	}
	if home := s.Files[s.agentHome]; home.Exists && !home.Private {
		add(uiFindingWarning, findingAgentHomeReadable, "fake agent home is host-readable", s.agentHome)
	}
	if !fakeFileContains(s.Files[s.agentHome+"/.zshrc"], "umask 007") {
		add(uiFindingWarning, findingAgentUmask, "fake agent shell lacks managed umask")
	}
	if !s.PF.Enabled || !s.PF.AnchorFile || !s.PF.AnchorLoaded {
		add(uiFindingFailure, findingPFFirewall, "fake pf state is not enforcing Hazmat rules")
	}
	if !s.DNS.BlocklistPresent || !fakeAllDomainsBlocked(s.DNS.BlockedDomains) {
		add(uiFindingFailure, findingDNSBlocklist, "fake DNS blocklist is absent or incomplete")
	}
	if !s.Launchd.PlistPresent || !s.Launchd.Loaded || !s.Launchd.PFAnchored {
		add(uiFindingFailure, findingLaunchdPersistence, "fake launchd persistence is incomplete")
	}
	if cred := s.Credentials[credentialHarnessClaudeState]; cred.AgentResidue {
		add(uiFindingWarning, findingCredentialClaudeStateResidue, "fake Claude state residue remains in agent home")
	}
	if cred := s.Credentials[credentialCloudS3SecretKey]; cred.LegacyResidue || !cred.HostStorePresent {
		add(uiFindingWarning, findingCredentialCloudSecretKeyLegacy, "fake legacy cloud secret key needs migration")
	}
	if cred := s.Credentials[credentialProviderOpenAIAPIKey]; cred.AgentResidue {
		add(uiFindingWarning, findingCredentialResidue, "fake provider API key residue remains in agent home")
	}
	if cred := s.Credentials[credentialHarnessGeminiKeychain]; cred.AdapterRequired {
		add(uiFindingWarning, findingCredentialAdapterRequired, "fake Gemini Keychain adapter is not available")
	}
	if !s.Optional.SSHKeyConfigured {
		add(uiFindingWarning, findingAgentSSHKey, "fake agent SSH key is not configured")
	}
	if !s.Optional.AnthropicAuthProvided {
		add(uiFindingWarning, findingAnthropicAPIKey, "fake Anthropic API key is not configured")
	}
	if claudeACL := s.ACLs[s.claudeProj]; !claudeACL.GroupWritable {
		add(uiFindingWarning, findingClaudeProjectPermissions, "fake Claude project directory is not group-writable", "path: "+s.claudeProj)
	}
	if integration := s.Integrations["beads"]; !integration.ToolchainResolved {
		add(uiFindingWarning, findingIntegrationToolchain, "fake beads toolchain metadata is unresolved")
	}
	if tool := s.Tools["golangci-lint"]; !tool.AgentExecutable {
		add(uiFindingWarning, findingGolangCILintAccess, "fake golangci-lint is not executable by the agent")
	}
	if tool := s.Tools["tla2tools.jar"]; !tool.AgentExecutable {
		add(uiFindingWarning, findingTLA2ToolsJar, "fake tla2tools.jar is not readable")
	}
	return findings
}

func (s *diagnosticFakeHostState) applyPlan(plan diagnosticRepairPlan) []diagnosticFakeRepairResult {
	results := make([]diagnosticFakeRepairResult, 0, len(plan.Items))
	for _, item := range plan.Items {
		results = append(results, s.applyRepair(diagnosticRepairActionID(item.RepairAction)))
	}
	return results
}

func (s *diagnosticFakeHostState) applyRepair(action diagnosticRepairActionID) diagnosticFakeRepairResult {
	result := diagnosticFakeRepairResult{Action: action}
	def, ok := diagnosticRepairAction(action)
	if !ok {
		result.Verification = &diagnosticVerificationFailure{Verification: "unknown", Details: []string{fmt.Sprintf("unknown repair action %s", action)}}
		return result
	}

	switch action {
	case "repair.workspace.setgid":
		acl := s.ACLs[s.projectPath]
		acl.Setgid = true
		s.ACLs[s.projectPath] = acl
	case "repair.workspace.access":
		acl := s.ACLs[s.projectPath]
		acl.GroupWritable = true
		acl.AgentCanTraverse = true
		s.ACLs[s.projectPath] = acl
	case "repair.agent-shell.umask":
		file := s.Files[s.agentHome+"/.zshrc"]
		file.Exists = true
		file.Content = file.Content + "# hazmat managed\numask 007\n"
		s.Files[s.agentHome+"/.zshrc"] = file
	case "repair.network.pf":
		s.PF.Enabled = true
		s.PF.AnchorFile = true
		s.PF.AnchorLoaded = true
	case "repair.network.dns-blocklist":
		s.DNS.BlocklistPresent = true
		for domain := range s.DNS.BlockedDomains {
			s.DNS.BlockedDomains[domain] = true
		}
	case "repair.network.persistence":
		s.Launchd.PlistPresent = true
		s.Launchd.Loaded = true
		s.Launchd.PFAnchored = true
	case "repair.credential.claude-state":
		cred := s.Credentials[credentialHarnessClaudeState]
		cred.HostStorePresent = true
		cred.AgentResidue = false
		s.Credentials[credentialHarnessClaudeState] = cred
	case "repair.credential.cloud-secret-key":
		cred := s.Credentials[credentialCloudS3SecretKey]
		cred.HostStorePresent = true
		cred.LegacyResidue = false
		s.Credentials[credentialCloudS3SecretKey] = cred
	case "repair.credential.residue":
		cred := s.Credentials[credentialProviderOpenAIAPIKey]
		cred.HostStorePresent = true
		cred.AgentResidue = false
		s.Credentials[credentialProviderOpenAIAPIKey] = cred
	case "repair.claude.project-permissions":
		acl := s.ACLs[s.claudeProj]
		acl.GroupWritable = true
		s.ACLs[s.claudeProj] = acl
	default:
		result.Verification = &diagnosticVerificationFailure{Verification: string(def.Verification), Details: []string{fmt.Sprintf("fake harness has no mutation for %s", action)}}
		return result
	}

	if failure := s.verifyRepair(def); failure != nil {
		result.Verification = failure
		return result
	}
	s.receiptCount++
	result.Receipt = diagnosticRepairReceipt{
		ID:       fmt.Sprintf("%s.%d", def.Receipt, s.receiptCount),
		Action:   string(action),
		Verified: true,
	}
	return result
}

func (s *diagnosticFakeHostState) verifyRepair(def diagnosticRepairActionDefinition) *diagnosticVerificationFailure {
	for _, finding := range s.probeFindings() {
		if finding.Typed && finding.Definition.RepairAction == def.ID {
			return &diagnosticVerificationFailure{
				Verification: string(def.Verification),
				Details:      []string{finding.Message},
			}
		}
	}
	return nil
}

func fakeFileContains(file diagnosticFakeFileState, needle string) bool {
	return file.Exists && strings.Contains(file.Content, needle)
}

func fakeAllDomainsBlocked(domains map[string]bool) bool {
	if len(domains) == 0 {
		return false
	}
	for _, blocked := range domains {
		if !blocked {
			return false
		}
	}
	return true
}

func fakeAnyDomainBlocked(domains map[string]bool) bool {
	for _, blocked := range domains {
		if blocked {
			return true
		}
	}
	return false
}

func fakeCredentialRollbackResourceID(id credentialID) string {
	switch id {
	case credentialHarnessClaudeState:
		return "credential.claude-state"
	case credentialCloudS3SecretKey:
		return "credential.cloud-secret-key"
	default:
		return "credential.residue"
	}
}

func fakePlanClassification(plan diagnosticRepairPlan, id diagnosticFindingID) (string, string, bool) {
	findingID := string(id)
	for _, item := range plan.Items {
		if item.FindingID == findingID {
			return "repair", item.Status, true
		}
	}
	for _, item := range plan.ManualItems {
		if item.FindingID == findingID {
			return "manual", item.Status, true
		}
	}
	for _, item := range plan.SkippedItems {
		if item.FindingID == findingID {
			return "skipped", item.Status, true
		}
	}
	return "", "", false
}

func assertFakeRollbackReceipt(t *testing.T, report diagnosticFakeRollbackReport, resourceID string, status diagnosticFakeRollbackStatus) {
	t.Helper()
	for _, receipt := range report.Receipts {
		if receipt.ResourceID == resourceID && receipt.Status == status {
			if receipt.RollbackBoundary == "" {
				t.Fatalf("receipt = %+v, want rollback boundary", receipt)
			}
			return
		}
	}
	t.Fatalf("rollback receipts = %+v, want %s/%s", report.Receipts, resourceID, status)
}

func fakeRollbackManualContains(report diagnosticFakeRollbackReport, id diagnosticFindingID) bool {
	for _, item := range report.ManualItems {
		if item.FindingID == string(id) {
			return true
		}
	}
	return false
}

func fakeFindingsContain(findings []uiFinding, id diagnosticFindingID) bool {
	for _, finding := range findings {
		if finding.Typed && finding.Definition.ID == id {
			return true
		}
	}
	return false
}

package runtimeprovider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hazmat/containment"
	"hazmat/hostfacts"
	"hazmat/sessionbackend"
	"hazmat/sessionmeta"
)

var _ Provider = fakeProvider{}

func TestPrepareRequestRequiresConstructedContract(t *testing.T) {
	if _, err := NewPrepareRequest("codex", "session-1", containment.Contract{}); err == nil {
		t.Fatal("NewPrepareRequest accepted a raw containment.Contract DTO")
	}

	req, err := NewPrepareRequest(" codex ", " session-1 ", testContract(t))
	if err != nil {
		t.Fatalf("NewPrepareRequest: %v", err)
	}
	if req.Target() != "codex" || req.SessionID() != "session-1" {
		t.Fatalf("PrepareRequest identifiers = %q/%q", req.Target(), req.SessionID())
	}
}

func TestAdmissionRequiresPreparedLaunchAndCopiesObligations(t *testing.T) {
	descriptor := mustDescriptor(t, KindDarwinNative)
	cleanup, err := NewCleanupPlan(CleanupGeneratedPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAdmission(descriptor, sessionbackend.PreparedLaunch{}, []AdmissionObligation{ObligationNoDowngrade}, cleanup, time.Unix(1, 0)); err == nil {
		t.Fatal("NewAdmission accepted zero PreparedLaunch")
	}

	obligations := []AdmissionObligation{ObligationNoDowngrade, ObligationEnforceContainment}
	admission, err := NewAdmission(descriptor, testPreparedLaunch(t), obligations, cleanup, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("NewAdmission: %v", err)
	}
	obligations[0] = ""
	if got := admission.Obligations()[0]; got != ObligationNoDowngrade {
		t.Fatalf("Admission obligations alias caller input: %q", got)
	}
	if admission.Cleanup().Obligations()[0] != CleanupGeneratedPolicy {
		t.Fatalf("Cleanup obligations = %v", admission.Cleanup().Obligations())
	}
}

func TestKnownDescriptorsCoverProviderVocabulary(t *testing.T) {
	seen := map[Kind]Descriptor{}
	for _, descriptor := range KnownDescriptors() {
		if err := descriptor.Validate(); err != nil {
			t.Fatalf("descriptor %q invalid: %v", descriptor.Kind, err)
		}
		if _, ok := seen[descriptor.Kind]; ok {
			t.Fatalf("duplicate provider descriptor %q", descriptor.Kind)
		}
		seen[descriptor.Kind] = descriptor
	}
	for _, kind := range []Kind{
		KindDarwinNative,
		KindDockerSandbox,
		KindAppleContainer,
		KindLinuxCurrentUser,
		KindLinuxAgentUser,
		KindRemoteEnvelope,
		KindUnsupportedNative,
	} {
		if _, ok := seen[kind]; !ok {
			t.Fatalf("missing provider descriptor %q", kind)
		}
	}
	if seen[KindAppleContainer].Backend != sessionbackend.KindAppleContainer {
		t.Fatalf("Apple Container descriptor = %+v", seen[KindAppleContainer])
	}
	if seen[KindAppleContainer].Kind == KindDockerSandbox {
		t.Fatal("Apple Container must remain its own provider owner")
	}
}

func TestProviderStatusVocabularyCoversKnownDescriptors(t *testing.T) {
	seenStatus := map[Status]bool{}
	for _, definition := range StatusDefinitions() {
		if definition.Label == "" || definition.Message == "" {
			t.Fatalf("status definition missing label/message: %+v", definition)
		}
		seenStatus[definition.Status] = true
	}
	for _, descriptor := range KnownDescriptors() {
		if !seenStatus[descriptor.Status] {
			t.Fatalf("descriptor %q status %q has no definition", descriptor.Kind, descriptor.Status)
		}
		record := descriptor.StatusRecord()
		if record.Provider != descriptor.Kind ||
			record.Backend != descriptor.Backend ||
			record.Status != descriptor.Status ||
			record.IdentityBoundary != descriptor.IdentityBoundary ||
			record.StatusLabel == "" {
			t.Fatalf("status record = %+v, descriptor = %+v", record, descriptor)
		}
	}
	if record := mustDescriptor(t, KindRemoteEnvelope).StatusRecord(); record.Executable || record.Status != StatusPlanOnly {
		t.Fatalf("remote status record = %+v, want non-executable plan-only", record)
	}
	if record := mustDescriptor(t, KindLinuxCurrentUser).StatusRecord(); record.Executable || record.Status != StatusPlanOnly {
		t.Fatalf("linux current-user status record = %+v, want non-executable plan-only", record)
	}
	if record := mustDescriptor(t, KindLinuxAgentUser).StatusRecord(); record.Executable || record.Status != StatusSetupRequired {
		t.Fatalf("linux agent-user status record = %+v, want setup-required", record)
	}
}

func TestRuntimeProviderStatusDocCoversKnownDescriptors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "runtime-provider-status.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, definition := range StatusDefinitions() {
		if !strings.Contains(doc, "| `"+string(definition.Status)+"` |") {
			t.Fatalf("runtime provider status doc missing status %q", definition.Status)
		}
	}
	for _, descriptor := range KnownDescriptors() {
		row := "| `" + string(descriptor.Kind) + "` | `" + string(descriptor.Backend) + "` | `" + string(descriptor.Status) + "` | `" + string(descriptor.IdentityBoundary) + "` |"
		if !strings.Contains(doc, row) {
			t.Fatalf("runtime provider status doc missing descriptor row %q", row)
		}
	}
	for _, phrase := range []string{
		"Provider admission must not silently downgrade",
		"linux.native-launch-helper-missing",
		"linux.setup-required",
		"Linux current-user VM smoke matrix",
	} {
		if !strings.Contains(doc, phrase) {
			t.Fatalf("runtime provider status doc missing %q", phrase)
		}
	}
}

func TestGapRecordRequiresStructuredIDAndRendersStableText(t *testing.T) {
	if _, err := NewGapRecord(KindLinuxCurrentUser, StatusPlanOnly, "", "missing helper", ""); err == nil {
		t.Fatal("NewGapRecord accepted empty id")
	}
	if _, err := NewGapRecord(KindLinuxCurrentUser, StatusPlanOnly, "linux.native-launch-helper-missing", "", ""); err == nil {
		t.Fatal("NewGapRecord accepted empty message")
	}
	gap := MustGapRecord(
		KindLinuxCurrentUser,
		StatusPlanOnly,
		"linux.native-launch-helper-missing",
		"Linux native launch helper is not implemented yet",
		"plan-only",
	)
	if got := RenderGap(gap); got != "linux.native-launch-helper-missing: Linux native launch helper is not implemented yet (plan-only)" {
		t.Fatalf("RenderGap = %q", got)
	}
}

func TestFakeProviderLifecycle(t *testing.T) {
	provider := fakeProvider{descriptor: mustDescriptor(t, KindDarwinNative)}
	req, err := NewPrepareRequest("codex", "session-1", testContract(t))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.Prepare(context.Background(), req)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	admission, err := provider.Admit(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	handle, err := provider.Launch(context.Background(), admission)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	result, err := provider.Monitor(context.Background(), handle)
	if err != nil {
		t.Fatalf("Monitor: %v", err)
	}
	if result.Phase != ResultContained || result.Classification != ResultPublic {
		t.Fatalf("Result = %+v", result)
	}
	cleanup, err := provider.Cleanup(context.Background(), admission.Cleanup())
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if len(cleanup.Completed) != 1 || cleanup.Completed[0] != CleanupGeneratedPolicy {
		t.Fatalf("CleanupResult = %+v", cleanup)
	}
}

func TestFakeProviderRefusesDowngradesBeforeLaunch(t *testing.T) {
	prepared := testPreparedLaunch(t)
	cases := []struct {
		name      string
		required  Requirements
		available Capabilities
		wantCode  GapCode
	}{
		{
			name: "agent user to current user",
			required: Requirements{
				IdentityBoundary: IdentityLinuxAgentUser,
			},
			available: Capabilities{
				IdentityBoundary: IdentityCurrentUser,
			},
			wantCode: GapIdentityBoundaryDowngrade,
		},
		{
			name: "root helper to rootless user namespace",
			required: Requirements{
				HelperStrategy: HelperRoot,
			},
			available: Capabilities{
				HelperStrategy: HelperRootlessUserNS,
			},
			wantCode: GapHelperStrategyDowngrade,
		},
		{
			name: "current user sandbox to ordinary same uid",
			required: Requirements{
				IdentityBoundary: IdentityCurrentUser,
				Containment:      ContainmentContractSandbox,
			},
			available: Capabilities{
				IdentityBoundary: IdentityCurrentUser,
				Containment:      ContainmentSameUIDProcess,
			},
			wantCode: GapContainmentDowngrade,
		},
		{
			name: "network none to advisory",
			required: Requirements{
				Network: NetworkNoneEnforced,
			},
			available: Capabilities{
				Network: NetworkAdvisory,
			},
			wantCode: GapNetworkDowngrade,
		},
		{
			name: "credential broker to env passthrough",
			required: Requirements{
				Credentials: CredentialBroker,
			},
			available: Capabilities{
				Credentials: CredentialEnvPassthrough,
			},
			wantCode: GapCredentialDowngrade,
		},
		{
			name: "private docker daemon to host socket",
			required: Requirements{
				Docker: DockerPrivateDaemon,
			},
			available: Capabilities{
				Docker: DockerHostSocket,
			},
			wantCode: GapDockerAuthorityDowngrade,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := downgradeRefusingProvider{
				fakeProvider: fakeProvider{descriptor: mustDescriptor(t, KindLinuxCurrentUser)},
				available:    tc.available,
			}
			admission, err := provider.AdmitWithRequirements(context.Background(), prepared, tc.required)
			if err == nil {
				if _, launchErr := provider.Launch(context.Background(), admission); launchErr != nil {
					t.Fatalf("Launch after unexpected admission: %v", launchErr)
				}
			}
			if provider.launched {
				t.Fatal("provider reached Launch after downgrade admission")
			}
			var downgrade DowngradeError
			if !errors.As(err, &downgrade) {
				t.Fatalf("err = %v, want DowngradeError", err)
			}
			if len(downgrade.Gaps) != 1 || downgrade.Gaps[0].Code != tc.wantCode {
				t.Fatalf("gaps = %+v, want %s", downgrade.Gaps, tc.wantCode)
			}
			if downgrade.Gaps[0].Message == "" || downgrade.Gaps[0].Required == "" || downgrade.Gaps[0].Available == "" {
				t.Fatalf("gap is not structured enough: %+v", downgrade.Gaps[0])
			}
		})
	}
}

func TestRequireCapabilitiesTreatsMissingProviderCapabilityAsGap(t *testing.T) {
	err := RequireCapabilities(
		Requirements{Network: NetworkNoneEnforced},
		Capabilities{},
	)
	var downgrade DowngradeError
	if !errors.As(err, &downgrade) {
		t.Fatalf("err = %v, want DowngradeError", err)
	}
	if len(downgrade.Gaps) != 1 {
		t.Fatalf("gaps = %+v, want one gap", downgrade.Gaps)
	}
	gap := downgrade.Gaps[0]
	if gap.Code != GapNetworkDowngrade || gap.Available != "unspecified" {
		t.Fatalf("gap = %+v, want network downgrade with unspecified availability", gap)
	}
}

func testContract(t *testing.T) containment.Contract {
	t.Helper()
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{{Path: "/home/agent/.ssh"}})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project: containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		AgentHome: containment.AgentHomePolicy{
			Path: "/home/agent",
		},
		Temp:    containment.TempPolicy{Path: "/tmp/hazmat-session"},
		Network: containment.NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process: containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func testPreparedLaunch(t *testing.T) sessionbackend.PreparedLaunch {
	t.Helper()
	plan := sessionbackend.BuildPlan(sessionbackend.Input{
		Target:     "codex",
		Mode:       sessionmeta.ModeNative,
		ProjectDir: "/workspace/project",
		HostFacts:  hostfacts.ForGOOS("darwin"),
	})
	prepared, err := sessionbackend.NewPreparedLaunch(
		plan,
		sessionbackend.NewDarwinSeatbeltArtifact(sessionbackend.DarwinSeatbelt{PolicyPath: "/tmp/hazmat.sb"}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func mustDescriptor(t *testing.T, kind Kind) Descriptor {
	t.Helper()
	for _, descriptor := range KnownDescriptors() {
		if descriptor.Kind == kind {
			return descriptor
		}
	}
	t.Fatalf("missing descriptor %q", kind)
	return Descriptor{}
}

type fakeProvider struct {
	descriptor Descriptor
}

func (p fakeProvider) Descriptor() Descriptor {
	return p.descriptor
}

func (p fakeProvider) Prepare(_ context.Context, req PrepareRequest) (sessionbackend.PreparedLaunch, error) {
	contract := req.Contract()
	plan := sessionbackend.BuildPlan(sessionbackend.Input{
		Target:     req.Target(),
		Mode:       sessionmeta.ModeNative,
		ProjectDir: contract.ProjectPath(),
		HostFacts:  hostfacts.ForGOOS("darwin"),
	})
	return sessionbackend.NewPreparedLaunch(
		plan,
		sessionbackend.NewDarwinSeatbeltArtifact(sessionbackend.DarwinSeatbelt{PolicyPath: "/tmp/hazmat.sb"}),
		nil,
	)
}

func (p fakeProvider) Admit(_ context.Context, launch sessionbackend.PreparedLaunch) (Admission, error) {
	cleanup, err := NewCleanupPlan(CleanupGeneratedPolicy)
	if err != nil {
		return Admission{}, err
	}
	return NewAdmission(p.descriptor, launch, []AdmissionObligation{
		ObligationNoDowngrade,
		ObligationVerifyIdentityBoundary,
		ObligationEnforceContainment,
	}, cleanup, time.Unix(1, 0))
}

func (p fakeProvider) Launch(context.Context, Admission) (LaunchHandle, error) {
	return NewLaunchHandle(p.descriptor.Kind, "fake-session")
}

func (fakeProvider) Monitor(context.Context, LaunchHandle) (Result, error) {
	return NewResult(ResultContained, ResultPublic, 0, "contained", map[string]string{"phase": "contained"})
}

func (fakeProvider) Cleanup(_ context.Context, plan CleanupPlan) (CleanupResult, error) {
	return NewCleanupResult(plan.Obligations(), nil), nil
}

type downgradeRefusingProvider struct {
	fakeProvider
	available Capabilities
	launched  bool
}

func (p *downgradeRefusingProvider) AdmitWithRequirements(ctx context.Context, launch sessionbackend.PreparedLaunch, required Requirements) (Admission, error) {
	if err := RequireCapabilities(required, p.available); err != nil {
		return Admission{}, err
	}
	return p.fakeProvider.Admit(ctx, launch)
}

func (p *downgradeRefusingProvider) Launch(ctx context.Context, admission Admission) (LaunchHandle, error) {
	p.launched = true
	return p.fakeProvider.Launch(ctx, admission)
}

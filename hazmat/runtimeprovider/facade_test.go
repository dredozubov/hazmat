package runtimeprovider

import (
	"context"
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

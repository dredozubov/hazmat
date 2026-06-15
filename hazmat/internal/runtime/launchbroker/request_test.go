package launchbroker

import "testing"

func TestAuthenticatePeerRequiresExpectedUID(t *testing.T) {
	if _, err := AuthenticatePeer(501, 501); err != nil {
		t.Fatalf("AuthenticatePeer matching uid: %v", err)
	}
	if _, err := AuthenticatePeer(502, 501); err == nil {
		t.Fatal("AuthenticatePeer accepted mismatched uid")
	}
	if _, err := AuthenticatePeer(0, 501); err == nil {
		t.Fatal("AuthenticatePeer accepted zero uid")
	}
}

func TestVerifyLaunchRequestRequiresAuthenticatedPeer(t *testing.T) {
	req := validDirectRequest()
	if _, err := VerifyLaunchRequest(AuthenticatedPeer{}, req); err == nil {
		t.Fatal("VerifyLaunchRequest accepted zero authenticated peer")
	}
}

func TestVerifyLaunchRequestValidatesDirectExecShape(t *testing.T) {
	peer, err := AuthenticatePeer(501, 501)
	if err != nil {
		t.Fatal(err)
	}

	req := validDirectRequest()
	if _, err := VerifyLaunchRequest(peer, req); err != nil {
		t.Fatalf("VerifyLaunchRequest valid direct request: %v", err)
	}

	req.WorkingDir = ""
	if _, err := VerifyLaunchRequest(peer, req); err == nil {
		t.Fatal("VerifyLaunchRequest accepted direct exec without working dir")
	}

	req = validDirectRequest()
	req.Script = "exec \"$@\""
	if _, err := VerifyLaunchRequest(peer, req); err == nil {
		t.Fatal("VerifyLaunchRequest accepted direct exec with shell script")
	}
}

func TestVerifiedLaunchRequestDefensivelyCopiesSlices(t *testing.T) {
	peer, err := AuthenticatePeer(501, 501)
	if err != nil {
		t.Fatal(err)
	}
	req := validDirectRequest()
	verified, err := VerifyLaunchRequest(peer, req)
	if err != nil {
		t.Fatal(err)
	}

	req.Args[0] = "/bin/false"
	req.EnvPairs[0] = "HOME=/tmp/changed"

	got := verified.Request()
	if got.Args[0] != "/usr/bin/true" {
		t.Fatalf("verified args aliased caller slice: %v", got.Args)
	}
	if got.EnvPairs[0] != "HOME=/Users/agent" {
		t.Fatalf("verified env aliased caller slice: %v", got.EnvPairs)
	}

	got.Args[0] = "/bin/false"
	if again := verified.Request(); again.Args[0] != "/usr/bin/true" {
		t.Fatalf("Request returned mutable internal args: %v", again.Args)
	}
}

func TestChildPlanRequiresFDCleanupPolicy(t *testing.T) {
	peer, err := AuthenticatePeer(501, 501)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyLaunchRequest(peer, validDirectRequest())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewChildPlan(verified, ChildFDPolicyUnset); err == nil {
		t.Fatal("NewChildPlan accepted unset fd cleanup policy")
	}
	plan, err := NewChildPlan(verified, ChildFDPolicyCloseInherited)
	if err != nil {
		t.Fatalf("NewChildPlan close-inherited policy: %v", err)
	}
	if !plan.RequiresInheritedFDCleanup() {
		t.Fatal("plan does not require inherited fd cleanup")
	}
}

func validDirectRequest() LaunchRequest {
	return LaunchRequest{
		PolicyPath:     "/private/tmp/hazmat-123.sb",
		DirectExec:     true,
		WorkingDir:     "/Users/dr/workspace/project",
		SessionTempDir: "/Users/agent/.cache/hazmat/tmp/123-456",
		EnvPairs:       []string{"HOME=/Users/agent", "PATH=/usr/bin"},
		Args:           []string{"/usr/bin/true"},
	}
}

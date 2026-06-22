//go:build beadpost_hostbroker

package hostbroker

import (
	"strings"
	"testing"
	"time"
)

func TestDeriveRouteFactsFromObservedInputs(t *testing.T) {
	got, err := DeriveRouteFacts(validRouteFactsInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.PrincipalKey != "uid:599" ||
		got.ProjectKey != "project:api" ||
		got.SessionKey != "session:session-1" ||
		got.BackendID != "hazmat-macos-local" ||
		got.Project != "api" ||
		got.Peer.UID != 599 ||
		got.RouteTokenID != "route-token-1" {
		t.Fatalf("route facts = %+v", got)
	}
}

func TestRouteFactsRejectWrongUID(t *testing.T) {
	input := validRouteFactsInput(t)
	input.Peer.UID = 600
	_, err := DeriveRouteFacts(input)
	if err == nil || !strings.Contains(err.Error(), "peer uid") {
		t.Fatalf("err = %v, want peer uid rejection", err)
	}
}

func TestRouteFactsRejectWrongProjectClaim(t *testing.T) {
	input := validRouteFactsInput(t)
	input.Claims.Project = "evil"
	_, err := DeriveRouteFacts(input)
	if err == nil || !strings.Contains(err.Error(), "project claim") {
		t.Fatalf("err = %v, want project claim rejection", err)
	}
}

func TestRouteFactsRejectWrongSessionClaim(t *testing.T) {
	input := validRouteFactsInput(t)
	input.Claims.SessionKey = "session:other"
	_, err := DeriveRouteFacts(input)
	if err == nil || !strings.Contains(err.Error(), "session_key claim") {
		t.Fatalf("err = %v, want session claim rejection", err)
	}
}

func TestRouteFactsRejectMissingPeerCredentials(t *testing.T) {
	input := validRouteFactsInput(t)
	input.Peer = PeerCredentials{}
	_, err := DeriveRouteFacts(input)
	if err == nil || !strings.Contains(err.Error(), "peer credentials") {
		t.Fatalf("err = %v, want missing peer credentials", err)
	}
}

func TestRouteFactsRejectExpiredToken(t *testing.T) {
	input := validRouteFactsInput(t)
	input.Token.ExpiresAt = input.Now
	_, err := DeriveRouteFacts(input)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v, want expired token", err)
	}
}

func TestRouteFactsRejectPayloadRouteMismatch(t *testing.T) {
	input := validRouteFactsInput(t)
	input.Claims.ProjectKey = "project:web"
	_, err := DeriveRouteFacts(input)
	if err == nil || !strings.Contains(err.Error(), "project_key claim") {
		t.Fatalf("err = %v, want project key mismatch", err)
	}
}

func TestRouteFactsDoNotTrustMatchingClaimsForAuthority(t *testing.T) {
	input := validRouteFactsInput(t)
	input.Claims = RouteClaims{
		PrincipalKey: "uid:599",
		ProjectKey:   "project:api",
		SessionKey:   "session:session-1",
		BackendID:    "hazmat-macos-local",
		Project:      "api",
	}
	got, err := DeriveRouteFacts(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Claims.PrincipalKey = "uid:0"
	if got.PrincipalKey != "uid:599" {
		t.Fatalf("route facts were claim-derived: %+v", got)
	}
}

func validRouteFactsInput(t *testing.T) RouteFactsInput {
	t.Helper()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	return RouteFactsInput{
		Launch:    validFacts(t),
		Peer:      PeerCredentials{Present: true, UID: 599, GID: 20, PID: 1234},
		Token:     RouteToken{ID: "route-token-1", ExpiresAt: now.Add(5 * time.Minute)},
		SessionID: "session-1",
		BackendID: "hazmat-macos-local",
		Now:       now,
	}
}

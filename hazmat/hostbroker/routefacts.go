//go:build beadpost_hostbroker

package hostbroker

import (
	"fmt"
	"strings"
	"time"
)

type PeerCredentials struct {
	Present bool
	UID     int
	GID     int
	PID     int
}

type RouteToken struct {
	ID        string
	ExpiresAt time.Time
}

type RouteClaims struct {
	PrincipalKey string
	ProjectKey   string
	SessionKey   string
	BackendID    string
	Project      string
}

type RouteFactsInput struct {
	Launch    LaunchFacts
	Peer      PeerCredentials
	Token     RouteToken
	Claims    RouteClaims
	SessionID string
	BackendID string
	Now       time.Time
}

type RouteFacts struct {
	PrincipalKey string          `json:"principal_key"`
	ProjectKey   string          `json:"project_key"`
	SessionKey   string          `json:"session_key"`
	BackendID    string          `json:"backend_id"`
	Project      string          `json:"project"`
	ProjectPath  string          `json:"project_path"`
	AgentUID     int             `json:"agent_uid"`
	Tier         string          `json:"tier"`
	Peer         PeerCredentials `json:"peer"`
	RouteTokenID string          `json:"route_token_id"`
	VerifiedAt   string          `json:"verified_at"`
}

func DeriveRouteFacts(input RouteFactsInput) (RouteFacts, error) {
	if err := confirmSandboxBoundary(input.Launch); err != nil {
		return RouteFacts{}, err
	}
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !input.Peer.Present {
		return RouteFacts{}, fmt.Errorf("route facts: peer credentials are required")
	}
	if input.Peer.UID != input.Launch.AgentUID {
		return RouteFacts{}, fmt.Errorf("route facts: peer uid %d does not match launch uid %d", input.Peer.UID, input.Launch.AgentUID)
	}
	if strings.TrimSpace(input.Token.ID) == "" {
		return RouteFacts{}, fmt.Errorf("route facts: route token id is required")
	}
	if input.Token.ExpiresAt.IsZero() || !now.Before(input.Token.ExpiresAt.UTC()) {
		return RouteFacts{}, fmt.Errorf("route facts: route token is expired")
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return RouteFacts{}, fmt.Errorf("route facts: session id is required")
	}
	if strings.TrimSpace(input.BackendID) == "" {
		return RouteFacts{}, fmt.Errorf("route facts: backend id is required")
	}
	facts := RouteFacts{
		PrincipalKey: fmt.Sprintf("uid:%d", input.Launch.AgentUID),
		ProjectKey:   "project:" + input.Launch.OriginProject,
		SessionKey:   "session:" + strings.TrimSpace(input.SessionID),
		BackendID:    strings.TrimSpace(input.BackendID),
		Project:      input.Launch.OriginProject,
		ProjectPath:  input.Launch.ProjectPath,
		AgentUID:     input.Launch.AgentUID,
		Tier:         string(input.Launch.Tier),
		Peer:         input.Peer,
		RouteTokenID: strings.TrimSpace(input.Token.ID),
		VerifiedAt:   now.Format(time.RFC3339),
	}
	if err := rejectClaimMismatch(input.Claims, facts); err != nil {
		return RouteFacts{}, err
	}
	return facts, nil
}

func rejectClaimMismatch(claims RouteClaims, facts RouteFacts) error {
	checks := []struct {
		name  string
		claim string
		want  string
	}{
		{name: "principal_key", claim: claims.PrincipalKey, want: facts.PrincipalKey},
		{name: "project_key", claim: claims.ProjectKey, want: facts.ProjectKey},
		{name: "session_key", claim: claims.SessionKey, want: facts.SessionKey},
		{name: "backend_id", claim: claims.BackendID, want: facts.BackendID},
		{name: "project", claim: claims.Project, want: facts.Project},
	}
	for _, check := range checks {
		if strings.TrimSpace(check.claim) == "" {
			continue
		}
		if check.claim != check.want {
			return fmt.Errorf("route facts: source-authored %s claim %q does not match observed %q", check.name, check.claim, check.want)
		}
	}
	return nil
}

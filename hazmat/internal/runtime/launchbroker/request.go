package launchbroker

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type PeerAuthState uint8

const (
	PeerUnauthenticated PeerAuthState = iota
	PeerAuthenticated
)

type AuthenticatedPeer struct {
	uid int
}

func AuthenticatePeer(uid, expectedUID int) (AuthenticatedPeer, error) {
	if uid <= 0 {
		return AuthenticatedPeer{}, fmt.Errorf("peer uid must be positive, got %d", uid)
	}
	if expectedUID <= 0 {
		return AuthenticatedPeer{}, fmt.Errorf("expected uid must be positive, got %d", expectedUID)
	}
	if uid != expectedUID {
		return AuthenticatedPeer{}, fmt.Errorf("peer uid %d does not match expected uid %d", uid, expectedUID)
	}
	return AuthenticatedPeer{uid: uid}, nil
}

func (p AuthenticatedPeer) UID() int {
	return p.uid
}

type LaunchRequest struct {
	PolicyPath      string
	MetadataJSON    string
	DirectExec      bool
	WorkingDir      string
	SessionTempDir  string
	EnvPairs        []string
	RuntimeEnvPairs []string
	Script          string
	Args            []string
}

type VerifiedLaunchRequest struct {
	peer AuthenticatedPeer
	req  LaunchRequest
}

func VerifyLaunchRequest(peer AuthenticatedPeer, req LaunchRequest) (VerifiedLaunchRequest, error) {
	if peer.uid <= 0 {
		return VerifiedLaunchRequest{}, errors.New("authenticated peer is required")
	}
	if err := validateRequest(req); err != nil {
		return VerifiedLaunchRequest{}, err
	}
	return VerifiedLaunchRequest{
		peer: peer,
		req: LaunchRequest{
			PolicyPath:      req.PolicyPath,
			MetadataJSON:    req.MetadataJSON,
			DirectExec:      req.DirectExec,
			WorkingDir:      req.WorkingDir,
			SessionTempDir:  req.SessionTempDir,
			EnvPairs:        append([]string(nil), req.EnvPairs...),
			RuntimeEnvPairs: append([]string(nil), req.RuntimeEnvPairs...),
			Script:          req.Script,
			Args:            append([]string(nil), req.Args...),
		},
	}, nil
}

func validateRequest(req LaunchRequest) error {
	if req.PolicyPath == "" {
		return errors.New("policy path is required")
	}
	if !filepath.IsAbs(req.PolicyPath) || filepath.Clean(req.PolicyPath) != req.PolicyPath {
		return fmt.Errorf("policy path %q must be absolute and clean", req.PolicyPath)
	}
	if len(req.Args) == 0 {
		return errors.New("launch args are required")
	}
	if req.DirectExec {
		if req.WorkingDir == "" {
			return errors.New("direct exec requires a working directory")
		}
		if !filepath.IsAbs(req.WorkingDir) || filepath.Clean(req.WorkingDir) != req.WorkingDir {
			return fmt.Errorf("working directory %q must be absolute and clean", req.WorkingDir)
		}
		if req.Script != "" {
			return errors.New("direct exec cannot include a shell script")
		}
	} else if strings.TrimSpace(req.Script) == "" {
		return errors.New("shell launch requires a script")
	}
	for _, pair := range append(append([]string{}, req.EnvPairs...), req.RuntimeEnvPairs...) {
		key, _, ok := strings.Cut(pair, "=")
		if !ok || key == "" || strings.ContainsAny(key, "\x00/") {
			return fmt.Errorf("invalid environment pair %q", pair)
		}
	}
	return nil
}

func (v VerifiedLaunchRequest) PeerUID() int {
	return v.peer.uid
}

func (v VerifiedLaunchRequest) Request() LaunchRequest {
	req := v.req
	req.EnvPairs = append([]string(nil), v.req.EnvPairs...)
	req.RuntimeEnvPairs = append([]string(nil), v.req.RuntimeEnvPairs...)
	req.Args = append([]string(nil), v.req.Args...)
	return req
}

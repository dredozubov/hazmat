package hazmat

import (
	"errors"
	"fmt"
	"os"

	"hazmat/pathpolicy"
	"hazmat/sessionrequest"
)

var credentialDenySubs = pathpolicy.CredentialDenySubpaths()

func canonicalizePath(path string) (string, error) {
	return pathpolicy.Canonicalize(path)
}

func resolveDir(target string, defaultToCwd bool) (string, error) {
	return pathpolicy.ResolveDir(target, defaultToCwd)
}

func resolveProjectRoot(project string) (string, error) {
	root, err := sessionrequest.ResolveProjectRoot(project, true, currentPathDenyPolicy())
	if err != nil {
		if isDenyZoneError(err) {
			return "", err
		}
		return "", fmt.Errorf("project: %w", err)
	}
	return root.String(), nil
}

func resolveReadOnlyGrantDirs(paths []string) ([]string, error) {
	grants, err := sessionrequest.ResolveReadOnlyGrants(paths, currentPathDenyPolicy())
	if err != nil {
		return nil, err
	}
	return readOnlyGrantDirs(grants), nil
}

func resolveReadWriteGrantDirs(paths []string) ([]string, error) {
	grants, err := sessionrequest.ResolveReadWriteGrants(paths, currentPathDenyPolicy())
	if err != nil {
		if isDenyZoneError(err) {
			return nil, err
		}
		return nil, fmt.Errorf("write dirs: %w", err)
	}
	return readWriteGrantDirs(grants), nil
}

func resolveValidatedSessionRequest(project string, readPaths, writePaths []string) (sessionrequest.Request, error) {
	request, err := sessionrequest.New(sessionrequest.Input{
		Project:             project,
		DefaultProjectToCwd: true,
		ReadOnlyPaths:       readPaths,
		ReadWritePaths:      writePaths,
		DenyPolicy:          currentPathDenyPolicy(),
	})
	if err != nil {
		return sessionrequest.Request{}, sessionRequestCompatError(err)
	}
	return request, nil
}

func sessionRequestCompatError(err error) error {
	var requestErr sessionrequest.Error
	if !errors.As(err, &requestErr) || requestErr.Err == nil {
		return err
	}
	switch requestErr.Stage {
	case sessionrequest.StageProject:
		if isDenyZoneError(requestErr.Err) {
			return requestErr.Err
		}
		return fmt.Errorf("project: %w", requestErr.Err)
	case sessionrequest.StageReadOnly:
		return requestErr.Err
	case sessionrequest.StageReadWrite:
		if isDenyZoneError(requestErr.Err) {
			return requestErr.Err
		}
		return fmt.Errorf("write dirs: %w", requestErr.Err)
	default:
		return requestErr.Err
	}
}

func expandTilde(path string) string {
	return pathpolicy.ExpandTilde(path, os.UserHomeDir)
}

func isCredentialDenyPath(canonical string) bool {
	return currentPathDenyPolicy().CredentialDenyPath(canonical)
}

func isHostStateDenyPath(canonical string) bool {
	return currentPathDenyPolicy().HostStateDenyPath(canonical)
}

func appendUniqueDirs(existing, additions []string) ([]string, []string) {
	return pathpolicy.AppendUniqueDirs(existing, additions)
}

func subtractResolvedDirs(candidates, existing []string) []string {
	return pathpolicy.SubtractResolvedDirs(candidates, existing)
}

func isDenyZoneError(err error) bool {
	var denyErr pathpolicy.DenyZoneError
	return errors.As(err, &denyErr)
}

func currentPathDenyPolicy() pathpolicy.DenyPolicy {
	home, _ := os.UserHomeDir()
	return pathpolicy.DefaultDenyPolicy(agentHome, home)
}

func readOnlyGrantDirs(grants []pathpolicy.ReadOnlyGrant) []string {
	if len(grants) == 0 {
		return nil
	}
	dirs := make([]string, 0, len(grants))
	for _, grant := range grants {
		dirs = append(dirs, grant.String())
	}
	return dirs
}

func readWriteGrantDirs(grants []pathpolicy.ReadWriteGrant) []string {
	if len(grants) == 0 {
		return nil
	}
	dirs := make([]string, 0, len(grants))
	for _, grant := range grants {
		dirs = append(dirs, grant.String())
	}
	return dirs
}

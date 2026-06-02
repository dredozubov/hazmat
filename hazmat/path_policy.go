package main

import (
	"errors"
	"fmt"
	"os"

	"hazmat/pathpolicy"
)

var credentialDenySubs = pathpolicy.CredentialDenySubpaths()

func canonicalizePath(path string) (string, error) {
	return pathpolicy.Canonicalize(path)
}

func resolveDir(target string, defaultToCwd bool) (string, error) {
	return pathpolicy.ResolveDir(target, defaultToCwd)
}

func resolveProjectRoot(project string) (string, error) {
	root, err := pathpolicy.ResolveProjectRoot(project, true, currentPathDenyPolicy())
	if err != nil {
		if isDenyZoneError(err) {
			return "", err
		}
		return "", fmt.Errorf("project: %w", err)
	}
	return root.String(), nil
}

func resolveReadOnlyGrantDirs(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	policy := currentPathDenyPolicy()
	seen := make(map[string]struct{}, len(paths))
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		grant, err := pathpolicy.ResolveReadOnlyGrant(path, policy)
		if err != nil {
			return nil, err
		}
		dir := grant.String()
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		resolved = append(resolved, dir)
	}
	return resolved, nil
}

func resolveReadWriteGrantDirs(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	policy := currentPathDenyPolicy()
	seen := make(map[string]struct{}, len(paths))
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		grant, err := pathpolicy.ResolveReadWriteGrant(path, policy)
		if err != nil {
			if isDenyZoneError(err) {
				return nil, err
			}
			return nil, fmt.Errorf("write dirs: %w", err)
		}
		dir := grant.String()
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		resolved = append(resolved, dir)
	}
	return resolved, nil
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

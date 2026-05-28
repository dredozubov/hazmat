package main

import (
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

func currentPathDenyPolicy() pathpolicy.DenyPolicy {
	home, _ := os.UserHomeDir()
	return pathpolicy.DefaultDenyPolicy(agentHome, home)
}

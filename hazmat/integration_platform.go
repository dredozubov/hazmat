package main

import "path/filepath"

type integrationPlatformConfig struct {
	Name                  string
	HomebrewCandidates    []string
	JavaHomePath          string
	GenericToolchainRoots []string
	ProbeEnv              []string
}

var integrationPlatform = platformIntegrationConfig()

func currentIntegrationPlatform() string {
	if integrationPlatform.Name == "" {
		return "unknown"
	}
	return integrationPlatform.Name
}

func platformGenericToolchainRoot(dir string) bool {
	clean := filepath.Clean(dir)
	for _, root := range integrationGenericToolchainRoots {
		if clean == filepath.Clean(root) {
			return true
		}
	}
	return false
}

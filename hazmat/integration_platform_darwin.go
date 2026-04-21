//go:build darwin

package main

func platformIntegrationConfig() integrationPlatformConfig {
	return integrationPlatformConfig{
		Name: "darwin",
		HomebrewCandidates: []string{
			"/opt/homebrew/bin/brew",
			"/usr/local/bin/brew",
		},
		JavaHomePath: "/usr/libexec/java_home",
		GenericToolchainRoots: []string{
			"/",
			"/System",
			"/Library",
			"/bin",
			"/sbin",
			"/usr",
			"/usr/local",
			"/opt",
			"/opt/homebrew",
		},
		ProbeEnv: []string{
			"HOMEBREW_NO_AUTO_UPDATE=1",
		},
	}
}

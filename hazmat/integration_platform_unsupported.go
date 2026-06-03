//go:build !darwin

package hazmat

import "runtime"

func platformIntegrationConfig() integrationPlatformConfig {
	return integrationPlatformConfig{
		Name: runtime.GOOS,
		GenericToolchainRoots: []string{
			"/",
			"/bin",
			"/sbin",
			"/usr",
			"/usr/local",
			"/opt",
		},
	}
}

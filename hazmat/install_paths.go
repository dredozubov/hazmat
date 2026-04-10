package main

import (
	"os"
	"path/filepath"
)

const systemHazmatBin = "/usr/local/bin/hazmat"

var currentExecutablePath = os.Executable

func userLocalPath(parts ...string) string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}

	segments := append([]string{home, ".local"}, parts...)
	return filepath.Join(segments...)
}

func userHazmatBin() string {
	return userLocalPath("bin", "hazmat")
}

func userLaunchHelper() string {
	return userLocalPath("libexec", "hazmat-launch")
}

func launchHelperPath() string {
	if override := os.Getenv("HAZMAT_LAUNCH_HELPER"); override != "" {
		return override
	}

	if exe, err := currentExecutablePath(); err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
			exe = resolved
		}
		switch exe {
		case userHazmatBin():
			return userLaunchHelper()
		case systemHazmatBin:
			return systemLaunchHelper
		}
	}

	if helper := userLaunchHelper(); helper != "" {
		if info, err := os.Stat(helper); err == nil && info.Mode().IsRegular() {
			return helper
		}
	}

	return systemLaunchHelper
}

func launchHelperUsesDigest(path string) bool {
	return path != systemLaunchHelper
}

package main

import (
	"reflect"
	"testing"
)

func TestHostToolGlobalsComeFromPlatformResolver(t *testing.T) {
	paths := platformHostToolPaths()
	got := hostToolPaths{
		sudo:      hostSudoPath,
		chmod:     hostChmodPath,
		chown:     hostChownPath,
		ls:        hostLsPath,
		log:       hostLogPath,
		dscl:      hostDsclPath,
		pfctl:     hostPfctlPath,
		launchctl: hostLaunchctlPath,
		uname:     hostUnamePath,
		script:    hostScriptPath,
		diff:      hostDiffPath,
		tee:       hostTeePath,

		gitAllowlistCandidates: gitAllowlistCandidates,
	}
	if !reflect.DeepEqual(got, paths) {
		t.Fatalf("host tool globals = %#v, want platform resolver %#v", got, paths)
	}
}

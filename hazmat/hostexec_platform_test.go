package hazmat

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
		lsof:      hostLsofPath,
		dscl:      hostDsclPath,
		pfctl:     hostPfctlPath,
		launchctl: hostLaunchctlPath,
		uname:     hostUnamePath,
		script:    hostScriptPath,
		diff:      hostDiffPath,
		tee:       hostTeePath,
		security:  hostSecurityPath,

		gitAllowlistCandidates: gitAllowlistCandidates,
	}
	if !reflect.DeepEqual(got, paths) {
		t.Fatalf("host tool globals = %#v, want platform resolver %#v", got, paths)
	}
}

package darwin

import (
	"reflect"
	"testing"
)

func TestCommandSudoArgsBuildsLaunchHelperInvocation(t *testing.T) {
	got := CommandSudoArgs(CommandRequest{
		AgentUser:        "agent",
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
		PolicyPath:       "/private/tmp/hazmat-test.sb",
		MetadataJSON:     `{"mode":"native"}`,
		EnvPairs:         []string{"HOME=/Users/agent", "PATH=/usr/bin"},
		RuntimeEnvPairs:  []string{"GIT_SSH_COMMAND=helper"},
		Script:           `exec "$@"`,
		Args:             []string{"claude", "-p", "hi"},
	})

	want := []string{
		"-u", "agent",
		"/usr/local/libexec/hazmat-launch", "/private/tmp/hazmat-test.sb",
		"--hazmat-metadata-json", `{"mode":"native"}`,
		"/usr/bin/env", "-i",
		"HOME=/Users/agent", "PATH=/usr/bin",
		"GIT_SSH_COMMAND=helper",
		"/bin/zsh", "-lc", `exec "$@"`, "zsh",
		"claude", "-p", "hi",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CommandSudoArgs() = %v, want %v", got, want)
	}
}

func TestCommandSudoArgsPassesLaunchProfileFlag(t *testing.T) {
	got := CommandSudoArgs(CommandRequest{
		AgentUser:        "agent",
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
		PolicyPath:       "/private/tmp/hazmat-test.sb",
		Profile:          true,
		EnvPairs:         []string{"HOME=/Users/agent", "PATH=/usr/bin"},
		Script:           `exec "$@"`,
		Args:             []string{"/usr/bin/true"},
	})

	wantPrefix := []string{
		"-u", "agent",
		"/usr/local/libexec/hazmat-launch",
		"--hazmat-launch-profile",
		"/private/tmp/hazmat-test.sb",
	}
	if !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("CommandSudoArgs() prefix = %v, want %v", got[:len(wantPrefix)], wantPrefix)
	}
}

func TestPlatformEnvPairsCopiesDarwinCompilerGuards(t *testing.T) {
	got := PlatformEnvPairs()
	for _, want := range []string{
		"HOMEBREW_NO_AUTO_UPDATE=1",
		"DEVELOPER_DIR=/Library/Developer/CommandLineTools",
		"SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk",
		"CC=/Library/Developer/CommandLineTools/usr/bin/cc",
		"CXX=/Library/Developer/CommandLineTools/usr/bin/c++",
	} {
		if !contains(got, want) {
			t.Fatalf("PlatformEnvPairs() missing %q: %v", want, got)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

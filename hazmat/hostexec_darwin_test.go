//go:build darwin

package main

import (
	"reflect"
	"testing"
)

func TestDarwinHostToolPaths(t *testing.T) {
	got := platformHostToolPaths()
	want := hostToolPaths{
		sudo:      "/usr/bin/sudo",
		chmod:     "/bin/chmod",
		chown:     "/usr/sbin/chown",
		ls:        "/bin/ls",
		dscl:      "/usr/bin/dscl",
		pfctl:     "/sbin/pfctl",
		launchctl: "/bin/launchctl",
		uname:     "/usr/bin/uname",
		script:    "/usr/bin/script",
		diff:      "/usr/bin/diff",
		tee:       "/usr/bin/tee",
		gitAllowlistCandidates: []string{
			"/opt/homebrew/bin/git",
			"/usr/local/bin/git",
			"/usr/bin/git",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Darwin host tool paths = %#v, want %#v", got, want)
	}
}

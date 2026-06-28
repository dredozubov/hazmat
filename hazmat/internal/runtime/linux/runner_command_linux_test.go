//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandAgentUserRootHelperExecutesHelperProtocol(t *testing.T) {
	dir := t.TempDir()
	helperPath := filepath.Join(dir, "hazmat-linux-root-helper")
	script := `#!/bin/sh
set -eu
if [ "$1" != "run-agent" ]; then
	exit 97
fi
metadata=""
while [ "$#" -gt 0 ]; do
	case "$1" in
		--metadata)
			shift
			metadata="$1"
			;;
	esac
	shift
done
if [ -z "$metadata" ]; then
	exit 98
fi
printf '[{"phase":"planned"},{"phase":"launched"},{"phase":"contained","enforcement_complete":true}]' > "$metadata"
printf 'helper stdout\n'
printf 'helper stderr\n' >&2
exit 13
`
	if err := os.WriteFile(helperPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	helper, err := NewCommandAgentUserRootHelper(helperPath)
	if err != nil {
		t.Fatalf("NewCommandAgentUserRootHelper: %v", err)
	}
	store := SidecarStore{Dir: dir}
	var stdout, stderr bytes.Buffer

	result, err := helper.Execute(context.Background(), AgentUserHelperRequest{
		SpecPath:     filepath.Join(dir, "launch.json"),
		SpecSHA256:   "sha256",
		SpecNonce:    "nonce",
		MetadataPath: store.MetadataPath(),
	}, RunOptions{
		Sidecar: store,
		Stdout:  &stdout,
		Stderr:  &stderr,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ExitCode != 13 {
		t.Fatalf("exit code = %d, want 13", result.ExitCode)
	}
	if stdout.String() != "helper stdout\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "helper stderr\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
	events, err := ReadMetadataSidecar(store.MetadataPath())
	if err != nil {
		t.Fatalf("ReadMetadataSidecar: %v", err)
	}
	if err := validateRunMetadata(events); err != nil {
		t.Fatalf("validateRunMetadata: %v", err)
	}
}

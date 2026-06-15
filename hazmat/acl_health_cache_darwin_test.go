//go:build darwin

package hazmat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadStartupACLsForPathsUsesValidatedCache(t *testing.T) {
	projectDir := t.TempDir()
	if _, ok := currentACLHealthPathState(projectDir); !ok {
		t.Skip("ACL health path state is not available")
	}

	savedPath := startupACLHealthCachePath
	cachePath := filepath.Join(t.TempDir(), "acl-health.json")
	startupACLHealthCachePath = func() string {
		return cachePath
	}
	t.Cleanup(func() {
		startupACLHealthCachePath = savedPath
	})

	backend := &pathRecordingACLBackend{
		rows: map[string][]ACLRow{
			projectDir: {rowForGrant(devGroupInheritableGrant)},
		},
	}
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return backend
	}
	t.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	first := readStartupACLsForPaths([]string{projectDir})
	if !first[projectDir].OK || !aclRowsSatisfy(first[projectDir].Rows, devGroupInheritableGrant) {
		t.Fatalf("first result = %#v, want dev ACL", first[projectDir])
	}
	if len(backend.reads) != 1 {
		t.Fatalf("initial ACL reads = %v, want one read", backend.reads)
	}

	backend.reads = nil
	second := readStartupACLsForPaths([]string{projectDir})
	if !second[projectDir].OK || !aclRowsSatisfy(second[projectDir].Rows, devGroupInheritableGrant) {
		t.Fatalf("second result = %#v, want cached dev ACL", second[projectDir])
	}
	if len(backend.reads) != 0 {
		t.Fatalf("cached ACL reads = %v, want no reads", backend.reads)
	}

	if err := os.Chmod(projectDir, 0o755); err != nil {
		t.Fatalf("chmod project dir: %v", err)
	}
	backend.reads = nil
	_ = readStartupACLsForPaths([]string{projectDir})
	if len(backend.reads) != 1 {
		t.Fatalf("ACL reads after metadata change = %v, want one fresh read", backend.reads)
	}
}

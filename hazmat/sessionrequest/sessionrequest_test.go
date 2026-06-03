package sessionrequest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hazmat/pathpolicy"
)

func TestNewBuildsTypedRequestAndDedupsGrants(t *testing.T) {
	project := t.TempDir()
	readDir := mkdir(t, project, "read")
	writeDir := mkdir(t, project, "write")
	wantProject := canonical(t, project)
	wantRead := canonical(t, readDir)
	wantWrite := canonical(t, writeDir)
	policy := pathpolicy.DefaultDenyPolicy(filepath.Join(project, "agent"), filepath.Join(project, "home"))

	request, err := New(Input{
		Project:        project,
		ReadOnlyPaths:  []string{readDir, readDir},
		ReadWritePaths: []string{writeDir, writeDir},
		DenyPolicy:     policy,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if request.ProjectDir() != wantProject {
		t.Fatalf("ProjectDir = %q, want %q", request.ProjectDir(), wantProject)
	}
	if got := request.ReadOnlyDirs(); len(got) != 1 || got[0] != wantRead {
		t.Fatalf("ReadOnlyDirs = %v", got)
	}
	if got := request.ReadWriteDirs(); len(got) != 1 || got[0] != wantWrite {
		t.Fatalf("ReadWriteDirs = %v", got)
	}

	readGrants := request.ReadOnlyGrants()
	readGrants[0] = pathpolicy.ReadOnlyGrant{}
	if got := request.ReadOnlyDirs(); len(got) != 1 || got[0] != wantRead {
		t.Fatalf("ReadOnlyGrants returned storage alias: %v", got)
	}
}

func TestNewReportsStageAndPreservesDenyZoneError(t *testing.T) {
	home := t.TempDir()
	project := mkdir(t, home, "project")
	ssh := mkdir(t, home, ".ssh")
	policy := pathpolicy.DefaultDenyPolicy(filepath.Join(home, "agent"), home)

	_, err := New(Input{
		Project:       project,
		ReadOnlyPaths: []string{ssh},
		DenyPolicy:    policy,
	})
	if err == nil {
		t.Fatal("New succeeded, want deny-zone error")
	}
	var requestErr Error
	if !errors.As(err, &requestErr) || requestErr.Stage != StageReadOnly {
		t.Fatalf("request error = %#v, want read-only stage", err)
	}
	var denyErr pathpolicy.DenyZoneError
	if !errors.As(err, &denyErr) || denyErr.Zone != "credential" {
		t.Fatalf("deny error = %#v", err)
	}
}

func TestNewRequiresConfiguredDenyPolicy(t *testing.T) {
	project := t.TempDir()
	_, err := New(Input{Project: project})
	if err == nil {
		t.Fatal("New succeeded with zero deny policy")
	}
	var requestErr Error
	if !errors.As(err, &requestErr) || requestErr.Stage != StageProject {
		t.Fatalf("request error = %#v, want project stage", err)
	}
}

func mkdir(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

func canonical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("eval symlinks %s: %v", path, err)
	}
	return filepath.Clean(resolved)
}

package hazmat

import (
	"errors"
	"reflect"
	"testing"
)

type recordingACLBackend struct {
	readPath   string
	chmodArgs  []string
	sudoReason string
	sudoArgs   []string
}

func (b *recordingACLBackend) ReadACLs(path string) ([]ACLRow, error) {
	b.readPath = path
	return []ACLRow{{Index: 0, Principal: agentTraverseGrant.Principal, Kind: ACLAllow}}, nil
}

func (b *recordingACLBackend) Chmod(args ...string) error {
	b.chmodArgs = append([]string(nil), args...)
	return nil
}

func (b *recordingACLBackend) SudoChmod(_ *Runner, reason string, args ...string) error {
	b.sudoReason = reason
	b.sudoArgs = append([]string(nil), args...)
	return nil
}

func TestACLHelpersRouteThroughPlatformBackend(t *testing.T) {
	backend := &recordingACLBackend{}
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return backend
	}
	t.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	rows, err := readACLs("/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if backend.readPath != "/tmp/project" {
		t.Fatalf("read path = %q, want /tmp/project", backend.readPath)
	}
	if len(rows) != 1 || rows[0].Principal != agentTraverseGrant.Principal {
		t.Fatalf("rows = %#v, want agent traverse row", rows)
	}

	if err := (directACLInvoker{}).Chmod("+a", "grant", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"+a", "grant", "/tmp/project"}; !reflect.DeepEqual(backend.chmodArgs, want) {
		t.Fatalf("direct chmod args = %v, want %v", backend.chmodArgs, want)
	}

	if err := (sudoACLInvoker{runner: &Runner{}, reason: "allow traverse"}).Chmod("-a", "grant", "/Users/dr"); err != nil {
		t.Fatal(err)
	}
	if backend.sudoReason != "allow traverse" {
		t.Fatalf("sudo reason = %q, want allow traverse", backend.sudoReason)
	}
	if want := []string{"-a", "grant", "/Users/dr"}; !reflect.DeepEqual(backend.sudoArgs, want) {
		t.Fatalf("sudo chmod args = %v, want %v", backend.sudoArgs, want)
	}
}

func TestACLHelpersPropagatePlatformBackendErrors(t *testing.T) {
	wantErr := errors.New("acl backend failed")
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return failingACLBackend{err: wantErr}
	}
	t.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	if _, err := readACLs("/tmp/project"); !errors.Is(err, wantErr) {
		t.Fatalf("readACLs error = %v, want %v", err, wantErr)
	}
	if err := (directACLInvoker{}).Chmod("+a", "grant", "/tmp/project"); !errors.Is(err, wantErr) {
		t.Fatalf("direct chmod error = %v, want %v", err, wantErr)
	}
	if err := (sudoACLInvoker{runner: &Runner{}, reason: "allow traverse"}).Chmod("+a", "grant", "/Users/dr"); !errors.Is(err, wantErr) {
		t.Fatalf("sudo chmod error = %v, want %v", err, wantErr)
	}
}

func TestReadACLsForPathsUsesOptionalBatchBackend(t *testing.T) {
	backend := &recordingBatchACLBackend{
		batchResults: map[string]aclReadResult{
			"/tmp/one": {Rows: []ACLRow{rowForGrant(agentTraverseGrant)}, OK: true},
		},
		fallbackRows: map[string][]ACLRow{
			"/tmp/two": {rowForGrant(devGroupGrant)},
		},
	}
	savedFactory := platformACLBackendFactory
	platformACLBackendFactory = func() platformACLBackend {
		return backend
	}
	t.Cleanup(func() {
		platformACLBackendFactory = savedFactory
	})

	results := readACLsForPaths([]string{"/tmp/one", "/tmp/two", "/tmp/one", ""})
	if want := []string{"/tmp/one", "/tmp/two"}; !reflect.DeepEqual(backend.batchPaths, want) {
		t.Fatalf("batch paths = %v, want %v", backend.batchPaths, want)
	}
	if want := []string{"/tmp/two"}; !reflect.DeepEqual(backend.fallbackReads, want) {
		t.Fatalf("fallback reads = %v, want %v", backend.fallbackReads, want)
	}
	if !results["/tmp/one"].OK || !aclRowsSatisfy(results["/tmp/one"].Rows, agentTraverseGrant) {
		t.Fatalf("/tmp/one result = %#v, want agent traverse rows", results["/tmp/one"])
	}
	if !results["/tmp/two"].OK || !aclRowsSatisfy(results["/tmp/two"].Rows, devGroupGrant) {
		t.Fatalf("/tmp/two result = %#v, want fallback dev rows", results["/tmp/two"])
	}
}

type failingACLBackend struct {
	err error
}

func (b failingACLBackend) ReadACLs(string) ([]ACLRow, error) {
	return nil, b.err
}

func (b failingACLBackend) Chmod(...string) error {
	return b.err
}

func (b failingACLBackend) SudoChmod(*Runner, string, ...string) error {
	return b.err
}

type recordingBatchACLBackend struct {
	batchPaths    []string
	batchResults  map[string]aclReadResult
	fallbackReads []string
	fallbackRows  map[string][]ACLRow
}

func (b *recordingBatchACLBackend) ReadACLs(path string) ([]ACLRow, error) {
	b.fallbackReads = append(b.fallbackReads, path)
	return b.fallbackRows[path], nil
}

func (b *recordingBatchACLBackend) ReadACLsForPaths(paths []string) map[string]aclReadResult {
	b.batchPaths = append(b.batchPaths, paths...)
	return b.batchResults
}

func (b *recordingBatchACLBackend) Chmod(...string) error {
	return nil
}

func (b *recordingBatchACLBackend) SudoChmod(*Runner, string, ...string) error {
	return nil
}

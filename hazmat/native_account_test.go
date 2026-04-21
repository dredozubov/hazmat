package main

import (
	"errors"
	"reflect"
	"testing"
)

type recordingNativeAccountBackend struct {
	calls []string

	setupErr  error
	uidErr    error
	gidErr    error
	memberErr error

	uidTaken bool
	gidTaken bool
	member   bool
}

func (b *recordingNativeAccountBackend) SetupAgentUser(*UI, *Runner) error {
	b.calls = append(b.calls, "setupAgentUser")
	return b.setupErr
}

func (b *recordingNativeAccountBackend) SetupDevGroup(_ *UI, _ *Runner, currentUser string) error {
	b.calls = append(b.calls, "setupDevGroup:"+currentUser)
	return b.setupErr
}

func (b *recordingNativeAccountBackend) RollbackAgentUser(*UI, *Runner) {
	b.calls = append(b.calls, "rollbackAgentUser")
}

func (b *recordingNativeAccountBackend) RollbackDevGroup(*UI, *Runner) {
	b.calls = append(b.calls, "rollbackDevGroup")
}

func (b *recordingNativeAccountBackend) UIDTaken(uid string) (bool, error) {
	b.calls = append(b.calls, "uidTaken:"+uid)
	return b.uidTaken, b.uidErr
}

func (b *recordingNativeAccountBackend) GIDTaken(gid string) (bool, error) {
	b.calls = append(b.calls, "gidTaken:"+gid)
	return b.gidTaken, b.gidErr
}

func (b *recordingNativeAccountBackend) GroupMembershipContains(group, username string) (bool, error) {
	b.calls = append(b.calls, "groupMembershipContains:"+group+":"+username)
	return b.member, b.memberErr
}

func TestAccountHelpersRouteThroughPlatformBackend(t *testing.T) {
	backend := &recordingNativeAccountBackend{
		uidTaken: true,
		gidTaken: true,
		member:   true,
	}
	savedFactory := nativeAccountBackendFactory
	nativeAccountBackendFactory = func() nativeAccountBackend {
		return backend
	}
	t.Cleanup(func() {
		nativeAccountBackendFactory = savedFactory
	})

	if err := setupAgentUser(&UI{}, &Runner{}); err != nil {
		t.Fatal(err)
	}
	if err := setupDevGroup(&UI{}, &Runner{}, "dr"); err != nil {
		t.Fatal(err)
	}
	if taken, err := uidTaken("599"); err != nil || !taken {
		t.Fatalf("uidTaken = %v, %v; want true, nil", taken, err)
	}
	if taken, err := gidTaken("599"); err != nil || !taken {
		t.Fatalf("gidTaken = %v, %v; want true, nil", taken, err)
	}
	if member, err := groupMembershipContains(sharedGroup, agentUser); err != nil || !member {
		t.Fatalf("groupMembershipContains = %v, %v; want true, nil", member, err)
	}
	rollbackAgentUser(&UI{}, &Runner{})
	rollbackDevGroup(&UI{}, &Runner{})

	want := []string{
		"setupAgentUser",
		"setupDevGroup:dr",
		"uidTaken:599",
		"gidTaken:599",
		"groupMembershipContains:" + sharedGroup + ":" + agentUser,
		"rollbackAgentUser",
		"rollbackDevGroup",
	}
	if !reflect.DeepEqual(backend.calls, want) {
		t.Fatalf("account backend calls = %v, want %v", backend.calls, want)
	}
}

func TestAccountHelpersPropagatePlatformBackendErrors(t *testing.T) {
	wantErr := errors.New("account backend failed")
	backend := &recordingNativeAccountBackend{
		setupErr:  wantErr,
		uidErr:    wantErr,
		gidErr:    wantErr,
		memberErr: wantErr,
	}
	savedFactory := nativeAccountBackendFactory
	nativeAccountBackendFactory = func() nativeAccountBackend {
		return backend
	}
	t.Cleanup(func() {
		nativeAccountBackendFactory = savedFactory
	})

	if err := setupAgentUser(&UI{}, &Runner{}); !errors.Is(err, wantErr) {
		t.Fatalf("setupAgentUser error = %v, want %v", err, wantErr)
	}
	if err := setupDevGroup(&UI{}, &Runner{}, "dr"); !errors.Is(err, wantErr) {
		t.Fatalf("setupDevGroup error = %v, want %v", err, wantErr)
	}
	if _, err := uidTaken("599"); !errors.Is(err, wantErr) {
		t.Fatalf("uidTaken error = %v, want %v", err, wantErr)
	}
	if _, err := gidTaken("599"); !errors.Is(err, wantErr) {
		t.Fatalf("gidTaken error = %v, want %v", err, wantErr)
	}
	if _, err := groupMembershipContains(sharedGroup, agentUser); !errors.Is(err, wantErr) {
		t.Fatalf("groupMembershipContains error = %v, want %v", err, wantErr)
	}
}

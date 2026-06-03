package setup

import (
	"errors"
	"testing"
)

func TestSetupHomeDirTraverseSkipsWhenACLAlreadyAllows(t *testing.T) {
	ui := &fakeToolingUI{}
	var ensured bool

	err := SetupHomeDirTraverse(HomeTraverseEnv{
		HomeDir:             "/Users/dr",
		AllowsAgentTraverse: func(string) bool { return true },
		HasAgentTraverseACL: func(string) bool { return true },
		EnsureAgentTraverseACL: func(string) error {
			ensured = true
			return nil
		},
	}, ui)
	if err != nil {
		t.Fatalf("SetupHomeDirTraverse: %v", err)
	}
	if ensured {
		t.Fatal("ensure callback ran even though traversal was already allowed")
	}
}

func TestSetupHomeDirTraverseEnsuresMissingACL(t *testing.T) {
	var ensuredPath string

	err := SetupHomeDirTraverse(HomeTraverseEnv{
		HomeDir:             "/Users/dr",
		AllowsAgentTraverse: func(string) bool { return false },
		HasAgentTraverseACL: func(string) bool { return false },
		EnsureAgentTraverseACL: func(path string) error {
			ensuredPath = path
			return nil
		},
	}, &fakeToolingUI{})
	if err != nil {
		t.Fatalf("SetupHomeDirTraverse: %v", err)
	}
	if ensuredPath != "/Users/dr" {
		t.Fatalf("ensured path = %q, want /Users/dr", ensuredPath)
	}
}

func TestRollbackHomeDirTraverseRemovesOnlyWhenACLPresent(t *testing.T) {
	var removedPath string

	RollbackHomeDirTraverse(HomeTraverseEnv{
		HomeDir:             "/Users/dr",
		HasAgentTraverseACL: func(string) bool { return true },
		RemoveAgentTraverseACL: func(path string) error {
			removedPath = path
			return nil
		},
	}, &fakeToolingUI{})

	if removedPath != "/Users/dr" {
		t.Fatalf("removed path = %q, want /Users/dr", removedPath)
	}
}

func TestRollbackHomeDirTraverseWarnsOnRemoveFailure(t *testing.T) {
	ui := &recordingStepStatusUI{}

	RollbackHomeDirTraverse(HomeTraverseEnv{
		HomeDir:             "/Users/dr",
		HasAgentTraverseACL: func(string) bool { return true },
		RemoveAgentTraverseACL: func(string) error {
			return errors.New("boom")
		},
	}, ui)

	if len(ui.warns) != 1 {
		t.Fatalf("warnings = %v, want one warning", ui.warns)
	}
}

type recordingStepStatusUI struct {
	warns []string
}

func (recordingStepStatusUI) Step(string)     {}
func (recordingStepStatusUI) SkipDone(string) {}
func (u *recordingStepStatusUI) WarnMsg(msg string) {
	u.warns = append(u.warns, msg)
}
func (recordingStepStatusUI) Ok(string) {}

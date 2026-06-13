package hazmat

import (
	"errors"
	"os"
	"os/user"
	"strings"
	"testing"
	"time"
)

func TestRequireAgentUser(t *testing.T) {
	originalLookup := lookupAgentUser
	t.Cleanup(func() {
		lookupAgentUser = originalLookup
	})

	lookupAgentUser = func() (*user.User, error) {
		return nil, errors.New("missing")
	}

	_, err := requireAgentUser()
	if err == nil || !strings.Contains(err.Error(), initBaselineAdvice) {
		t.Fatalf("err = %v, want init guidance", err)
	}

	lookupAgentUser = func() (*user.User, error) {
		return &user.User{Username: agentUser, HomeDir: agentHome}, nil
	}

	agentInfo, err := requireAgentUser()
	if err != nil {
		t.Fatalf("requireAgentUser returned error: %v", err)
	}
	if agentInfo.Username != agentUser {
		t.Fatalf("agentInfo.Username = %q, want %q", agentInfo.Username, agentUser)
	}
}

func TestRequireInitRoutesPartialSetupDriftToDoctor(t *testing.T) {
	originalLookup := lookupAgentUser
	originalStat := statSetupArtifact
	t.Cleanup(func() {
		lookupAgentUser = originalLookup
		statSetupArtifact = originalStat
	})

	lookupAgentUser = func() (*user.User, error) {
		return &user.User{Username: agentUser, HomeDir: agentHome}, nil
	}

	t.Run("missing sudoers", func(t *testing.T) {
		statSetupArtifact = func(string) (os.FileInfo, error) {
			return nil, errors.New("missing")
		}
		err := requireInit()
		if err == nil {
			t.Fatal("requireInit succeeded, want setup drift error")
		}
		assertSetupDriftAdvice(t, err.Error())
		if !strings.Contains(err.Error(), "sudoers rule missing") {
			t.Fatalf("err = %q, want sudoers context", err)
		}
	})

	t.Run("missing launch helper", func(t *testing.T) {
		statSetupArtifact = func(path string) (os.FileInfo, error) {
			if path == sudoersFile {
				return fakeFileInfo{}, nil
			}
			return nil, errors.New("missing")
		}
		err := requireInit()
		if err == nil {
			t.Fatal("requireInit succeeded, want setup drift error")
		}
		assertSetupDriftAdvice(t, err.Error())
		if !strings.Contains(err.Error(), "launch helper missing") {
			t.Fatalf("err = %q, want launch helper context", err)
		}
	})
}

func TestRequireInitMissingAgentUserKeepsFreshSetupGuidance(t *testing.T) {
	originalLookup := lookupAgentUser
	t.Cleanup(func() {
		lookupAgentUser = originalLookup
	})

	lookupAgentUser = func() (*user.User, error) {
		return nil, errors.New("missing")
	}

	err := requireInit()
	if err == nil {
		t.Fatal("requireInit succeeded, want missing baseline setup error")
	}
	if !strings.Contains(err.Error(), initBaselineAdvice) {
		t.Fatalf("err = %q, want baseline init advice", err)
	}
	if strings.Contains(err.Error(), "hazmat doctor --fix") {
		t.Fatalf("err = %q, missing baseline user should not be classified as setup drift", err)
	}
}

func TestInspectAgentProbeGateMissingAgentUserDefersToRepairPlan(t *testing.T) {
	originalLookup := lookupAgentUser
	originalStat := statSetupArtifact
	t.Cleanup(func() {
		lookupAgentUser = originalLookup
		statSetupArtifact = originalStat
	})

	lookupAgentUser = func() (*user.User, error) {
		return nil, errors.New("missing")
	}
	statSetupArtifact = func(string) (os.FileInfo, error) {
		return fakeExecutableFileInfo{}, nil
	}

	gate := inspectAgentProbeGate()
	if gate.Allowed() {
		t.Fatal("agent probe gate allowed probes, want blocked for missing agent user")
	}
	reason := gate.Reason()
	if !strings.Contains(reason, "baseline setup is missing") || !strings.Contains(reason, "typed repair plan") {
		t.Fatalf("reason = %q, want baseline context that defers to repair plan", reason)
	}
	for _, stale := range []string{"hazmat init", "hazmat doctor --fix", "hazmat doctor --dry-run"} {
		if strings.Contains(reason, stale) {
			t.Fatalf("reason = %q, skip reason should not compete with repair_plan command %q", reason, stale)
		}
	}
}

func TestInspectAgentProbeGatePartialSetupDriftUsesDoctorRepairPath(t *testing.T) {
	originalLookup := lookupAgentUser
	originalStat := statSetupArtifact
	t.Cleanup(func() {
		lookupAgentUser = originalLookup
		statSetupArtifact = originalStat
	})

	lookupAgentUser = func() (*user.User, error) {
		return &user.User{Username: agentUser, HomeDir: agentHome}, nil
	}
	statSetupArtifact = func(path string) (os.FileInfo, error) {
		if path == sudoersFile {
			return nil, errors.New("missing")
		}
		return fakeExecutableFileInfo{}, nil
	}

	gate := inspectAgentProbeGate()
	if gate.Allowed() {
		t.Fatal("agent probe gate allowed probes, want blocked for partial setup drift")
	}
	reason := gate.Reason()
	assertSetupDriftAdvice(t, reason)
	if strings.Contains(reason, "run hazmat init first") {
		t.Fatalf("reason = %q, want no init retry advice for setup drift", reason)
	}
}

func assertSetupDriftAdvice(t *testing.T, text string) {
	t.Helper()
	for _, want := range []string{
		"setup drift",
		"hazmat doctor --fix",
		"hazmat doctor --dry-run",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, missing %q", text, want)
		}
	}
	if strings.Contains(text, "run 'hazmat init' first") {
		t.Fatalf("text = %q, want no init retry advice for setup drift", text)
	}
}

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "fake" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }

type fakeExecutableFileInfo struct {
	fakeFileInfo
}

func (fakeExecutableFileInfo) Mode() os.FileMode { return 0o755 }

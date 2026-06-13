package hazmat

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSeatbeltWrapperFileAcceptsManagedTemplate(t *testing.T) {
	err := validateSeatbeltWrapperFile(
		func(path string) ([]byte, error) {
			if path != seatbeltWrapperPath {
				t.Fatalf("read path = %q, want %q", path, seatbeltWrapperPath)
			}
			return []byte(seatbeltWrapperContent), nil
		},
		func(path string) (bool, error) {
			if path != seatbeltWrapperPath {
				t.Fatalf("executable path = %q, want %q", path, seatbeltWrapperPath)
			}
			return true, nil
		},
	)
	if err != nil {
		t.Fatalf("validateSeatbeltWrapperFile() = %v, want nil", err)
	}
}

func TestValidateSeatbeltWrapperFileRejectsMissingExecutable(t *testing.T) {
	err := validateSeatbeltWrapperFile(
		func(string) ([]byte, error) { return []byte(seatbeltWrapperContent), nil },
		func(string) (bool, error) { return false, nil },
	)
	if err == nil || !strings.Contains(err.Error(), "missing or not executable") {
		t.Fatalf("validateSeatbeltWrapperFile() = %v, want executable error", err)
	}
}

func TestValidateSeatbeltWrapperFileRejectsReadFailure(t *testing.T) {
	errBoom := errors.New("agent read failed")
	err := validateSeatbeltWrapperFile(
		func(string) ([]byte, error) { return nil, errBoom },
		func(string) (bool, error) { return true, nil },
	)
	if err == nil || !strings.Contains(err.Error(), "read seatbelt wrapper as agent") || !errors.Is(err, errBoom) {
		t.Fatalf("validateSeatbeltWrapperFile() = %v, want wrapped read failure", err)
	}
}

func TestValidateSeatbeltWrapperFileRejectsContentDrift(t *testing.T) {
	stale := strings.Replace(seatbeltWrapperContent, "CLAUDE_BIN=/Users/agent/.local/bin/claude", "CLAUDE_BIN=/tmp/claude", 1)
	err := validateSeatbeltWrapperFile(
		func(string) ([]byte, error) { return []byte(stale), nil },
		func(string) (bool, error) { return true, nil },
	)
	if err == nil || !strings.Contains(err.Error(), "content drifted") {
		t.Fatalf("validateSeatbeltWrapperFile() = %v, want content drift error", err)
	}
}

package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hazmat/internal/setup"
)

func TestValidateHostWrapperRequiresPinnedExecutableHazmatBinary(t *testing.T) {
	tmp := t.TempDir()
	hazmatBin := filepath.Join(tmp, "hazmat")
	if err := os.WriteFile(hazmatBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(tmp, "hazmat-shell")
	if err := os.WriteFile(wrapper, []byte(setup.HostWrapperContent(hazmatBin, "shell")), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := validateHostWrapper(wrapper); err != nil {
		t.Fatalf("validateHostWrapper() = %v, want nil", err)
	}
}

func TestValidateHostWrapperRejectsMissingPinnedHazmatBinary(t *testing.T) {
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "hazmat-shell")
	missing := filepath.Join(tmp, "missing-hazmat")
	if err := os.WriteFile(wrapper, []byte(setup.HostWrapperContent(missing, "shell")), 0o755); err != nil {
		t.Fatal(err)
	}

	err := validateHostWrapper(wrapper)
	if err == nil || !strings.Contains(err.Error(), "pins missing Hazmat binary") {
		t.Fatalf("validateHostWrapper() = %v, want missing pinned binary error", err)
	}
}

func TestValidateHostWrapperRejectsMalformedPinnedHazmatBinary(t *testing.T) {
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "hazmat-shell")
	if err := os.WriteFile(wrapper, []byte("HAZMAT_BIN=\"unterminated\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := validateHostWrapper(wrapper)
	if err == nil || !strings.Contains(err.Error(), "invalid pinned Hazmat binary") {
		t.Fatalf("validateHostWrapper() = %v, want invalid pinned binary error", err)
	}
}

func TestValidateHostWrapperRejectsUnpinnedWrapper(t *testing.T) {
	tmp := t.TempDir()
	wrapper := filepath.Join(tmp, "hazmat-shell")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec hazmat shell \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := validateHostWrapper(wrapper)
	if err == nil || !strings.Contains(err.Error(), "does not pin HAZMAT_BIN") {
		t.Fatalf("validateHostWrapper() = %v, want missing pin error", err)
	}
}

func TestValidateHostWrapperRejectsNonExecutablePinnedHazmatBinary(t *testing.T) {
	tmp := t.TempDir()
	hazmatBin := filepath.Join(tmp, "hazmat")
	if err := os.WriteFile(hazmatBin, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(tmp, "hazmat-shell")
	if err := os.WriteFile(wrapper, []byte(setup.HostWrapperContent(hazmatBin, "shell")), 0o755); err != nil {
		t.Fatal(err)
	}

	err := validateHostWrapper(wrapper)
	if err == nil || !strings.Contains(err.Error(), "pins non-executable Hazmat binary") {
		t.Fatalf("validateHostWrapper() = %v, want non-executable pinned binary error", err)
	}
}

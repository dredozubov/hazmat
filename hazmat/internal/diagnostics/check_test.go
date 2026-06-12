package diagnostics

import (
	"strings"
	"testing"
)

func TestCheckCommandRunsQuickByDefault(t *testing.T) {
	var gotOptions CheckOptions
	var called bool
	cmd := NewCheckCommand(func(options CheckOptions) error {
		called = true
		gotOptions = options
		return nil
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if !called || gotOptions.Command != "check" || !gotOptions.Quick || gotOptions.JSON || gotOptions.Fix {
		t.Fatalf("runner called=%v options=%+v, want called quick non-json", called, gotOptions)
	}
}

func TestCheckCommandFullDisablesQuickMode(t *testing.T) {
	var gotQuick = true
	cmd := NewCheckCommand(func(options CheckOptions) error {
		gotQuick = options.Quick
		return nil
	})
	cmd.SetArgs([]string{"--full"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if gotQuick {
		t.Fatal("quick = true, want false for --full")
	}
}

func TestCheckCommandJSONFlag(t *testing.T) {
	var gotOptions CheckOptions
	cmd := NewCheckCommand(func(options CheckOptions) error {
		gotOptions = options
		return nil
	})
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if !gotOptions.JSON || !gotOptions.Quick {
		t.Fatalf("options = %+v, want json quick", gotOptions)
	}
}

func TestCheckCommandRejectsFixFlag(t *testing.T) {
	cmd := NewCheckCommand(func(CheckOptions) error {
		t.Fatal("runner should not be called")
		return nil
	})
	cmd.SetArgs([]string{"--fix"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() succeeded, want unknown --fix flag for check")
	}
}

func TestCheckCommandHelpNamesDirectRepairPath(t *testing.T) {
	cmd := NewCheckCommand(nil)
	if !strings.Contains(cmd.Long, "hazmat doctor --fix") {
		t.Fatalf("check help = %q, want direct doctor --fix repair path", cmd.Long)
	}
	if !strings.Contains(cmd.Long, "hazmat doctor --dry-run") {
		t.Fatalf("check help = %q, want explicit doctor --dry-run preview path", cmd.Long)
	}
	if strings.Contains(cmd.Long, "hazmat init") {
		t.Fatalf("check help = %q, want no init retry guidance", cmd.Long)
	}
}

func TestDoctorCommandFixFlag(t *testing.T) {
	var gotOptions CheckOptions
	cmd := NewDoctorCommand(func(options CheckOptions) error {
		gotOptions = options
		return nil
	})
	cmd.SetArgs([]string{"--fix"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if gotOptions.Command != "doctor" || !gotOptions.Fix {
		t.Fatalf("options = %+v, want doctor fix", gotOptions)
	}
}

func TestDoctorCommandJSONFixFlags(t *testing.T) {
	var gotOptions CheckOptions
	cmd := NewDoctorCommand(func(options CheckOptions) error {
		gotOptions = options
		return nil
	})
	cmd.SetArgs([]string{"--json", "--fix"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if gotOptions.Command != "doctor" || !gotOptions.JSON || !gotOptions.Fix {
		t.Fatalf("options = %+v, want doctor json fix", gotOptions)
	}
}

func TestDoctorCommandFullDisablesQuickMode(t *testing.T) {
	var gotQuick = true
	cmd := NewDoctorCommand(func(options CheckOptions) error {
		gotQuick = options.Quick
		return nil
	})
	cmd.SetArgs([]string{"--full"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if cmd.Name() != "doctor" {
		t.Fatalf("command name = %q, want doctor", cmd.Name())
	}
	if gotQuick {
		t.Fatal("quick = true, want false for --full")
	}
}

func TestDoctorCommandHelpNamesExplicitDryRunPreview(t *testing.T) {
	cmd := NewDoctorCommand(nil)
	if !strings.Contains(cmd.Long, "hazmat doctor --dry-run") {
		t.Fatalf("doctor help = %q, want explicit dry-run preview spelling", cmd.Long)
	}
	if !strings.Contains(cmd.Long, "--fix") || !strings.Contains(cmd.Long, "--yes") {
		t.Fatalf("doctor help = %q, want fix and non-interactive consent contract", cmd.Long)
	}
}

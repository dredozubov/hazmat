package diagnostics

import "testing"

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
	if !called || !gotOptions.Quick || gotOptions.JSON {
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

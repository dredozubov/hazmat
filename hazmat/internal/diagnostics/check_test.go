package diagnostics

import "testing"

func TestCheckCommandRunsQuickByDefault(t *testing.T) {
	var gotQuick bool
	var called bool
	cmd := NewCheckCommand(func(quick bool) error {
		called = true
		gotQuick = quick
		return nil
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if !called || !gotQuick {
		t.Fatalf("runner called=%v quick=%v, want called quick", called, gotQuick)
	}
}

func TestCheckCommandFullDisablesQuickMode(t *testing.T) {
	var gotQuick = true
	cmd := NewCheckCommand(func(quick bool) error {
		gotQuick = quick
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

func TestDoctorCommandFullDisablesQuickMode(t *testing.T) {
	var gotQuick = true
	cmd := NewDoctorCommand(func(quick bool) error {
		gotQuick = quick
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

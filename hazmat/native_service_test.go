package main

import (
	"errors"
	"reflect"
	"testing"
)

type recordingNativeServiceBackend struct {
	calls []string
	err   error

	launchSudoersInstalled           bool
	agentMaintenanceSudoersInstalled bool
	genericPasswordlessAvailable     bool
	brewLaunchHelper                 string
}

func (b *recordingNativeServiceBackend) record(call string) {
	b.calls = append(b.calls, call)
}

func (b *recordingNativeServiceBackend) SetupLaunchHelper(*UI, *Runner) error {
	b.record("setupLaunchHelper")
	return b.err
}

func (b *recordingNativeServiceBackend) SetupSudoers(_ *UI, _ *Runner, currentUser string) error {
	b.record("setupSudoers:" + currentUser)
	return b.err
}

func (b *recordingNativeServiceBackend) MaybeSetupOptionalAgentMaintenanceSudoers(_ *UI, _ *Runner, currentUser string) error {
	b.record("maybeSetupOptionalAgentMaintenanceSudoers:" + currentUser)
	return b.err
}

func (b *recordingNativeServiceBackend) SetupPfFirewall(*UI, *Runner) error {
	b.record("setupPfFirewall")
	return b.err
}

func (b *recordingNativeServiceBackend) SetupDNSBlocklist(*UI, *Runner) error {
	b.record("setupDNSBlocklist")
	return b.err
}

func (b *recordingNativeServiceBackend) SetupLaunchDaemon(*UI, *Runner) error {
	b.record("setupLaunchDaemon")
	return b.err
}

func (b *recordingNativeServiceBackend) RollbackLaunchDaemon(*UI, *Runner) {
	b.record("rollbackLaunchDaemon")
}

func (b *recordingNativeServiceBackend) RollbackPfFirewall(*UI, *Runner) {
	b.record("rollbackPfFirewall")
}

func (b *recordingNativeServiceBackend) RollbackDNSBlocklist(*UI, *Runner) {
	b.record("rollbackDNSBlocklist")
}

func (b *recordingNativeServiceBackend) RollbackSudoers(*UI, *Runner) {
	b.record("rollbackSudoers")
}

func (b *recordingNativeServiceBackend) InstallAgentMaintenanceSudoers(_ *UI, _ *Runner, currentUser string) error {
	b.record("installAgentMaintenanceSudoers:" + currentUser)
	return b.err
}

func (b *recordingNativeServiceBackend) UninstallAgentMaintenanceSudoers(*UI, *Runner) error {
	b.record("uninstallAgentMaintenanceSudoers")
	return b.err
}

func (b *recordingNativeServiceBackend) LaunchSudoersInstalled() bool {
	b.record("launchSudoersInstalled")
	return b.launchSudoersInstalled
}

func (b *recordingNativeServiceBackend) AgentMaintenanceSudoersInstalled() bool {
	b.record("agentMaintenanceSudoersInstalled")
	return b.agentMaintenanceSudoersInstalled
}

func (b *recordingNativeServiceBackend) GenericAgentPasswordlessAvailable() bool {
	b.record("genericAgentPasswordlessAvailable")
	return b.genericPasswordlessAvailable
}

func (b *recordingNativeServiceBackend) FindBrewLaunchHelper() string {
	b.record("findBrewLaunchHelper")
	return b.brewLaunchHelper
}

func TestNativeServiceSetupAndRollbackRouteThroughPlatformBackend(t *testing.T) {
	backend := &recordingNativeServiceBackend{
		brewLaunchHelper: "/opt/homebrew/opt/hazmat/libexec/hazmat-launch",
	}
	savedFactory := nativeServiceBackendFactory
	nativeServiceBackendFactory = func() nativeServiceBackend {
		return backend
	}
	t.Cleanup(func() {
		nativeServiceBackendFactory = savedFactory
	})

	if err := setupLaunchHelper(&UI{}, &Runner{}); err != nil {
		t.Fatal(err)
	}
	if err := setupSudoers(&UI{}, &Runner{}, "dr"); err != nil {
		t.Fatal(err)
	}
	if err := maybeSetupOptionalAgentMaintenanceSudoers(&UI{}, &Runner{}, "dr"); err != nil {
		t.Fatal(err)
	}
	if err := setupPfFirewall(&UI{}, &Runner{}); err != nil {
		t.Fatal(err)
	}
	if err := setupDNSBlocklist(&UI{}, &Runner{}); err != nil {
		t.Fatal(err)
	}
	if err := setupLaunchDaemon(&UI{}, &Runner{}); err != nil {
		t.Fatal(err)
	}
	rollbackSudoers(&UI{}, &Runner{})
	rollbackLaunchDaemon(&UI{}, &Runner{})
	rollbackPfFirewall(&UI{}, &Runner{})
	rollbackDNSBlocklist(&UI{}, &Runner{})
	if got := findBrewLaunchHelper(); got != backend.brewLaunchHelper {
		t.Fatalf("findBrewLaunchHelper() = %q, want %q", got, backend.brewLaunchHelper)
	}

	want := []string{
		"setupLaunchHelper",
		"setupSudoers:dr",
		"maybeSetupOptionalAgentMaintenanceSudoers:dr",
		"setupPfFirewall",
		"setupDNSBlocklist",
		"setupLaunchDaemon",
		"rollbackSudoers",
		"rollbackLaunchDaemon",
		"rollbackPfFirewall",
		"rollbackDNSBlocklist",
		"findBrewLaunchHelper",
	}
	if !reflect.DeepEqual(backend.calls, want) {
		t.Fatalf("native service backend calls = %v, want %v", backend.calls, want)
	}
}

func TestNativeServiceSetupPropagatesPlatformBackendErrors(t *testing.T) {
	wantErr := errors.New("native service backend failed")
	backend := &recordingNativeServiceBackend{err: wantErr}
	savedFactory := nativeServiceBackendFactory
	nativeServiceBackendFactory = func() nativeServiceBackend {
		return backend
	}
	t.Cleanup(func() {
		nativeServiceBackendFactory = savedFactory
	})

	for name, fn := range map[string]func() error{
		"setupLaunchHelper": func() error {
			return setupLaunchHelper(&UI{}, &Runner{})
		},
		"setupSudoers": func() error {
			return setupSudoers(&UI{}, &Runner{}, "dr")
		},
		"maybeSetupOptionalAgentMaintenanceSudoers": func() error {
			return maybeSetupOptionalAgentMaintenanceSudoers(&UI{}, &Runner{}, "dr")
		},
		"setupPfFirewall": func() error {
			return setupPfFirewall(&UI{}, &Runner{})
		},
		"setupDNSBlocklist": func() error {
			return setupDNSBlocklist(&UI{}, &Runner{})
		},
		"setupLaunchDaemon": func() error {
			return setupLaunchDaemon(&UI{}, &Runner{})
		},
	} {
		if err := fn(); !errors.Is(err, wantErr) {
			t.Fatalf("%s error = %v, want %v", name, err, wantErr)
		}
	}
}

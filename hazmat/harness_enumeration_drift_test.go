package hazmat

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// These tests guard the hardcoded harness enumerations that live outside the Go
// registry against drift. The gemini->antigravity migration's only failures
// were stale enumerations; a hardcoded list that names a removed harness breaks
// `git push` (the pre-push hook smokes each one) or silently desyncs the TLA
// model from the code. The managed-harness registry is the single source of
// truth — every other list must match it exactly.

func registryHarnessIDSet() map[string]bool {
	ids := map[string]bool{}
	for _, h := range managedHarnessRegistry {
		ids[string(h.Spec.ID)] = true
	}
	return ids
}

func assertHarnessSetMatchesRegistry(t *testing.T, source string, got map[string]bool) {
	t.Helper()
	want := registryHarnessIDSet()
	for id := range want {
		if !got[id] {
			t.Errorf("%s is missing managed harness %q (add it, or it drifts from managedHarnessRegistry)", source, id)
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("%s lists unknown harness %q not in managedHarnessRegistry", source, id)
		}
	}
}

func TestPrePushHarnessLoopMatchesRegistry(t *testing.T) {
	raw, err := os.ReadFile("../.hazmat/hooks/pre-push.sh")
	if err != nil {
		t.Fatalf("read pre-push hook: %v", err)
	}
	m := regexp.MustCompile(`(?m)^\s*for harness in (.+?); do`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("could not find 'for harness in ...; do' loop in .hazmat/hooks/pre-push.sh")
	}
	got := map[string]bool{}
	for _, field := range strings.Fields(m[1]) {
		got[field] = true
	}
	assertHarnessSetMatchesRegistry(t, ".hazmat/hooks/pre-push.sh harness loop", got)
}

func TestPrePushBootstrapSmokesMatchRegistry(t *testing.T) {
	raw, err := os.ReadFile("../.hazmat/hooks/pre-push.sh")
	if err != nil {
		t.Fatalf("read pre-push hook: %v", err)
	}
	got := map[string]bool{}
	for _, m := range regexp.MustCompile(`run_smoke "bootstrap (\S+) --help"`).FindAllStringSubmatch(string(raw), -1) {
		got[m[1]] = true
	}
	assertHarnessSetMatchesRegistry(t, ".hazmat/hooks/pre-push.sh bootstrap --help smokes", got)
}

// scripts/check-cli-smoke.sh is the CI-only CLI smoke (it is not part of the
// local pre-push hook), so it drifts without local detection. The gemini->
// antigravity migration missed it and reddened CI for days. Guard its two
// all-harness enumerations against the registry.
func TestCLISmokeHarnessLoopMatchesRegistry(t *testing.T) {
	raw, err := os.ReadFile("../scripts/check-cli-smoke.sh")
	if err != nil {
		t.Fatalf("read check-cli-smoke.sh: %v", err)
	}
	m := regexp.MustCompile(`(?m)^\s*for harness in (.+?); do`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("could not find 'for harness in ...; do' loop in scripts/check-cli-smoke.sh")
	}
	got := map[string]bool{}
	for _, field := range strings.Fields(m[1]) {
		got[field] = true
	}
	assertHarnessSetMatchesRegistry(t, "scripts/check-cli-smoke.sh harness loop", got)
}

func TestCLISmokeBootstrapSmokesMatchRegistry(t *testing.T) {
	raw, err := os.ReadFile("../scripts/check-cli-smoke.sh")
	if err != nil {
		t.Fatalf("read check-cli-smoke.sh: %v", err)
	}
	got := map[string]bool{}
	for _, m := range regexp.MustCompile(`run_smoke "bootstrap (\S+) --help"`).FindAllStringSubmatch(string(raw), -1) {
		got[m[1]] = true
	}
	assertHarnessSetMatchesRegistry(t, "scripts/check-cli-smoke.sh bootstrap --help smokes", got)
}

// scripts/e2e-harness-smoke-native.sh is the sudo-gated native launch smoke and
// is not run in CI, so its SMOKE_HARNESSES list drifts without any CI signal —
// it still named the removed gemini harness after the migration. Guard it from
// the Go suite so a stale entry fails `go test`.
func TestNativeSmokeHarnessListMatchesRegistry(t *testing.T) {
	raw, err := os.ReadFile("../scripts/e2e-harness-smoke-native.sh")
	if err != nil {
		t.Fatalf("read e2e-harness-smoke-native.sh: %v", err)
	}
	m := regexp.MustCompile(`(?m)^SMOKE_HARNESSES="([^"]*)"`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("could not find 'SMOKE_HARNESSES=\"...\"' in scripts/e2e-harness-smoke-native.sh")
	}
	got := map[string]bool{}
	for _, field := range strings.Fields(m[1]) {
		got[field] = true
	}
	assertHarnessSetMatchesRegistry(t, "scripts/e2e-harness-smoke-native.sh SMOKE_HARNESSES", got)
}

func TestTLAHarnessSetMatchesRegistry(t *testing.T) {
	raw, err := os.ReadFile("../tla/MC_HarnessLifecycle.tla")
	if err != nil {
		t.Fatalf("read MC_HarnessLifecycle.tla: %v", err)
	}
	// Anchor at line start so ImportableHarnesses (a superset substring) is not matched.
	m := regexp.MustCompile(`(?m)^Harnesses == \{([^}]*)\}`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("could not find 'Harnesses == {...}' in tla/MC_HarnessLifecycle.tla")
	}
	got := map[string]bool{}
	for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m[1], -1) {
		got[q[1]] = true
	}
	assertHarnessSetMatchesRegistry(t, "tla/MC_HarnessLifecycle.tla Harnesses set", got)
}

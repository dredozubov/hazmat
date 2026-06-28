package linux

import (
	"errors"
	"strings"
	"testing"

	"hazmat/containment"
	linuxspec "hazmat/containment/linux"
	platformlinux "hazmat/platform/linux"
	"hazmat/sessionmeta"
)

func TestBuildPolicyPlanLandlockRulesCoverPlannedAuthority(t *testing.T) {
	spec := currentUserSpec(t, sessionmeta.NetworkNone)
	plan, err := BuildPolicyPlan(spec)
	if err != nil {
		t.Fatalf("BuildPolicyPlan: %v", err)
	}
	if !plan.Landlock.Enforced {
		t.Fatal("Landlock.Enforced = false")
	}
	for _, want := range []struct {
		path   string
		access containment.PathAccess
	}{
		{path: "/workspace/project", access: containment.PathReadWrite},
		{path: "/opt/sdk", access: containment.PathReadOnly},
		{path: "/tmp/hazmat-session/home", access: containment.PathReadWrite},
		{path: "/tmp/hazmat-session", access: containment.PathReadWrite},
	} {
		if !hasLandlockRule(plan.Landlock.Rules, want.path, want.access) {
			t.Fatalf("Landlock rules missing %s %s: %+v", want.path, want.access, plan.Landlock.Rules)
		}
	}
	for _, rule := range plan.Landlock.Rules {
		if strings.HasPrefix(rule.Path, "/home/user/.ssh") {
			t.Fatalf("Landlock rule covers credential deny path: %+v", rule)
		}
	}
}

func TestBuildPolicyPlanSupportsAgentUserSpec(t *testing.T) {
	spec := agentUserSpec(t, sessionmeta.NetworkNone)
	plan, err := BuildPolicyPlan(spec)
	if err != nil {
		t.Fatalf("BuildPolicyPlan(agent-user): %v", err)
	}
	if !plan.Landlock.Enforced || !hasLandlockRule(plan.Landlock.Rules, "/tmp/hazmat-session/home", containment.PathReadWrite) {
		t.Fatalf("agent-user policy missing enforced agent home rule: %+v", plan.Landlock)
	}
	if !plan.Seccomp.NoNewPrivs || plan.Seccomp.DefaultAction != "errno" {
		t.Fatalf("agent-user seccomp = %+v, want no_new_privs errno policy", plan.Seccomp)
	}
}

func TestBuildPolicyPlanRequiresNoNewPrivsAndBuildsSeccompPlan(t *testing.T) {
	spec := currentUserSpec(t, sessionmeta.NetworkNone)
	spec.Process.AllowFork = false
	plan, err := BuildPolicyPlan(spec)
	if err != nil {
		t.Fatalf("BuildPolicyPlan: %v", err)
	}
	if !plan.Seccomp.NoNewPrivs || plan.Seccomp.DefaultAction != "errno" || plan.Seccomp.AllowFork {
		t.Fatalf("Seccomp = %+v, want no_new_privs, errno default, fork denied", plan.Seccomp)
	}

	spec.Process.NoNewPrivs = false
	if _, err := BuildPolicyPlan(spec); err == nil || !strings.Contains(err.Error(), "no_new_privs") {
		t.Fatalf("err = %v, want no_new_privs requirement", err)
	}
}

func TestBuildPolicyPlanRejectsCredentialDenyOverlap(t *testing.T) {
	spec := currentUserSpec(t, sessionmeta.NetworkNone)
	spec.CredentialDenies = []linuxspec.CredentialDenySpec{{Path: "/workspace/project/.ssh"}}
	_, err := BuildPolicyPlan(spec)
	if err == nil || !strings.Contains(err.Error(), "overlaps credential deny path") {
		t.Fatalf("err = %v, want Landlock credential deny overlap rejection", err)
	}
}

func TestAdmitCurrentUserRejectsLandlockAndSeccompGaps(t *testing.T) {
	spec := currentUserSpec(t, sessionmeta.NetworkDefault)
	report := availableReport()
	report.Features.Landlock = platformlinux.FeatureReport{State: platformlinux.FeatureUnavailable, Source: "landlock"}
	report.Features.Seccomp = platformlinux.FeatureReport{State: platformlinux.FeatureUnavailable, Source: "seccomp"}
	_, err := AdmitCurrentUser(spec, report)
	var gapErr GapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("err = %v, want GapError", err)
	}
	for _, code := range []string{linuxspec.GapLandlockUnavailable, linuxspec.GapSeccompUnavailable} {
		if !hasGap(gapErr.Gaps, code) {
			t.Fatalf("gaps missing %q: %+v", code, gapErr.Gaps)
		}
	}
}

func TestAdmitCurrentUserNetworkNoneRequiresNetworkNamespace(t *testing.T) {
	spec := currentUserSpec(t, sessionmeta.NetworkNone)
	report := availableReport()
	report.Features.NetworkNamespaces = platformlinux.FeatureReport{State: platformlinux.FeatureUnavailable, Source: "netns"}
	_, err := AdmitCurrentUser(spec, report)
	var gapErr GapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("err = %v, want GapError", err)
	}
	if !hasGap(gapErr.Gaps, linuxspec.GapNetworkNamespaceUnavailable) {
		t.Fatalf("gaps missing network namespace gap: %+v", gapErr.Gaps)
	}
}

func hasLandlockRule(rules []LandlockRule, path string, access containment.PathAccess) bool {
	for _, rule := range rules {
		if rule.Path == path && rule.Access == access {
			return true
		}
	}
	return false
}

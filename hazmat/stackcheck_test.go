package main

import (
	"errors"
	"os/user"
	"strings"
	"testing"
)

func TestSelectStackMatrixRepos(t *testing.T) {
	manifest := stackMatrixManifest{
		SchemaVersion: stackMatrixSchemaVersion,
		Repos: []stackMatrixRepo{
			{ID: "next-js", Track: stackMatrixTrackRequired, Wave: 1},
			{ID: "ruff", Track: stackMatrixTrackRequired, Wave: 2},
			{ID: "ollama", Track: stackMatrixTrackInformational, Wave: 3},
		},
	}

	selected := selectStackMatrixRepos(manifest, stackMatrixSelection{
		Track: stackMatrixTrackRequired,
		Wave:  2,
	})
	if len(selected) != 1 || selected[0].ID != "ruff" {
		t.Fatalf("selected = %+v, want [ruff]", selected)
	}

	selected = selectStackMatrixRepos(manifest, stackMatrixSelection{
		Track: "all",
		IDs:   map[string]struct{}{"ollama": {}},
	})
	if len(selected) != 1 || selected[0].ID != "ollama" {
		t.Fatalf("selected = %+v, want [ollama]", selected)
	}
}

func TestValidateStackcheckDetect(t *testing.T) {
	repo := stackMatrixRepo{
		ID:                  "next-js",
		ExpectedSuggestions: []string{"node"},
	}

	if failureClass, _ := validateStackcheckDetect(repo, explainJSONPreview{
		SuggestedIntegrations: []string{"node"},
	}); failureClass != "" {
		t.Fatalf("failureClass = %q, want empty", failureClass)
	}

	if failureClass, message := validateStackcheckDetect(repo, explainJSONPreview{}); failureClass != "detect_false_negative" || !strings.Contains(message, "missing suggested integrations") {
		t.Fatalf("failure = (%q, %q)", failureClass, message)
	}

	if failureClass, message := validateStackcheckDetect(repo, explainJSONPreview{
		SuggestedIntegrations: []string{"node", "go"},
	}); failureClass != "detect_false_positive" || !strings.Contains(message, "unexpected suggested integrations") {
		t.Fatalf("failure = (%q, %q)", failureClass, message)
	}
}

func TestValidateStackcheckContract(t *testing.T) {
	repo := stackMatrixRepo{
		ID:       "pydantic-ai",
		Activate: []string{"python-uv"},
	}

	if failureClass, _ := validateStackcheckContract(repo, explainJSONPreview{
		ActiveIntegrations: []string{"python-uv"},
		IntegrationSources: []string{"python-uv (uv.lock)"},
	}); failureClass != "" {
		t.Fatalf("failureClass = %q, want empty", failureClass)
	}

	if failureClass, message := validateStackcheckContract(repo, explainJSONPreview{
		ActiveIntegrations: []string{"node"},
		IntegrationSources: []string{"node (package.json)"},
	}); failureClass != "contract_mismatch" || !strings.Contains(message, "missing active integrations") {
		t.Fatalf("failure = (%q, %q)", failureClass, message)
	}

	if failureClass, message := validateStackcheckContract(repo, explainJSONPreview{
		ActiveIntegrations: []string{"python-uv"},
	}); failureClass != "" || message != "" {
		t.Fatalf("failure = (%q, %q)", failureClass, message)
	}
}

func TestSummarizeStackcheckResults(t *testing.T) {
	summary := summarizeStackcheckResults(stackcheckResultSet{
		Mode: "detect",
		Results: []stackcheckRepoResult{
			{ID: "next-js", Status: stackcheckStatusPass},
			{ID: "ruff", Status: stackcheckStatusFail, FailureClass: "detect_false_negative", Message: "missing rust"},
		},
	})
	for _, want := range []string{
		"hazmat: stackcheck detect",
		"PASS next-js",
		"FAIL ruff [detect_false_negative] missing rust",
		"1 passed, 1 failed",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q in:\n%s", want, summary)
		}
	}
}

func TestValidateStackcheckModePrereqs(t *testing.T) {
	originalLookup := lookupAgentUser
	t.Cleanup(func() {
		lookupAgentUser = originalLookup
	})

	lookupAgentUser = func() (*user.User, error) {
		return nil, errors.New("missing")
	}

	if err := validateStackcheckModePrereqs(stackcheckModeDetect); err != nil {
		t.Fatalf("detect prereq err = %v, want nil", err)
	}

	err := validateStackcheckModePrereqs(stackcheckModeSmoke)
	if err == nil || !strings.Contains(err.Error(), "stackcheck smoke requires local Hazmat initialization") {
		t.Fatalf("smoke prereq err = %v, want init guidance", err)
	}

	lookupAgentUser = func() (*user.User, error) {
		return &user.User{Username: agentUser, HomeDir: agentHome}, nil
	}
	if err := validateStackcheckModePrereqs(stackcheckModeSmoke); err != nil {
		t.Fatalf("smoke prereq err = %v, want nil", err)
	}
}

func TestValidateStackcheckSmokeRepoPrereqs(t *testing.T) {
	originalCheck := stackcheckMissingRequiredFormulas
	t.Cleanup(func() {
		stackcheckMissingRequiredFormulas = originalCheck
	})

	repo := stackMatrixRepo{
		ID:               "apache-maven",
		RequiredFormulas: []string{"openjdk", "maven"},
	}

	if failureClass, message := validateStackcheckSmokeRepoPrereqs(stackcheckModeContract, repo); failureClass != "" || message != "" {
		t.Fatalf("contract smoke prereq failure = (%q, %q), want empty", failureClass, message)
	}

	stackcheckMissingRequiredFormulas = func(formulas []string) ([]string, error) {
		if len(formulas) != 2 {
			t.Fatalf("formulas = %v, want two formulas", formulas)
		}
		return []string{"maven"}, nil
	}

	failureClass, message := validateStackcheckSmokeRepoPrereqs(stackcheckModeSmoke, repo)
	if failureClass != "toolchain_missing" || !strings.Contains(message, "missing required Homebrew formulas: maven") {
		t.Fatalf("failure = (%q, %q)", failureClass, message)
	}

	stackcheckMissingRequiredFormulas = func(formulas []string) ([]string, error) {
		return nil, nil
	}
	if failureClass, message := validateStackcheckSmokeRepoPrereqs(stackcheckModeSmoke, repo); failureClass != "" || message != "" {
		t.Fatalf("smoke prereq failure = (%q, %q), want empty", failureClass, message)
	}
}

func TestResolveStackcheckCheckoutTarget(t *testing.T) {
	repo := stackMatrixRepo{
		ID:   "next-js",
		Repo: "https://github.com/vercel/next.js",
		Ref:  strings.Repeat("a", 40),
	}

	target, err := resolveStackcheckCheckoutTarget(repo, false)
	if err != nil {
		t.Fatalf("resolve pinned target returned error: %v", err)
	}
	if target.ref != repo.Ref || target.source != "pinned" || target.dirLabel != repo.Ref[:12] {
		t.Fatalf("pinned target = %+v", target)
	}

	originalResolve := stackcheckResolveUpstreamHead
	t.Cleanup(func() {
		stackcheckResolveUpstreamHead = originalResolve
	})
	stackcheckResolveUpstreamHead = func(repoURL string) (string, error) {
		if repoURL != repo.Repo {
			t.Fatalf("repoURL = %q, want %q", repoURL, repo.Repo)
		}
		return strings.Repeat("b", 40), nil
	}

	target, err = resolveStackcheckCheckoutTarget(repo, true)
	if err != nil {
		t.Fatalf("resolve upstream target returned error: %v", err)
	}
	if target.ref != strings.Repeat("b", 40) || target.source != "upstream_head" || target.dirLabel != "head-"+strings.Repeat("b", 12) {
		t.Fatalf("upstream target = %+v", target)
	}
}

func TestParseStackcheckLSRemoteHead(t *testing.T) {
	ref, err := parseStackcheckLSRemoteHead(strings.Repeat("c", 40) + "\tHEAD\n")
	if err != nil {
		t.Fatalf("parseStackcheckLSRemoteHead returned error: %v", err)
	}
	if ref != strings.Repeat("c", 40) {
		t.Fatalf("ref = %q", ref)
	}

	if _, err := parseStackcheckLSRemoteHead("HEAD\n"); err == nil {
		t.Fatal("expected parse error for malformed ls-remote output")
	}
}

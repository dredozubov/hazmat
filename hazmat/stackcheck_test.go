package main

import (
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
	}); failureClass != "contract_mismatch" || !strings.Contains(message, "integration_sources") {
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

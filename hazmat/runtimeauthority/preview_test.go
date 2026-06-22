package runtimeauthority

import (
	"encoding/json"
	"strings"
	"testing"
)

const basicFixture = `{
  "schema": "runtime.authority.v1",
  "authority_id": "runtime-authority-demo-1",
  "route_id": "project-x-to-project-y",
  "source_project": "project-x",
  "target_project": "project-y",
  "requested_isolation_tier": "same_uid_process",
  "workspace_grants": [
    "read:repo:project-y",
    "read:state:project-y"
  ],
  "network_grants": [
    "allow:tailnet",
    "deny:public-internet"
  ],
  "credential_scope_refs": [
    "credential:none"
  ],
  "service_grants": [
    "beads:read",
    "git:status"
  ],
  "broker_grant_refs": [
    "grant:none"
  ],
  "required_capability_set_fingerprint": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
  "policy_hash": "sha256:3333333333333333333333333333333333333333333333333333333333333333",
  "nonce_namespace": "runtime.authority.route-token.v1",
  "restore_epoch": 1,
  "trust_root_epoch": 7
}`

func TestPreviewPlanescapeBasicFixtureDeterministic(t *testing.T) {
	preview, err := PreviewJSON([]byte(basicFixture))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Schema != Schema ||
		preview.AuthorityID != "runtime-authority-demo-1" ||
		preview.NetworkPosture != string(NetworkTailnetOnly) ||
		preview.HazmatNetworkMode != "default" ||
		preview.CredentialMode != string(CredentialNone) ||
		preview.SessionHome.Mode != HomeModeSessionLocalEmpty ||
		preview.RestoreEpoch != 1 ||
		preview.TrustRootEpoch != 7 {
		t.Fatalf("unexpected preview = %+v", preview)
	}
	if len(preview.UnsupportedFields) != 3 {
		t.Fatalf("UnsupportedFields = %+v, want logical workspace grants and tailnet note", preview.UnsupportedFields)
	}
	if len(preview.LogicalResources) != 6 {
		t.Fatalf("LogicalResources = %v, want all workspace/network/service grants", preview.LogicalResources)
	}

	first, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	secondPreview, err := PreviewJSON([]byte(basicFixture))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(secondPreview)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("preview is not deterministic:\n%s\n%s", first, second)
	}
}

func TestPreviewPathGrants(t *testing.T) {
	jsonText := strings.ReplaceAll(basicFixture,
		`"read:repo:project-y",
    "read:state:project-y"`,
		`"write:path:/tmp/project/build",
    "read:path:/tmp/project"`,
	)
	preview, err := PreviewJSON([]byte(jsonText))
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.PathGrants) != 2 {
		t.Fatalf("PathGrants = %+v, want two grants", preview.PathGrants)
	}
	if got := preview.PathGrants[0]; got.Path != "/tmp/project" || got.Access != "read-only" {
		t.Fatalf("first path grant = %+v", got)
	}
	if got := preview.PathGrants[1]; got.Path != "/tmp/project/build" || got.Access != "read-write" {
		t.Fatalf("second path grant = %+v", got)
	}
}

func TestPreviewNetworkDeniedMode(t *testing.T) {
	jsonText := strings.ReplaceAll(basicFixture,
		`"allow:tailnet",
    "deny:public-internet"`,
		`"deny:all"`,
	)
	preview, err := PreviewJSON([]byte(jsonText))
	if err != nil {
		t.Fatal(err)
	}
	if preview.NetworkPosture != string(NetworkDeniedOnly) || preview.HazmatNetworkMode != "none" {
		t.Fatalf("network preview = %+v, want deny-all/none", preview)
	}
}

func TestPreviewCredentialBrokerRequired(t *testing.T) {
	jsonText := strings.NewReplacer(
		`"credential:none"`, `"credential:deploy/project-y"`,
		`"grant:none"`, `"grant:broker/project-y/deploy"`,
	).Replace(basicFixture)
	preview, err := PreviewJSON([]byte(jsonText))
	if err != nil {
		t.Fatal(err)
	}
	if preview.CredentialMode != string(CredentialBrokerRequired) {
		t.Fatalf("CredentialMode = %q, want broker required", preview.CredentialMode)
	}
}

func TestParseRejectsUnknownAuthorityField(t *testing.T) {
	jsonText := strings.Replace(basicFixture,
		`"trust_root_epoch": 7`,
		`"unexpected_authority": "x", "trust_root_epoch": 7`,
		1,
	)
	_, err := PreviewJSON([]byte(jsonText))
	if err == nil || !strings.Contains(err.Error(), `unknown field "unexpected_authority"`) {
		t.Fatalf("err = %v, want unknown field rejection", err)
	}
}

func TestParseRejectsUnknownIsolationTier(t *testing.T) {
	jsonText := strings.Replace(basicFixture, `"same_uid_process"`, `"firecracker"`, 1)
	_, err := PreviewJSON([]byte(jsonText))
	if err == nil || !strings.Contains(err.Error(), "unsupported requested_isolation_tier") {
		t.Fatalf("err = %v, want isolation tier rejection", err)
	}
}

func TestParseRejectsMalformedAndDuplicateInputs(t *testing.T) {
	if _, err := PreviewJSON([]byte(`{"schema":`)); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	jsonText := strings.Replace(basicFixture,
		`"beads:read",
    "git:status"`,
		`"beads:read",
    "beads:read"`,
		1,
	)
	_, err := PreviewJSON([]byte(jsonText))
	if err == nil || !strings.Contains(err.Error(), "service_grants contains duplicate") {
		t.Fatalf("err = %v, want duplicate rejection", err)
	}
}

func TestRequestAccessorsDefensivelyCopy(t *testing.T) {
	req, err := ParseJSON([]byte(basicFixture))
	if err != nil {
		t.Fatal(err)
	}
	workspace := req.WorkspaceGrants()
	workspace[0] = "mutated"
	if got := req.WorkspaceGrants()[0]; got == "mutated" {
		t.Fatal("WorkspaceGrants returned internal storage")
	}
}

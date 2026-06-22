// Package runtimeauthority previews neutral runtime.authority.v1 requests for
// Hazmat without launching a session or mutating host state.
package runtimeauthority

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"hazmat/containment"
	"hazmat/sessionmeta"
)

const (
	Schema = "runtime.authority.v1"

	HomeModeSessionLocalEmpty = "session-local-empty"
)

var sha256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type IsolationTier string

const (
	IsolationSameUIDProcess IsolationTier = "same_uid_process"
	IsolationOSSandbox      IsolationTier = "os_sandbox"
	IsolationContainer      IsolationTier = "container"
	IsolationVM             IsolationTier = "vm"
	IsolationMicroVM        IsolationTier = "microvm"
)

type CredentialMode string

const (
	CredentialNone           CredentialMode = "none"
	CredentialBrokerRequired CredentialMode = "broker-required"
)

type NetworkPosture string

const (
	NetworkDeniedOnly       NetworkPosture = "deny-all"
	NetworkTailnetOnly      NetworkPosture = "tailnet-only-requested"
	NetworkDefaultRequested NetworkPosture = "default-requested"
	NetworkUnsupported      NetworkPosture = "unsupported"
)

type Request struct {
	schema                           string
	authorityID                      string
	routeID                          string
	sourceProject                    string
	targetProject                    string
	requestedIsolationTier           IsolationTier
	workspaceGrants                  []string
	networkGrants                    []string
	credentialScopeRefs              []string
	serviceGrants                    []string
	brokerGrantRefs                  []string
	requiredCapabilitySetFingerprint string
	policyHash                       string
	nonceNamespace                   string
	restoreEpoch                     uint64
	trustRootEpoch                   uint64
}

type Preview struct {
	Schema                           string             `json:"schema"`
	AuthorityID                      string             `json:"authority_id"`
	RouteID                          string             `json:"route_id"`
	SourceProject                    string             `json:"source_project"`
	TargetProject                    string             `json:"target_project"`
	RequestedIsolationTier           string             `json:"requested_isolation_tier"`
	WorkspaceGrants                  []string           `json:"workspace_grants,omitempty"`
	PathGrants                       []PathGrantPreview `json:"path_grants,omitempty"`
	NetworkPosture                   string             `json:"network_posture"`
	HazmatNetworkMode                string             `json:"hazmat_network_mode"`
	CredentialMode                   string             `json:"credential_mode"`
	ServiceGrants                    []string           `json:"service_grants,omitempty"`
	LogicalResources                 []string           `json:"logical_resources,omitempty"`
	SessionHome                      SessionHomePreview `json:"session_home"`
	RequiredCapabilitySetFingerprint string             `json:"required_capability_set_fingerprint"`
	PolicyHash                       string             `json:"policy_hash"`
	NonceNamespace                   string             `json:"nonce_namespace"`
	RestoreEpoch                     uint64             `json:"restore_epoch"`
	TrustRootEpoch                   uint64             `json:"trust_root_epoch"`
	UnsupportedFields                []UnsupportedField `json:"unsupported_fields,omitempty"`
}

type PathGrantPreview struct {
	Path   string `json:"path"`
	Access string `json:"access"`
	Source string `json:"source"`
}

type SessionHomePreview struct {
	Mode               string   `json:"mode"`
	PersistentHome     string   `json:"persistent_home,omitempty"`
	DurableBridgeRoots []string `json:"durable_bridge_roots,omitempty"`
}

type UnsupportedField struct {
	Field  string `json:"field"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

type requestDTO struct {
	Schema                           string   `json:"schema"`
	AuthorityID                      string   `json:"authority_id"`
	RouteID                          string   `json:"route_id"`
	SourceProject                    string   `json:"source_project"`
	TargetProject                    string   `json:"target_project"`
	RequestedIsolationTier           string   `json:"requested_isolation_tier"`
	WorkspaceGrants                  []string `json:"workspace_grants"`
	NetworkGrants                    []string `json:"network_grants"`
	CredentialScopeRefs              []string `json:"credential_scope_refs"`
	ServiceGrants                    []string `json:"service_grants"`
	BrokerGrantRefs                  []string `json:"broker_grant_refs"`
	RequiredCapabilitySetFingerprint string   `json:"required_capability_set_fingerprint"`
	PolicyHash                       string   `json:"policy_hash"`
	NonceNamespace                   string   `json:"nonce_namespace"`
	RestoreEpoch                     uint64   `json:"restore_epoch"`
	TrustRootEpoch                   uint64   `json:"trust_root_epoch"`
}

func ParseJSON(data []byte) (Request, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var dto requestDTO
	if err := decoder.Decode(&dto); err != nil {
		return Request{}, fmt.Errorf("runtimeauthority: parse runtime.authority.v1: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Request{}, err
	}
	return newRequest(dto)
}

func BuildPreview(req Request) Preview {
	pathGrants, workspaceUnsupported := previewPathGrants(req.workspaceGrants)
	posture, networkMode, networkUnsupported := previewNetwork(req.networkGrants)
	credentialMode := previewCredentialMode(req.credentialScopeRefs, req.brokerGrantRefs)
	serviceGrants := copyStrings(req.serviceGrants)
	logicalResources := logicalResources(req.workspaceGrants, req.networkGrants, req.serviceGrants)
	unsupported := append(workspaceUnsupported, networkUnsupported...)
	return Preview{
		Schema:                           req.schema,
		AuthorityID:                      req.authorityID,
		RouteID:                          req.routeID,
		SourceProject:                    req.sourceProject,
		TargetProject:                    req.targetProject,
		RequestedIsolationTier:           string(req.requestedIsolationTier),
		WorkspaceGrants:                  copyStrings(req.workspaceGrants),
		PathGrants:                       pathGrants,
		NetworkPosture:                   string(posture),
		HazmatNetworkMode:                networkMode.String(),
		CredentialMode:                   string(credentialMode),
		ServiceGrants:                    serviceGrants,
		LogicalResources:                 logicalResources,
		SessionHome:                      SessionHomePreview{Mode: HomeModeSessionLocalEmpty},
		RequiredCapabilitySetFingerprint: req.requiredCapabilitySetFingerprint,
		PolicyHash:                       req.policyHash,
		NonceNamespace:                   req.nonceNamespace,
		RestoreEpoch:                     req.restoreEpoch,
		TrustRootEpoch:                   req.trustRootEpoch,
		UnsupportedFields:                unsupported,
	}
}

func PreviewJSON(data []byte) (Preview, error) {
	req, err := ParseJSON(data)
	if err != nil {
		return Preview{}, err
	}
	return BuildPreview(req), nil
}

func (r Request) WorkspaceGrants() []string {
	return copyStrings(r.workspaceGrants)
}

func (r Request) NetworkGrants() []string {
	return copyStrings(r.networkGrants)
}

func newRequest(dto requestDTO) (Request, error) {
	if dto.Schema != Schema {
		return Request{}, fmt.Errorf("runtimeauthority: schema %q is not supported", dto.Schema)
	}
	tier, err := parseIsolationTier(dto.RequestedIsolationTier)
	if err != nil {
		return Request{}, err
	}
	workspaceGrants, err := normalizedList("workspace_grants", dto.WorkspaceGrants)
	if err != nil {
		return Request{}, err
	}
	networkGrants, err := normalizedList("network_grants", dto.NetworkGrants)
	if err != nil {
		return Request{}, err
	}
	credentialScopeRefs, err := normalizedList("credential_scope_refs", dto.CredentialScopeRefs)
	if err != nil {
		return Request{}, err
	}
	serviceGrants, err := normalizedList("service_grants", dto.ServiceGrants)
	if err != nil {
		return Request{}, err
	}
	brokerGrantRefs, err := normalizedList("broker_grant_refs", dto.BrokerGrantRefs)
	if err != nil {
		return Request{}, err
	}
	if err := requireText("authority_id", dto.AuthorityID); err != nil {
		return Request{}, err
	}
	if err := requireText("route_id", dto.RouteID); err != nil {
		return Request{}, err
	}
	if err := requireText("source_project", dto.SourceProject); err != nil {
		return Request{}, err
	}
	if err := requireText("target_project", dto.TargetProject); err != nil {
		return Request{}, err
	}
	if err := requireText("nonce_namespace", dto.NonceNamespace); err != nil {
		return Request{}, err
	}
	if err := requireFingerprint("required_capability_set_fingerprint", dto.RequiredCapabilitySetFingerprint); err != nil {
		return Request{}, err
	}
	if err := requireFingerprint("policy_hash", dto.PolicyHash); err != nil {
		return Request{}, err
	}
	return Request{
		schema:                           dto.Schema,
		authorityID:                      strings.TrimSpace(dto.AuthorityID),
		routeID:                          strings.TrimSpace(dto.RouteID),
		sourceProject:                    strings.TrimSpace(dto.SourceProject),
		targetProject:                    strings.TrimSpace(dto.TargetProject),
		requestedIsolationTier:           tier,
		workspaceGrants:                  workspaceGrants,
		networkGrants:                    networkGrants,
		credentialScopeRefs:              credentialScopeRefs,
		serviceGrants:                    serviceGrants,
		brokerGrantRefs:                  brokerGrantRefs,
		requiredCapabilitySetFingerprint: dto.RequiredCapabilitySetFingerprint,
		policyHash:                       dto.PolicyHash,
		nonceNamespace:                   strings.TrimSpace(dto.NonceNamespace),
		restoreEpoch:                     dto.RestoreEpoch,
		trustRootEpoch:                   dto.TrustRootEpoch,
	}, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing struct{}
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("runtimeauthority: parse trailing JSON: %w", err)
	}
	return fmt.Errorf("runtimeauthority: trailing JSON value")
}

func parseIsolationTier(raw string) (IsolationTier, error) {
	tier := IsolationTier(strings.TrimSpace(raw))
	switch tier {
	case IsolationSameUIDProcess, IsolationOSSandbox, IsolationContainer, IsolationVM, IsolationMicroVM:
		return tier, nil
	case "":
		return "", fmt.Errorf("runtimeauthority: requested_isolation_tier is required")
	default:
		return "", fmt.Errorf("runtimeauthority: unsupported requested_isolation_tier %q", raw)
	}
}

func requireText(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("runtimeauthority: %s is required", field)
	}
	return nil
}

func requireFingerprint(field, value string) error {
	if !sha256Pattern.MatchString(value) {
		return fmt.Errorf("runtimeauthority: %s must be a lowercase sha256 fingerprint", field)
	}
	return nil
}

func normalizedList(field string, values []string) ([]string, error) {
	if values == nil {
		return nil, fmt.Errorf("runtimeauthority: %s is required", field)
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for i, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil, fmt.Errorf("runtimeauthority: %s[%d] is empty", field, i)
		}
		if _, ok := seen[value]; ok {
			return nil, fmt.Errorf("runtimeauthority: %s contains duplicate value %q", field, value)
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func previewPathGrants(grants []string) ([]PathGrantPreview, []UnsupportedField) {
	var paths []PathGrantPreview
	var unsupported []UnsupportedField
	for _, grant := range grants {
		access, path, ok := pathGrant(grant)
		if !ok {
			unsupported = append(unsupported, UnsupportedField{
				Field:  "workspace_grants",
				Value:  grant,
				Reason: "logical workspace grant requires broker resolution before Hazmat path compilation",
			})
			continue
		}
		paths = append(paths, PathGrantPreview{
			Path:   filepath.Clean(path),
			Access: string(access),
			Source: grant,
		})
	}
	sort.Slice(paths, func(i, j int) bool {
		if paths[i].Path == paths[j].Path {
			return paths[i].Access < paths[j].Access
		}
		return paths[i].Path < paths[j].Path
	})
	return paths, unsupported
}

func pathGrant(grant string) (containment.PathAccess, string, bool) {
	for _, prefix := range []struct {
		text   string
		access containment.PathAccess
	}{
		{text: "read:path:", access: containment.PathReadOnly},
		{text: "write:path:", access: containment.PathReadWrite},
	} {
		if strings.HasPrefix(grant, prefix.text) {
			path := strings.TrimPrefix(grant, prefix.text)
			if filepath.IsAbs(path) {
				return prefix.access, path, true
			}
			return "", "", false
		}
	}
	return "", "", false
}

func previewNetwork(grants []string) (NetworkPosture, sessionmeta.NetworkMode, []UnsupportedField) {
	if len(grants) == 0 || hasGrant(grants, "deny:all") || hasGrant(grants, "network:none") {
		return NetworkDeniedOnly, sessionmeta.NetworkNone, nil
	}
	if hasGrant(grants, "allow:tailnet") && hasGrant(grants, "deny:public-internet") && !hasGrant(grants, "allow:public-internet") {
		return NetworkTailnetOnly, sessionmeta.NetworkDefault, []UnsupportedField{{
			Field:  "network_grants",
			Value:  "allow:tailnet",
			Reason: "Hazmat preview cannot prove tailnet-only egress without a runtime capability declaration",
		}}
	}
	if hasGrant(grants, "allow:public-internet") {
		return NetworkDefaultRequested, sessionmeta.NetworkDefault, nil
	}
	out := make([]UnsupportedField, 0, len(grants))
	for _, grant := range grants {
		out = append(out, UnsupportedField{
			Field:  "network_grants",
			Value:  grant,
			Reason: "network grant is not recognized by the Hazmat preview adapter",
		})
	}
	return NetworkUnsupported, sessionmeta.NetworkDefault, out
}

func previewCredentialMode(scopeRefs, grantRefs []string) CredentialMode {
	if onlyValue(scopeRefs, "credential:none") && onlyValue(grantRefs, "grant:none") {
		return CredentialNone
	}
	return CredentialBrokerRequired
}

func logicalResources(workspaceGrants, networkGrants, serviceGrants []string) []string {
	values := make([]string, 0, len(workspaceGrants)+len(networkGrants)+len(serviceGrants))
	for _, grant := range workspaceGrants {
		values = append(values, "workspace:"+grant)
	}
	for _, grant := range networkGrants {
		values = append(values, "network:"+grant)
	}
	for _, grant := range serviceGrants {
		values = append(values, "service:"+grant)
	}
	sort.Strings(values)
	return values
}

func hasGrant(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func onlyValue(values []string, want string) bool {
	if len(values) != 1 {
		return false
	}
	return values[0] == want
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

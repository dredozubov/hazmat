package runtimecapability

import (
	"fmt"
	"strings"
	"time"

	"hazmat/attestationkey"
)

type RoutingEligibilityInput struct {
	Declaration             Declaration
	Catalog                 CoverageCatalog
	VerifierResult          SignedVerifierResult
	RevocationFeed          SignedRevocationFeed
	DeclarationKey          attestationkey.Key
	VerifierKey             attestationkey.Key
	RevocationFeedKey       attestationkey.Key
	ExpectedSignerTrustRoot string
	ExpectedVerifier        string
	ExpectedBackendVersion  string
	Now                     time.Time
	ExpectedArtifactHashes  map[string]string
	ExpectedObligations     map[CapabilityFlag]string
}

type RoutingEligibility struct {
	CapabilitySetID          string
	CapabilitySetFingerprint string
	BackendID                string
	BackendKind              BackendKind
	BackendVersion           string
	IsolationTier            IsolationTier
	WorkspaceGrantPatterns   []string
	NetworkGrantPatterns     []string
	CredentialModes          []CredentialMode
	ServiceGrantPatterns     []string
	Lifecycle                LifecycleDecision
}

func VerifyRoutingEligibility(input RoutingEligibilityInput) (RoutingEligibility, error) {
	expectedRoot := strings.TrimSpace(input.ExpectedSignerTrustRoot)
	if expectedRoot == "" {
		return RoutingEligibility{}, fmt.Errorf("runtimecapability: expected signer trust root is required for routing")
	}
	if err := VerifyCapabilityConformance(input.Declaration, input.Catalog, input.VerifierResult, ConformanceVerifyInput{
		DeclarationKey:          input.DeclarationKey,
		VerifierKey:             input.VerifierKey,
		ExpectedSignerTrustRoot: expectedRoot,
		ExpectedVerifier:        input.ExpectedVerifier,
		ExpectedBackendVersion:  input.ExpectedBackendVersion,
		Now:                     input.Now,
		ExpectedArtifactHashes:  input.ExpectedArtifactHashes,
		ExpectedObligations:     input.ExpectedObligations,
	}); err != nil {
		return RoutingEligibility{}, err
	}
	lifecycle, err := EvaluateLifecycle(input.Declaration, input.RevocationFeed, LifecycleVerifyInput{
		DeclarationKey: input.DeclarationKey,
		FeedKey:        input.RevocationFeedKey,
		Now:            input.Now,
	})
	if err != nil {
		return RoutingEligibility{}, err
	}
	if lifecycle.Dispatch != DispatchAllow {
		return RoutingEligibility{}, fmt.Errorf("runtimecapability: lifecycle dispatch denied: %s", lifecycle.Dispatch)
	}
	capability, err := capabilityFromPayload(input.Declaration.Capability)
	if err != nil {
		return RoutingEligibility{}, err
	}
	return RoutingEligibility{
		CapabilitySetID:          capability.capabilitySetID,
		CapabilitySetFingerprint: input.Declaration.CapabilitySetFingerprint,
		BackendID:                capability.backendID,
		BackendKind:              capability.backendKind,
		BackendVersion:           capability.backendVersion,
		IsolationTier:            capability.isolationTier,
		WorkspaceGrantPatterns:   copyStrings(capability.workspaceGrantPatterns),
		NetworkGrantPatterns:     copyStrings(capability.networkGrantPatterns),
		CredentialModes:          copyCredentialModes(capability.credentialModes),
		ServiceGrantPatterns:     copyStrings(capability.serviceGrantPatterns),
		Lifecycle:                lifecycle,
	}, nil
}

func copyCredentialModes(values []CredentialMode) []CredentialMode {
	if len(values) == 0 {
		return nil
	}
	out := make([]CredentialMode, len(values))
	copy(out, values)
	return out
}

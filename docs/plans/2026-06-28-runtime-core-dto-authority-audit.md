# Runtime Core DTO And Authority Audit

Date: 2026-06-28

Parent bead: `sandboxing-xuar.1.2`

Design sources:

- `docs/plans/2026-06-27-reusable-runtime-core-user-isolation-design.md`
- `docs/plans/2026-06-27-linux-support-two-lane-design.md`
- `docs/plans/2026-06-13-linux-run-agent-readiness-gates.md`

## Scope

This audit covers the reusable-core packages named by the runtime core design:
`sessionrequest`, `pathpolicy`, `sessionplanner`, `sessioncontract`,
`containment`, `sessionbackend`, `credentials`, `harnesses`, `integrations`,
`runtimeauthority`, and `runtimecapability`.

The question is whether authority-bearing values can be created from raw DTOs
or public fields without passing through constructor, parser, validation,
defensive-copy, or verification gates.

## Audit Table

| Package or surface | Raw DTO/input | Authority type | Constructor or validator | Evidence | Verdict |
| --- | --- | --- | --- | --- | --- |
| `sessionrequest` | `Input` with raw project/read/write paths | `Request` with private `ProjectRoot`, `ReadOnlyGrant`, `ReadWriteGrant` fields | `New`, `ResolveProjectRoot`, `ResolveReadOnlyGrants`, `ResolveReadWriteGrants` | `hazmat/sessionrequest/sessionrequest.go`; `TestNewBuildsTypedRequestAndDedupsGrants`; `TestNewReportsStageAndPreservesDenyZoneError`; `TestNewRequiresConfiguredDenyPolicy` | Pass. Raw paths become authority only through `pathpolicy` constructors. Accessors defensively copy grant slices. |
| `pathpolicy` | Raw strings and base homes | `AbsolutePath`, `ExistingDir`, `CanonicalDir`, `ProjectRoot`, `ReadOnlyGrant`, `ReadWriteGrant`, `DenyPolicy` | `NewDenyPolicy`, `DefaultDenyPolicy`, `NewAbsolutePath`, `NewExistingDir`, `NewCanonicalDir`, `NewProjectRoot`, `NewReadOnlyGrant`, `NewReadWriteGrant` | `hazmat/pathpolicy/pathpolicy.go`; `TestValidatedConstructorsRequireDenyPolicy`; `TestValidatedConstructorsRejectZeroCanonicalDir`; `TestValidatedConstructorsRejectCredentialDenyZones`; `TestValidatedConstructorsRejectHostStateDenyZones`; `TestDenyPolicyHostStateOverlap`; `TestAppendUniqueDirsCopiesAndReportsAdded` | Pass. Constructors require configured deny policy before path authority exists. |
| `containment.Contract` | `ContractInput`, `PathGrant`, `AgentHomePolicy`, `TempPolicy`, `NetworkPolicy`, `ProcessPolicy`, `ServiceGrant` | `Contract` plus structural `CredentialFloor` | `NewCredentialFloor`, `CredentialFloorFromDenies`, `WithHostAuthorityDenies`, `NewContract`, `Contract.Validate` | `hazmat/containment/contract.go`; `TestContractCopiesPathLists`; `TestNewContractDerivesCredentialFloor`; `TestContractValidateRejectsUnconstructedCredentialFloor`; `TestContractValidateRejectsMutatedCredentialFloor`; `TestNewContractRejectsCredentialDenyOverlap`; `TestNewContractRejectsMismatchedPathGrantAccess` | Pass with a compatibility boundary. `Contract` has exported JSON fields, but backend compilers call `Validate`, and credential floor private state prevents raw DTOs from acting as constructed contracts. |
| `containment` backend compilers | Constructed `containment.Contract` plus backend options | Darwin SBPL, Linux launch spec, Docker spec, Apple Container spec | `darwin.Compile`, `linux.Compile`, `docker.Compile`, `applecontainer.Compile` | `hazmat/containment/darwin/sbpl.go`; `hazmat/containment/linux/spec.go`; `hazmat/containment/docker/spec.go`; `hazmat/containment/applecontainer/spec.go`; each compiler calls `contract.Validate`; tests reject unconstructed credential floors and backend-specific unsafe options | Pass. Compilers reject invalid contracts before emitting enforcement artifacts. |
| `sessionbackend.Plan` | `Input` with target, mode, paths, integrations, host facts | `Plan` backend/gap DTO | `BuildPlan`, `BackendFor`, capability gap builders | `hazmat/sessionbackend/plan.go`; `TestBuildPlanForDarwinNativeCopiesInputs`; `TestBuildPlanReportsLinuxNativeGap`; `TestBuildPlanRequiresExplicitGOOSForNative`; `TestBuildPlanReportsDockerIntegrationEnvGap`; `TestBuildPlanForAppleContainerReportsPlanOnlyGapAndCleanupArtifact` | Pass as data-only preview. Not sufficient as launch authority by itself. |
| `sessionbackend.PreparedLaunch` | `Plan`, `DarwinSeatbelt`, `LinuxLaunchSpec`, `DockerSandboxSpec`, `RemoteEnvelope`, `AppleContainerLaunchSpec`, `AcceptedGap` | `PreparedLaunch` with sealed artifact variant and private fields | `NewDarwinSeatbeltArtifact`, `NewLinuxLaunchArtifact`, `NewDockerSandboxArtifact`, `NewRemoteEnvelopeArtifact`, `NewAppleContainerArtifact`, `NewPreparedLaunch` | `hazmat/sessionbackend/artifact.go`; `TestPreparedLaunchHasNoExportedAuthorityFields`; `TestPreparedArtifactIsSealed`; `TestNewPreparedLaunchRejectsBackendMismatch`; `TestNewPreparedLaunchRequiresAcceptedCapabilityGaps`; `TestNewPreparedLaunchRejectsExtraAcceptedCapabilityGap`; `TestPreparedLaunchRequiresExplicitDTOForJSON` | Partial. Sealing, backend matching, gap acceptance, defensive copies, and explicit DTO scope exist. Follow-up `sandboxing-xuar.1.4` must validate raw `Plan` and artifact DTO fields before they can become prepared launch authority. Disclosure hardening continues in `sandboxing-xuar.1.3`. |
| `sessioncontract` | `Request`, `PlanInput` | `Plan` preview DTO | `Request.Normalized`, `LaunchMetadataInput`, `BuildPlan` | `hazmat/sessioncontract/sessioncontract.go`; `TestRequestNormalizedAndLaunchMetadataInput`; `TestBuildPlanCopiesAndSortsStableFields`; `TestBuildPlanDockerNetworkMetadata` | Pass as data-only preview. It does not launch, mutate, or compile enforcement artifacts. |
| `sessionplanner` | `Input`, `HarnessRequirement`, `Warning` | Composed `Plan` preview DTO | `Build`, `BuildContractPlan`, `BuildBackendPlan` | `hazmat/sessionplanner/plan.go`; `TestBuildComposesContractAndBackendPlans`; `TestGoldenSessionPlannerPlanBaselines` | Pass as data-only composition. It delegates authority construction to `sessioncontract` and `sessionbackend`. |
| `credentials` | `RegistryPaths`, descriptor IDs/env vars | `Descriptor` registry entries and derived paths | `BuiltinDescriptors`, `FindDescriptor`, `ProviderDescriptorForEnvVarAndHarness`, `StorePathForHome`, `StorePathForConfig`, `AgentMaterializationPath` | `hazmat/credentials/registry.go`; `TestBuiltinDescriptorsUseSuppliedPaths`; `TestBuiltinDescriptorsReturnIndependentSlices`; `TestMaterializedPathMustStayUnderAgentHome`; `TestStorePathForConfigUsesConfigDirectory` | Pass for reusable descriptors. Secret store operations and materialization remain outside this package in runtime code. |
| `harnesses` | Built-in metadata table | `Metadata` and `Spec` descriptors | `BuiltinMetadata`, `MetadataByID`, `MustMetadata`, `SpecByID`, `MustSpec` | `hazmat/harnesses/harnesses.go`; `TestBuiltinMetadataIsCompleteAndUnique`; `TestImportPolicyDocumentsSupportedAndNoImportHarnesses`; `TestBuiltinMetadataReturnsCopy` | Pass. Metadata is descriptive and returned by copy. |
| `integrations` | YAML `Spec`, `Resolved`, platform/env strings | `AuthoritySpec`, `ResolvedAuthority` | `LoadSpec`, `NewAuthoritySpec`, `NewPlatformID`, `NewResolvedAuthority`, `MergeResolved`, `ValidateSchema`, `ValidateEnvKeys` | `hazmat/integrations/manifest.go`; `hazmat/integrations/merge.go`; `TestLoadSpecRejectsCredentialEnvKey`; `TestNewAuthoritySpecNormalizesAndCopies`; `TestNewResolvedAuthorityRejectsDuplicateResolvedEnvAfterNormalization`; `TestMergeResolvedRejectsInvalidPlatform` | Pass. Credential-shaped env passthrough is rejected before integration authority exists. DTO accessors return copies. |
| `runtimeauthority` | JSON `requestDTO` | `Request` with private fields; `Preview` DTO | `ParseJSON`, `newRequest`, `BuildPreview`, `PreviewJSON` | `hazmat/runtimeauthority/preview.go`; `TestParseRejectsUnknownAuthorityField`; `TestParseRejectsUnknownIsolationTier`; `TestParseRejectsMalformedAndDuplicateInputs`; `TestRequestAccessorsDefensivelyCopy` | Pass. JSON parsing rejects unknown fields and routes through private authority state. Preview output is DTO only. |
| `runtimecapability` capability declarations | `CapabilityPayload`, `Declaration`, `SignedVerifierResult`, `SignedRevocationFeed` | `Capability`, verified declaration/conformance/lifecycle decisions | `NewCapability`, `ParseCapabilityJSON`, `SignDeclaration`, `ParseDeclarationJSON`, `VerifyDeclaration`, `NewCoverageCatalog`, `SignVerifierResult`, `VerifyCapabilityConformance`, `SignRevocationFeed`, `EvaluateLifecycle` | `hazmat/runtimecapability/*.go`; `TestParseCapabilityRejectsUnknownFieldsAndBooleanFlags`; `TestVerifyRejectsUnsignedExpiredFutureWrongSignerAndWrongVersion`; `TestVerifyRejectsTamperedDeclarationAndPayload`; `TestVerifyCapabilityConformance`; `TestVerifyConformanceRejectsMissingFlagCoverage`; `TestLifecycleMissingCoverageFailsClosed`; `TestLifecycleRejectsTamperedFeedSignature` | Pass for local verification APIs. Routing must not trust raw `Declaration` or `CapabilityPayload`; field classification and routing eligibility remain tracked by `sandboxing-xuar.2.3`. |

## Follow-Up Beads

| Bead | Reason |
| --- | --- |
| `sandboxing-xuar.1.3` | PreparedLaunch disclosure scope still needs a dedicated field/scope table and golden tests for operator-private and secret-adjacent fields. |
| `sandboxing-xuar.1.4` | `PreparedLaunch` construction must reject raw or partially-filled `Plan` and backend artifact DTOs before they can become launch authority. |
| `sandboxing-xuar.2.3` | Runtime authority and capability records need field classification before any record can influence provider routing. |

## Conclusion

No reusable-core package currently imports host-effect runtime plumbing after
`sandboxing-xuar.1.1`. Most authority-bearing values already route through
constructors, validators, defensive-copy accessors, or signature/conformance
verification. The remaining unsafe authority state is localized to
`sessionbackend.PreparedLaunch` input validation and is tracked by
`sandboxing-xuar.1.4`. Runtime capability routing is intentionally not enabled
until `sandboxing-xuar.2.3` classifies fields and requires verification,
expiry, trust-root, conformance, and revocation evidence.

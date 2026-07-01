----------------------------- MODULE MC_ServiceHarnessLifecycle -----------------------------

EXTENDS Naturals, TLC

\* Session-scoped service/proxy lifecycle model.
\*
\* This spec covers OpenHands-style service harnesses and service-shaped
\* proxy frontends (local API proxy, future HTTP MCP proxy) before adapter code
\* exists. Stdio MCP remains a foreground child-process proxy and reuses the
\* launch/fd-isolation model instead of this service lifecycle. This model
\* covers only Hazmat's host/session lifecycle contract:
\*   - prior service residue is recovered or recorded before a new service starts
\*   - unsupported service features fail closed before side effects
\*   - current-service metadata is recorded before credentials, process start,
\*     attach authority, or user-visible attach details
\*   - health/readiness gates attach
\*   - local attach points stay local and localhost ports require a token
\*   - terminal residue is cleaned up or has a recorded cleanup failure
\*
\* It does not model a concrete service protocol, HTTP request bodies, Docker
\* internals, browser automation, or OpenHands behavior after launch.

ServiceKinds == {"service-harness", "api-proxy", "http-mcp-proxy"}
Backends == {"native", "docker-sandbox", "vm"}
AttachKinds == {"stdio", "uds", "localhost-port", "lan-port"}
TokenPolicies == {"none", "session-token"}
CredentialKinds == {"none", "typed", "untyped"}
StartResults == {"succeed", "fail-no-residue", "fail-with-residue"}
ExitKinds == {"none", "normal", "crash"}
Phases == 0..10

Requests ==
    [serviceKind : ServiceKinds,
     backend : Backends,
     attachKind : AttachKinds,
     tokenPolicy : TokenPolicies,
     credentialKind : CredentialKinds,
     requiresContainer : BOOLEAN,
     hostDockerSocketRequested : BOOLEAN,
     profileImportRequested : BOOLEAN,
     persistentDaemonRequested : BOOLEAN,
     browserAutomationRequested : BOOLEAN,
     integrationEnvRequested : BOOLEAN,
     startResult : StartResults,
     healthOK : BOOLEAN]

PriorStates ==
    [serviceResidue : BOOLEAN,
     credentialResidue : BOOLEAN,
     attachResidue : BOOLEAN,
     metadataRecorded : BOOLEAN]

CurrentStates ==
    [metadataRecorded : BOOLEAN,
     serviceStarted : BOOLEAN,
     serviceRunning : BOOLEAN,
     readinessEvidence : BOOLEAN,
     attached : BOOLEAN,
     attachDetailsPrinted : BOOLEAN,
     attachAuthorityActive : BOOLEAN,
     credentialMaterialized : BOOLEAN,
     credentialRemoved : BOOLEAN,
     cleanupFailureRecorded : BOOLEAN,
     failed : BOOLEAN,
     rejected : BOOLEAN,
     exitKind : ExitKinds]

InitialCurrent ==
    [metadataRecorded |-> FALSE,
     serviceStarted |-> FALSE,
     serviceRunning |-> FALSE,
     readinessEvidence |-> FALSE,
     attached |-> FALSE,
     attachDetailsPrinted |-> FALSE,
     attachAuthorityActive |-> FALSE,
     credentialMaterialized |-> FALSE,
     credentialRemoved |-> FALSE,
     cleanupFailureRecorded |-> FALSE,
     failed |-> FALSE,
     rejected |-> FALSE,
     exitKind |-> "none"]

VARIABLES
    req,
    phase,
    prior,
    genesisPrior,
    cur

vars == <<req, phase, prior, genesisPrior, cur>>

PriorResiduePresentFor(p) ==
    \/ p.serviceResidue
    \/ p.credentialResidue
    \/ p.attachResidue

PriorResiduePresent == PriorResiduePresentFor(prior)

CurrentResiduePresent ==
    \/ cur.serviceRunning
    \/ cur.attachAuthorityActive
    \/ cur.credentialMaterialized

AnyResiduePresent ==
    \/ PriorResiduePresent
    \/ CurrentResiduePresent

ForbiddenFeatureRequested ==
    \/ req.hostDockerSocketRequested
    \/ req.profileImportRequested
    \/ req.persistentDaemonRequested
    \/ req.browserAutomationRequested
    \/ req.integrationEnvRequested

BadAttachPolicy ==
    \/ req.attachKind = "lan-port"
    \/ /\ req.attachKind = "localhost-port"
       /\ req.tokenPolicy # "session-token"

UnsupportedContainerBackend ==
    /\ req.requiresContainer
    /\ req.backend = "native"

UnsupportedCredential ==
    req.credentialKind = "untyped"

ProxyServiceKind ==
    req.serviceKind \in {"api-proxy", "http-mcp-proxy"}

BadProxyAttachShape ==
    /\ ProxyServiceKind
    /\ req.attachKind = "stdio"

UnsupportedRequest ==
    \/ ForbiddenFeatureRequested
    \/ BadAttachPolicy
    \/ UnsupportedContainerBackend
    \/ UnsupportedCredential
    \/ BadProxyAttachShape

LocalAttachPolicySatisfied ==
    \/ req.attachKind \in {"stdio", "uds"}
    \/ /\ req.attachKind = "localhost-port"
       /\ req.tokenPolicy = "session-token"

TypeOK ==
    /\ req \in Requests
    /\ phase \in Phases
    /\ prior \in PriorStates
    /\ genesisPrior \in PriorStates
    /\ cur \in CurrentStates

Init ==
    /\ req \in Requests
    /\ phase = 0
    /\ prior \in PriorStates
    /\ prior.metadataRecorded = PriorResiduePresentFor(prior)
    /\ genesisPrior = prior
    /\ cur = InitialCurrent

NoPriorResidue ==
    /\ phase = 0
    /\ ~PriorResiduePresent
    /\ phase' = 1
    /\ UNCHANGED <<req, prior, genesisPrior, cur>>

RecoverPriorResidueSucceed ==
    /\ phase = 0
    /\ PriorResiduePresent
    /\ prior' =
        [prior EXCEPT
            !.serviceResidue = FALSE,
            !.credentialResidue = FALSE,
            !.attachResidue = FALSE,
            !.metadataRecorded = FALSE]
    /\ phase' = 1
    /\ UNCHANGED <<req, genesisPrior, cur>>

RecoverPriorResidueFail ==
    /\ phase = 0
    /\ PriorResiduePresent
    /\ cur' =
        [cur EXCEPT
            !.cleanupFailureRecorded = TRUE,
            !.failed = TRUE]
    /\ phase' = 10
    /\ UNCHANGED <<req, prior, genesisPrior>>

RejectUnsupportedRequest ==
    /\ phase = 1
    /\ UnsupportedRequest
    /\ cur' =
        [cur EXCEPT
            !.failed = TRUE,
            !.rejected = TRUE]
    /\ phase' = 10
    /\ UNCHANGED <<req, prior, genesisPrior>>

PlanService ==
    /\ phase = 1
    /\ ~UnsupportedRequest
    /\ cur' = [cur EXCEPT !.metadataRecorded = TRUE]
    /\ phase' = 2
    /\ UNCHANGED <<req, prior, genesisPrior>>

MaterializeCredential ==
    /\ phase = 2
    /\ req.credentialKind = "typed"
    /\ cur' = [cur EXCEPT !.credentialMaterialized = TRUE]
    /\ phase' = 3
    /\ UNCHANGED <<req, prior, genesisPrior>>

SkipCredential ==
    /\ phase = 2
    /\ req.credentialKind = "none"
    /\ phase' = 3
    /\ UNCHANGED <<req, prior, genesisPrior, cur>>

StartServiceSucceed ==
    /\ phase = 3
    /\ req.startResult = "succeed"
    /\ cur' =
        [cur EXCEPT
            !.serviceStarted = TRUE,
            !.serviceRunning = TRUE]
    /\ phase' = 4
    /\ UNCHANGED <<req, prior, genesisPrior>>

StartServiceFailNoResidue ==
    /\ phase = 3
    /\ req.startResult = "fail-no-residue"
    /\ cur' = [cur EXCEPT !.failed = TRUE]
    /\ phase' = 7
    /\ UNCHANGED <<req, prior, genesisPrior>>

StartServiceFailWithResidue ==
    /\ phase = 3
    /\ req.startResult = "fail-with-residue"
    /\ cur' =
        [cur EXCEPT
            !.serviceStarted = TRUE,
            !.serviceRunning = TRUE,
            !.failed = TRUE]
    /\ phase' = 7
    /\ UNCHANGED <<req, prior, genesisPrior>>

HealthPass ==
    /\ phase = 4
    /\ req.healthOK
    /\ cur' = [cur EXCEPT !.readinessEvidence = TRUE]
    /\ phase' = 5
    /\ UNCHANGED <<req, prior, genesisPrior>>

HealthFail ==
    /\ phase = 4
    /\ ~req.healthOK
    /\ cur' =
        [cur EXCEPT
            !.failed = TRUE]
    /\ phase' = 7
    /\ UNCHANGED <<req, prior, genesisPrior>>

AttachService ==
    /\ phase = 5
    /\ cur.readinessEvidence
    /\ cur' =
        [cur EXCEPT
            !.attached = TRUE,
            !.attachDetailsPrinted = TRUE,
            !.attachAuthorityActive = req.attachKind \in {"uds", "localhost-port"}]
    /\ phase' = 6
    /\ UNCHANGED <<req, prior, genesisPrior>>

SessionExit(kind) ==
    /\ phase = 6
    /\ kind \in {"normal", "crash"}
    /\ cur' = [cur EXCEPT !.exitKind = kind]
    /\ phase' = 7
    /\ UNCHANGED <<req, prior, genesisPrior>>

CleanupServiceSucceed ==
    /\ phase = 7
    /\ cur.serviceRunning \/ cur.attachAuthorityActive
    /\ cur' =
        [cur EXCEPT
            !.serviceRunning = FALSE,
            !.attachAuthorityActive = FALSE]
    /\ phase' = 8
    /\ UNCHANGED <<req, prior, genesisPrior>>

CleanupServiceFail ==
    /\ phase = 7
    /\ cur.serviceRunning \/ cur.attachAuthorityActive
    /\ cur' =
        [cur EXCEPT
            !.cleanupFailureRecorded = TRUE,
            !.failed = TRUE]
    /\ phase' = 8
    /\ UNCHANGED <<req, prior, genesisPrior>>

SkipServiceCleanup ==
    /\ phase = 7
    /\ ~cur.serviceRunning
    /\ ~cur.attachAuthorityActive
    /\ phase' = 8
    /\ UNCHANGED <<req, prior, genesisPrior, cur>>

CleanupCredentialSucceed ==
    /\ phase = 8
    /\ cur.credentialMaterialized
    /\ cur' =
        [cur EXCEPT
            !.credentialMaterialized = FALSE,
            !.credentialRemoved = TRUE]
    /\ phase' = 10
    /\ UNCHANGED <<req, prior, genesisPrior>>

CleanupCredentialFail ==
    /\ phase = 8
    /\ cur.credentialMaterialized
    /\ cur' =
        [cur EXCEPT
            !.cleanupFailureRecorded = TRUE,
            !.failed = TRUE]
    /\ phase' = 10
    /\ UNCHANGED <<req, prior, genesisPrior>>

SkipCredentialCleanup ==
    /\ phase = 8
    /\ ~cur.credentialMaterialized
    /\ phase' = 10
    /\ UNCHANGED <<req, prior, genesisPrior, cur>>

Done ==
    /\ phase = 10
    /\ UNCHANGED vars

Next ==
    \/ NoPriorResidue
    \/ RecoverPriorResidueSucceed
    \/ RecoverPriorResidueFail
    \/ RejectUnsupportedRequest
    \/ PlanService
    \/ MaterializeCredential
    \/ SkipCredential
    \/ StartServiceSucceed
    \/ StartServiceFailNoResidue
    \/ StartServiceFailWithResidue
    \/ HealthPass
    \/ HealthFail
    \/ AttachService
    \/ \E kind \in {"normal", "crash"} : SessionExit(kind)
    \/ CleanupServiceSucceed
    \/ CleanupServiceFail
    \/ SkipServiceCleanup
    \/ CleanupCredentialSucceed
    \/ CleanupCredentialFail
    \/ SkipCredentialCleanup
    \/ Done

Spec == Init /\ [][Next]_vars

PriorResidueHasMetadata ==
    PriorResiduePresent => prior.metadataRecorded

SideEffectsHaveMetadata ==
    (cur.serviceStarted \/ CurrentResiduePresent \/ cur.attachDetailsPrinted)
        => cur.metadataRecorded

AttachAuthorityHasMetadata ==
    cur.attachAuthorityActive => cur.metadataRecorded

StartOnlyAfterPriorResidueHandled ==
    cur.serviceStarted => ~PriorResiduePresent

UnsupportedRequestsFailClosed ==
    UnsupportedRequest =>
        /\ ~cur.serviceStarted
        /\ ~CurrentResiduePresent
        /\ ~cur.attachDetailsPrinted

CredentialMaterializationGated ==
    cur.credentialMaterialized =>
        /\ cur.metadataRecorded
        /\ req.credentialKind = "typed"
        /\ ~UnsupportedRequest

ReadyRequiresHealth ==
    cur.readinessEvidence =>
        /\ req.healthOK
        /\ (phase \in {5, 6, 7} => cur.serviceRunning)

AttachOnlyAfterReady ==
    cur.attached => cur.readinessEvidence

AttachDetailsAfterReady ==
    cur.attachDetailsPrinted => cur.readinessEvidence

AttachPolicyLocalOnly ==
    cur.attachDetailsPrinted => LocalAttachPolicySatisfied

LocalhostPortRequiresToken ==
    /\ cur.attachDetailsPrinted
    /\ req.attachKind = "localhost-port"
    => req.tokenPolicy = "session-token"

ProxyServiceAttachPolicy ==
    /\ cur.attachDetailsPrinted
    /\ ProxyServiceKind
    => /\ req.attachKind \in {"uds", "localhost-port"}
       /\ LocalAttachPolicySatisfied

NoHostDockerSocketExposure ==
    cur.serviceStarted => ~req.hostDockerSocketRequested

NoNativeContainerStart ==
    /\ cur.serviceStarted
    /\ req.requiresContainer
    => req.backend # "native"

NoProfileDaemonBrowserOrEnvStart ==
    cur.serviceStarted =>
        /\ ~req.profileImportRequested
        /\ ~req.persistentDaemonRequested
        /\ ~req.browserAutomationRequested
        /\ ~req.integrationEnvRequested

TerminalResidueHandled ==
    phase = 10 => (~AnyResiduePresent \/ cur.cleanupFailureRecorded)

RejectedRequestsHaveNoCurrentSideEffects ==
    cur.rejected =>
        /\ ~cur.serviceStarted
        /\ ~CurrentResiduePresent
        /\ ~cur.attachDetailsPrinted

CredentialRemovedOnlyAfterTypedPlan ==
    cur.credentialRemoved =>
        /\ cur.metadataRecorded
        /\ req.credentialKind = "typed"

=============================================================================

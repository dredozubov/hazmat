---- MODULE MC_LaunchFDIsolation ----
\* Launch-time file descriptor isolation for the Tier 2 native helper path.
\*
\* This spec models Hazmat's host-side launch chains:
\*   direct:   hazmat (invoker uid) -> sudo -> hazmat-launch -> sandbox_init() -> exec agent
\*   brokered: hazmat (invoker uid) -> agent launch broker -> child -> sandbox_init() -> exec agent
\*
\* The key threat is an already-open descriptor inherited from the invoker's
\* process tree. Seatbelt path denies do not revoke access granted by an
\* inherited live descriptor, so the helper must sanitize its fd table before
\* calling sandbox_init().
\*
\* The model treats upstream launch behavior as adversarial environment knobs:
\*   1. Go's exec path may or may not collapse hazmat -> sudo to stdio only.
\*   2. sudo may or may not apply closefrom-style cleanup before execing the helper.
\*   3. a persistent launch broker child may inherit broker-owned descriptors.
\*   4. broker startup may inherit host descriptors unless routed through the
\*      same fd-cleaning helper exec boundary.
\*
\* The proved design obligations are:
\*   - the launch executor closes every inherited fd >= 3 before sandbox_init()
\*   - the long-lived broker closes startup-inherited fds before listening
\*   - any fd the helper opens itself for policy validation is CLOEXEC
\*   - the brokered path authenticates the peer before forking a launch child
\*
\* Beadpost attestation boundary (2026-06-09 minimal addition):
\*   The contained-agent submitter + dr-owned host broker design requires that
\*   broker authority and attestation minting happen ONLY after containment is
\*   actually confirmed, i.e. after sandbox_init() succeeds and the helper emits
\*   its confirmed-containment metadata line. A pre-sudo "prepared launch" is not
\*   confirmed containment. This spec adds the minimal launch-order facts
\*   (metadataEmitted -> brokerActive -> tokenMinted) and proves the broker/mint
\*   never precede confirmation, plus that no fd carrying host authority material
\*   (a leaked signing-key fd) survives to the final agent exec. Request routing
\*   itself is out of scope here (see MC_BeadpostBrokerBoundary).
\*
\* Governed code:
\*   hazmat/native_launch.go — backend-neutral native launch contract
\*   hazmat/native_launch_darwin.go — Darwin sudo + helper command shape
\*   hazmat/internal/runtime/darwin/runtime.go — shared helper argv builder
\*   hazmat/agent_launch.go — fixed-script native launch wrapper
\*   hazmat/session.go — runAgentSeatbeltScriptWithUI()
\*   hazmat/cmd/hazmat-launch/main.go — helper-side fd sanitization, policy read, sandbox_init, exec
\*   hazmat/internal/runtime/launchbroker/*.go — authenticated broker request and child-plan fd cleanup contract
\*   hazmat/internal/agententry/commands.go, hazmat/launch_broker_agent_entry.go — agent-side broker service entrypoint
\*   hazmat/launch_broker_supervisor.go — host-side broker startup command construction

EXTENDS Naturals, FiniteSets

CONSTANTS
    HelperClosesInheritedFDs,
    PolicyFileUsesCloexec,
    BrokerAuthenticatesPeer,
    BrokerStartupClosesInheritedFDs

FDs == 0..7
StdioFDs == 0..2
InheritedExtraFDs == {3, 4}
PolicyFD == 5
BrokerListenFD == 6
BrokerConnFD == 7

Targets == {"stdio", "credential", "benign", "policy", "authority", "broker_socket", "broker_request", "unused"}
Origins == {"shell", "helper", "broker", "none"}
Stages == {"hazmat", "sudo", "broker_starting", "broker_listening", "broker", "helper", "helper_sanitized", "policy_opened", "temp_prepared", "sandboxed", "agent"}
LaunchModes == {"unset", "sudo_helper", "brokered"}

AllowedHelperTargetsAtSandbox == {"stdio", "policy"}
AllowedAgentTargets == {"stdio"}

VARIABLES
    stage,
    launchMode,
    hazmatFds,
    sudoFds,
    brokerFds,
    helperFds,
    agentFds,
    fdTarget,
    fdOrigin,
    fdCloexec,
    goExecClosesParentFDs,
    sudoClosesInheritedFDs,
    peerAuthenticated,
    metadataEmitted,
    brokerActive,
    tokenMinted

vars ==
    <<stage, launchMode, hazmatFds, sudoFds, brokerFds, helperFds, agentFds,
      fdTarget, fdOrigin, fdCloexec,
      goExecClosesParentFDs, sudoClosesInheritedFDs, peerAuthenticated,
      metadataEmitted, brokerActive, tokenMinted>>

TypeOK ==
    /\ stage \in Stages
    /\ launchMode \in LaunchModes
    /\ hazmatFds \subseteq FDs
    /\ sudoFds \subseteq FDs
    /\ brokerFds \subseteq FDs
    /\ helperFds \subseteq FDs
    /\ agentFds \subseteq FDs
    /\ fdTarget \in [FDs -> Targets]
    /\ fdOrigin \in [FDs -> Origins]
    /\ fdCloexec \in [FDs -> BOOLEAN]
    /\ goExecClosesParentFDs \in BOOLEAN
    /\ sudoClosesInheritedFDs \in BOOLEAN
    /\ peerAuthenticated \in BOOLEAN
    /\ metadataEmitted \in BOOLEAN
    /\ brokerActive \in BOOLEAN
    /\ tokenMinted \in BOOLEAN

Init ==
    /\ \E inherited \in SUBSET InheritedExtraFDs :
        hazmatFds = StdioFDs \cup inherited
    /\ launchMode = "unset"
    /\ sudoFds = {}
    /\ brokerFds = {}
    /\ helperFds = {}
    /\ agentFds = {}
    \* fd 3 carries credential material; fd 4 is an inherited extra fd that may
    \* adversarially carry host AUTHORITY material (e.g. a leaked broker
    \* signing-key fd) or be benign. Either way it must be sanitized before
    \* sandbox_init() and must never reach the final agent exec.
    /\ \E t4 \in {"benign", "authority"} :
        fdTarget =
            [fd \in FDs |->
                CASE fd \in StdioFDs -> "stdio"
                  [] fd = 3 -> "credential"
                  [] fd = 4 -> t4
                  [] fd = BrokerListenFD -> "broker_socket"
                  [] fd = BrokerConnFD -> "broker_request"
                  [] OTHER -> "unused"]
    /\ fdOrigin =
        [fd \in FDs |->
            IF fd \in StdioFDs \cup InheritedExtraFDs
                THEN "shell"
            ELSE IF fd \in {BrokerListenFD, BrokerConnFD}
                THEN "broker"
            ELSE "none"]
    /\ fdCloexec = [fd \in FDs |-> FALSE]
    /\ goExecClosesParentFDs \in BOOLEAN
    /\ sudoClosesInheritedFDs \in BOOLEAN
    /\ peerAuthenticated = FALSE
    /\ metadataEmitted = FALSE
    /\ brokerActive = FALSE
    /\ tokenMinted = FALSE
    /\ stage = "hazmat"

HazmatExecsSudo ==
    /\ stage = "hazmat"
    /\ launchMode = "unset"
    /\ sudoFds' =
        IF goExecClosesParentFDs
            THEN StdioFDs
            ELSE hazmatFds
    /\ launchMode' = "sudo_helper"
    /\ stage' = "sudo"
    /\ UNCHANGED <<hazmatFds, brokerFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive, tokenMinted>>

HazmatStartsLaunchBroker ==
    /\ stage = "hazmat"
    /\ launchMode = "unset"
    /\ launchMode' = "brokered"
    \* Starting the long-lived agent broker may inherit the same host-origin fds
    \* as the invoking hazmat process. The startup path must sanitize them before
    \* the broker opens its listener and accepts launch requests.
    /\ brokerFds' = hazmatFds
    /\ stage' = "broker_starting"
    /\ UNCHANGED <<hazmatFds, sudoFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive, tokenMinted>>

BrokerStartupSanitizesFDTable ==
    /\ stage = "broker_starting"
    /\ launchMode = "brokered"
    /\ brokerFds' =
        IF BrokerStartupClosesInheritedFDs
            THEN brokerFds \cap StdioFDs
            ELSE brokerFds
    /\ stage' = "broker_listening"
    /\ UNCHANGED <<launchMode, hazmatFds, sudoFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive, tokenMinted>>

BrokerAcceptsAuthenticatedLaunch ==
    /\ stage = "broker_listening"
    /\ launchMode = "brokered"
    \* After startup cleanup, the broker may open its listener and hold the
    \* accepted request socket. The forked launch child must still sanitize
    \* broker-owned descriptors before sandbox_init().
    /\ brokerFds' = brokerFds \cup {BrokerListenFD, BrokerConnFD}
    /\ peerAuthenticated' = BrokerAuthenticatesPeer
    /\ stage' = "broker"
    /\ UNCHANGED <<launchMode, hazmatFds, sudoFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   metadataEmitted, brokerActive, tokenMinted>>

SudoExecsHelper ==
    /\ stage = "sudo"
    /\ launchMode = "sudo_helper"
    /\ helperFds' =
        IF sudoClosesInheritedFDs
            THEN sudoFds \cap StdioFDs
            ELSE sudoFds
    /\ stage' = "helper"
    /\ UNCHANGED <<launchMode, hazmatFds, sudoFds, brokerFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive, tokenMinted>>

BrokerForksLaunchChild ==
    /\ stage = "broker"
    /\ launchMode = "brokered"
    /\ peerAuthenticated
    /\ helperFds' = brokerFds
    /\ stage' = "helper"
    /\ UNCHANGED <<launchMode, hazmatFds, sudoFds, brokerFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive, tokenMinted>>

HelperSanitizesFDTable ==
    /\ stage = "helper"
    /\ helperFds' =
        IF HelperClosesInheritedFDs
            THEN helperFds \cap StdioFDs
            ELSE helperFds
    /\ stage' = "helper_sanitized"
    /\ UNCHANGED <<launchMode, hazmatFds, sudoFds, brokerFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive, tokenMinted>>

HelperOpensPolicyFile ==
    /\ stage = "helper_sanitized"
    /\ helperFds' = helperFds \cup {PolicyFD}
    /\ fdTarget' = [fdTarget EXCEPT ![PolicyFD] = "policy"]
    /\ fdOrigin' = [fdOrigin EXCEPT ![PolicyFD] = "helper"]
    /\ fdCloexec' = [fdCloexec EXCEPT ![PolicyFD] = PolicyFileUsesCloexec]
    /\ stage' = "policy_opened"
    /\ UNCHANGED <<launchMode, hazmatFds, sudoFds, brokerFds, agentFds,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive, tokenMinted>>

HelperPreparesSessionTempDir ==
    /\ stage = "policy_opened"
    \* The helper may create the already-policy-approved session temp directory
    \* before sandbox_init(), but this phase must not leave additional live fds.
    /\ stage' = "temp_prepared"
    /\ UNCHANGED <<launchMode, hazmatFds, sudoFds, brokerFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive, tokenMinted>>

HelperCallsSandboxInit ==
    /\ stage = "temp_prepared"
    /\ stage' = "sandboxed"
    /\ UNCHANGED <<launchMode, hazmatFds, sudoFds, brokerFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive, tokenMinted>>

\* After sandbox_init() returns, hazmat-launch emits its confirmed-containment
\* metadata line. Only at this point is containment actually established (a
\* pre-sudo "prepared launch" is not confirmed containment).
HelperEmitsConfirmedContainmentMetadata ==
    /\ stage = "sandboxed"
    /\ ~metadataEmitted
    /\ metadataEmitted' = TRUE
    /\ UNCHANGED <<stage, launchMode, hazmatFds, sudoFds, brokerFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, brokerActive, tokenMinted>>

\* The dr-owned host broker may activate only after confirmed containment.
HostBrokerActivates ==
    /\ metadataEmitted
    /\ ~brokerActive
    /\ brokerActive' = TRUE
    /\ UNCHANGED <<stage, launchMode, hazmatFds, sudoFds, brokerFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, tokenMinted>>

\* The host broker mints attestation authority only after it is active.
HostMintsToken ==
    /\ brokerActive
    /\ ~tokenMinted
    /\ tokenMinted' = TRUE
    /\ UNCHANGED <<stage, launchMode, hazmatFds, sudoFds, brokerFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive>>

HelperExecsAgent ==
    /\ stage = "sandboxed"
    /\ agentFds' = {fd \in helperFds : ~fdCloexec[fd]}
    /\ stage' = "agent"
    /\ UNCHANGED <<launchMode, hazmatFds, sudoFds, brokerFds, helperFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   peerAuthenticated, metadataEmitted, brokerActive, tokenMinted>>

Done ==
    /\ stage = "agent"
    /\ UNCHANGED vars

Next ==
    \/ HazmatExecsSudo
    \/ HazmatStartsLaunchBroker
    \/ BrokerStartupSanitizesFDTable
    \/ BrokerAcceptsAuthenticatedLaunch
    \/ SudoExecsHelper
    \/ BrokerForksLaunchChild
    \/ HelperSanitizesFDTable
    \/ HelperOpensPolicyFile
    \/ HelperPreparesSessionTempDir
    \/ HelperCallsSandboxInit
    \/ HelperEmitsConfirmedContainmentMetadata
    \/ HostBrokerActivates
    \/ HostMintsToken
    \/ HelperExecsAgent
    \/ Done

Spec == Init /\ [][Next]_vars

SandboxReached == stage \in {"sandboxed", "agent"}

\* The helper must present sandbox_init() with a deliberately curated fd table:
\* stdio plus helper-opened policy state only.
HelperFDTableAllowlistedAtSandbox ==
    SandboxReached =>
        \A fd \in helperFds : fdTarget[fd] \in AllowedHelperTargetsAtSandbox

BrokerLaunchRequiresAuthenticatedPeer ==
    launchMode = "brokered" /\ stage \in {"helper", "helper_sanitized", "policy_opened", "temp_prepared", "sandboxed", "agent"} =>
        peerAuthenticated

\* The long-lived broker must not retain host-origin non-stdio descriptors once
\* it is listening or serving requests. Child cleanup protects sandbox_init(),
\* but broker startup cleanup keeps the unsandboxed steady-state service from
\* holding a leaked credential or authority fd indefinitely.
BrokerFDTableDropsHostInheritedFDs ==
    launchMode = "brokered" /\ stage \in {"broker_listening", "broker", "helper", "helper_sanitized", "policy_opened", "temp_prepared", "sandboxed", "agent"} =>
        \A fd \in brokerFds :
            \/ fd \in StdioFDs
            \/ fdOrigin[fd] /= "shell"

\* No shell-origin fd >= 3 may survive to the helper once sandboxing starts.
NoInheritedShellFDsAtSandbox ==
    SandboxReached =>
        \A fd \in helperFds :
            \/ fd \in StdioFDs
            \/ fdOrigin[fd] /= "shell"

\* Credential-bearing descriptors must be gone before sandbox_init(), because
\* path-based denies do not revoke already-open handles.
CredentialFDsGoneBeforeSandbox ==
    SandboxReached =>
        \A fd \in helperFds : fdTarget[fd] /= "credential"

\* The final exec'd agent may keep only stdio. Any helper-opened policy fd must
\* be CLOEXEC so it is dropped by the exec chain.
AgentFDTableAllowlisted ==
    stage = "agent" =>
        \A fd \in agentFds : fdTarget[fd] \in AllowedAgentTargets

StdioSurvivesToAgent ==
    stage = "agent" => StdioFDs \subseteq agentFds

\* ═══════════════════════════════════════════════════════════════════════════════
\* Beadpost attestation boundary — confirmed-ordering and authority-fd hygiene
\* ═══════════════════════════════════════════════════════════════════════════════

\* The host broker becomes active only after containment is confirmed: the helper
\* must have reached sandbox_init() (stage sandboxed/agent) and emitted the
\* confirmed-containment metadata. A prepared-but-unconfirmed launch never
\* activates the broker.
BrokerStartsOnlyAfterSandboxConfirmed ==
    brokerActive => (stage \in {"sandboxed", "agent"} /\ metadataEmitted)

\* Attestation minting follows broker activation, which follows confirmation.
\* (Distinct from BrokerStartsOnlyAfterSandboxConfirmed: this gates the mint, not
\* the broker, so the chain confirm -> broker -> mint is proved end to end.)
TokenMintedOnlyAfterSandboxConfirmed ==
    tokenMinted => (brokerActive /\ metadataEmitted)

\* No fd carrying host authority material (a leaked signing-key fd) survives to
\* the final agent exec. Non-vacuous: fd 4 may be inherited as "authority", and
\* the helper's closefrom must remove it before sandbox_init().
AgentFDTableDoesNotCarryAuthority ==
    stage = "agent" =>
        \A fd \in agentFds : fdTarget[fd] /= "authority"

=============================================================================

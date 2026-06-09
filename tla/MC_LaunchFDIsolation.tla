---- MODULE MC_LaunchFDIsolation ----
\* Launch-time file descriptor isolation for the Tier 2 native helper path.
\*
\* This spec models Hazmat's host-side launch chain:
\*   hazmat (invoker uid) -> sudo -> hazmat-launch -> sandbox_init() -> exec agent
\*
\* The key threat is an already-open descriptor inherited from the invoker's
\* process tree. Seatbelt path denies do not revoke access granted by an
\* inherited live descriptor, so the helper must sanitize its fd table before
\* calling sandbox_init().
\*
\* The model treats two upstream behaviors as adversarial environment knobs:
\*   1. Go's exec path may or may not collapse hazmat -> sudo to stdio only.
\*   2. sudo may or may not apply closefrom-style cleanup before execing the helper.
\*
\* The proved design obligations are:
\*   - hazmat-launch closes every inherited fd >= 3 before sandbox_init()
\*   - any fd the helper opens itself for policy validation is CLOEXEC
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
\*   hazmat/agent_launch.go — fixed-script native launch wrapper
\*   hazmat/session.go — runAgentSeatbeltScriptWithUI()
\*   hazmat/cmd/hazmat-launch/main.go — helper-side fd sanitization, policy read, sandbox_init, exec

EXTENDS Naturals, FiniteSets

CONSTANTS
    HelperClosesInheritedFDs,
    PolicyFileUsesCloexec

FDs == 0..5
StdioFDs == 0..2
InheritedExtraFDs == {3, 4}
PolicyFD == 5

Targets == {"stdio", "credential", "benign", "policy", "authority", "unused"}
Origins == {"shell", "helper", "none"}
Stages == {"hazmat", "sudo", "helper", "helper_sanitized", "policy_opened", "sandboxed", "agent"}

AllowedHelperTargetsAtSandbox == {"stdio", "policy"}
AllowedAgentTargets == {"stdio"}

VARIABLES
    stage,
    hazmatFds,
    sudoFds,
    helperFds,
    agentFds,
    fdTarget,
    fdOrigin,
    fdCloexec,
    goExecClosesParentFDs,
    sudoClosesInheritedFDs,
    metadataEmitted,
    brokerActive,
    tokenMinted

vars ==
    <<stage, hazmatFds, sudoFds, helperFds, agentFds,
      fdTarget, fdOrigin, fdCloexec,
      goExecClosesParentFDs, sudoClosesInheritedFDs,
      metadataEmitted, brokerActive, tokenMinted>>

TypeOK ==
    /\ stage \in Stages
    /\ hazmatFds \subseteq FDs
    /\ sudoFds \subseteq FDs
    /\ helperFds \subseteq FDs
    /\ agentFds \subseteq FDs
    /\ fdTarget \in [FDs -> Targets]
    /\ fdOrigin \in [FDs -> Origins]
    /\ fdCloexec \in [FDs -> BOOLEAN]
    /\ goExecClosesParentFDs \in BOOLEAN
    /\ sudoClosesInheritedFDs \in BOOLEAN
    /\ metadataEmitted \in BOOLEAN
    /\ brokerActive \in BOOLEAN
    /\ tokenMinted \in BOOLEAN

Init ==
    /\ \E inherited \in SUBSET InheritedExtraFDs :
        hazmatFds = StdioFDs \cup inherited
    /\ sudoFds = {}
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
                  [] OTHER -> "unused"]
    /\ fdOrigin =
        [fd \in FDs |->
            IF fd \in StdioFDs \cup InheritedExtraFDs
                THEN "shell"
                ELSE "none"]
    /\ fdCloexec = [fd \in FDs |-> FALSE]
    /\ goExecClosesParentFDs \in BOOLEAN
    /\ sudoClosesInheritedFDs \in BOOLEAN
    /\ metadataEmitted = FALSE
    /\ brokerActive = FALSE
    /\ tokenMinted = FALSE
    /\ stage = "hazmat"

HazmatExecsSudo ==
    /\ stage = "hazmat"
    /\ sudoFds' =
        IF goExecClosesParentFDs
            THEN StdioFDs
            ELSE hazmatFds
    /\ stage' = "sudo"
    /\ UNCHANGED <<hazmatFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   metadataEmitted, brokerActive, tokenMinted>>

SudoExecsHelper ==
    /\ stage = "sudo"
    /\ helperFds' =
        IF sudoClosesInheritedFDs
            THEN sudoFds \cap StdioFDs
            ELSE sudoFds
    /\ stage' = "helper"
    /\ UNCHANGED <<hazmatFds, sudoFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   metadataEmitted, brokerActive, tokenMinted>>

HelperSanitizesFDTable ==
    /\ stage = "helper"
    /\ helperFds' =
        IF HelperClosesInheritedFDs
            THEN helperFds \cap StdioFDs
            ELSE helperFds
    /\ stage' = "helper_sanitized"
    /\ UNCHANGED <<hazmatFds, sudoFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   metadataEmitted, brokerActive, tokenMinted>>

HelperOpensPolicyFile ==
    /\ stage = "helper_sanitized"
    /\ helperFds' = helperFds \cup {PolicyFD}
    /\ fdTarget' = [fdTarget EXCEPT ![PolicyFD] = "policy"]
    /\ fdOrigin' = [fdOrigin EXCEPT ![PolicyFD] = "helper"]
    /\ fdCloexec' = [fdCloexec EXCEPT ![PolicyFD] = PolicyFileUsesCloexec]
    /\ stage' = "policy_opened"
    /\ UNCHANGED <<hazmatFds, sudoFds, agentFds,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   metadataEmitted, brokerActive, tokenMinted>>

HelperCallsSandboxInit ==
    /\ stage = "policy_opened"
    /\ stage' = "sandboxed"
    /\ UNCHANGED <<hazmatFds, sudoFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   metadataEmitted, brokerActive, tokenMinted>>

\* After sandbox_init() returns, hazmat-launch emits its confirmed-containment
\* metadata line. Only at this point is containment actually established (a
\* pre-sudo "prepared launch" is not confirmed containment).
HelperEmitsConfirmedContainmentMetadata ==
    /\ stage = "sandboxed"
    /\ ~metadataEmitted
    /\ metadataEmitted' = TRUE
    /\ UNCHANGED <<stage, hazmatFds, sudoFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   brokerActive, tokenMinted>>

\* The dr-owned host broker may activate only after confirmed containment.
HostBrokerActivates ==
    /\ metadataEmitted
    /\ ~brokerActive
    /\ brokerActive' = TRUE
    /\ UNCHANGED <<stage, hazmatFds, sudoFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   metadataEmitted, tokenMinted>>

\* The host broker mints attestation authority only after it is active.
HostMintsToken ==
    /\ brokerActive
    /\ ~tokenMinted
    /\ tokenMinted' = TRUE
    /\ UNCHANGED <<stage, hazmatFds, sudoFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   metadataEmitted, brokerActive>>

HelperExecsAgent ==
    /\ stage = "sandboxed"
    /\ agentFds' = {fd \in helperFds : ~fdCloexec[fd]}
    /\ stage' = "agent"
    /\ UNCHANGED <<hazmatFds, sudoFds, helperFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs,
                   metadataEmitted, brokerActive, tokenMinted>>

Done ==
    /\ stage = "agent"
    /\ UNCHANGED vars

Next ==
    \/ HazmatExecsSudo
    \/ SudoExecsHelper
    \/ HelperSanitizesFDTable
    \/ HelperOpensPolicyFile
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

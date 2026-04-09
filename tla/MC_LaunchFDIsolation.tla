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
\* Governed code:
\*   hazmat/agent_launch.go — native sudo + helper launch path
\*   hazmat/session.go — runAgentSeatbeltScriptWithUI(), policy-file generation
\*   hazmat/cmd/hazmat-launch/main.go — helper-side fd sanitization, policy read, sandbox_init, exec

EXTENDS Naturals, FiniteSets

CONSTANTS
    HelperClosesInheritedFDs,
    PolicyFileUsesCloexec

FDs == 0..5
InheritedExtraFDs == {3, 4}
PolicyFD == 5

Targets == {"stdio", "credential", "benign", "policy", "unused"}
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
    sudoClosesInheritedFDs

vars ==
    <<stage, hazmatFds, sudoFds, helperFds, agentFds,
      fdTarget, fdOrigin, fdCloexec,
      goExecClosesParentFDs, sudoClosesInheritedFDs>>

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

Init ==
    /\ \E inherited \in SUBSET InheritedExtraFDs :
        hazmatFds = {0, 1, 2} \cup inherited
    /\ sudoFds = {}
    /\ helperFds = {}
    /\ agentFds = {}
    /\ fdTarget =
        [fd \in FDs |->
            CASE fd \in {0, 1, 2} -> "stdio"
              [] fd = 3 -> "credential"
              [] fd = 4 -> "benign"
              [] OTHER -> "unused"]
    /\ fdOrigin =
        [fd \in FDs |->
            IF fd \in {0, 1, 2} \cup InheritedExtraFDs
                THEN "shell"
                ELSE "none"]
    /\ fdCloexec = [fd \in FDs |-> FALSE]
    /\ goExecClosesParentFDs \in BOOLEAN
    /\ sudoClosesInheritedFDs \in BOOLEAN
    /\ stage = "hazmat"

HazmatExecsSudo ==
    /\ stage = "hazmat"
    /\ sudoFds' =
        IF goExecClosesParentFDs
            THEN {0, 1, 2}
            ELSE hazmatFds
    /\ stage' = "sudo"
    /\ UNCHANGED <<hazmatFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs>>

SudoExecsHelper ==
    /\ stage = "sudo"
    /\ helperFds' =
        IF sudoClosesInheritedFDs
            THEN {fd \in sudoFds : fd < 3}
            ELSE sudoFds
    /\ stage' = "helper"
    /\ UNCHANGED <<hazmatFds, sudoFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs>>

HelperSanitizesFDTable ==
    /\ stage = "helper"
    /\ helperFds' =
        IF HelperClosesInheritedFDs
            THEN {fd \in helperFds : fd < 3}
            ELSE helperFds
    /\ stage' = "helper_sanitized"
    /\ UNCHANGED <<hazmatFds, sudoFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs>>

HelperOpensPolicyFile ==
    /\ stage = "helper_sanitized"
    /\ helperFds' = helperFds \cup {PolicyFD}
    /\ fdTarget' = [fdTarget EXCEPT ![PolicyFD] = "policy"]
    /\ fdOrigin' = [fdOrigin EXCEPT ![PolicyFD] = "helper"]
    /\ fdCloexec' = [fdCloexec EXCEPT ![PolicyFD] = PolicyFileUsesCloexec]
    /\ stage' = "policy_opened"
    /\ UNCHANGED <<hazmatFds, sudoFds, agentFds,
                   goExecClosesParentFDs, sudoClosesInheritedFDs>>

HelperCallsSandboxInit ==
    /\ stage = "policy_opened"
    /\ stage' = "sandboxed"
    /\ UNCHANGED <<hazmatFds, sudoFds, helperFds, agentFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs>>

HelperExecsAgent ==
    /\ stage = "sandboxed"
    /\ agentFds' = {fd \in helperFds : ~fdCloexec[fd]}
    /\ stage' = "agent"
    /\ UNCHANGED <<hazmatFds, sudoFds, helperFds,
                   fdTarget, fdOrigin, fdCloexec,
                   goExecClosesParentFDs, sudoClosesInheritedFDs>>

Done ==
    /\ stage = "agent"
    /\ UNCHANGED vars

Next ==
    \/ HazmatExecsSudo
    \/ SudoExecsHelper
    \/ HelperSanitizesFDTable
    \/ HelperOpensPolicyFile
    \/ HelperCallsSandboxInit
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
            \/ fd < 3
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
    stage = "agent" => {0, 1, 2} \subseteq agentFds

=============================================================================

---- MODULE MC_BeadpostBrokerBoundary ----
\* Beadpost attestation-boundary broker membrane.
\*
\* Model: contained-agent submitter + dr-owned host broker.
\* A contained agent submits CLOSED request payloads (request CONTENT plus an
\* unguessable session capability, never authority fields) to a per-session,
\* dr-owned broker socket. The broker is
\* created only after a session's containment is confirmed (modeled abstractly as
\* the "confirmed" transition; the launch-time sandbox_init ordering itself is
\* proved in MC_LaunchFDIsolation). The broker DERIVES project/session/tier
\* authority from host launch facts and NEVER trusts agent-supplied authority —
\* indeed the agent has no authority field to supply. The broker invokes delivery
\* only for an accepted request, and a closed session leaves no residual authority.
\*
\* This spec proves the MEMBRANE only. Beadpost internals (registry, ledger,
\* bead mutation, real delivery side effects) are deliberately abstract.
\*
\* Style mirrors MC_GitSSHRouting: finite domains, validation/confirmation before
\* readiness, deterministic per-session socket binding, no cross-session confusion.
\*
\* Governed code (implemented, behind //go:build beadpost_hostbroker):
\*   hazmat/hostbroker/session.go — Open(), confirmSandboxBoundary(),
\*     allocateBrokerSocket(), deriveAuthorityFromLaunchFacts(), invokeDelivery(), Close()

EXTENDS Naturals, FiniteSets

CONSTANTS
    Sessions,        \* finite set of session identifiers
    Projects,        \* finite set of project identifiers
    Tiers,           \* finite set of containment tier labels
    BrokerSockets,   \* finite set of host-owned per-session broker socket ids
    Capabilities,    \* finite set of unguessable, per-session capability ids
    RequestContent,  \* finite set of opaque request-content payloads (NO authority)
    NoSocket,        \* sentinel: no socket bound
    NoAuthority,     \* sentinel: no authority stamped
    NoContent        \* sentinel: no request submitted

\* Host-derived authority is a (project, tier) pair. The agent never names it.
Authority == Projects \X Tiers

ASSUME NoSocket \notin BrokerSockets
ASSUME NoContent \notin RequestContent
ASSUME NoAuthority \notin Authority
ASSUME Cardinality(Capabilities) >= Cardinality(Sessions)

VARIABLES
    launchFacts,        \* Sessions -> Authority — host-set at confirmation time, write-once
    genesisFacts,       \* snapshot of launchFacts at Init, never changed (write-once witness)
    sessionState,       \* Sessions -> {prepared, confirmed, accepted, closed}
    confirmedSessions,  \* SUBSET Sessions — sessions whose containment was confirmed
    activeSessions,     \* SUBSET Sessions — confirmed and not yet closed
    brokerSocket,       \* Sessions -> BrokerSockets \cup {NoSocket}
    sessionCapability,  \* Sessions -> Capabilities, unique host-minted binding
    submittedCapability, \* Sessions -> Capabilities, untrusted peer presentation
    agentContent,       \* Sessions -> RequestContent \cup {NoContent} — agent-supplied CONTENT only
    deliveredAuthority, \* Sessions -> Authority \cup {NoAuthority} — what the broker stamped
    requestAccepted     \* Sessions -> BOOLEAN

vars ==
    <<launchFacts, genesisFacts, sessionState, confirmedSessions, activeSessions,
      brokerSocket, sessionCapability, submittedCapability, agentContent,
      deliveredAuthority, requestAccepted>>

TypeOK ==
    /\ launchFacts        \in [Sessions -> Authority]
    /\ genesisFacts       \in [Sessions -> Authority]
    /\ sessionState       \in [Sessions -> {"prepared", "confirmed", "accepted", "closed"}]
    /\ confirmedSessions  \subseteq Sessions
    /\ activeSessions     \subseteq Sessions
    /\ brokerSocket       \in [Sessions -> BrokerSockets \cup {NoSocket}]
    /\ sessionCapability  \in [Sessions -> Capabilities]
    /\ submittedCapability \in [Sessions -> Capabilities]
    /\ agentContent       \in [Sessions -> RequestContent \cup {NoContent}]
    /\ deliveredAuthority \in [Sessions -> Authority \cup {NoAuthority}]
    /\ requestAccepted    \in [Sessions -> BOOLEAN]

Init ==
    /\ launchFacts        \in [Sessions -> Authority]
    /\ genesisFacts       = launchFacts
    /\ sessionState       = [s \in Sessions |-> "prepared"]
    /\ confirmedSessions  = {}
    /\ activeSessions     = {}
    /\ brokerSocket       = [s \in Sessions |-> NoSocket]
    /\ sessionCapability  \in [Sessions -> Capabilities]
    /\ \A s1, s2 \in Sessions :
        s1 /= s2 => sessionCapability[s1] /= sessionCapability[s2]
    /\ submittedCapability \in [Sessions -> Capabilities]
    /\ agentContent       = [s \in Sessions |-> NoContent]
    /\ deliveredAuthority = [s \in Sessions |-> NoAuthority]
    /\ requestAccepted    = [s \in Sessions |-> FALSE]

\* Containment confirmed: the host opens a per-session broker socket. The socket
\* is chosen deterministically-unique (not held by any other session), so two
\* sessions never share a socket. (The launch-time confirmation ordering itself
\* — sandbox_init then metadata — is proved in MC_LaunchFDIsolation.)
SessionConfirmSandboxed(s) ==
    /\ sessionState[s] = "prepared"
    /\ \E sock \in BrokerSockets :
        /\ \A o \in Sessions : brokerSocket[o] /= sock
        /\ brokerSocket' = [brokerSocket EXCEPT ![s] = sock]
    /\ sessionState'      = [sessionState EXCEPT ![s] = "confirmed"]
    /\ confirmedSessions' = confirmedSessions \cup {s}
    /\ activeSessions'    = activeSessions \cup {s}
    /\ UNCHANGED <<launchFacts, genesisFacts, sessionCapability,
                   submittedCapability, agentContent, deliveredAuthority, requestAccepted>>

\* A socket peer submits a CLOSED request payload and presents a capability.
\* There is no authority field for the peer to set, and a peer that merely knows
\* the socket path may choose the wrong capability modeled here.
AgentSubmitsRequest(s) ==
    /\ s \in confirmedSessions
    /\ sessionState[s] = "confirmed"
    /\ brokerSocket[s] /= NoSocket
    /\ agentContent[s] = NoContent
    /\ \E c \in RequestContent : agentContent' = [agentContent EXCEPT ![s] = c]
    /\ \E cap \in Capabilities :
        submittedCapability' = [submittedCapability EXCEPT ![s] = cap]
    /\ UNCHANGED <<launchFacts, genesisFacts, sessionState, confirmedSessions,
                   activeSessions, brokerSocket, sessionCapability,
                   deliveredAuthority, requestAccepted>>

\* The broker DERIVES authority from host launch facts (never from agentContent)
\* and accepts the request only after the session capability matches. There is
\* no agent-driven authority validation path because the agent supplied no
\* authority to validate.
BrokerDeriveAndAccept(s) ==
    /\ s \in confirmedSessions
    /\ sessionState[s] = "confirmed"
    /\ agentContent[s] /= NoContent
    /\ submittedCapability[s] = sessionCapability[s]
    /\ ~requestAccepted[s]
    /\ deliveredAuthority' = [deliveredAuthority EXCEPT ![s] = launchFacts[s]]
    /\ requestAccepted'    = [requestAccepted EXCEPT ![s] = TRUE]
    /\ UNCHANGED <<launchFacts, genesisFacts, sessionState, confirmedSessions,
                   activeSessions, brokerSocket, sessionCapability,
                   submittedCapability, agentContent>>

\* Delivery happens only for an accepted request.
BrokerInvokeDelivery(s) ==
    /\ requestAccepted[s]
    /\ sessionState[s] = "confirmed"
    /\ sessionState' = [sessionState EXCEPT ![s] = "accepted"]
    /\ UNCHANGED <<launchFacts, genesisFacts, confirmedSessions, activeSessions,
                   brokerSocket, sessionCapability, submittedCapability,
                   agentContent, deliveredAuthority, requestAccepted>>

\* Session close releases the socket and clears all residual request authority.
SessionClose(s) ==
    /\ s \in activeSessions
    /\ sessionState'      = [sessionState EXCEPT ![s] = "closed"]
    /\ activeSessions'    = activeSessions \ {s}
    /\ brokerSocket'      = [brokerSocket EXCEPT ![s] = NoSocket]
    /\ agentContent'      = [agentContent EXCEPT ![s] = NoContent]
    /\ deliveredAuthority' = [deliveredAuthority EXCEPT ![s] = NoAuthority]
    /\ requestAccepted'   = [requestAccepted EXCEPT ![s] = FALSE]
    /\ UNCHANGED <<launchFacts, genesisFacts, confirmedSessions,
                   sessionCapability, submittedCapability>>

\* Terminal stutter when every session has closed.
Done ==
    /\ \A s \in Sessions : sessionState[s] = "closed"
    /\ UNCHANGED vars

Next ==
    \/ \E s \in Sessions : SessionConfirmSandboxed(s)
    \/ \E s \in Sessions : AgentSubmitsRequest(s)
    \/ \E s \in Sessions : BrokerDeriveAndAccept(s)
    \/ \E s \in Sessions : BrokerInvokeDelivery(s)
    \/ \E s \in Sessions : SessionClose(s)
    \/ Done

Spec == Init /\ [][Next]_vars

\* ═══════════════════════════════════════════════════════════════════════════════
\* Membrane invariants
\* ═══════════════════════════════════════════════════════════════════════════════

\* A broker socket exists only for a session whose containment was confirmed.
BrokerSocketOnlyAfterConfirmedSession ==
    \A s \in Sessions : brokerSocket[s] /= NoSocket => s \in confirmedSessions

\* A request can only be accepted for a confirmed session.
AcceptedRequestHasConfirmedSession ==
    \A s \in Sessions : requestAccepted[s] => s \in confirmedSessions

\* Possession of a socket path is insufficient: acceptance requires the
\* unguessable capability minted for that exact session.
AcceptedRequestHasSessionCapability ==
    \A s \in Sessions :
        requestAccepted[s] => submittedCapability[s] = sessionCapability[s]

\* Any stamped authority is exactly the host launch facts — the agent cannot
\* supply or influence authority (it stamps launchFacts unconditionally).
AgentCannotSupplyAuthorityFields ==
    \A s \in Sessions :
        deliveredAuthority[s] /= NoAuthority => deliveredAuthority[s] = launchFacts[s]

\* An accepted request's authority equals the host launch facts.
AcceptedAuthorityEqualsLaunchFacts ==
    \A s \in Sessions :
        requestAccepted[s] => deliveredAuthority[s] = launchFacts[s]

\* No two sessions ever share a broker socket (deterministic per-session binding).
NoCrossSessionRequest ==
    \A s1, s2 \in Sessions :
        (s1 /= s2 /\ brokerSocket[s1] /= NoSocket /\ brokerSocket[s2] /= NoSocket)
            => brokerSocket[s1] /= brokerSocket[s2]

\* A closed session retains no socket, content, authority, or acceptance.
NoRequestAfterSessionClose ==
    \A s \in Sessions :
        sessionState[s] = "closed" =>
            /\ brokerSocket[s] = NoSocket
            /\ agentContent[s] = NoContent
            /\ deliveredAuthority[s] = NoAuthority
            /\ requestAccepted[s] = FALSE

\* Host authority is write-once: no action (least of all an agent action) mutates
\* the launch facts. The genesis snapshot witnesses immutability.
HostAuthorityNeverAgentReadable ==
    launchFacts = genesisFacts

\* Delivery (the transition to "accepted") only follows an accepted request.
DeliveryOnlyFromAcceptedRequest ==
    \A s \in Sessions :
        sessionState[s] = "accepted" => requestAccepted[s]

====

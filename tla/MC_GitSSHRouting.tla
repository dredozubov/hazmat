---- MODULE MC_GitSSHRouting ----
\* Multi-key Git-SSH routing — verifies that every destination host maps to
\* exactly one configured SSH key (bound to a unique identity-agent socket)
\* or is rejected, and that an ambiguous or socket-colliding config cannot
\* reach a session.
\*
\* Abstraction:
\*   A project SSH config is a finite set of named keys. Each key carries:
\*     - a declared host set (after glob expansion, before normalization)
\*     - an identity-agent socket it binds to at runtime
\*   A project may be in "legacy single-key" mode: exactly one present key
\*   with an empty declared host set, which normalizes to "any host." With
\*   two or more present keys, any empty declared host set is an error.
\*
\*   The wrapper lookup on host h returns:
\*     - reject                           — no configured key matches h
\*     - [name, socket]                   — exactly one configured key matches
\*   The lookup must never return two candidates, and the returned socket
\*   must be the unique socket bound to the selected key.
\*
\* Governed code (planned):
\*   hazmat/config.go  — config-set-time overlap + socket-collision checks,
\*                       legacy single-key normalization.
\*   hazmat/git_ssh.go — wrapper socket selection, host allowlist enforcement,
\*                       IdentityAgent emission.
\*
\* Scope boundary:
\*   This spec models the routing relation after glob expansion and the
\*   socket-to-key binding. It does not model glob syntax, shell quoting,
\*   signal handling, or ssh-agent liveness. Those are governed by unit
\*   tests against the wrapper script and runtime code.

EXTENDS Naturals, FiniteSets

\* ═══════════════════════════════════════════════════════════════════════════════
\* Constants
\* ═══════════════════════════════════════════════════════════════════════════════

CONSTANTS
    Hosts,      \* finite set of candidate destination hosts
    KeyNames,   \* finite set of candidate key identifiers
    Sockets     \* finite set of identity-agent socket identifiers

\* ═══════════════════════════════════════════════════════════════════════════════
\* Variables
\* ═══════════════════════════════════════════════════════════════════════════════

VARIABLES
    assignment,   \* KeyNames -> SUBSET Hosts  (declared hosts per key)
    socket,       \* KeyNames -> Sockets       (identity-agent socket binding)
    present,      \* SUBSET KeyNames           (keys actually configured)
    effective,    \* KeyNames -> SUBSET Hosts  (hosts after normalization)
    configValid,  \* BOOLEAN                   (passed config-set validation)
    phase         \* "init" | "ready" | "rejected"

vars == <<assignment, socket, present, effective, configValid, phase>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Legacy fallback — exactly one present key with empty declared hosts
\* normalizes to "any host." Two or more present keys with any empty
\* declared set is an error.
\* ═══════════════════════════════════════════════════════════════════════════════

LegacySingle ==
    /\ Cardinality(present) = 1
    /\ \E k \in present : assignment[k] = {}

LegacyMultiInvalid ==
    /\ Cardinality(present) >= 2
    /\ \E k \in present : assignment[k] = {}

Normalize(k) ==
    IF LegacySingle /\ k \in present /\ assignment[k] = {}
    THEN Hosts
    ELSE assignment[k]

\* ═══════════════════════════════════════════════════════════════════════════════
\* Init — nondeterministic choice of config
\* ═══════════════════════════════════════════════════════════════════════════════

Init ==
    /\ assignment  \in [KeyNames -> SUBSET Hosts]
    /\ socket      \in [KeyNames -> Sockets]
    /\ present     \in (SUBSET KeyNames) \ {{}}   \* empty configs are out of scope
    /\ effective   = [k \in KeyNames |-> {}]
    /\ configValid = FALSE
    /\ phase       = "init"

\* ═══════════════════════════════════════════════════════════════════════════════
\* Validation — checks performed before a config is allowed to reach "ready."
\*   1. Legacy multi-key ambiguity        — owned by config.go at config-set time
\*   2. Effective host-set overlap        — owned by config.go at config-set time
\*   3. Identity-agent socket collision   — owned by git_ssh.go during session
\*                                          preparation; sockets are runtime
\*                                          artifacts allocated per session, not
\*                                          values stored in the config file.
\* The spec checks all three together because the union is what every wrapper
\* invocation actually relies on.
\* ═══════════════════════════════════════════════════════════════════════════════

Validate ==
    /\ phase = "init"
    /\ effective' = [k \in KeyNames |-> Normalize(k)]
    /\ LET eff == [k \in KeyNames |-> Normalize(k)]
           HasOverlap ==
               \E k1, k2 \in present :
                   k1 /= k2 /\ (eff[k1] \cap eff[k2]) /= {}
           SocketCollision ==
               \E k1, k2 \in present :
                   k1 /= k2 /\ socket[k1] = socket[k2]
       IN  IF LegacyMultiInvalid \/ HasOverlap \/ SocketCollision
           THEN /\ configValid' = FALSE
                /\ phase'       = "rejected"
           ELSE /\ configValid' = TRUE
                /\ phase'       = "ready"
    /\ UNCHANGED <<assignment, socket, present>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Routing — wrapper lookup on a destination host
\* ═══════════════════════════════════════════════════════════════════════════════

Matching(h) == {k \in present : h \in effective[k]}

\* Lookup is only defined once the config has been accepted. Callers assert
\* phase = "ready" before invoking the wrapper. When a unique key matches,
\* the returned record carries both the key name and the socket that the
\* wrapper must pass as IdentityAgent.
Lookup(h) ==
    IF Matching(h) = {}
    THEN [outcome |-> "reject", name |-> "none", socket |-> "none"]
    ELSE LET k == CHOOSE k \in Matching(h) : TRUE
         IN  [outcome |-> "select", name |-> k, socket |-> socket[k]]

Next ==
    \/ Validate
    \/ /\ phase \in {"ready", "rejected"}
       /\ UNCHANGED vars

Spec == Init /\ [][Next]_vars

\* ═══════════════════════════════════════════════════════════════════════════════
\* Type invariant
\* ═══════════════════════════════════════════════════════════════════════════════

TypeOK ==
    /\ assignment  \in [KeyNames -> SUBSET Hosts]
    /\ socket      \in [KeyNames -> Sockets]
    /\ present     \subseteq KeyNames
    /\ effective   \in [KeyNames -> SUBSET Hosts]
    /\ configValid \in BOOLEAN
    /\ phase       \in {"init", "ready", "rejected"}

\* ═══════════════════════════════════════════════════════════════════════════════
\* Safety invariants
\* ═══════════════════════════════════════════════════════════════════════════════

\* --- DeterministicRouting ---
\* A ready config never admits two keys for the same host.
DeterministicRouting ==
    phase = "ready" =>
        \A h \in Hosts : Cardinality(Matching(h)) <= 1

\* --- OverlapRejectedAtConfigTime ---
\* An overlapping effective host-set config is rejected before it reaches "ready."
OverlapRejectedAtConfigTime ==
    phase = "ready" =>
        \A k1, k2 \in present :
            k1 /= k2 => (effective[k1] \cap effective[k2]) = {}

\* --- HostsOutsideAllowlistRejected ---
\* When no configured key matches the host, the wrapper rejects.
HostsOutsideAllowlistRejected ==
    phase = "ready" =>
        \A h \in Hosts :
            (Matching(h) = {}) => (Lookup(h).outcome = "reject")

\* --- LegacyFallbackSingleOnly ---
\* The legacy any-host fallback is only safe with exactly one present key.
\* Any config with 2+ present keys where any key has an empty declared host
\* set must be rejected.
LegacyFallbackSingleOnly ==
    phase = "ready" =>
        ((\E k \in present : assignment[k] = {}) => Cardinality(present) = 1)

\* --- SocketsDistinctForPresent ---
\* No two present keys share an identity-agent socket. Config-set validation
\* must reject collisions.
SocketsDistinctForPresent ==
    phase = "ready" =>
        \A k1, k2 \in present :
            k1 /= k2 => socket[k1] /= socket[k2]

\* --- NoCrossKey ---
\* When exactly one key matches the host, the lookup's socket is the
\* binding of that key and no other present key shares it. This is the
\* routing-plus-binding half of the per-key socket contract.
NoCrossKey ==
    phase = "ready" =>
        \A h \in Hosts :
            Cardinality(Matching(h)) = 1 =>
                \A selected \in Matching(h) :
                    /\ Lookup(h).outcome = "select"
                    /\ Lookup(h).name    = selected
                    /\ Lookup(h).socket  = socket[selected]
                    /\ \A other \in present :
                         other /= selected => socket[other] /= Lookup(h).socket

====

---- MODULE MC_GitSSHRouting ----
\* Multi-key Git-SSH routing — verifies that every destination host maps to
\* exactly one configured SSH key (bound to a unique identity-agent socket)
\* or is rejected, and that an ambiguous, socket-colliding, or
\* dangling-profile config cannot reach a session.
\*
\* Abstraction:
\*   A project SSH config is a finite set of named keys. Each key carries:
\*     - a declared host set (after glob expansion, before normalization)
\*     - an identity-agent socket it binds to at runtime
\*     - an optional reference to a named profile (NoProfile when inline)
\*   A profile, when defined, contributes default host inheritance: if the
\*   project key declares no hosts of its own, the profile's default hosts
\*   apply. Profile references that point to undefined profiles are a
\*   config-load failure.
\*
\*   Inline keys must declare at least one host. The legacy single-key
\*   any-host fallback has been retired. A key that references a profile may
\*   omit declared hosts and inherit the profile's default hosts instead
\*   (which may itself be empty, leaving the key unrouted).
\*
\*   The wrapper lookup on host h returns:
\*     - reject                           — no configured key matches h
\*     - [name, socket]                   — exactly one configured key matches
\*   The lookup must never return two candidates, and the returned socket
\*   must be the unique socket bound to the selected key.
\*
\* Governed code:
\*   hazmat/config.go  — config-set-time overlap check, inline-empty-host
\*                       rejection, profile/inline mutual-exclusion check,
\*                       dangling-profile rejection, profile default-host
\*                       inheritance.
\*   hazmat/git_ssh.go — wrapper socket selection, host allowlist
\*                       enforcement, IdentityAgent emission, session-time
\*                       socket-collision check.
\*
\* Scope boundary:
\*   This spec models the routing relation after glob expansion and the
\*   socket-to-key binding, including the profile resolution layer above
\*   that. It does not model glob syntax, shell quoting, signal handling,
\*   ssh-agent liveness, or profile rename / removal cascade semantics.
\*   Those are governed by unit tests against the wrapper script and the
\*   CLI surface in config.go.

EXTENDS Naturals, FiniteSets

\* ═══════════════════════════════════════════════════════════════════════════════
\* Constants
\* ═══════════════════════════════════════════════════════════════════════════════

CONSTANTS
    Hosts,        \* finite set of candidate destination hosts
    KeyNames,     \* finite set of candidate key identifiers
    Sockets,      \* finite set of identity-agent socket identifiers
    ProfileNames, \* finite set of candidate profile identifiers
    NoProfile     \* sentinel meaning "no profile reference" (inline key)

ASSUME NoProfile \notin ProfileNames

\* ═══════════════════════════════════════════════════════════════════════════════
\* Variables
\* ═══════════════════════════════════════════════════════════════════════════════

VARIABLES
    assignment,            \* KeyNames -> SUBSET Hosts (declared per-key hosts)
    socket,                \* KeyNames -> Sockets (per-key socket binding)
    present,               \* SUBSET KeyNames (keys actually configured)
    keyProfile,            \* KeyNames -> ProfileNames \cup {NoProfile}
    inlineMaterial,        \* KeyNames -> BOOLEAN (key declares 'private_key:' or 'key:')
    profileDefaultHosts,   \* ProfileNames -> SUBSET Hosts
    definedProfiles,       \* SUBSET ProfileNames (profiles in ssh_profiles:)
    effective,             \* KeyNames -> SUBSET Hosts (after normalization)
    configValid,           \* BOOLEAN (passed config-set validation)
    phase                  \* "init" | "ready" | "rejected"

vars == <<assignment, socket, present, keyProfile, inlineMaterial,
          profileDefaultHosts, definedProfiles, effective, configValid, phase>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Key classification
\*   InlineKey(k)                — k is a present key with no profile reference
\*   ProfileKey(k)               — k is a present key referencing a defined profile
\*   HasProfileInlineConflict(k) — k declares both profile and inline material
\*   IsOrphanKey(k)              — k has no identity source
\*   InlineKeyEmptyHosts(k)      — k is inline and declares no hosts (now illegal)
\*
\* The legacy any-host fallback that previously admitted a single inline key
\* with empty declared hosts has been retired — every inline key must declare
\* at least one host. Profile-referencing keys still inherit `default_hosts`
\* from the profile when their own declared host list is empty.
\* ═══════════════════════════════════════════════════════════════════════════════

InlineKey(k) ==
    /\ k \in present
    /\ keyProfile[k] = NoProfile

ProfileKey(k) ==
    /\ k \in present
    /\ keyProfile[k] \in definedProfiles

\* A key that declares both a profile reference AND inline identity
\* material (private_key: / key:) is a schema-level conflict. The Go
\* validator must reject it at config-load time.
HasProfileInlineConflict(k) ==
    /\ k \in present
    /\ keyProfile[k] /= NoProfile
    /\ inlineMaterial[k]

\* A present key with no profile reference AND no inline material has no
\* identity source at all. The Go validator must reject it.
IsOrphanKey(k) ==
    /\ k \in present
    /\ keyProfile[k] = NoProfile
    /\ ~inlineMaterial[k]

\* An inline key that declares no hosts would previously have been expanded
\* to the any-host fallback. After force-migration (sandboxing-qq9b) this is
\* a config-load failure.
InlineKeyEmptyHosts(k) ==
    /\ InlineKey(k)
    /\ inlineMaterial[k]
    /\ assignment[k] = {}

\* Normalize(k) computes the effective host set for key k after profile
\* inheritance. A key that references a profile inherits the profile's
\* default hosts when its own host list is empty; otherwise the declared
\* hosts apply as-is. No legacy any-host expansion.
Normalize(k) ==
    IF k \in present /\ keyProfile[k] \in definedProfiles /\ assignment[k] = {}
    THEN profileDefaultHosts[keyProfile[k]]
    ELSE assignment[k]

\* ═══════════════════════════════════════════════════════════════════════════════
\* Init — nondeterministic choice of config
\* ═══════════════════════════════════════════════════════════════════════════════

Init ==
    /\ assignment          \in [KeyNames -> SUBSET Hosts]
    /\ socket              \in [KeyNames -> Sockets]
    /\ present             \in (SUBSET KeyNames) \ {{}}
    /\ keyProfile          \in [KeyNames -> ProfileNames \cup {NoProfile}]
    /\ inlineMaterial      \in [KeyNames -> BOOLEAN]
    /\ profileDefaultHosts \in [ProfileNames -> SUBSET Hosts]
    /\ definedProfiles     \in SUBSET ProfileNames
    /\ effective           = [k \in KeyNames |-> {}]
    /\ configValid         = FALSE
    /\ phase               = "init"

\* ═══════════════════════════════════════════════════════════════════════════════
\* Validation — checks performed before a config is allowed to reach "ready."
\*   1. Effective host-set overlap        — owned by config.go at config-set time
\*   2. Dangling profile reference        — owned by config.go at config-load time
\*   3. Profile + inline identity clash   — owned by config.go at config-load time
\*   4. Orphan key (no identity source)   — owned by config.go at config-load time
\*   5. Inline key with empty hosts       — owned by config.go at config-load time
\*                                          (legacy any-host fallback retired)
\*   6. Identity-agent socket collision   — owned by git_ssh.go during session
\*                                          preparation; sockets are runtime
\*                                          artifacts allocated per session, not
\*                                          values stored in the config file.
\* The spec checks all six together because the union is what every wrapper
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
           HasDanglingProfile ==
               \E k \in present :
                   keyProfile[k] /= NoProfile /\ keyProfile[k] \notin definedProfiles
           HasInlineConflict ==
               \E k \in present : HasProfileInlineConflict(k)
           HasOrphan ==
               \E k \in present : IsOrphanKey(k)
           HasInlineEmpty ==
               \E k \in present : InlineKeyEmptyHosts(k)
       IN  IF \/ HasOverlap
              \/ SocketCollision
              \/ HasDanglingProfile
              \/ HasInlineConflict
              \/ HasOrphan
              \/ HasInlineEmpty
           THEN /\ configValid' = FALSE
                /\ phase'       = "rejected"
           ELSE /\ configValid' = TRUE
                /\ phase'       = "ready"
    /\ UNCHANGED <<assignment, socket, present, keyProfile, inlineMaterial,
                   profileDefaultHosts, definedProfiles>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Routing — wrapper lookup on a destination host
\* ═══════════════════════════════════════════════════════════════════════════════

Matching(h) == {k \in present : h \in effective[k]}

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
    /\ assignment          \in [KeyNames -> SUBSET Hosts]
    /\ socket              \in [KeyNames -> Sockets]
    /\ present             \subseteq KeyNames
    /\ keyProfile          \in [KeyNames -> ProfileNames \cup {NoProfile}]
    /\ inlineMaterial      \in [KeyNames -> BOOLEAN]
    /\ profileDefaultHosts \in [ProfileNames -> SUBSET Hosts]
    /\ definedProfiles     \subseteq ProfileNames
    /\ effective           \in [KeyNames -> SUBSET Hosts]
    /\ configValid         \in BOOLEAN
    /\ phase               \in {"init", "ready", "rejected"}

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

\* --- InlineKeysHaveDeclaredHosts ---
\* After retiring the any-host fallback (sandboxing-qq9b), every inline key
\* must declare at least one host. An inline key with empty declared hosts
\* never reaches "ready." Profile-referencing keys are unaffected: they may
\* omit declared hosts and inherit the profile's default_hosts.
InlineKeysHaveDeclaredHosts ==
    phase = "ready" =>
        \A k \in present :
            InlineKey(k) => assignment[k] /= {}

\* --- SocketsDistinctForPresent ---
\* No two present keys share an identity-agent socket. Two project keys that
\* reference the same profile still allocate distinct per-session sockets,
\* so this invariant is unchanged by profile resolution.
SocketsDistinctForPresent ==
    phase = "ready" =>
        \A k1, k2 \in present :
            k1 /= k2 => socket[k1] /= socket[k2]

\* --- NoDanglingProfileRefs ---
\* Every profile reference resolves to a defined profile. Dangling references
\* are a config-load failure, not a session-launch failure.
NoDanglingProfileRefs ==
    phase = "ready" =>
        \A k \in present :
            \/ keyProfile[k] = NoProfile
            \/ keyProfile[k] \in definedProfiles

\* --- NoProfileInlineConflict ---
\* A key that declares both a profile reference and inline identity
\* material (private_key: / key:) is a schema-level conflict. The Go
\* validator must catch it at config-load; the spec checks that no such
\* key reaches a ready config.
NoProfileInlineConflict ==
    phase = "ready" =>
        \A k \in present : ~HasProfileInlineConflict(k)

\* --- PresentKeysHaveIdentity ---
\* Every present key has either a profile reference or inline identity
\* material. An orphan key (neither) must never reach a ready config.
PresentKeysHaveIdentity ==
    phase = "ready" =>
        \A k \in present : ~IsOrphanKey(k)

\* --- NoCrossKey ---
\* When exactly one key matches the host, the lookup's socket is the
\* binding of that key and no other present key shares it.
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

----------------------------- MODULE MC_SecretStoreRecovery -----------------------------

EXTENDS TLC

\* Crash recovery for host-owned harness auth material.
\*
\* The model tracks one auth artifact per harness. The durable host copy lives
\* under ~/.hazmat/secrets; the agent copy is the session-local materialization
\* under /Users/agent. Hazmat may crash between recovery, materialization,
\* harness token refresh, harvest, and removal. On the next launch it must
\* reconcile any leftover agent copy before a new session starts.
\*
\* Divergent copies are intentionally handled without trying to infer freshness:
\* the agent residue is promoted into the host store, and the previous host copy
\* is archived in a host-owned conflict set before it can be overwritten.
\*
\* The agent has TWO runtime credential sinks: the materialized file copy
\* (`agent`) and the agent-account login keychain (`keychain`). On
\* keychain-preferring Claude releases an OAuth refresh rotates the token into
\* the keychain and rewrites the file copy to the logged-out empty shape. The
\* rotation invalidates the previous token server-side, so harvest and recovery
\* must promote whichever runtime sink holds the live value -- otherwise the
\* host store retains a dead token and the next session is logged out.

CONSTANTS
    Harnesses,
    Versions,
    NoSecret,
    NoHarness

ASSUME /\ Harnesses # {}
       /\ Versions # {}
       /\ NoSecret \notin Versions
       /\ NoHarness \notin Harnesses

SecretVals == Versions \cup {NoSecret}
Phases ==
    {"idle",
     "recovering",
     "probing",
     "materializing",
     "running",
     "harvesting",
     "removing"}
ActivePhases == {"probing", "materializing", "running", "harvesting", "removing"}

VARIABLES
    phase,
    active,
    host,
    agent,
    keychain,
    conflicts,
    latest,
    recovered,
    baseline

vars ==
    << phase,
       active,
       host,
       agent,
       keychain,
       conflicts,
       latest,
       recovered,
       baseline >>

EmptySecrets ==
    [h \in Harnesses |-> NoSecret]

\* The live runtime credential the agent left behind. The file copy is
\* authoritative when present; otherwise the keychain holds the rotated token
\* that a keychain-backed refresh wrote while emptying the file.
AgentEffective(h) ==
    IF agent[h] # NoSecret THEN agent[h] ELSE keychain[h]

LatestKnown(h) ==
    \/ latest[h] = NoSecret
    \/ latest[h] = host[h]
    \/ latest[h] = agent[h]
    \/ latest[h] = keychain[h]
    \/ latest[h] \in conflicts[h]

Init ==
    /\ phase = "idle"
    /\ active = NoHarness
    /\ host \in [Harnesses -> SecretVals]
    /\ agent \in [Harnesses -> SecretVals]
    /\ keychain \in [Harnesses -> SecretVals]
    /\ conflicts = [h \in Harnesses |-> {}]
    /\ latest \in [Harnesses -> SecretVals]
    /\ \A h \in Harnesses :
        \/ latest[h] = NoSecret
        \/ latest[h] = host[h]
        \/ latest[h] = agent[h]
        \/ latest[h] = keychain[h]
    \* The file copy and the keychain are never both live: a keychain-backed
    \* refresh empties the file, and cleanup/recovery clears the keychain
    \* between sessions, so a single crashed session never strands both.
    /\ \A h \in Harnesses :
        \/ agent[h] = NoSecret
        \/ keychain[h] = NoSecret
    /\ recovered = {}
    /\ baseline = EmptySecrets

BeginRecover ==
    /\ phase = "idle"
    /\ active = NoHarness
    /\ recovered # Harnesses
    /\ phase' = "recovering"
    /\ UNCHANGED << active,
                    host,
                    agent,
                    keychain,
                    conflicts,
                    latest,
                    recovered,
                    baseline >>

RecoveredHost(h) ==
    IF AgentEffective(h) = NoSecret THEN host[h] ELSE AgentEffective(h)

RecoveredConflicts(h) ==
    IF /\ AgentEffective(h) # NoSecret
       /\ host[h] # NoSecret
       /\ host[h] # AgentEffective(h)
    THEN conflicts[h] \cup {host[h]}
    ELSE conflicts[h]

RecoverOne(h) ==
    /\ phase = "recovering"
    /\ h \in Harnesses \ recovered
    /\ host' = [host EXCEPT ![h] = RecoveredHost(h)]
    /\ agent' = [agent EXCEPT ![h] = NoSecret]
    /\ keychain' = [keychain EXCEPT ![h] = NoSecret]
    /\ conflicts' = [conflicts EXCEPT ![h] = RecoveredConflicts(h)]
    /\ recovered' = recovered \cup {h}
    /\ UNCHANGED << phase,
                    active,
                    latest,
                    baseline >>

FinishRecover ==
    /\ phase = "recovering"
    /\ recovered = Harnesses
    /\ phase' = "idle"
    /\ UNCHANGED << active,
                    host,
                    agent,
                    keychain,
                    conflicts,
                    latest,
                    recovered,
                    baseline >>

BeginLaunch(h) ==
    /\ phase = "idle"
    /\ active = NoHarness
    /\ h \in Harnesses
    /\ recovered = Harnesses
    /\ \A x \in Harnesses : agent[x] = NoSecret
    /\ \A x \in Harnesses : keychain[x] = NoSecret
    /\ phase' = "probing"
    /\ active' = h
    /\ baseline' = [baseline EXCEPT ![h] = host[h]]
    /\ UNCHANGED << host,
                    agent,
                    keychain,
                    conflicts,
                    latest,
                    recovered >>

\* Harness version discovery may execute an agent-owned binary before the
\* native sandbox is active. It must therefore finish before any host-owned
\* credential is copied into the agent home.
ProbeHarnessVersion(h) ==
    /\ phase = "probing"
    /\ h \in Harnesses
    /\ active = h
    /\ \A x \in Harnesses : agent[x] = NoSecret
    /\ \A x \in Harnesses : keychain[x] = NoSecret
    /\ phase' = "materializing"
    /\ UNCHANGED << active,
                    host,
                    agent,
                    keychain,
                    conflicts,
                    latest,
                    recovered,
                    baseline >>

MaterializeStored(h) ==
    /\ phase = "materializing"
    /\ h \in Harnesses
    /\ active = h
    /\ host[h] # NoSecret
    /\ agent[h] = NoSecret
    /\ agent' = [agent EXCEPT ![h] = host[h]]
    /\ phase' = "running"
    /\ UNCHANGED << active,
                    host,
                    keychain,
                    conflicts,
                    latest,
                    recovered,
                    baseline >>

MaterializeAbsent(h) ==
    /\ phase = "materializing"
    /\ h \in Harnesses
    /\ active = h
    /\ host[h] = NoSecret
    /\ agent[h] = NoSecret
    /\ phase' = "running"
    /\ UNCHANGED << active,
                    host,
                    agent,
                    keychain,
                    conflicts,
                    latest,
                    recovered,
                    baseline >>

\* A file-backed refresh writes the rotated token into the materialized file.
\* File-backed releases never populate the keychain, so it stays empty here;
\* this keeps the file and keychain from being live at the same time.
ToolRefresh(h, v) ==
    /\ phase = "running"
    /\ h \in Harnesses
    /\ active = h
    /\ v \in Versions
    /\ keychain[h] = NoSecret
    /\ agent' = [agent EXCEPT ![h] = v]
    /\ latest' = [latest EXCEPT ![h] = v]
    /\ UNCHANGED << phase,
                    active,
                    host,
                    keychain,
                    conflicts,
                    recovered,
                    baseline >>

\* A keychain-backed refresh rotates the live token into the agent login
\* keychain and rewrites the materialized file to the logged-out empty shape.
\* The rotated value must still survive into the host store at harvest.
ToolRefreshKeychain(h, v) ==
    /\ phase = "running"
    /\ h \in Harnesses
    /\ active = h
    /\ v \in Versions
    /\ keychain' = [keychain EXCEPT ![h] = v]
    /\ agent' = [agent EXCEPT ![h] = NoSecret]
    /\ latest' = [latest EXCEPT ![h] = v]
    /\ UNCHANGED << phase,
                    active,
                    host,
                    conflicts,
                    recovered,
                    baseline >>

\* Some harness updates rewrite materialized runtime auth into a logged-out or
\* empty shape before any credential refresh. The implementation normalizes
\* those non-harvestable artifacts to NoSecret: do not overwrite the host-owned
\* copy, and let cleanup remove the residue.
ToolLogout(h) ==
    /\ phase = "running"
    /\ h \in Harnesses
    /\ active = h
    /\ agent[h] # NoSecret
    /\ agent[h] = baseline[h]
    /\ keychain[h] = NoSecret
    /\ agent' = [agent EXCEPT ![h] = NoSecret]
    /\ UNCHANGED << phase,
                    active,
                    host,
                    keychain,
                    conflicts,
                    latest,
                    recovered,
                    baseline >>

\* A host-side import or manual repair may change the durable store between
\* sessions. Concurrent host-store writes during a running session require
\* revision metadata to prove stronger than content-diff preservation, so they
\* are intentionally outside this crash-recovery model.
ExternalStoreUpdate(h, v) ==
    /\ phase = "idle"
    /\ h \in Harnesses
    /\ v \in Versions
    /\ host[h] # v
    /\ host' = [host EXCEPT ![h] = v]
    /\ latest' = [latest EXCEPT ![h] = v]
    /\ UNCHANGED << phase,
                    active,
                    agent,
                    keychain,
                    conflicts,
                    recovered,
                    baseline >>

BeginHarvest(h) ==
    /\ phase = "running"
    /\ h \in Harnesses
    /\ active = h
    /\ phase' = "harvesting"
    /\ UNCHANGED << active,
                    host,
                    agent,
                    keychain,
                    conflicts,
                    latest,
                    recovered,
                    baseline >>

HarvestConflicts(h) ==
    IF h \notin Harnesses THEN conflicts
    ELSE
        IF /\ AgentEffective(h) # NoSecret
           /\ host[h] # NoSecret
           /\ host[h] # AgentEffective(h)
           /\ host[h] # baseline[h]
        THEN [conflicts EXCEPT ![h] = conflicts[h] \cup {host[h]}]
        ELSE conflicts

HarvestHost(h) ==
    IF h \notin Harnesses THEN host
    ELSE
        IF AgentEffective(h) = NoSecret
        THEN host
        ELSE [host EXCEPT ![h] = AgentEffective(h)]

Harvest(h) ==
    /\ phase = "harvesting"
    /\ h \in Harnesses
    /\ active = h
    /\ conflicts' = HarvestConflicts(h)
    /\ host' = HarvestHost(h)
    /\ phase' = "removing"
    /\ UNCHANGED << active,
                    agent,
                    keychain,
                    latest,
                    recovered,
                    baseline >>

RemoveAgentCopy(h) ==
    /\ phase = "removing"
    /\ h \in Harnesses
    /\ active = h
    /\ agent' = [agent EXCEPT ![h] = NoSecret]
    /\ keychain' = [keychain EXCEPT ![h] = NoSecret]
    /\ baseline' = [baseline EXCEPT ![h] = NoSecret]
    /\ active' = NoHarness
    /\ phase' = "idle"
    /\ recovered' = Harnesses
    /\ UNCHANGED << host,
                    conflicts,
                    latest >>

Crash ==
    /\ phase # "idle"
    /\ phase' = "idle"
    /\ active' = NoHarness
    /\ recovered' = {}
    /\ baseline' = EmptySecrets
    /\ UNCHANGED << host,
                    agent,
                    keychain,
                    conflicts,
                    latest >>

Stutter ==
    UNCHANGED vars

Next ==
    \/ BeginRecover
    \/ \E h \in Harnesses : RecoverOne(h)
    \/ FinishRecover
    \/ \E h \in Harnesses : BeginLaunch(h)
    \/ \E h \in Harnesses : ProbeHarnessVersion(h)
    \/ \E h \in Harnesses : MaterializeStored(h)
    \/ \E h \in Harnesses : MaterializeAbsent(h)
    \/ \E h \in Harnesses, v \in Versions : ToolRefresh(h, v)
    \/ \E h \in Harnesses, v \in Versions : ToolRefreshKeychain(h, v)
    \/ \E h \in Harnesses : ToolLogout(h)
    \/ \E h \in Harnesses, v \in Versions : ExternalStoreUpdate(h, v)
    \/ \E h \in Harnesses : BeginHarvest(h)
    \/ \E h \in Harnesses : Harvest(h)
    \/ \E h \in Harnesses : RemoveAgentCopy(h)
    \/ Crash
    \/ Stutter

Spec ==
    Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in Phases
    /\ active \in Harnesses \cup {NoHarness}
    /\ host \in [Harnesses -> SecretVals]
    /\ agent \in [Harnesses -> SecretVals]
    /\ keychain \in [Harnesses -> SecretVals]
    /\ conflicts \in [Harnesses -> SUBSET Versions]
    /\ latest \in [Harnesses -> SecretVals]
    /\ recovered \subseteq Harnesses
    /\ baseline \in [Harnesses -> SecretVals]

LatestValueNeverSilentlyLost ==
    \A h \in Harnesses : LatestKnown(h)

\* The materialized file copy and the agent keychain are never both live at
\* once. This is what lets harvest/recovery pick a single unambiguous runtime
\* value, and it depends on cleanup clearing the keychain between sessions.
AgentKeychainNeverBothLive ==
    \A h \in Harnesses :
        \/ agent[h] = NoSecret
        \/ keychain[h] = NoSecret

CleanRecoveredStateHasNoAgentResidue ==
    /\ phase = "idle"
    /\ active = NoHarness
    /\ recovered = Harnesses
    =>
    \A h \in Harnesses :
        /\ agent[h] = NoSecret
        /\ keychain[h] = NoSecret

CleanRecoveredStateKeepsLatestHostOwned ==
    /\ phase = "idle"
    /\ active = NoHarness
    /\ recovered = Harnesses
    =>
    \A h \in Harnesses :
        \/ latest[h] = NoSecret
        \/ latest[h] = host[h]
        \/ latest[h] \in conflicts[h]

NoCrossHarnessAgentExposure ==
    phase \in ActivePhases =>
        /\ active \in Harnesses
        /\ \A h \in Harnesses \ {active} :
            /\ agent[h] = NoSecret
            /\ keychain[h] = NoSecret

LaunchOnlyAfterRecovery ==
    phase \in ActivePhases => recovered = Harnesses

NoSecretDuringPreSandboxProbe ==
    phase = "probing" =>
        \A h \in Harnesses :
            /\ agent[h] = NoSecret
            /\ keychain[h] = NoSecret

IdleClearsSessionBaseline ==
    phase = "idle" =>
        \A h \in Harnesses : baseline[h] = NoSecret

=============================================================================

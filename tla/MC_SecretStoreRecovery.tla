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
     "materializing",
     "running",
     "harvesting",
     "removing"}
ActivePhases == {"materializing", "running", "harvesting", "removing"}

VARIABLES
    phase,
    active,
    host,
    agent,
    conflicts,
    latest,
    recovered,
    baseline

vars ==
    << phase,
       active,
       host,
       agent,
       conflicts,
       latest,
       recovered,
       baseline >>

EmptySecrets ==
    [h \in Harnesses |-> NoSecret]

LatestKnown(h) ==
    \/ latest[h] = NoSecret
    \/ latest[h] = host[h]
    \/ latest[h] = agent[h]
    \/ latest[h] \in conflicts[h]

Init ==
    /\ phase = "idle"
    /\ active = NoHarness
    /\ host \in [Harnesses -> SecretVals]
    /\ agent \in [Harnesses -> SecretVals]
    /\ conflicts = [h \in Harnesses |-> {}]
    /\ latest \in [Harnesses -> SecretVals]
    /\ \A h \in Harnesses :
        latest[h] = NoSecret \/ latest[h] = host[h] \/ latest[h] = agent[h]
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
                    conflicts,
                    latest,
                    recovered,
                    baseline >>

RecoveredHost(h) ==
    IF agent[h] = NoSecret THEN host[h] ELSE agent[h]

RecoveredConflicts(h) ==
    IF /\ agent[h] # NoSecret
       /\ host[h] # NoSecret
       /\ host[h] # agent[h]
    THEN conflicts[h] \cup {host[h]}
    ELSE conflicts[h]

RecoverOne(h) ==
    /\ phase = "recovering"
    /\ h \in Harnesses \ recovered
    /\ host' = [host EXCEPT ![h] = RecoveredHost(h)]
    /\ agent' = [agent EXCEPT ![h] = NoSecret]
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
    /\ phase' = "materializing"
    /\ active' = h
    /\ baseline' = [baseline EXCEPT ![h] = host[h]]
    /\ UNCHANGED << host,
                    agent,
                    conflicts,
                    latest,
                    recovered >>

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
                    conflicts,
                    latest,
                    recovered,
                    baseline >>

ToolRefresh(h, v) ==
    /\ phase = "running"
    /\ h \in Harnesses
    /\ active = h
    /\ v \in Versions
    /\ agent' = [agent EXCEPT ![h] = v]
    /\ latest' = [latest EXCEPT ![h] = v]
    /\ UNCHANGED << phase,
                    active,
                    host,
                    conflicts,
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
                    conflicts,
                    latest,
                    recovered,
                    baseline >>

HarvestConflicts(h) ==
    IF h \notin Harnesses THEN conflicts
    ELSE
        IF /\ agent[h] # NoSecret
           /\ host[h] # NoSecret
           /\ host[h] # agent[h]
           /\ host[h] # baseline[h]
        THEN [conflicts EXCEPT ![h] = conflicts[h] \cup {host[h]}]
        ELSE conflicts

HarvestHost(h) ==
    IF h \notin Harnesses THEN host
    ELSE
        IF agent[h] = NoSecret
        THEN host
        ELSE [host EXCEPT ![h] = agent[h]]

Harvest(h) ==
    /\ phase = "harvesting"
    /\ h \in Harnesses
    /\ active = h
    /\ conflicts' = HarvestConflicts(h)
    /\ host' = HarvestHost(h)
    /\ phase' = "removing"
    /\ UNCHANGED << active,
                    agent,
                    latest,
                    recovered,
                    baseline >>

RemoveAgentCopy(h) ==
    /\ phase = "removing"
    /\ h \in Harnesses
    /\ active = h
    /\ agent' = [agent EXCEPT ![h] = NoSecret]
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
                    conflicts,
                    latest >>

Stutter ==
    UNCHANGED vars

Next ==
    \/ BeginRecover
    \/ \E h \in Harnesses : RecoverOne(h)
    \/ FinishRecover
    \/ \E h \in Harnesses : BeginLaunch(h)
    \/ \E h \in Harnesses : MaterializeStored(h)
    \/ \E h \in Harnesses : MaterializeAbsent(h)
    \/ \E h \in Harnesses, v \in Versions : ToolRefresh(h, v)
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
    /\ conflicts \in [Harnesses -> SUBSET Versions]
    /\ latest \in [Harnesses -> SecretVals]
    /\ recovered \subseteq Harnesses
    /\ baseline \in [Harnesses -> SecretVals]

LatestValueNeverSilentlyLost ==
    \A h \in Harnesses : LatestKnown(h)

CleanRecoveredStateHasNoAgentResidue ==
    /\ phase = "idle"
    /\ active = NoHarness
    /\ recovered = Harnesses
    =>
    \A h \in Harnesses : agent[h] = NoSecret

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
        /\ \A h \in Harnesses \ {active} : agent[h] = NoSecret

LaunchOnlyAfterRecovery ==
    phase \in ActivePhases => recovered = Harnesses

IdleClearsSessionBaseline ==
    phase = "idle" =>
        \A h \in Harnesses : baseline[h] = NoSecret

=============================================================================

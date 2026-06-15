----------------------------- MODULE MC_SessionPermissionRepairs -----------------------------

EXTENDS TLC

\* Session-time permission repairs are planned from the current host state,
\* previewed by `hazmat explain`, optionally applied before launch, and never
\* reverted by core rollback. This model abstracts the four currently
\* user-visible launch repair classes, the native-vs-Tier-3 split, and the
\* observation mode used to read the repair snapshot.
\*
\* ProjectACL is intentionally the bounded startup repair: it prepares the
\* project root and a finite set of likely-mutable existing paths so launch is
\* not proportional to repository size. Historical full-tree ACL backfill is
\* represented separately as ProjectBackfill and is not an automatic startup
\* mutation.
\*
\* The implementation may observe permission state via per-path probes, a
\* batched probe, or a metadata-validated cache hit. All modes must produce
\* the same planned repair set for the same host snapshot; a cached snapshot is
\* usable only while its validation metadata is fresh. Cache entries may be
\* evicted to keep startup bounded; a missing entry is equivalent to choosing a
\* fresh single/batched probe for that path.
\*
\* Governed code:
\*   hazmat/session_mutation.go — repair planning and preview/apply flow
\*   hazmat/workspace_acl.go — project/traverse ACL detection and repair
\*   hazmat/acl_*.go — platform ACL mechanics used by repair actions

ProjectACL == "projectACL"
ProjectBackfill == "projectBackfill"
TraverseACL == "traverseACL"
GitACL == "gitACL"
HomebrewMode == "homebrewMode"

Mutations == {ProjectACL, TraverseACL, GitACL, HomebrewMode}
NativeMutations == {ProjectACL, TraverseACL, GitACL}
SessionModes == {"unset", "native", "docker"}
Phases == {"idle", "previewed", "planned", "launched", "rolledBack"}
ProbeModes == {"unset", "singleProbe", "batchedProbe", "validatedCache"}
PlanningProbeModes == ProbeModes \ {"unset"}

VARIABLES
    phase,
    sessionMode,
    probeMode,
    cacheValid,
    projectBroken,
    projectBackfillNeeded,
    backfillApplied,
    traverseBroken,
    gitBroken,
    homebrewBroken,
    homebrewEligible,
    applied,
    planned,
    baseApplied,
    rollbackSnapshot

vars ==
    << phase,
       sessionMode,
       probeMode,
       cacheValid,
       projectBroken,
       projectBackfillNeeded,
       backfillApplied,
       traverseBroken,
       gitBroken,
       homebrewBroken,
       homebrewEligible,
       applied,
       planned,
       baseApplied,
       rollbackSnapshot >>

NeedsProject(repairSet) ==
    projectBroken /\ ProjectACL \notin repairSet

NeedsProjectBackfill ==
    projectBackfillNeeded /\ ~backfillApplied

NeedsTraverse(repairSet) ==
    traverseBroken /\ TraverseACL \notin repairSet

NeedsGit(repairSet) ==
    gitBroken /\ GitACL \notin repairSet

NeedsHomebrew(repairSet) ==
    homebrewEligible /\ homebrewBroken /\ HomebrewMode \notin repairSet

ExpectedPlan(mode, repairSet) ==
    (IF mode = "native" THEN
         (IF NeedsProject(repairSet) THEN {ProjectACL} ELSE {}) \cup
         (IF NeedsTraverse(repairSet) THEN {TraverseACL} ELSE {}) \cup
         (IF NeedsGit(repairSet) THEN {GitACL} ELSE {})
     ELSE {})
    \cup
    (IF mode \in {"native", "docker"} /\ NeedsHomebrew(repairSet)
        THEN {HomebrewMode}
        ELSE {})

Init ==
    /\ phase = "idle"
    /\ sessionMode = "unset"
    /\ probeMode = "unset"
    /\ cacheValid \in BOOLEAN
    /\ projectBroken \in BOOLEAN
    /\ projectBackfillNeeded \in BOOLEAN
    /\ backfillApplied \in BOOLEAN
    /\ backfillApplied => ~projectBackfillNeeded
    /\ traverseBroken \in BOOLEAN
    /\ gitBroken \in BOOLEAN
    /\ homebrewBroken \in BOOLEAN
    /\ homebrewEligible \in BOOLEAN
    /\ applied \in SUBSET Mutations
    /\ planned = {}
    /\ baseApplied = {}
    /\ rollbackSnapshot = {}

ProbeUsable(p) ==
    p \in PlanningProbeModes /\ (p = "validatedCache" => cacheValid)

Preview(m, p) ==
    /\ phase = "idle"
    /\ m \in {"native", "docker"}
    /\ ProbeUsable(p)
    /\ phase' = "previewed"
    /\ sessionMode' = m
    /\ probeMode' = p
    /\ planned' = ExpectedPlan(m, applied)
    /\ baseApplied' = applied
    /\ UNCHANGED << projectBroken,
                    cacheValid,
                    projectBackfillNeeded,
                    backfillApplied,
                    traverseBroken,
                    gitBroken,
                    homebrewBroken,
                    homebrewEligible,
                    applied,
                    rollbackSnapshot >>

PlanLaunch(m, p) ==
    /\ phase = "idle"
    /\ m \in {"native", "docker"}
    /\ ProbeUsable(p)
    /\ phase' = "planned"
    /\ sessionMode' = m
    /\ probeMode' = p
    /\ planned' = ExpectedPlan(m, applied)
    /\ baseApplied' = applied
    /\ UNCHANGED << projectBroken,
                    cacheValid,
                    projectBackfillNeeded,
                    backfillApplied,
                    traverseBroken,
                    gitBroken,
                    homebrewBroken,
                    homebrewEligible,
                    applied,
                    rollbackSnapshot >>

ApplyRepair(r) ==
    /\ phase = "planned"
    /\ r \in planned \ applied
    /\ applied' = applied \cup {r}
    /\ cacheValid' = FALSE
    /\ UNCHANGED << phase,
                    sessionMode,
                    probeMode,
                    projectBroken,
                    projectBackfillNeeded,
                    backfillApplied,
                    traverseBroken,
                    gitBroken,
                    homebrewBroken,
                    homebrewEligible,
                    planned,
                    baseApplied,
                    rollbackSnapshot >>

Launch ==
    /\ phase = "planned"
    /\ ~NeedsGit(applied)
    /\ ~NeedsHomebrew(applied)
    /\ phase' = "launched"
    /\ UNCHANGED << sessionMode,
                    probeMode,
                    cacheValid,
                    projectBroken,
                    projectBackfillNeeded,
                    backfillApplied,
                    traverseBroken,
                    gitBroken,
                    homebrewBroken,
                    homebrewEligible,
                    applied,
                    planned,
                    baseApplied,
                    rollbackSnapshot >>

Rollback ==
    /\ phase \in {"idle", "previewed", "planned", "launched"}
    /\ phase' = "rolledBack"
    /\ rollbackSnapshot' = applied
    /\ UNCHANGED << sessionMode,
                    probeMode,
                    cacheValid,
                    projectBroken,
                    projectBackfillNeeded,
                    backfillApplied,
                    traverseBroken,
                    gitBroken,
                    homebrewBroken,
                    homebrewEligible,
                    applied,
                    planned,
                    baseApplied >>

OperatorProjectBackfill ==
    /\ phase = "idle"
    /\ NeedsProjectBackfill
    /\ projectBroken' = FALSE
    /\ projectBackfillNeeded' = FALSE
    /\ backfillApplied' = TRUE
    /\ cacheValid' = FALSE
    /\ UNCHANGED << phase,
                    sessionMode,
                    probeMode,
                    traverseBroken,
                    gitBroken,
                    homebrewBroken,
                    homebrewEligible,
                    applied,
                    planned,
                    baseApplied,
                    rollbackSnapshot >>

Stutter ==
    UNCHANGED vars

Next ==
    \/ \E m \in {"native", "docker"} :
        \E p \in PlanningProbeModes : Preview(m, p)
    \/ \E m \in {"native", "docker"} :
        \E p \in PlanningProbeModes : PlanLaunch(m, p)
    \/ \E r \in Mutations : ApplyRepair(r)
    \/ OperatorProjectBackfill
    \/ Launch
    \/ Rollback
    \/ Stutter

Spec ==
    Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in Phases
    /\ sessionMode \in SessionModes
    /\ probeMode \in ProbeModes
    /\ cacheValid \in BOOLEAN
    /\ projectBroken \in BOOLEAN
    /\ projectBackfillNeeded \in BOOLEAN
    /\ backfillApplied \in BOOLEAN
    /\ backfillApplied => ~projectBackfillNeeded
    /\ traverseBroken \in BOOLEAN
    /\ gitBroken \in BOOLEAN
    /\ homebrewBroken \in BOOLEAN
    /\ homebrewEligible \in BOOLEAN
    /\ applied \subseteq Mutations
    /\ planned \subseteq Mutations
    /\ baseApplied \subseteq Mutations
    /\ rollbackSnapshot \subseteq Mutations

PlannedRepairsMatchSnapshot ==
    phase # "idle" => planned = ExpectedPlan(sessionMode, baseApplied)

ValidatedCacheRequiresFreshMetadata ==
    phase # "idle" /\ probeMode = "validatedCache" /\ applied = baseApplied => cacheValid

PreviewIsReadOnly ==
    phase = "previewed" => applied = baseApplied

DockerSkipsNativeACLRepairs ==
    phase # "idle" /\ sessionMode = "docker" => planned \cap NativeMutations = {}

HomebrewRepairRequiresEligibleCellar ==
    HomebrewMode \in planned => homebrewEligible /\ NeedsHomebrew(baseApplied)

LaunchClearsFatalRepairNeeds ==
    phase = "launched" => /\ ~NeedsGit(applied)
                          /\ ~NeedsHomebrew(applied)

RollbackPreservesSessionRepairs ==
    phase = "rolledBack" => applied = rollbackSnapshot

BackfillIsOutsideStartupPlan ==
    ProjectBackfill \notin planned

=============================================================================

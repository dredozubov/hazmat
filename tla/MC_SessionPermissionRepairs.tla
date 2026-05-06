----------------------------- MODULE MC_SessionPermissionRepairs -----------------------------

EXTENDS TLC

\* Session-time permission repairs are planned from the current host state,
\* previewed by `hazmat explain`, optionally applied before launch, and never
\* reverted by core rollback. This model abstracts the four currently
\* user-visible launch repair classes and the native-vs-Tier-3 split.
\*
\* ProjectACL is intentionally the bounded startup repair: it prepares the
\* project root and a finite set of likely-mutable existing paths so launch is
\* not proportional to repository size. Historical full-tree ACL backfill is
\* represented separately as ProjectBackfill and is not an automatic startup
\* mutation.
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

VARIABLES
    phase,
    sessionMode,
    projectBroken,
    projectBackfillNeeded,
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
       projectBroken,
       projectBackfillNeeded,
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
    /\ projectBroken \in BOOLEAN
    /\ projectBackfillNeeded \in BOOLEAN
    /\ traverseBroken \in BOOLEAN
    /\ gitBroken \in BOOLEAN
    /\ homebrewBroken \in BOOLEAN
    /\ homebrewEligible \in BOOLEAN
    /\ applied \in SUBSET Mutations
    /\ planned = {}
    /\ baseApplied = {}
    /\ rollbackSnapshot = {}

Preview(m) ==
    /\ phase = "idle"
    /\ m \in {"native", "docker"}
    /\ phase' = "previewed"
    /\ sessionMode' = m
    /\ planned' = ExpectedPlan(m, applied)
    /\ baseApplied' = applied
    /\ UNCHANGED << projectBroken,
                    projectBackfillNeeded,
                    traverseBroken,
                    gitBroken,
                    homebrewBroken,
                    homebrewEligible,
                    applied,
                    rollbackSnapshot >>

PlanLaunch(m) ==
    /\ phase = "idle"
    /\ m \in {"native", "docker"}
    /\ phase' = "planned"
    /\ sessionMode' = m
    /\ planned' = ExpectedPlan(m, applied)
    /\ baseApplied' = applied
    /\ UNCHANGED << projectBroken,
                    projectBackfillNeeded,
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
    /\ UNCHANGED << phase,
                    sessionMode,
                    projectBroken,
                    projectBackfillNeeded,
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
                    projectBroken,
                    projectBackfillNeeded,
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
                    projectBroken,
                    projectBackfillNeeded,
                    traverseBroken,
                    gitBroken,
                    homebrewBroken,
                    homebrewEligible,
                    applied,
                    planned,
                    baseApplied >>

Stutter ==
    UNCHANGED vars

Next ==
    \/ \E m \in {"native", "docker"} : Preview(m)
    \/ \E m \in {"native", "docker"} : PlanLaunch(m)
    \/ \E r \in Mutations : ApplyRepair(r)
    \/ Launch
    \/ Rollback
    \/ Stutter

Spec ==
    Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in Phases
    /\ sessionMode \in SessionModes
    /\ projectBroken \in BOOLEAN
    /\ projectBackfillNeeded \in BOOLEAN
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

---- MODULE MC_Migration ----
\* Hazmat version migration state machine — verifies that upgrading from any
\* previous init version to the current binary version produces a consistent
\* system state, AND that rollback from any state reaches a clean system
\* without violating AgentContained.
\*
\* Expected TLC result: No error has been found.
\*
\* Governed code:
\*   hazmat/init.go       — runInit(), migration dispatch
\*   hazmat/migrate.go    — migration functions (to be created)
\*   hazmat/rollback.go   — runRollback()
\*   ~/.hazmat/state.json — version tracking file

EXTENDS Naturals, Sequences, FiniteSets

\* ═══════════════════════════════════════════════════════════════════════════════
\* Concrete model: 3 hazmat versions
\* ═══════════════════════════════════════════════════════════════════════════════

V1 == "v0.1.0"   \* Initial release: workspace concept
V2 == "v0.2.0"   \* Workspace removed, lazy per-project ACL
V3 == "v0.3.0"   \* Supply chain hardening (npmrc, pip.conf)

Versions == {V1, V2, V3}
BinaryVersion == V3

VersionOrd(v) ==
    IF v = V1 THEN 1
    ELSE IF v = V2 THEN 2
    ELSE 3

VersionLT(a, b) == VersionOrd(a) < VersionOrd(b)

\* Artifacts expected after a successful init at each version.
Expected(v) ==
    IF v = V1 THEN
        {"agentUser", "devGroup", "workspace", "workspaceACL",
         "agentSymlink", "umask", "seatbelt", "wrappers",
         "launchHelper", "sudoers", "pfAnchor", "dnsBlocklist",
         "launchDaemon", "localRepo"}
    ELSE IF v = V2 THEN
        {"agentUser", "devGroup", "homeDirTraverse",
         "umask", "seatbelt", "wrappers",
         "launchHelper", "sudoers", "pfAnchor", "dnsBlocklist",
         "launchDaemon", "localRepo"}
    ELSE \* V3
        {"agentUser", "devGroup", "homeDirTraverse",
         "umask", "seatbelt", "wrappers",
         "launchHelper", "sudoers", "pfAnchor", "dnsBlocklist",
         "launchDaemon", "localRepo", "npmrc"}

\* The union of ALL artifacts across ALL versions. Rollback must know how
\* to remove everything, including artifacts from older versions that the
\* current binary didn't create.
AllArtifacts ==
    UNION {Expected(v) : v \in Versions}

\* Which adjacent migration functions exist.
HasMigration(from, to) ==
    \/ (from = V1 /\ to = V2)
    \/ (from = V2 /\ to = V3)

\* Successor version.
NextVersion(v) ==
    IF v = V1 THEN V2
    ELSE IF v = V2 THEN V3
    ELSE v

\* ═══════════════════════════════════════════════════════════════════════════════
\* Rollback ordering
\* ═══════════════════════════════════════════════════════════════════════════════

\* Rollback removes artifacts in a specific order. The critical constraint:
\* sudoers must be removed BEFORE pfAnchor (revoke privilege before removing
\* containment). We model rollback as removing one artifact at a time.

\* Can this artifact be removed given what's currently present?
\* Encodes the ordering constraint: sudoers before pfAnchor.
CanRemove(a, arts) ==
    IF a = "pfAnchor" THEN "sudoers" \notin arts  \* pf only after sudoers gone
    ELSE IF a = "dnsBlocklist" THEN "sudoers" \notin arts  \* dns only after sudoers
    ELSE IF a = "launchDaemon" THEN "sudoers" \notin arts  \* daemon only after sudoers
    ELSE IF a = "agentUser" THEN "sudoers" \notin arts     \* user only after sudoers
    ELSE IF a = "devGroup" THEN "agentUser" \notin arts    \* group only after user
    ELSE TRUE  \* everything else can be removed in any order

\* ═══════════════════════════════════════════════════════════════════════════════
\* Variables
\* ═══════════════════════════════════════════════════════════════════════════════

VARIABLES
    initVersion,       \* Version recorded in state.json
    artifacts,         \* Set of currently active system artifacts
    phase,             \* "idle" | "migrating" | "initializing" | "done" |
                       \* "failed" | "rolling_back" | "clean"
    migrateFrom        \* Version we're migrating FROM (current step)

vars == <<initVersion, artifacts, phase, migrateFrom>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Initial state: system was initialized at some version <= BinaryVersion
\* ═══════════════════════════════════════════════════════════════════════════════

Init ==
    \E v \in Versions:
        /\ VersionOrd(v) <= VersionOrd(BinaryVersion)
        /\ initVersion = v
        /\ artifacts = Expected(v)
        /\ phase = "idle"
        /\ migrateFrom = v

\* ═══════════════════════════════════════════════════════════════════════════════
\* Actions — Init / Migration
\* ═══════════════════════════════════════════════════════════════════════════════

\* User runs "hazmat init".
StartInit ==
    /\ phase = "idle"
    /\ IF initVersion = BinaryVersion
       THEN
            /\ phase' = "initializing"
            /\ UNCHANGED <<initVersion, artifacts, migrateFrom>>
       ELSE
            /\ phase' = "migrating"
            /\ migrateFrom' = initVersion
            /\ UNCHANGED <<initVersion, artifacts>>

\* Apply one migration step (success).
MigrateSucceed ==
    /\ phase = "migrating"
    /\ migrateFrom /= BinaryVersion
    /\ LET to == NextVersion(migrateFrom)
       IN
        /\ HasMigration(migrateFrom, to)
        /\ artifacts' = (artifacts \ (Expected(migrateFrom) \ Expected(to)))
                        \cup (Expected(to) \ Expected(migrateFrom))
        /\ initVersion' = to
        /\ migrateFrom' = to
        /\ IF to = BinaryVersion
           THEN phase' = "initializing"
           ELSE phase' = "migrating"

\* Migration step fails.
MigrateFail ==
    /\ phase = "migrating"
    /\ migrateFrom /= BinaryVersion
    /\ phase' = "failed"
    /\ UNCHANGED <<initVersion, artifacts, migrateFrom>>

\* Idempotent init — ensures all expected artifacts are present.
RunInit ==
    /\ phase = "initializing"
    /\ artifacts' = Expected(BinaryVersion)
    /\ initVersion' = BinaryVersion
    /\ phase' = "done"
    /\ UNCHANGED migrateFrom

\* Recovery: user re-runs init after failure.
Recover ==
    /\ phase = "failed"
    /\ phase' = "idle"
    /\ UNCHANGED <<initVersion, artifacts, migrateFrom>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Actions — Rollback
\* ═══════════════════════════════════════════════════════════════════════════════

\* User starts rollback. Can be initiated from any non-clean phase.
StartRollback ==
    /\ phase \in {"idle", "done", "failed"}
    /\ artifacts /= {}
    /\ phase' = "rolling_back"
    /\ UNCHANGED <<initVersion, artifacts, migrateFrom>>

\* Remove one artifact during rollback, respecting ordering constraints.
RollbackStep ==
    /\ phase = "rolling_back"
    /\ artifacts /= {}
    /\ \E a \in artifacts:
        /\ CanRemove(a, artifacts \ {a})
        /\ artifacts' = artifacts \ {a}
        /\ UNCHANGED <<initVersion, phase, migrateFrom>>

\* Rollback step fails.
RollbackFail ==
    /\ phase = "rolling_back"
    /\ artifacts /= {}
    /\ phase' = "failed"
    /\ UNCHANGED <<initVersion, artifacts, migrateFrom>>

\* Rollback completes — all artifacts removed.
RollbackDone ==
    /\ phase = "rolling_back"
    /\ artifacts = {}
    /\ phase' = "clean"
    /\ UNCHANGED <<initVersion, artifacts, migrateFrom>>

\* Stay in terminal states.
Stutter ==
    /\ phase \in {"done", "clean"}
    /\ UNCHANGED vars

\* ═══════════════════════════════════════════════════════════════════════════════
\* Next-state relation
\* ═══════════════════════════════════════════════════════════════════════════════

Next ==
    \/ StartInit
    \/ MigrateSucceed
    \/ MigrateFail
    \/ RunInit
    \/ Recover
    \/ StartRollback
    \/ RollbackStep
    \/ RollbackFail
    \/ RollbackDone
    \/ Stutter

\* ═══════════════════════════════════════════════════════════════════════════════
\* Invariants (safety)
\* ═══════════════════════════════════════════════════════════════════════════════

\* Agent must never be launchable without firewall — in ANY state.
AgentContained ==
    "sudoers" \in artifacts => "pfAnchor" \in artifacts

\* After init completes, all expected artifacts are present.
InitComplete ==
    phase = "done" => artifacts = Expected(BinaryVersion)

\* After init completes, recorded version matches binary.
VersionConsistent ==
    phase = "done" => initVersion = BinaryVersion

\* A failed state can always be retried (init or rollback).
FailureRecoverable ==
    phase = "failed" => (ENABLED Recover \/ ENABLED StartRollback)

\* Migration only moves forward.
MigrationForward ==
    phase = "migrating" => VersionOrd(migrateFrom) <= VersionOrd(BinaryVersion)

\* After rollback completes, no artifacts remain.
RollbackClean ==
    phase = "clean" => artifacts = {}

\* Rollback can always be started from idle, done, or failed states.
RollbackAlwaysAvailable ==
    (phase \in {"idle", "done", "failed"} /\ artifacts /= {}) => ENABLED StartRollback

Safety ==
    /\ AgentContained
    /\ InitComplete
    /\ VersionConsistent
    /\ FailureRecoverable
    /\ MigrationForward
    /\ RollbackClean
    /\ RollbackAlwaysAvailable

\* ═══════════════════════════════════════════════════════════════════════════════
\* Liveness
\* ═══════════════════════════════════════════════════════════════════════════════

\* Strong fairness on succeed actions (transient failure assumption).
Fairness ==
    /\ SF_vars(MigrateSucceed)
    /\ SF_vars(RollbackStep)
    /\ WF_vars(StartInit)
    /\ WF_vars(RunInit)
    /\ WF_vars(Recover)
    /\ WF_vars(RollbackDone)

\* Init eventually completes (if user runs init, not rollback).
EventuallyComplete == <>(phase = "done" \/ phase = "clean")

Spec == Init /\ [][Next]_vars /\ Fairness

====

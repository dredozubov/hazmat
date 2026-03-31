---- MODULE MC_Migration ----
\* Hazmat version migration state machine — verifies that upgrading from any
\* previous init version to the current binary version produces a consistent
\* system state, with no skipped migrations, no stale artifacts, and the
\* AgentContained invariant preserved throughout.
\*
\* Expected TLC result: No error has been found.
\*
\* Governed code:
\*   hazmat/init.go       — runInit(), migration dispatch
\*   hazmat/migrate.go    — migration functions (to be created)
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

\* Which adjacent migration functions exist.
HasMigration(from, to) ==
    \/ (from = V1 /\ to = V2)
    \/ (from = V2 /\ to = V3)

\* Successor version (for building migration chain).
NextVersion(v) ==
    IF v = V1 THEN V2
    ELSE IF v = V2 THEN V3
    ELSE v  \* V3 has no successor

\* ═══════════════════════════════════════════════════════════════════════════════
\* Variables
\* ═══════════════════════════════════════════════════════════════════════════════

VARIABLES
    initVersion,       \* Version recorded in state.json
    artifacts,         \* Set of currently active system artifacts
    phase,             \* "idle" | "migrating" | "initializing" | "done" | "failed"
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
\* Actions
\* ═══════════════════════════════════════════════════════════════════════════════

\* User runs "hazmat init".
StartInit ==
    /\ phase = "idle"
    /\ IF initVersion = BinaryVersion
       THEN \* Already current — skip to idempotent init.
            /\ phase' = "initializing"
            /\ UNCHANGED <<initVersion, artifacts, migrateFrom>>
       ELSE \* Need migration from initVersion.
            /\ phase' = "migrating"
            /\ migrateFrom' = initVersion
            /\ UNCHANGED <<initVersion, artifacts>>

\* Apply one migration step (success).
\* Transforms artifacts from migrateFrom's expected set to next version's.
MigrateSucceed ==
    /\ phase = "migrating"
    /\ migrateFrom /= BinaryVersion
    /\ LET to == NextVersion(migrateFrom)
       IN
        /\ HasMigration(migrateFrom, to)
        \* Remove artifacts only in old version, add artifacts only in new.
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

\* Stay done (stuttering allowed by TLA+, but explicit for clarity).
Done ==
    /\ phase = "done"
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
    \/ Done

\* ═══════════════════════════════════════════════════════════════════════════════
\* Invariants
\* ═══════════════════════════════════════════════════════════════════════════════

\* Agent must never be launchable without firewall — even mid-migration.
AgentContained ==
    "sudoers" \in artifacts => "pfAnchor" \in artifacts

\* After init completes, all expected artifacts are present.
InitComplete ==
    phase = "done" => artifacts = Expected(BinaryVersion)

\* After init completes, recorded version matches binary.
VersionConsistent ==
    phase = "done" => initVersion = BinaryVersion

\* A failed migration can always be retried.
MigrationRecoverable ==
    phase = "failed" => ENABLED Recover

\* Migration only moves forward.
MigrationForward ==
    phase = "migrating" => VersionOrd(migrateFrom) <= VersionOrd(BinaryVersion)

Safety ==
    /\ AgentContained
    /\ InitComplete
    /\ VersionConsistent
    /\ MigrationRecoverable
    /\ MigrationForward

\* ═══════════════════════════════════════════════════════════════════════════════
\* Liveness
\* ═══════════════════════════════════════════════════════════════════════════════

\* Strong fairness on MigrateSucceed: if it's repeatedly enabled (user keeps
\* retrying after failures), it eventually fires. This models the assumption
\* that failures are transient — if the user keeps trying, migration succeeds.
Fairness == SF_vars(MigrateSucceed) /\ WF_vars(StartInit) /\ WF_vars(RunInit) /\ WF_vars(Recover)

\* Under strong fairness (transient failures), init eventually completes.
EventuallyComplete == <>(phase = "done")

Spec == Init /\ [][Next]_vars /\ Fairness

====

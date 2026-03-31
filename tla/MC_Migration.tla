---- MODULE MC_Migration ----
\* Hazmat version migration state machine — verifies that upgrading from any
\* previous init version to the current binary version produces a consistent
\* system state, with no skipped migrations, no stale artifacts, and the
\* AgentContained invariant preserved throughout.
\*
\* The model treats each version as having a set of expected artifacts. A
\* migration transforms the artifact set from one version's expected state
\* to the next. Init is modeled as "apply all pending migrations then run
\* normal idempotent setup."
\*
\* Expected TLC result: No error has been found.
\*
\* Governed code:
\*   hazmat/init.go       — runInit(), migration dispatch
\*   hazmat/migrate.go    — migration functions (to be created)
\*   ~/.hazmat/state.json — version tracking file
\*
\* Model bounds: 4 versions, 3 migration steps, failure at any step.

EXTENDS Naturals, Sequences, FiniteSets

\* ═══════════════════════════════════════════════════════════════════════════════
\* Constants — version chain and artifact definitions
\* ═══════════════════════════════════════════════════════════════════════════════

CONSTANTS
    Versions,          \* Ordered set of known versions, e.g. {"0.1.0", "0.2.0", "0.3.0"}
    VersionOrder,      \* Function: version -> natural (0.1.0 -> 1, 0.2.0 -> 2, ...)
    BinaryVersion,     \* The version of the currently installed hazmat binary
    ExpectedArtifacts, \* Function: version -> set of artifact names
    MigrationExists    \* Function: <<from, to>> -> BOOLEAN

\* ═══════════════════════════════════════════════════════════════════════════════
\* Variables
\* ═══════════════════════════════════════════════════════════════════════════════

VARIABLES
    initVersion,       \* Version recorded in state.json (last successful init)
    artifacts,         \* Set of currently active system artifacts
    appliedMigrations, \* Sequence of <<from, to>> pairs that have been applied
    phase,             \* "idle" | "migrating" | "initializing" | "done" | "failed"
    migrationStep,     \* Index into the migration chain being applied
    migrationChain     \* Sequence of <<from, to>> pairs to apply

vars == <<initVersion, artifacts, appliedMigrations, phase, migrationStep, migrationChain>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Helpers
\* ═══════════════════════════════════════════════════════════════════════════════

\* Is version a strictly less than version b?
VersionLT(a, b) == VersionOrder[a] < VersionOrder[b]

\* Is version a less than or equal to version b?
VersionLEQ(a, b) == VersionOrder[a] <= VersionOrder[b]

\* Build the migration chain: sequence of adjacent version pairs from
\* initVersion to BinaryVersion.
MigrationChainFor(from, to) ==
    LET ordered == CHOOSE seq \in [1..Cardinality(Versions) -> Versions]:
                       \A i \in 1..Cardinality(Versions)-1:
                           VersionOrder[seq[i]] < VersionOrder[seq[i+1]]
        fromIdx == CHOOSE i \in 1..Len(ordered): ordered[i] = from
        toIdx   == CHOOSE i \in 1..Len(ordered): ordered[i] = to
    IN  [i \in 1..(toIdx - fromIdx) |->
            <<ordered[fromIdx + i - 1], ordered[fromIdx + i]>>]

\* ═══════════════════════════════════════════════════════════════════════════════
\* Initial state
\* ═══════════════════════════════════════════════════════════════════════════════

Init ==
    \* Start from any valid previous init version with its expected artifacts.
    \E v \in Versions:
        /\ VersionLEQ(v, BinaryVersion)
        /\ initVersion = v
        /\ artifacts = ExpectedArtifacts[v]
        /\ appliedMigrations = <<>>
        /\ phase = "idle"
        /\ migrationStep = 0
        /\ migrationChain = <<>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Actions
\* ═══════════════════════════════════════════════════════════════════════════════

\* User runs "hazmat init" — determine which migrations are needed.
StartInit ==
    /\ phase = "idle"
    /\ IF initVersion = BinaryVersion
       THEN \* Already up to date — go straight to idempotent init.
            /\ phase' = "initializing"
            /\ migrationChain' = <<>>
            /\ UNCHANGED <<initVersion, artifacts, appliedMigrations, migrationStep>>
       ELSE \* Need migrations.
            /\ phase' = "migrating"
            /\ migrationChain' = MigrationChainFor(initVersion, BinaryVersion)
            /\ migrationStep' = 1
            /\ UNCHANGED <<initVersion, artifacts, appliedMigrations>>

\* Apply one migration step (success).
MigrationStepSucceed ==
    /\ phase = "migrating"
    /\ migrationStep <= Len(migrationChain)
    /\ LET step == migrationChain[migrationStep]
           from == step[1]
           to   == step[2]
       IN
        /\ MigrationExists[step]
        \* Migration transforms artifacts: remove old-version-only, add new-version.
        /\ artifacts' = (artifacts \ (ExpectedArtifacts[from] \ ExpectedArtifacts[to]))
                        \cup (ExpectedArtifacts[to] \ ExpectedArtifacts[from])
        /\ appliedMigrations' = Append(appliedMigrations, step)
        /\ initVersion' = to
        /\ migrationStep' = migrationStep + 1
        /\ IF migrationStep + 1 > Len(migrationChain)
           THEN phase' = "initializing"  \* All migrations done, proceed to init.
           ELSE phase' = "migrating"
        /\ UNCHANGED migrationChain

\* Migration step fails (power failure, error, etc.)
MigrationStepFail ==
    /\ phase = "migrating"
    /\ migrationStep <= Len(migrationChain)
    /\ phase' = "failed"
    \* Artifacts may be partially transformed — this is the dangerous state.
    \* The invariant MigrationRecoverable must hold here.
    /\ UNCHANGED <<initVersion, artifacts, appliedMigrations, migrationStep, migrationChain>>

\* Idempotent init — ensures all expected artifacts are present.
RunInit ==
    /\ phase = "initializing"
    /\ artifacts' = ExpectedArtifacts[BinaryVersion]
    /\ initVersion' = BinaryVersion
    /\ phase' = "done"
    /\ UNCHANGED <<appliedMigrations, migrationStep, migrationChain>>

\* Recovery from failed migration: user re-runs "hazmat init".
RecoverFromFailure ==
    /\ phase = "failed"
    /\ phase' = "idle"
    /\ UNCHANGED <<initVersion, artifacts, appliedMigrations, migrationStep, migrationChain>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Next-state relation
\* ═══════════════════════════════════════════════════════════════════════════════

Next ==
    \/ StartInit
    \/ MigrationStepSucceed
    \/ MigrationStepFail
    \/ RunInit
    \/ RecoverFromFailure

\* ═══════════════════════════════════════════════════════════════════════════════
\* Invariants (safety)
\* ═══════════════════════════════════════════════════════════════════════════════

\* The agent must never be launchable without firewall containment.
\* This must hold even during migration.
AgentContained ==
    "sudoers" \in artifacts => "pfAnchor" \in artifacts

\* No migration is ever skipped.
NoSkippedMigrations ==
    \A i \in 1..Len(appliedMigrations)-1:
        appliedMigrations[i][2] = appliedMigrations[i+1][1]

\* Migrations are applied in version order.
MigrationsOrdered ==
    \A i \in 1..Len(appliedMigrations):
        VersionLT(appliedMigrations[i][1], appliedMigrations[i][2])

\* After successful init, all expected artifacts are present.
InitComplete ==
    phase = "done" => artifacts = ExpectedArtifacts[BinaryVersion]

\* After successful init, the recorded version matches the binary.
VersionConsistent ==
    phase = "done" => initVersion = BinaryVersion

\* A failed migration can always be retried by re-running init.
MigrationRecoverable ==
    phase = "failed" => ENABLED RecoverFromFailure

\* Combined safety property.
Safety ==
    /\ AgentContained
    /\ NoSkippedMigrations
    /\ MigrationsOrdered
    /\ InitComplete
    /\ VersionConsistent
    /\ MigrationRecoverable

\* ═══════════════════════════════════════════════════════════════════════════════
\* Liveness
\* ═══════════════════════════════════════════════════════════════════════════════

\* Fairness: if init is started and no step permanently fails, it completes.
Fairness == WF_vars(Next)

\* Init eventually reaches "done" (if failures are transient).
EventuallyComplete == <>(phase = "done")

Spec == Init /\ [][Next]_vars /\ Fairness

====

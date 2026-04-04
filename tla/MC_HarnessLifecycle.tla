----------------------------- MODULE MC_HarnessLifecycle -----------------------------

EXTENDS TLC

\* Hazmat now records per-harness metadata under ~/.hazmat/state.json while the
\* actual harness files live in the agent home. This model focuses on the
\* lifecycle contract the Go code currently implements:
\*   - successful bootstrap/import writes harness metadata unless the command is
\*     a dry-run
\*   - core saveState preserves existing harness metadata
\*   - rollback always removes the host-owned state.json metadata
\*   - rollback only removes agent-home harness artifacts when --delete-user is used

Harnesses == {"claude", "codex", "opencode"}
ImportableHarnesses == {"claude", "opencode"}
HarnessVersion == [h \in Harnesses |-> "1"]
ActionKinds ==
    {"none",
     "enable-core",
     "bootstrap",
     "bootstrap-dry",
     "import",
     "import-dry",
     "save",
     "rollback-keep-user",
     "rollback-delete-user"}
Phases == {"idle", "rolledBack"}

VARIABLES
    phase,
    coreReady,
    stateFilePresent,
    initRecorded,
    installedArtifacts,
    importedArtifacts,
    recordedVersion,
    recordedImported,
    lastAction,
    snapshotRecordedVersion,
    snapshotRecordedImported,
    snapshotStateFilePresent,
    snapshotInstalledArtifacts,
    snapshotImportedArtifacts

vars ==
    << phase,
       coreReady,
       stateFilePresent,
       initRecorded,
       installedArtifacts,
       importedArtifacts,
       recordedVersion,
       recordedImported,
       lastAction,
       snapshotRecordedVersion,
       snapshotRecordedImported,
       snapshotStateFilePresent,
       snapshotInstalledArtifacts,
       snapshotImportedArtifacts >>

EmptyRecordedVersion ==
    [h \in Harnesses |-> ""]

Init ==
    /\ phase = "idle"
    /\ coreReady \in BOOLEAN
    /\ stateFilePresent = FALSE
    /\ initRecorded = FALSE
    /\ installedArtifacts = {}
    /\ importedArtifacts = {}
    /\ recordedVersion = EmptyRecordedVersion
    /\ recordedImported = {}
    /\ lastAction = "none"
    /\ snapshotRecordedVersion = EmptyRecordedVersion
    /\ snapshotRecordedImported = {}
    /\ snapshotStateFilePresent = FALSE
    /\ snapshotInstalledArtifacts = {}
    /\ snapshotImportedArtifacts = {}

EnableCore ==
    /\ phase = "idle"
    /\ ~coreReady
    /\ coreReady' = TRUE
    /\ lastAction' = "enable-core"
    /\ UNCHANGED << phase,
                    stateFilePresent,
                    initRecorded,
                    installedArtifacts,
                    importedArtifacts,
                    recordedVersion,
                    recordedImported,
                    snapshotRecordedVersion,
                    snapshotRecordedImported,
                    snapshotStateFilePresent,
                    snapshotInstalledArtifacts,
                    snapshotImportedArtifacts >>

Bootstrap(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in Harnesses
    /\ installedArtifacts' = installedArtifacts \cup {h}
    /\ recordedVersion' = [recordedVersion EXCEPT ![h] = HarnessVersion[h]]
    /\ stateFilePresent' = TRUE
    /\ lastAction' = "bootstrap"
    /\ UNCHANGED << phase,
                    coreReady,
                    initRecorded,
                    importedArtifacts,
                    recordedImported,
                    snapshotRecordedVersion,
                    snapshotRecordedImported,
                    snapshotStateFilePresent,
                    snapshotInstalledArtifacts,
                    snapshotImportedArtifacts >>

BootstrapDryRun(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in Harnesses
    /\ lastAction' = "bootstrap-dry"
    /\ snapshotRecordedVersion' = recordedVersion
    /\ snapshotRecordedImported' = recordedImported
    /\ snapshotStateFilePresent' = stateFilePresent
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts
    /\ UNCHANGED << phase,
                    coreReady,
                    stateFilePresent,
                    initRecorded,
                    installedArtifacts,
                    importedArtifacts,
                    recordedVersion,
                    recordedImported >>

ImportBasics(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in ImportableHarnesses
    /\ importedArtifacts' = importedArtifacts \cup {h}
    /\ recordedVersion' = [recordedVersion EXCEPT ![h] = HarnessVersion[h]]
    /\ recordedImported' = recordedImported \cup {h}
    /\ stateFilePresent' = TRUE
    /\ lastAction' = "import"
    /\ UNCHANGED << phase,
                    coreReady,
                    initRecorded,
                    installedArtifacts,
                    snapshotRecordedVersion,
                    snapshotRecordedImported,
                    snapshotStateFilePresent,
                    snapshotInstalledArtifacts,
                    snapshotImportedArtifacts >>

ImportDryRun(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in ImportableHarnesses
    /\ lastAction' = "import-dry"
    /\ snapshotRecordedVersion' = recordedVersion
    /\ snapshotRecordedImported' = recordedImported
    /\ snapshotStateFilePresent' = stateFilePresent
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts
    /\ UNCHANGED << phase,
                    coreReady,
                    stateFilePresent,
                    initRecorded,
                    installedArtifacts,
                    importedArtifacts,
                    recordedVersion,
                    recordedImported >>

SaveCoreState ==
    /\ phase = "idle"
    /\ coreReady
    /\ snapshotRecordedVersion' = recordedVersion
    /\ snapshotRecordedImported' = recordedImported
    /\ snapshotStateFilePresent' = stateFilePresent
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts
    /\ stateFilePresent' = TRUE
    /\ initRecorded' = TRUE
    /\ lastAction' = "save"
    /\ UNCHANGED << phase,
                    coreReady,
                    installedArtifacts,
                    importedArtifacts,
                    recordedVersion,
                    recordedImported >>

RollbackKeepUser ==
    /\ phase = "idle"
    /\ phase' = "rolledBack"
    /\ coreReady' = FALSE
    /\ stateFilePresent' = FALSE
    /\ initRecorded' = FALSE
    /\ recordedVersion' = EmptyRecordedVersion
    /\ recordedImported' = {}
    /\ lastAction' = "rollback-keep-user"
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts
    /\ UNCHANGED << installedArtifacts,
                    importedArtifacts,
                    snapshotRecordedVersion,
                    snapshotRecordedImported,
                    snapshotStateFilePresent >>

RollbackDeleteUser ==
    /\ phase = "idle"
    /\ phase' = "rolledBack"
    /\ coreReady' = FALSE
    /\ stateFilePresent' = FALSE
    /\ initRecorded' = FALSE
    /\ installedArtifacts' = {}
    /\ importedArtifacts' = {}
    /\ recordedVersion' = EmptyRecordedVersion
    /\ recordedImported' = {}
    /\ lastAction' = "rollback-delete-user"
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts
    /\ UNCHANGED << snapshotRecordedVersion,
                    snapshotRecordedImported,
                    snapshotStateFilePresent >>

Stutter ==
    UNCHANGED vars

Next ==
    \/ EnableCore
    \/ \E h \in Harnesses : Bootstrap(h)
    \/ \E h \in Harnesses : BootstrapDryRun(h)
    \/ \E h \in ImportableHarnesses : ImportBasics(h)
    \/ \E h \in ImportableHarnesses : ImportDryRun(h)
    \/ SaveCoreState
    \/ RollbackKeepUser
    \/ RollbackDeleteUser
    \/ Stutter

Spec ==
    Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in Phases
    /\ coreReady \in BOOLEAN
    /\ stateFilePresent \in BOOLEAN
    /\ initRecorded \in BOOLEAN
    /\ installedArtifacts \subseteq Harnesses
    /\ importedArtifacts \subseteq ImportableHarnesses
    /\ recordedVersion \in [Harnesses -> {"", "1"}]
    /\ recordedImported \subseteq ImportableHarnesses
    /\ lastAction \in ActionKinds
    /\ snapshotRecordedVersion \in [Harnesses -> {"", "1"}]
    /\ snapshotRecordedImported \subseteq ImportableHarnesses
    /\ snapshotStateFilePresent \in BOOLEAN
    /\ snapshotInstalledArtifacts \subseteq Harnesses
    /\ snapshotImportedArtifacts \subseteq ImportableHarnesses

RecordedHarnessVersionsMatchSpec ==
    \A h \in Harnesses :
        recordedVersion[h] = "" \/ recordedVersion[h] = HarnessVersion[h]

ImportedMetadataCarriesVersion ==
    \A h \in recordedImported : recordedVersion[h] = HarnessVersion[h]

StateFilePresentWhenMetadataExists ==
    (initRecorded
        \/ recordedImported # {}
        \/ (\E h \in Harnesses : recordedVersion[h] # ""))
        => stateFilePresent

DryRunLeavesStateUntouched ==
    lastAction \in {"bootstrap-dry", "import-dry"} =>
        /\ recordedVersion = snapshotRecordedVersion
        /\ recordedImported = snapshotRecordedImported
        /\ stateFilePresent = snapshotStateFilePresent
        /\ installedArtifacts = snapshotInstalledArtifacts
        /\ importedArtifacts = snapshotImportedArtifacts

SaveCoreStatePreservesHarnessMetadata ==
    lastAction = "save" =>
        /\ recordedVersion = snapshotRecordedVersion
        /\ recordedImported = snapshotRecordedImported
        /\ installedArtifacts = snapshotInstalledArtifacts
        /\ importedArtifacts = snapshotImportedArtifacts

RollbackClearsMetadata ==
    phase = "rolledBack" =>
        /\ ~stateFilePresent
        /\ ~initRecorded
        /\ recordedImported = {}
        /\ \A h \in Harnesses : recordedVersion[h] = ""

RollbackWithoutDeleteUserPreservesArtifacts ==
    lastAction = "rollback-keep-user" =>
        /\ phase = "rolledBack"
        /\ installedArtifacts = snapshotInstalledArtifacts
        /\ importedArtifacts = snapshotImportedArtifacts

RollbackDeleteUserRemovesArtifacts ==
    lastAction = "rollback-delete-user" =>
        /\ phase = "rolledBack"
        /\ installedArtifacts = {}
        /\ importedArtifacts = {}

=============================================================================

----------------------------- MODULE MC_HarnessLifecycle -----------------------------

EXTENDS TLC

\* Hazmat now records per-harness metadata under ~/.hazmat/state.json while the
\* actual harness files live in the agent home. This model focuses on the
\* lifecycle contract the Go code currently implements:
\*   - successful bootstrap/import writes harness metadata unless the command is
\*     a dry-run
\*   - core saveState preserves existing harness metadata
\*   - explicit harness uninstall removes harness code artifacts and host-owned
\*     harness metadata, but preserves imported/profile state by default
\*   - rollback always removes the host-owned state.json metadata
\*   - rollback only removes agent-home harness artifacts when --delete-user is used

Harnesses == {"claude", "codex", "opencode", "gemini", "hermes", "qwen", "cursor-agent", "pi"}
ImportableHarnesses == {"claude", "codex", "opencode", "gemini"}
HarnessVersion == [h \in Harnesses |-> "1"]
ActionKinds ==
    {"none",
     "enable-core",
     "bootstrap",
     "bootstrap-dry",
     "import",
     "import-dry",
     "uninstall",
     "uninstall-dry",
     "save",
     "rollback-keep-user",
     "rollback-delete-user"}
Phases == {"idle", "rolledBack"}
NoHarness == "none"

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
    lastHarness,
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
       lastHarness,
       snapshotRecordedVersion,
       snapshotRecordedImported,
       snapshotStateFilePresent,
       snapshotInstalledArtifacts,
       snapshotImportedArtifacts >>

EmptyRecordedVersion ==
    [h \in Harnesses |-> ""]

ClearSnapshots ==
    /\ snapshotRecordedVersion' = EmptyRecordedVersion
    /\ snapshotRecordedImported' = {}
    /\ snapshotStateFilePresent' = FALSE
    /\ snapshotInstalledArtifacts' = {}
    /\ snapshotImportedArtifacts' = {}

MetadataPresentAfterUninstall(h) ==
    \/ initRecorded
    \/ (recordedImported \ {h}) # {}
    \/ \E other \in Harnesses \ {h} : recordedVersion[other] # ""

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
    /\ lastHarness = NoHarness
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
    /\ lastHarness' = NoHarness
    /\ ClearSnapshots
    /\ UNCHANGED << phase,
                    stateFilePresent,
                    initRecorded,
                    installedArtifacts,
                    importedArtifacts,
                    recordedVersion,
                    recordedImported >>

Bootstrap(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in Harnesses
    /\ installedArtifacts' = installedArtifacts \cup {h}
    /\ recordedVersion' = [recordedVersion EXCEPT ![h] = HarnessVersion[h]]
    /\ stateFilePresent' = TRUE
    /\ lastAction' = "bootstrap"
    /\ lastHarness' = h
    /\ ClearSnapshots
    /\ UNCHANGED << phase,
                    coreReady,
                    initRecorded,
                    importedArtifacts,
                    recordedImported >>

BootstrapDryRun(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in Harnesses
    /\ lastAction' = "bootstrap-dry"
    /\ lastHarness' = h
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
    /\ lastHarness' = h
    /\ ClearSnapshots
    /\ UNCHANGED << phase,
                    coreReady,
                    initRecorded,
                    installedArtifacts >>

ImportDryRun(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in ImportableHarnesses
    /\ lastAction' = "import-dry"
    /\ lastHarness' = h
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

Uninstall(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in Harnesses
    /\ snapshotRecordedVersion' = recordedVersion
    /\ snapshotRecordedImported' = recordedImported
    /\ snapshotStateFilePresent' = stateFilePresent
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts
    /\ installedArtifacts' = installedArtifacts \ {h}
    /\ importedArtifacts' = importedArtifacts
    /\ recordedVersion' = [recordedVersion EXCEPT ![h] = ""]
    /\ recordedImported' = recordedImported \ {h}
    /\ stateFilePresent' = MetadataPresentAfterUninstall(h)
    /\ lastAction' = "uninstall"
    /\ lastHarness' = h
    /\ UNCHANGED << phase,
                    coreReady,
                    initRecorded >>

UninstallDryRun(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in Harnesses
    /\ lastAction' = "uninstall-dry"
    /\ lastHarness' = h
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
    /\ lastHarness' = NoHarness
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
    /\ lastHarness' = NoHarness
    /\ snapshotRecordedVersion' = EmptyRecordedVersion
    /\ snapshotRecordedImported' = {}
    /\ snapshotStateFilePresent' = FALSE
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts
    /\ UNCHANGED << installedArtifacts,
                    importedArtifacts >>

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
    /\ lastHarness' = NoHarness
    /\ snapshotRecordedVersion' = EmptyRecordedVersion
    /\ snapshotRecordedImported' = {}
    /\ snapshotStateFilePresent' = FALSE
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts

Stutter ==
    UNCHANGED vars

Next ==
    \/ EnableCore
    \/ \E h \in Harnesses : Bootstrap(h)
    \/ \E h \in Harnesses : BootstrapDryRun(h)
    \/ \E h \in ImportableHarnesses : ImportBasics(h)
    \/ \E h \in ImportableHarnesses : ImportDryRun(h)
    \/ \E h \in Harnesses : Uninstall(h)
    \/ \E h \in Harnesses : UninstallDryRun(h)
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
    /\ lastHarness \in Harnesses \cup {NoHarness}
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
    lastAction \in {"bootstrap-dry", "import-dry", "uninstall-dry"} =>
        /\ recordedVersion = snapshotRecordedVersion
        /\ recordedImported = snapshotRecordedImported
        /\ stateFilePresent = snapshotStateFilePresent
        /\ installedArtifacts = snapshotInstalledArtifacts
        /\ importedArtifacts = snapshotImportedArtifacts

UninstallRemovesOnlyCodeAndMetadata ==
    lastAction = "uninstall" =>
        /\ lastHarness \in Harnesses
        /\ installedArtifacts = snapshotInstalledArtifacts \ {lastHarness}
        /\ importedArtifacts = snapshotImportedArtifacts
        /\ recordedVersion = [snapshotRecordedVersion EXCEPT ![lastHarness] = ""]
        /\ recordedImported = snapshotRecordedImported \ {lastHarness}
        /\ stateFilePresent =
            (\/ initRecorded
             \/ recordedImported # {}
             \/ \E h \in Harnesses : recordedVersion[h] # "")

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

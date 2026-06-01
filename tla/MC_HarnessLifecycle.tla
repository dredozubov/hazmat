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
\*   - harness-specific profile reset may remove managed profile state without
\*     removing installed/imported harness artifacts or host-owned metadata

Harnesses == {"claude", "codex", "opencode", "gemini", "hermes"}
ImportableHarnesses == {"claude", "codex", "opencode", "gemini"}
ManagedProfileHarnesses == {"hermes"}
HarnessVersion == [h \in Harnesses |-> "1"]
ActionKinds ==
    {"none",
     "enable-core",
     "bootstrap",
     "bootstrap-dry",
     "import",
     "import-dry",
     "profile-create",
     "profile-reset",
     "profile-reset-dry",
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
    managedProfileArtifacts,
    recordedVersion,
    recordedImported,
    lastAction,
    snapshotRecordedVersion,
    snapshotRecordedImported,
    snapshotStateFilePresent,
    snapshotInstalledArtifacts,
    snapshotImportedArtifacts,
    snapshotManagedProfileArtifacts

vars ==
    << phase,
       coreReady,
       stateFilePresent,
       initRecorded,
       installedArtifacts,
       importedArtifacts,
       managedProfileArtifacts,
       recordedVersion,
       recordedImported,
       lastAction,
       snapshotRecordedVersion,
       snapshotRecordedImported,
       snapshotStateFilePresent,
       snapshotInstalledArtifacts,
       snapshotImportedArtifacts,
       snapshotManagedProfileArtifacts >>

EmptyRecordedVersion ==
    [h \in Harnesses |-> ""]

Init ==
    /\ phase = "idle"
    /\ coreReady \in BOOLEAN
    /\ stateFilePresent = FALSE
    /\ initRecorded = FALSE
    /\ installedArtifacts = {}
    /\ importedArtifacts = {}
    /\ managedProfileArtifacts = {}
    /\ recordedVersion = EmptyRecordedVersion
    /\ recordedImported = {}
    /\ lastAction = "none"
    /\ snapshotRecordedVersion = EmptyRecordedVersion
    /\ snapshotRecordedImported = {}
    /\ snapshotStateFilePresent = FALSE
    /\ snapshotInstalledArtifacts = {}
    /\ snapshotImportedArtifacts = {}
    /\ snapshotManagedProfileArtifacts = {}

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
                    managedProfileArtifacts,
                    recordedVersion,
                    recordedImported,
                    snapshotRecordedVersion,
                    snapshotRecordedImported,
                    snapshotStateFilePresent,
                    snapshotInstalledArtifacts,
                    snapshotImportedArtifacts,
                    snapshotManagedProfileArtifacts >>

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
                    managedProfileArtifacts,
                    recordedImported,
                    snapshotRecordedVersion,
                    snapshotRecordedImported,
                    snapshotStateFilePresent,
                    snapshotInstalledArtifacts,
                    snapshotImportedArtifacts,
                    snapshotManagedProfileArtifacts >>

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
    /\ snapshotManagedProfileArtifacts' = managedProfileArtifacts
    /\ UNCHANGED << phase,
                    coreReady,
                    stateFilePresent,
                    initRecorded,
                    installedArtifacts,
                    importedArtifacts,
                    managedProfileArtifacts,
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
                    managedProfileArtifacts,
                    snapshotRecordedVersion,
                    snapshotRecordedImported,
                    snapshotStateFilePresent,
                    snapshotInstalledArtifacts,
                    snapshotImportedArtifacts,
                    snapshotManagedProfileArtifacts >>

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
    /\ snapshotManagedProfileArtifacts' = managedProfileArtifacts
    /\ UNCHANGED << phase,
                    coreReady,
                    stateFilePresent,
                    initRecorded,
                    installedArtifacts,
                    importedArtifacts,
                    managedProfileArtifacts,
                    recordedVersion,
                    recordedImported >>

CreateManagedProfile(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in ManagedProfileHarnesses
    /\ managedProfileArtifacts' = managedProfileArtifacts \cup {h}
    /\ lastAction' = "profile-create"
    /\ UNCHANGED << phase,
                    coreReady,
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
                    snapshotImportedArtifacts,
                    snapshotManagedProfileArtifacts >>

ResetManagedProfile(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in ManagedProfileHarnesses
    /\ snapshotRecordedVersion' = recordedVersion
    /\ snapshotRecordedImported' = recordedImported
    /\ snapshotStateFilePresent' = stateFilePresent
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts
    /\ snapshotManagedProfileArtifacts' = managedProfileArtifacts
    /\ managedProfileArtifacts' = managedProfileArtifacts \ {h}
    /\ lastAction' = "profile-reset"
    /\ UNCHANGED << phase,
                    coreReady,
                    initRecorded,
                    installedArtifacts,
                    importedArtifacts,
                    recordedVersion,
                    recordedImported,
                    stateFilePresent >>

ResetManagedProfileDryRun(h) ==
    /\ phase = "idle"
    /\ coreReady
    /\ h \in ManagedProfileHarnesses
    /\ lastAction' = "profile-reset-dry"
    /\ snapshotRecordedVersion' = recordedVersion
    /\ snapshotRecordedImported' = recordedImported
    /\ snapshotStateFilePresent' = stateFilePresent
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts
    /\ snapshotManagedProfileArtifacts' = managedProfileArtifacts
    /\ UNCHANGED << phase,
                    coreReady,
                    stateFilePresent,
                    initRecorded,
                    installedArtifacts,
                    importedArtifacts,
                    managedProfileArtifacts,
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
    /\ snapshotManagedProfileArtifacts' = managedProfileArtifacts
    /\ stateFilePresent' = TRUE
    /\ initRecorded' = TRUE
    /\ lastAction' = "save"
    /\ UNCHANGED << phase,
                    coreReady,
                    installedArtifacts,
                    importedArtifacts,
                    managedProfileArtifacts,
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
    /\ snapshotManagedProfileArtifacts' = managedProfileArtifacts
    /\ UNCHANGED << installedArtifacts,
                    importedArtifacts,
                    managedProfileArtifacts,
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
    /\ managedProfileArtifacts' = {}
    /\ recordedVersion' = EmptyRecordedVersion
    /\ recordedImported' = {}
    /\ lastAction' = "rollback-delete-user"
    /\ snapshotInstalledArtifacts' = installedArtifacts
    /\ snapshotImportedArtifacts' = importedArtifacts
    /\ snapshotManagedProfileArtifacts' = managedProfileArtifacts
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
    \/ \E h \in ManagedProfileHarnesses : CreateManagedProfile(h)
    \/ \E h \in ManagedProfileHarnesses : ResetManagedProfile(h)
    \/ \E h \in ManagedProfileHarnesses : ResetManagedProfileDryRun(h)
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
    /\ managedProfileArtifacts \subseteq ManagedProfileHarnesses
    /\ recordedVersion \in [Harnesses -> {"", "1"}]
    /\ recordedImported \subseteq ImportableHarnesses
    /\ lastAction \in ActionKinds
    /\ snapshotRecordedVersion \in [Harnesses -> {"", "1"}]
    /\ snapshotRecordedImported \subseteq ImportableHarnesses
    /\ snapshotStateFilePresent \in BOOLEAN
    /\ snapshotInstalledArtifacts \subseteq Harnesses
    /\ snapshotImportedArtifacts \subseteq ImportableHarnesses
    /\ snapshotManagedProfileArtifacts \subseteq ManagedProfileHarnesses

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
    lastAction \in {"bootstrap-dry", "import-dry", "profile-reset-dry"} =>
        /\ recordedVersion = snapshotRecordedVersion
        /\ recordedImported = snapshotRecordedImported
        /\ stateFilePresent = snapshotStateFilePresent
        /\ installedArtifacts = snapshotInstalledArtifacts
        /\ importedArtifacts = snapshotImportedArtifacts
        /\ managedProfileArtifacts = snapshotManagedProfileArtifacts

SaveCoreStatePreservesHarnessMetadata ==
    lastAction = "save" =>
        /\ recordedVersion = snapshotRecordedVersion
        /\ recordedImported = snapshotRecordedImported
        /\ installedArtifacts = snapshotInstalledArtifacts
        /\ importedArtifacts = snapshotImportedArtifacts
        /\ managedProfileArtifacts = snapshotManagedProfileArtifacts

ProfileResetPreservesHarnessMetadata ==
    lastAction = "profile-reset" =>
        /\ recordedVersion = snapshotRecordedVersion
        /\ recordedImported = snapshotRecordedImported
        /\ stateFilePresent = snapshotStateFilePresent
        /\ installedArtifacts = snapshotInstalledArtifacts
        /\ importedArtifacts = snapshotImportedArtifacts

ProfileResetRemovesOnlyManagedProfile ==
    lastAction = "profile-reset" =>
        managedProfileArtifacts = snapshotManagedProfileArtifacts \ ManagedProfileHarnesses

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
        /\ managedProfileArtifacts = snapshotManagedProfileArtifacts

RollbackDeleteUserRemovesArtifacts ==
    lastAction = "rollback-delete-user" =>
        /\ phase = "rolledBack"
        /\ installedArtifacts = {}
        /\ importedArtifacts = {}
        /\ managedProfileArtifacts = {}

=============================================================================

---- MODULE MC_AppleContainerLaunch ----
\* Apple Container backend host-side launch containment (design model).
\*
\* This spec models the launch contract for the planned `apple-container`
\* backend (docs/plans/2026-06-10-apple-container-backend-design.md):
\*   1. Reject integration env passthrough, SSH agent forwarding, and
\*      socket publishing before any other work
\*   2. Validate and plan mounts: reject credential deny paths and parents,
\*      omit covered read-only grants
\*   3. Require host admission (macOS 26 Apple silicon, healthy CLI as the
\*      `agent` user, approved image) before launch
\*   4. Fail closed on network policies the backend cannot enforce
\*   5. Materialize credential artifacts session-scoped, only after
\*      admission, and clean them up or record the cleanup failure
\*   6. Remove the session container on exit or record the cleanup failure;
\*      never touch containers Hazmat does not own
\*
\* It does NOT model Apple Container VM internals, VirtioFS ownership
\* mapping, guest-side processes, image contents, or `container machine`.
\* The scope is the host-side launch boundary only.
\*
\* Governed code (future): containment/applecontainer compiler and
\* internal/runtime/applecontainer runtime. No executable launch code may
\* land before this model is kept green.

EXTENDS Naturals, FiniteSets

CONSTANTS
    Paths,
    CredentialLeaves,
    ProjectChoices,
    ReadChoices,
    NetworkModes,
    SupportedNetworkModes,
    workspaceRoot,
    projectRoot,
    projectSub,
    safeRef,
    safeRefChild,
    invokerHome,
    sshDir,
    awsDir,
    agentHome,
    agentSecretsDir

ASSUME CredentialLeaves \subseteq Paths
ASSUME ProjectChoices \subseteq Paths
ASSUME ReadChoices \subseteq Paths
ASSUME SupportedNetworkModes \subseteq NetworkModes

Contains(child, parent) ==
    \/ child = parent
    \/ (child = projectRoot /\ parent = workspaceRoot)
    \/ (child = projectSub /\ parent = projectRoot)
    \/ (child = projectSub /\ parent = workspaceRoot)
    \/ (child = safeRefChild /\ parent = safeRef)
    \/ (child = sshDir /\ parent = invokerHome)
    \/ (child = awsDir /\ parent = invokerHome)
    \/ (child = agentSecretsDir /\ parent = agentHome)

\* A path is unsafe to mount if it is itself a credential path or a parent
\* of one. invokerHome and agentHome are parents of credential leaves, so
\* mounting either wholesale is rejected by construction.
IsCredentialDenyPath(p) ==
    \E cred \in CredentialLeaves : Contains(cred, p)

PlannedReadDirs(project, dirs) ==
    {d \in dirs :
        /\ ~IsCredentialDenyPath(d)
        /\ ~Contains(d, project)
        /\ ~(\E other \in dirs : other /= d /\ ~IsCredentialDenyPath(other) /\ Contains(d, other))}

Mount(path, mode) == [path |-> path, mode |-> mode]

VARIABLES
    projectDir,
    readDirs,
    integrationEnvRequested,
    sshForwardRequested,
    socketPublishRequested,
    credDeliveryRequested,
    networkMode,
    hostAdmitted,
    phase,
    mounts,
    launchEnv,
    credFileMaterialized,
    credFileScope,
    credFileRemoved,
    containerCreated,
    containerRemoved,
    cleanupFailureRecorded,
    exitKind,
    launched,
    failed,
    foreignContainer,
    genesisForeign

vars ==
    <<projectDir, readDirs, integrationEnvRequested, sshForwardRequested,
      socketPublishRequested, credDeliveryRequested, networkMode,
      hostAdmitted, phase, mounts, launchEnv, credFileMaterialized,
      credFileScope, credFileRemoved, containerCreated, containerRemoved,
      cleanupFailureRecorded, exitKind, launched, failed,
      foreignContainer, genesisForeign>>

requestVars ==
    <<projectDir, readDirs, integrationEnvRequested, sshForwardRequested,
      socketPublishRequested, credDeliveryRequested, networkMode,
      hostAdmitted>>

foreignVars == <<foreignContainer, genesisForeign>>

TypeOK ==
    /\ projectDir \in Paths
    /\ readDirs \subseteq Paths
    /\ integrationEnvRequested \in BOOLEAN
    /\ sshForwardRequested \in BOOLEAN
    /\ socketPublishRequested \in BOOLEAN
    /\ credDeliveryRequested \in BOOLEAN
    /\ networkMode \in NetworkModes
    /\ hostAdmitted \in BOOLEAN
    /\ phase \in 0..9
    /\ mounts \subseteq [path : Paths, mode : {"ro", "rw"}]
    /\ launchEnv = {}
    /\ credFileMaterialized \in BOOLEAN
    /\ credFileScope \in {"none", "session"}
    /\ credFileRemoved \in BOOLEAN
    /\ containerCreated \in BOOLEAN
    /\ containerRemoved \in BOOLEAN
    /\ cleanupFailureRecorded \in BOOLEAN
    /\ exitKind \in {"none", "normal", "interrupted"}
    /\ launched \in BOOLEAN
    /\ failed \in BOOLEAN
    /\ foreignContainer \in BOOLEAN
    /\ genesisForeign \in BOOLEAN

Init ==
    /\ projectDir \in ProjectChoices
    /\ readDirs \in SUBSET ReadChoices
    /\ integrationEnvRequested \in BOOLEAN
    /\ sshForwardRequested \in BOOLEAN
    /\ socketPublishRequested \in BOOLEAN
    /\ credDeliveryRequested \in BOOLEAN
    /\ networkMode \in NetworkModes
    /\ hostAdmitted \in BOOLEAN
    /\ phase = 0
    /\ mounts = {}
    /\ launchEnv = {}
    /\ credFileMaterialized = FALSE
    /\ credFileScope = "none"
    /\ credFileRemoved = FALSE
    /\ containerCreated = FALSE
    /\ containerRemoved = FALSE
    /\ cleanupFailureRecorded = FALSE
    /\ exitKind = "none"
    /\ launched = FALSE
    /\ failed = FALSE
    /\ foreignContainer \in BOOLEAN
    /\ genesisForeign = foreignContainer

ForbiddenFeatureRequested ==
    \/ integrationEnvRequested
    \/ sshForwardRequested
    \/ socketPublishRequested

\* Phase 0: forbidden launch features are rejected before any other work.
RejectForbiddenFeature ==
    /\ phase = 0
    /\ ForbiddenFeatureRequested
    /\ failed' = TRUE
    /\ phase' = 9
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, foreignVars>>

AcceptFeatureGate ==
    /\ phase = 0
    /\ ~ForbiddenFeatureRequested
    /\ phase' = 1
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, failed, foreignVars>>

\* Phase 1: mount inputs are validated at compile time. Credential deny
\* paths and parents of credential paths are rejected outright.
MountInputsPass ==
    /\ phase = 1
    /\ ~IsCredentialDenyPath(projectDir)
    /\ \A d \in readDirs : ~IsCredentialDenyPath(d)
    /\ phase' = 2
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, failed, foreignVars>>

MountInputsFail ==
    /\ phase = 1
    /\ (IsCredentialDenyPath(projectDir) \/ (\E d \in readDirs : IsCredentialDenyPath(d)))
    /\ failed' = TRUE
    /\ phase' = 9
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, foreignVars>>

\* Phase 2: host admission. hostAdmitted abstracts the full admission
\* conjunction: macOS 26+, Apple silicon, approved CLI path, healthy API
\* server, supported CLI version, runnable as the `agent` user, and an
\* explicit policy-approved image.
AdmissionPass ==
    /\ phase = 2
    /\ hostAdmitted
    /\ phase' = 3
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, failed, foreignVars>>

AdmissionFail ==
    /\ phase = 2
    /\ ~hostAdmitted
    /\ failed' = TRUE
    /\ phase' = 9
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, foreignVars>>

\* Phase 3: network policy enforceability. Anything the backend cannot
\* honestly enforce fails closed before launch.
NetworkPolicyPass ==
    /\ phase = 3
    /\ networkMode \in SupportedNetworkModes
    /\ phase' = 4
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, failed, foreignVars>>

NetworkPolicyFail ==
    /\ phase = 3
    /\ networkMode \notin SupportedNetworkModes
    /\ failed' = TRUE
    /\ phase' = 9
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, foreignVars>>

\* Phase 4: credential materialization. Generated env/secret files are
\* session-scoped by construction and exist only after admission passed.
MaterializeCredFile ==
    /\ phase = 4
    /\ credDeliveryRequested
    /\ credFileMaterialized' = TRUE
    /\ credFileScope' = "session"
    /\ phase' = 5
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileRemoved,
                   containerCreated, containerRemoved,
                   cleanupFailureRecorded, exitKind, launched, failed,
                   foreignVars>>

SkipCredFile ==
    /\ phase = 4
    /\ ~credDeliveryRequested
    /\ phase' = 5
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, failed, foreignVars>>

\* Phase 5: start the named session container with the planned mounts.
StartContainer ==
    /\ phase = 5
    /\ mounts' =
        {Mount(projectDir, "rw")} \cup
        {Mount(d, "ro") : d \in PlannedReadDirs(projectDir, readDirs)}
    /\ containerCreated' = TRUE
    /\ launched' = TRUE
    /\ phase' = 6
    /\ UNCHANGED <<requestVars, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerRemoved,
                   cleanupFailureRecorded, exitKind, failed, foreignVars>>

\* `container run` can fail without leaving a container behind...
StartFailNoResidue ==
    /\ phase = 5
    /\ failed' = TRUE
    /\ phase' = 7
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, foreignVars>>

\* ...or fail after creating the container record. Either way the cleanup
\* chain still runs, so generated credential files never outlive the
\* session silently.
StartFailWithResidue ==
    /\ phase = 5
    /\ containerCreated' = TRUE
    /\ failed' = TRUE
    /\ phase' = 7
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerRemoved,
                   cleanupFailureRecorded, exitKind, launched, foreignVars>>

\* Phase 6: the session runs and exits, normally or interrupted. Both exit
\* kinds enter the same cleanup chain.
SessionExit ==
    /\ phase = 6
    /\ exitKind' \in {"normal", "interrupted"}
    /\ phase' = 7
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, launched,
                   failed, foreignVars>>

\* Phase 7: remove the session container by exact name, or record the
\* cleanup failure in session metadata. Foreign containers are untouched.
CleanupContainerSucceed ==
    /\ phase = 7
    /\ containerCreated
    /\ containerRemoved' = TRUE
    /\ phase' = 8
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   cleanupFailureRecorded, exitKind, launched, failed,
                   foreignVars>>

CleanupContainerFail ==
    /\ phase = 7
    /\ containerCreated
    /\ cleanupFailureRecorded' = TRUE
    /\ phase' = 8
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, exitKind, launched, failed,
                   foreignVars>>

CleanupContainerSkip ==
    /\ phase = 7
    /\ ~containerCreated
    /\ phase' = 8
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, failed, foreignVars>>

\* Phase 8: remove the generated credential file, or record the failure.
CleanupCredFileSucceed ==
    /\ phase = 8
    /\ credFileMaterialized
    /\ credFileRemoved' = TRUE
    /\ phase' = 9
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, containerCreated, containerRemoved,
                   cleanupFailureRecorded, exitKind, launched, failed,
                   foreignVars>>

CleanupCredFileFail ==
    /\ phase = 8
    /\ credFileMaterialized
    /\ cleanupFailureRecorded' = TRUE
    /\ phase' = 9
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, exitKind, launched, failed,
                   foreignVars>>

CleanupCredFileSkip ==
    /\ phase = 8
    /\ ~credFileMaterialized
    /\ phase' = 9
    /\ UNCHANGED <<requestVars, mounts, launchEnv, credFileMaterialized,
                   credFileScope, credFileRemoved, containerCreated,
                   containerRemoved, cleanupFailureRecorded, exitKind,
                   launched, failed, foreignVars>>

Done ==
    /\ phase = 9
    /\ UNCHANGED vars

Next ==
    \/ RejectForbiddenFeature
    \/ AcceptFeatureGate
    \/ MountInputsPass
    \/ MountInputsFail
    \/ AdmissionPass
    \/ AdmissionFail
    \/ NetworkPolicyPass
    \/ NetworkPolicyFail
    \/ MaterializeCredFile
    \/ SkipCredFile
    \/ StartContainer
    \/ StartFailNoResidue
    \/ StartFailWithResidue
    \/ SessionExit
    \/ CleanupContainerSucceed
    \/ CleanupContainerFail
    \/ CleanupContainerSkip
    \/ CleanupCredFileSucceed
    \/ CleanupCredFileFail
    \/ CleanupCredFileSkip
    \/ Done

Spec == Init /\ [][Next]_vars

\* Safety invariants.

\* Credential deny paths and their parents are never part of the mount plan.
CredentialPathsNeverMounted ==
    \A m \in mounts : ~IsCredentialDenyPath(m.path)

\* The invoking user's home is never mounted, in any mode.
InvokerHomeNeverMounted ==
    \A m \in mounts : m.path /= invokerHome

\* The agent user's home is never mounted wholesale, in any mode.
AgentHomeNeverMountedWholesale ==
    \A m \in mounts : m.path /= agentHome

ProjectMountedRW ==
    launched =>
        Mount(projectDir, "rw") \in mounts

PlannedReadDirsMountedRO ==
    launched =>
        \A d \in PlannedReadDirs(projectDir, readDirs) :
            Mount(d, "ro") \in mounts

CoveredReadDirsOmitted ==
    launched =>
        \A d \in readDirs :
            IF IsCredentialDenyPath(d)
               \/ Contains(d, projectDir)
               \/ (\E other \in readDirs : other /= d /\ ~IsCredentialDenyPath(other) /\ Contains(d, other))
            THEN Mount(d, "ro") \notin mounts
            ELSE Mount(d, "ro") \in mounts

\* The launch carries no host shell environment or integration env.
NoUnexpectedLaunchEnv ==
    launched =>
        launchEnv = {}

IntegrationEnvRejected ==
    integrationEnvRequested => ~launched

SSHForwardingRejected ==
    sshForwardRequested => ~launched

SocketPublishingRejected ==
    socketPublishRequested => ~launched

\* Backend admission happens before launch.
AdmissionBeforeLaunch ==
    launched => hostAdmitted

\* Network policies the backend cannot enforce fail closed.
UnsupportedNetworkFailsClosed ==
    launched => networkMode \in SupportedNetworkModes

\* Credential artifacts exist only after admission and network gating, and
\* only when delivery was requested.
CredentialMaterializationGated ==
    credFileMaterialized =>
        /\ hostAdmitted
        /\ networkMode \in SupportedNetworkModes
        /\ credDeliveryRequested

\* Credential artifacts are session-scoped by construction.
CredentialArtifactSessionScoped ==
    credFileMaterialized => credFileScope = "session"

\* A terminal session never leaves a generated credential file behind
\* silently: it is removed, or the cleanup failure is recorded.
TerminalCredResidueHandled ==
    (phase = 9 /\ credFileMaterialized) =>
        (credFileRemoved \/ cleanupFailureRecorded)

\* A terminal session never leaves the session container behind silently.
TerminalContainerHandled ==
    (phase = 9 /\ containerCreated) =>
        (containerRemoved \/ cleanupFailureRecorded)

\* Cleanup is by exact session artifact name only. Containers Hazmat does
\* not own survive every action, including cleanup after failures.
ForeignContainersUntouched ==
    foreignContainer = genesisForeign

====

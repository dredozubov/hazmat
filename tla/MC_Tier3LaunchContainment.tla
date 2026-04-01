---- MODULE MC_Tier3LaunchContainment ----
\* Tier 3 host-side launch containment for Docker-capable sessions.
\*
\* This spec models Hazmat's launch contract in hazmat/sandbox.go:
\*   1. Reject stack-pack env passthrough
\*   2. Validate backend readiness (support, identity, health, profile)
\*   3. Reject credential deny paths from project/read-only mounts
\*   4. Filter covered read-only mounts
\*   5. Apply network policy before launch
\*
\* It does NOT model Docker Sandbox microVM internals, Dockerfile behavior,
\* or container-internal runtime semantics. The scope here is the host-side
\* launch boundary only.
\*
\* Governed code:
\*   hazmat/sandbox.go — buildSandboxLaunchSpec(), prepareSandboxLaunch(),
\*                        loadHealthySandboxLaunchBackend(),
\*                        dockerSandboxesBackend.PrepareLaunch()
\*   hazmat/pack.go    — isCredentialDenyPath()
\*   hazmat/session.go — isWithinDir()

EXTENDS Naturals, FiniteSets

CONSTANTS
    Paths,
    CredentialLeaves,
    ProjectChoices,
    ReadChoices,
    workspaceRoot,
    projectRoot,
    projectSub,
    safeRef,
    safeRefChild,
    invokerHome,
    sshDir,
    awsDir

ASSUME CredentialLeaves \subseteq Paths
ASSUME ProjectChoices \subseteq Paths
ASSUME ReadChoices \subseteq Paths

Contains(child, parent) ==
    \/ child = parent
    \/ (child = projectRoot /\ parent = workspaceRoot)
    \/ (child = projectSub /\ parent = projectRoot)
    \/ (child = projectSub /\ parent = workspaceRoot)
    \/ (child = safeRefChild /\ parent = safeRef)
    \/ (child = sshDir /\ parent = invokerHome)
    \/ (child = awsDir /\ parent = invokerHome)

\* A path is unsafe to mount if it is itself a credential path or a parent of one.
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
    agent,
    packEnvRequested,
    backendReady,
    approvalGranted,
    shellSupported,
    extraMountsSupported,
    phase,
    mounts,
    launchEnv,
    sandboxCreated,
    policyApplied,
    launched,
    failed

vars ==
    <<projectDir, readDirs, agent, packEnvRequested,
      backendReady,
      approvalGranted, shellSupported, extraMountsSupported,
      phase, mounts, launchEnv, sandboxCreated, policyApplied, launched, failed>>

TypeOK ==
    /\ projectDir \in Paths
    /\ readDirs \subseteq Paths
    /\ agent \in {"claude", "shell"}
    /\ packEnvRequested \in BOOLEAN
    /\ backendReady \in BOOLEAN
    /\ approvalGranted \in BOOLEAN
    /\ shellSupported \in BOOLEAN
    /\ extraMountsSupported \in BOOLEAN
    /\ phase \in 0..8
    /\ mounts \subseteq [path : Paths, mode : {"ro", "rw"}]
    /\ launchEnv = {}
    /\ sandboxCreated \in BOOLEAN
    /\ policyApplied \in BOOLEAN
    /\ launched \in BOOLEAN
    /\ failed \in BOOLEAN

Init ==
    /\ projectDir \in ProjectChoices
    /\ readDirs \in SUBSET ReadChoices
    /\ agent \in {"claude", "shell"}
    /\ packEnvRequested \in BOOLEAN
    /\ backendReady \in BOOLEAN
    /\ approvalGranted \in BOOLEAN
    /\ shellSupported \in BOOLEAN
    /\ extraMountsSupported \in BOOLEAN
    /\ phase = 0
    /\ mounts = {}
    /\ launchEnv = {}
    /\ sandboxCreated = FALSE
    /\ policyApplied = FALSE
    /\ launched = FALSE
    /\ failed = FALSE

RejectPackEnv ==
    /\ phase = 0
    /\ packEnvRequested
    /\ failed' = TRUE
    /\ phase' = 8
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, launched>>

AcceptNoPackEnv ==
    /\ phase = 0
    /\ ~packEnvRequested
    /\ phase' = 1
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, launched, failed>>

BackendChecksPass ==
    /\ phase = 1
    /\ backendReady
    /\ phase' = 2
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, launched, failed>>

BackendChecksFail ==
    /\ phase = 1
    /\ ~backendReady
    /\ failed' = TRUE
    /\ phase' = 8
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, launched>>

MountInputsPass ==
    /\ phase = 2
    /\ ~IsCredentialDenyPath(projectDir)
    /\ \A d \in readDirs : ~IsCredentialDenyPath(d)
    /\ phase' = 3
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, launched, failed>>

MountInputsFail ==
    /\ phase = 2
    /\ (IsCredentialDenyPath(projectDir) \/ (\E d \in readDirs : IsCredentialDenyPath(d)))
    /\ failed' = TRUE
    /\ phase' = 8
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, launched>>

CompatibilityPass ==
    /\ phase = 3
    /\ ~(agent = "shell" /\ ~shellSupported)
    /\ ~(PlannedReadDirs(projectDir, readDirs) # {} /\ ~extraMountsSupported)
    /\ phase' = 4
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, launched, failed>>

CompatibilityFail ==
    /\ phase = 3
    /\ ((agent = "shell" /\ ~shellSupported)
         \/ (PlannedReadDirs(projectDir, readDirs) # {} /\ ~extraMountsSupported))
    /\ failed' = TRUE
    /\ phase' = 8
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, launched>>

ApprovalPass ==
    /\ phase = 4
    /\ approvalGranted
    /\ phase' = 5
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, launched, failed>>

ApprovalFail ==
    /\ phase = 4
    /\ ~approvalGranted
    /\ failed' = TRUE
    /\ phase' = 8
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, launched>>

CreateSandbox ==
    /\ phase = 5
    /\ mounts' =
        {Mount(projectDir, "rw")} \cup
        {Mount(d, "ro") : d \in PlannedReadDirs(projectDir, readDirs)}
    /\ sandboxCreated' = TRUE
    /\ phase' = 6
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   launchEnv, policyApplied, launched, failed>>

ApplyPolicy ==
    /\ phase = 6
    /\ sandboxCreated
    /\ policyApplied' = TRUE
    /\ phase' = 7
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, launched, failed>>

LaunchAgent ==
    /\ phase = 7
    /\ policyApplied
    /\ launched' = TRUE
    /\ phase' = 8
    /\ UNCHANGED <<projectDir, readDirs, agent, packEnvRequested,
                   backendReady,
                   approvalGranted, shellSupported, extraMountsSupported,
                   mounts, launchEnv, sandboxCreated, policyApplied, failed>>

Done ==
    /\ phase = 8
    /\ UNCHANGED vars

Next ==
    \/ RejectPackEnv
    \/ AcceptNoPackEnv
    \/ BackendChecksPass
    \/ BackendChecksFail
    \/ MountInputsPass
    \/ MountInputsFail
    \/ CompatibilityPass
    \/ CompatibilityFail
    \/ ApprovalPass
    \/ ApprovalFail
    \/ CreateSandbox
    \/ ApplyPolicy
    \/ LaunchAgent
    \/ Done

Spec == Init /\ [][Next]_vars

\* Safety invariants.
CredentialPathsNeverMounted ==
    sandboxCreated =>
        \A m \in mounts : ~IsCredentialDenyPath(m.path)

ProjectMountedRW ==
    launched =>
        Mount(projectDir, "rw") \in mounts

PlannedReadDirsMountedRO ==
    launched =>
        \A d \in PlannedReadDirs(projectDir, readDirs) :
            Mount(d, "ro") \in mounts

CoveredReadDirsOmitted ==
    sandboxCreated =>
        \A d \in readDirs :
            IF IsCredentialDenyPath(d)
               \/ Contains(d, projectDir)
               \/ (\E other \in readDirs : other /= d /\ ~IsCredentialDenyPath(other) /\ Contains(d, other))
            THEN Mount(d, "ro") \notin mounts
            ELSE Mount(d, "ro") \in mounts

NoUnexpectedLaunchEnv ==
    launched =>
        launchEnv = {}

BackendValidationBeforeLaunch ==
    launched =>
        backendReady

PolicyBeforeLaunch ==
    launched => policyApplied

ApprovalBeforeLaunch ==
    launched => approvalGranted

PackEnvRejected ==
    packEnvRequested => ~launched

ShellVersionGate ==
    launched /\ agent = "shell" => shellSupported

ExtraWorkspaceVersionGate ==
    launched /\ PlannedReadDirs(projectDir, readDirs) # {} => extraMountsSupported

====

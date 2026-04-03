---- MODULE MC_TierPolicyEquivalence ----
\* Compare Tier 2 native containment and Tier 3 Docker Sandbox launches at the
\* level of an abstract session policy.
\*
\* This spec separates two claims:
\*   1. Exact backend policy identity
\*   2. A narrower backend-neutral core containment contract
\*
\* The exact-identity claim is intentionally too strong for the current product:
\* Tier 3 rejects integration env passthrough, keeps resume history sandbox-
\* local, and may rewrite ancestor inputs into sibling mounts. The useful claim
\* is that, after removing those intentional differences and backend-specific
\* launch gates, both tiers can still implement the same core project/read/write
\* containment contract.

EXTENDS Naturals, FiniteSets

CONSTANTS
    Paths,
    CredentialLeaves,
    ProjectChoices,
    ReadChoices,
    WriteChoices,
    workspaceRoot,
    projectRoot,
    projectSub,
    readRoot,
    readChild,
    writeRoot,
    writeChild,
    invokerHome,
    sshDir,
    awsDir,
    resumeHost

ASSUME CredentialLeaves \subseteq Paths
ASSUME ProjectChoices \subseteq Paths
ASSUME ReadChoices \subseteq Paths
ASSUME WriteChoices \subseteq Paths

Contains(child, parent) ==
    \/ child = parent
    \/ (child = projectRoot /\ parent = workspaceRoot)
    \/ (child = projectSub /\ parent = projectRoot)
    \/ (child = projectSub /\ parent = workspaceRoot)
    \/ (child = readChild /\ parent = readRoot)
    \/ (child = writeChild /\ parent = writeRoot)
    \/ (child = sshDir /\ parent = invokerHome)
    \/ (child = awsDir /\ parent = invokerHome)
    \/ (child = resumeHost /\ parent = invokerHome)

IsCredentialDenyPath(p) ==
    \E cred \in CredentialLeaves : Contains(cred, p)

NormalizedWriteRoots(project, writes) ==
    {d \in writes :
        /\ ~Contains(d, project)
        /\ ~IsCredentialDenyPath(d)
        /\ ~(\E other \in writes :
                /\ other /= d
                /\ ~IsCredentialDenyPath(other)
                /\ Contains(d, other))}

NormalizedReadRoots(project, reads, writes) ==
    {d \in reads :
        /\ ~Contains(d, project)
        /\ ~IsCredentialDenyPath(d)
        /\ ~(\E w \in writes :
                /\ ~IsCredentialDenyPath(w)
                /\ Contains(d, w))
        /\ ~(\E other \in reads :
                /\ other /= d
                /\ ~IsCredentialDenyPath(other)
                /\ Contains(d, other))}

VARIABLES
    projectDir,
    readDirs,
    writeDirs,
    integrationEnvRequested,
    resumeRequested,
    backendReady,
    approvalGranted,
    extraMountsSupported

vars ==
    <<projectDir, readDirs, writeDirs, integrationEnvRequested,
      resumeRequested, backendReady, approvalGranted, extraMountsSupported>>

TypeOK ==
    /\ projectDir \in Paths
    /\ readDirs \subseteq Paths
    /\ writeDirs \subseteq Paths
    /\ integrationEnvRequested \in BOOLEAN
    /\ resumeRequested \in BOOLEAN
    /\ backendReady \in BOOLEAN
    /\ approvalGranted \in BOOLEAN
    /\ extraMountsSupported \in BOOLEAN

Init ==
    /\ projectDir \in ProjectChoices
    /\ readDirs \in SUBSET ReadChoices
    /\ writeDirs \in SUBSET WriteChoices
    /\ integrationEnvRequested \in BOOLEAN
    /\ resumeRequested \in BOOLEAN
    /\ backendReady \in BOOLEAN
    /\ approvalGranted \in BOOLEAN
    /\ extraMountsSupported \in BOOLEAN

Next == UNCHANGED vars

Spec == Init /\ [][Next]_vars

HasCredentialInput ==
    \/ IsCredentialDenyPath(projectDir)
    \/ (\E d \in readDirs : IsCredentialDenyPath(d))
    \/ (\E d \in writeDirs : IsCredentialDenyPath(d))

Tier2ReadRoots ==
    NormalizedReadRoots(projectDir, readDirs, writeDirs)

Tier2WriteRoots ==
    NormalizedWriteRoots(projectDir, writeDirs)

Tier2LaunchAllowed ==
    ~HasCredentialInput

Tier2CorePolicy ==
    [launchAllowed |-> Tier2LaunchAllowed,
     roRoots       |-> IF Tier2LaunchAllowed THEN Tier2ReadRoots ELSE {},
     rwRoots       |-> IF Tier2LaunchAllowed THEN Tier2WriteRoots \cup {projectDir} ELSE {}]

\* Docker Sandbox rewrites some ancestor inputs instead of mounting them
\* directly. We only need to know when exact backend identity is impossible,
\* not to model the rewritten sibling set in detail.
AncestorRewriteNeeded ==
    \/ (\E d \in readDirs \cup writeDirs :
            /\ d /= projectDir
            /\ Contains(projectDir, d))
    \/ (\E r \in readDirs :
            \E w \in writeDirs :
                /\ r /= w
                /\ Contains(w, r))

Tier3ReadRoots ==
    NormalizedReadRoots(projectDir, readDirs, writeDirs)

Tier3WriteRoots ==
    NormalizedWriteRoots(projectDir, writeDirs)

Tier3ExtraMountsRequired ==
    \/ Tier3ReadRoots # {}
    \/ Tier3WriteRoots # {}

Tier3LaunchAllowed ==
    /\ ~HasCredentialInput
    /\ ~integrationEnvRequested
    /\ backendReady
    /\ approvalGranted
    /\ (extraMountsSupported \/ ~Tier3ExtraMountsRequired)

Tier3CorePolicy ==
    [launchAllowed |-> Tier3LaunchAllowed,
     roRoots       |-> IF Tier3LaunchAllowed THEN Tier3ReadRoots ELSE {},
     rwRoots       |-> IF Tier3LaunchAllowed THEN Tier3WriteRoots \cup {projectDir} ELSE {}]

Tier2ExactPolicy ==
    [launchAllowed           |-> Tier2LaunchAllowed,
     roRoots                 |-> IF Tier2LaunchAllowed THEN Tier2ReadRoots ELSE {},
     rwRoots                 |-> IF Tier2LaunchAllowed THEN Tier2WriteRoots \cup {projectDir} ELSE {},
     integrationEnvPassthru  |-> integrationEnvRequested,
     hostResumeSync          |-> resumeRequested,
     ancestorRewriteNeeded   |-> FALSE]

Tier3ExactPolicy ==
    [launchAllowed           |-> Tier3LaunchAllowed,
     roRoots                 |-> IF Tier3LaunchAllowed THEN Tier3ReadRoots ELSE {},
     rwRoots                 |-> IF Tier3LaunchAllowed THEN Tier3WriteRoots \cup {projectDir} ELSE {},
     integrationEnvPassthru  |-> FALSE,
     hostResumeSync          |-> FALSE,
     ancestorRewriteNeeded   |-> AncestorRewriteNeeded]

ExactPolicyIdentity ==
    Tier2ExactPolicy = Tier3ExactPolicy

ExtraMountGateSatisfied ==
    extraMountsSupported \/ ~Tier3ExtraMountsRequired

\* Comparable inputs for the narrower, useful claim:
\* no credential overlap, no backend-specific env/resume behavior, all Tier 3
\* launch gates satisfied, and no ancestor rewrite path shape.
CanonicalComparableInputs ==
    /\ ~HasCredentialInput
    /\ ~integrationEnvRequested
    /\ ~resumeRequested
    /\ backendReady
    /\ approvalGranted
    /\ ExtraMountGateSatisfied
    /\ ~AncestorRewriteNeeded

CredentialInputsRejectedInBoth ==
    HasCredentialInput =>
        /\ ~Tier2LaunchAllowed
        /\ ~Tier3LaunchAllowed

IntegrationEnvBreaksExactIdentity ==
    /\ ~HasCredentialInput
    /\ integrationEnvRequested
    /\ backendReady
    /\ approvalGranted
    /\ ExtraMountGateSatisfied
    => /\ Tier2LaunchAllowed
       /\ ~Tier3LaunchAllowed
       /\ ~ExactPolicyIdentity

ResumeBreaksExactIdentity ==
    /\ ~HasCredentialInput
    /\ ~integrationEnvRequested
    /\ resumeRequested
    /\ backendReady
    /\ approvalGranted
    /\ ExtraMountGateSatisfied
    => /\ Tier2LaunchAllowed
       /\ Tier3LaunchAllowed
       /\ Tier2ExactPolicy.hostResumeSync
       /\ ~Tier3ExactPolicy.hostResumeSync
       /\ ~ExactPolicyIdentity

AncestorRewriteBreaksExactIdentity ==
    /\ ~HasCredentialInput
    /\ ~integrationEnvRequested
    /\ ~resumeRequested
    /\ backendReady
    /\ approvalGranted
    /\ ExtraMountGateSatisfied
    /\ AncestorRewriteNeeded
    => /\ Tier2LaunchAllowed
       /\ Tier3LaunchAllowed
       /\ ~ExactPolicyIdentity

CanonicalCoreContainmentEquivalent ==
    CanonicalComparableInputs =>
        /\ Tier2CorePolicy = Tier3CorePolicy
        /\ Tier2CorePolicy.launchAllowed

=============================================================================

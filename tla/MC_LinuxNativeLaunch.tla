---- MODULE MC_LinuxNativeLaunch ----
\* Launch ordering model for the future experimental Linux native helper.
\*
\* The model is intentionally implementation-facing, not a Linux kernel model.
\* It proves the helper state machine fails closed across nondeterministic host
\* feature availability and launch-spec requirements:
\*
\*   validate spec -> close inherited fds -> create namespaces -> apply mounts
\*   -> configure network -> drop privileges + no_new_privs
\*   -> Landlock decision -> seccomp decision -> emit metadata -> exec
\*
\* Governed future code:
\*   hazmat/containment/linux       -- launch spec compiler
\*   future Linux native helper      -- validation/enforcement/metadata/exec

EXTENDS Naturals

Stages == {
    "start",
    "validated",
    "fds_closed",
    "namespaces",
    "mounts",
    "network",
    "privileges",
    "landlock",
    "seccomp",
    "metadata",
    "exec",
    "failed"
}

FailReasons == {
    "none",
    "invalid_spec",
    "missing_namespace",
    "missing_landlock",
    "missing_seccomp"
}

StateType == [
    stage: Stages,

    \* Launch-spec inputs.
    specValid: BOOLEAN,
    mountPlanValid: BOOLEAN,
    networkNone: BOOLEAN,
    specAllowsLandlockGap: BOOLEAN,
    specAllowsSeccompGap: BOOLEAN,

    \* Host capability inputs.
    userNSAvailable: BOOLEAN,
    mountNSAvailable: BOOLEAN,
    networkNSAvailable: BOOLEAN,
    landlockSupported: BOOLEAN,
    seccompSupported: BOOLEAN,

    \* Enforcement facts accumulated by the helper.
    specValidated: BOOLEAN,
    fdsClosed: BOOLEAN,
    userNSCreated: BOOLEAN,
    mountNSCreated: BOOLEAN,
    networkNSCreated: BOOLEAN,
    mountsApplied: BOOLEAN,
    networkConfigured: BOOLEAN,
    networkDenied: BOOLEAN,
    networkDefaultAllowed: BOOLEAN,
    privilegesDropped: BOOLEAN,
    noNewPrivs: BOOLEAN,
    landlockApplied: BOOLEAN,
    landlockSkipped: BOOLEAN,
    seccompApplied: BOOLEAN,
    seccompSkipped: BOOLEAN,
    metadataEmitted: BOOLEAN,
    execed: BOOLEAN,
    failed: BOOLEAN,
    failReason: FailReasons
]

VARIABLE s

vars == <<s>>

TypeOK == s \in StateType

Init ==
    \E specValid \in BOOLEAN,
      mountPlanValid \in BOOLEAN,
      networkNone \in BOOLEAN,
      specAllowsLandlockGap \in BOOLEAN,
      specAllowsSeccompGap \in BOOLEAN,
      userNSAvailable \in BOOLEAN,
      mountNSAvailable \in BOOLEAN,
      networkNSAvailable \in BOOLEAN,
      landlockSupported \in BOOLEAN,
      seccompSupported \in BOOLEAN :
        s = [
            stage |-> "start",
            specValid |-> specValid,
            mountPlanValid |-> mountPlanValid,
            networkNone |-> networkNone,
            specAllowsLandlockGap |-> specAllowsLandlockGap,
            specAllowsSeccompGap |-> specAllowsSeccompGap,
            userNSAvailable |-> userNSAvailable,
            mountNSAvailable |-> mountNSAvailable,
            networkNSAvailable |-> networkNSAvailable,
            landlockSupported |-> landlockSupported,
            seccompSupported |-> seccompSupported,
            specValidated |-> FALSE,
            fdsClosed |-> FALSE,
            userNSCreated |-> FALSE,
            mountNSCreated |-> FALSE,
            networkNSCreated |-> FALSE,
            mountsApplied |-> FALSE,
            networkConfigured |-> FALSE,
            networkDenied |-> FALSE,
            networkDefaultAllowed |-> FALSE,
            privilegesDropped |-> FALSE,
            noNewPrivs |-> FALSE,
            landlockApplied |-> FALSE,
            landlockSkipped |-> FALSE,
            seccompApplied |-> FALSE,
            seccompSkipped |-> FALSE,
            metadataEmitted |-> FALSE,
            execed |-> FALSE,
            failed |-> FALSE,
            failReason |-> "none"
        ]

ValidateSpec ==
    /\ s.stage = "start"
    /\ IF s.specValid /\ s.mountPlanValid
        THEN s' = [s EXCEPT !.stage = "validated",
                            !.specValidated = TRUE]
        ELSE s' = [s EXCEPT !.stage = "failed",
                            !.failed = TRUE,
                            !.failReason = "invalid_spec"]

CloseInheritedFDs ==
    /\ s.stage = "validated"
    /\ s' = [s EXCEPT !.stage = "fds_closed",
                      !.fdsClosed = TRUE]

NamespaceRequirementsMet ==
    /\ s.userNSAvailable
    /\ s.mountNSAvailable
    /\ (~s.networkNone \/ s.networkNSAvailable)

CreateNamespaces ==
    /\ s.stage = "fds_closed"
    /\ IF NamespaceRequirementsMet
        THEN s' = [s EXCEPT !.stage = "namespaces",
                            !.userNSCreated = TRUE,
                            !.mountNSCreated = TRUE,
                            !.networkNSCreated = s.networkNone]
        ELSE s' = [s EXCEPT !.stage = "failed",
                            !.failed = TRUE,
                            !.failReason = "missing_namespace"]

ApplyMounts ==
    /\ s.stage = "namespaces"
    /\ s' = [s EXCEPT !.stage = "mounts",
                      !.mountsApplied = TRUE]

ConfigureNetwork ==
    /\ s.stage = "mounts"
    /\ s' = [s EXCEPT !.stage = "network",
                      !.networkConfigured = TRUE,
                      !.networkDenied = s.networkNone,
                      !.networkDefaultAllowed = ~s.networkNone]

DropPrivileges ==
    /\ s.stage = "network"
    /\ s' = [s EXCEPT !.stage = "privileges",
                      !.privilegesDropped = TRUE,
                      !.noNewPrivs = TRUE]

ApplyLandlock ==
    /\ s.stage = "privileges"
    /\ IF s.landlockSupported
        THEN s' = [s EXCEPT !.stage = "landlock",
                            !.landlockApplied = TRUE]
        ELSE IF s.specAllowsLandlockGap
            THEN s' = [s EXCEPT !.stage = "landlock",
                                !.landlockSkipped = TRUE]
            ELSE s' = [s EXCEPT !.stage = "failed",
                                !.failed = TRUE,
                                !.failReason = "missing_landlock"]

ApplySeccomp ==
    /\ s.stage = "landlock"
    /\ IF s.seccompSupported
        THEN s' = [s EXCEPT !.stage = "seccomp",
                            !.seccompApplied = TRUE]
        ELSE IF s.specAllowsSeccompGap
            THEN s' = [s EXCEPT !.stage = "seccomp",
                                !.seccompSkipped = TRUE]
            ELSE s' = [s EXCEPT !.stage = "failed",
                                !.failed = TRUE,
                                !.failReason = "missing_seccomp"]

EmitMetadata ==
    /\ s.stage = "seccomp"
    /\ s' = [s EXCEPT !.stage = "metadata",
                      !.metadataEmitted = TRUE]

ExecTarget ==
    /\ s.stage = "metadata"
    /\ s' = [s EXCEPT !.stage = "exec",
                      !.execed = TRUE]

Done ==
    /\ s.stage \in {"exec", "failed"}
    /\ UNCHANGED s

Next ==
    \/ ValidateSpec
    \/ CloseInheritedFDs
    \/ CreateNamespaces
    \/ ApplyMounts
    \/ ConfigureNetwork
    \/ DropPrivileges
    \/ ApplyLandlock
    \/ ApplySeccomp
    \/ EmitMetadata
    \/ ExecTarget
    \/ Done

Spec == Init /\ [][Next]_vars

LandlockDecisionSatisfied ==
    IF s.landlockSupported
        THEN s.landlockApplied /\ ~s.landlockSkipped
        ELSE s.landlockSkipped /\ s.specAllowsLandlockGap /\ ~s.landlockApplied

SeccompDecisionSatisfied ==
    IF s.seccompSupported
        THEN s.seccompApplied /\ ~s.seccompSkipped
        ELSE s.seccompSkipped /\ s.specAllowsSeccompGap /\ ~s.seccompApplied

SpecValidatedBeforeSideEffects ==
    (s.fdsClosed \/ s.userNSCreated \/ s.mountNSCreated \/ s.mountsApplied \/
     s.networkConfigured \/ s.privilegesDropped \/ s.metadataEmitted \/ s.execed)
        => s.specValidated

FDsClosedBeforeNamespaces ==
    (s.userNSCreated \/ s.mountNSCreated \/ s.networkNSCreated \/
     s.mountsApplied \/ s.metadataEmitted \/ s.execed)
        => s.fdsClosed

MountsAfterNamespaces ==
    s.mountsApplied => (s.userNSCreated /\ s.mountNSCreated)

NetworkNoneDeniedBeforeMetadata ==
    (s.metadataEmitted \/ s.execed) /\ s.networkNone
        => (s.networkConfigured /\ s.networkDenied /\ s.networkNSCreated)

PrivilegeDropBeforeLSMAndMetadata ==
    (s.landlockApplied \/ s.landlockSkipped \/ s.seccompApplied \/
     s.seccompSkipped \/ s.metadataEmitted \/ s.execed)
        => (s.privilegesDropped /\ s.noNewPrivs)

MetadataAfterEnforcement ==
    s.metadataEmitted =>
        /\ s.specValidated
        /\ s.fdsClosed
        /\ s.userNSCreated
        /\ s.mountNSCreated
        /\ s.mountsApplied
        /\ s.networkConfigured
        /\ s.privilegesDropped
        /\ s.noNewPrivs
        /\ LandlockDecisionSatisfied
        /\ SeccompDecisionSatisfied

ExecAfterMetadata ==
    s.execed => s.metadataEmitted

NoExecOnFailure ==
    s.failed => /\ s.stage = "failed"
                /\ ~s.execed
                /\ ~s.metadataEmitted

NoExecWithMissingRequiredFeature ==
    s.execed =>
        /\ s.userNSAvailable
        /\ s.mountNSAvailable
        /\ (~s.networkNone \/ s.networkNSAvailable)
        /\ (s.landlockSupported \/ s.specAllowsLandlockGap)
        /\ (s.seccompSupported \/ s.specAllowsSeccompGap)

=============================================================================

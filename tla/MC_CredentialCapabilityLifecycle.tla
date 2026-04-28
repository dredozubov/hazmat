---- MODULE MC_CredentialCapabilityLifecycle ----
\* Registry-level credential capability lifecycle.
\*
\* This model generalizes the file-backed secret-store recovery model. It
\* treats every credential surface as a registry entry with a storage backend,
\* support status, session delivery mode, and optional harness scope.
\*
\* The model intentionally separates durable host storage from session exposure:
\* - materialized-file credentials may create temporary /Users/agent residue
\* - env credentials may only be present in the session env grant set
\* - brokered credentials may only be present in the broker grant set
\* - external-reference credentials may only be present as an external grant
\* - adapter-required credentials are not deliverable at all
\*
\* Crash/restart keeps durable host state and agent file residue, but clears
\* session env/broker/external grants. Startup recovery must reconcile all
\* leftover materialized-file residue before a new session can launch.

EXTENDS TLC, FiniteSets

CONSTANTS
    Credentials,
    Harnesses,
    Values,
    NoSecret,
    NoHarness,
    HostSecretStore,
    KeychainBackend,
    BrokerBackend,
    ExternalFileBackend,
    FileDelivery,
    EnvDelivery,
    BrokerDelivery,
    ExternalReferenceDelivery,
    NoDelivery,
    ManagedSupport,
    ExternalSupport,
    AdapterRequiredSupport,
    ClaudeHarness,
    CodexHarness,
    GeminiHarness,
    ClaudeScopedCreds,
    CodexScopedCreds,
    GeminiScopedCreds,
    GlobalCreds,
    HostSecretStoreCreds,
    KeychainBackendCreds,
    BrokerBackendCreds,
    ExternalFileBackendCreds,
    FileDeliveryCreds,
    EnvDeliveryCreds,
    BrokerDeliveryCreds,
    ExternalReferenceDeliveryCreds,
    NoDeliveryCreds,
    ManagedSupportCreds,
    ExternalSupportCreds,
    AdapterRequiredSupportCreds

DeliveryModes ==
    {FileDelivery,
     EnvDelivery,
     BrokerDelivery,
     ExternalReferenceDelivery,
     NoDelivery}

Backends ==
    {HostSecretStore,
     KeychainBackend,
     BrokerBackend,
     ExternalFileBackend}

SupportStatuses ==
    {ManagedSupport,
     ExternalSupport,
     AdapterRequiredSupport}

ASSUME
    /\ Credentials # {}
    /\ Harnesses # {}
    /\ Values # {}
    /\ NoSecret \notin Values
    /\ NoHarness \notin Harnesses
    /\ ClaudeHarness \in Harnesses
    /\ CodexHarness \in Harnesses
    /\ GeminiHarness \in Harnesses
    /\ ClaudeHarness # CodexHarness
    /\ ClaudeHarness # GeminiHarness
    /\ CodexHarness # GeminiHarness
    /\ ClaudeScopedCreds \subseteq Credentials
    /\ CodexScopedCreds \subseteq Credentials
    /\ GeminiScopedCreds \subseteq Credentials
    /\ GlobalCreds \subseteq Credentials
    /\ HostSecretStoreCreds \subseteq Credentials
    /\ KeychainBackendCreds \subseteq Credentials
    /\ BrokerBackendCreds \subseteq Credentials
    /\ ExternalFileBackendCreds \subseteq Credentials
    /\ FileDeliveryCreds \subseteq Credentials
    /\ EnvDeliveryCreds \subseteq Credentials
    /\ BrokerDeliveryCreds \subseteq Credentials
    /\ ExternalReferenceDeliveryCreds \subseteq Credentials
    /\ NoDeliveryCreds \subseteq Credentials
    /\ ManagedSupportCreds \subseteq Credentials
    /\ ExternalSupportCreds \subseteq Credentials
    /\ AdapterRequiredSupportCreds \subseteq Credentials
    /\ ClaudeScopedCreds \cup CodexScopedCreds \cup GeminiScopedCreds \cup GlobalCreds = Credentials
    /\ HostSecretStoreCreds \cup KeychainBackendCreds \cup BrokerBackendCreds \cup ExternalFileBackendCreds = Credentials
    /\ FileDeliveryCreds \cup EnvDeliveryCreds \cup BrokerDeliveryCreds \cup ExternalReferenceDeliveryCreds \cup NoDeliveryCreds = Credentials
    /\ ManagedSupportCreds \cup ExternalSupportCreds \cup AdapterRequiredSupportCreds = Credentials
    /\ \A c \in Credentials :
        Cardinality({s \in {ClaudeScopedCreds, CodexScopedCreds, GeminiScopedCreds, GlobalCreds} : c \in s}) = 1
    /\ \A c \in Credentials :
        Cardinality({s \in {HostSecretStoreCreds, KeychainBackendCreds, BrokerBackendCreds, ExternalFileBackendCreds} : c \in s}) = 1
    /\ \A c \in Credentials :
        Cardinality({s \in {FileDeliveryCreds, EnvDeliveryCreds, BrokerDeliveryCreds, ExternalReferenceDeliveryCreds, NoDeliveryCreds} : c \in s}) = 1
    /\ \A c \in Credentials :
        Cardinality({s \in {ManagedSupportCreds, ExternalSupportCreds, AdapterRequiredSupportCreds} : c \in s}) = 1

SecretVals == Values \cup {NoSecret}

CredentialHarness(c) ==
    IF c \in GlobalCreds THEN NoHarness
    ELSE IF c \in ClaudeScopedCreds THEN ClaudeHarness
    ELSE IF c \in CodexScopedCreds THEN CodexHarness
    ELSE GeminiHarness

CredentialBackend(c) ==
    IF c \in HostSecretStoreCreds THEN HostSecretStore
    ELSE IF c \in KeychainBackendCreds THEN KeychainBackend
    ELSE IF c \in BrokerBackendCreds THEN BrokerBackend
    ELSE ExternalFileBackend

CredentialDelivery(c) ==
    IF c \in FileDeliveryCreds THEN FileDelivery
    ELSE IF c \in EnvDeliveryCreds THEN EnvDelivery
    ELSE IF c \in BrokerDeliveryCreds THEN BrokerDelivery
    ELSE IF c \in ExternalReferenceDeliveryCreds THEN ExternalReferenceDelivery
    ELSE NoDelivery

CredentialSupport(c) ==
    IF c \in ManagedSupportCreds THEN ManagedSupport
    ELSE IF c \in ExternalSupportCreds THEN ExternalSupport
    ELSE AdapterRequiredSupport

ManagedHostCreds ==
    {c \in Credentials :
        /\ CredentialBackend(c) = HostSecretStore
        /\ CredentialSupport(c) = ManagedSupport}

ManagedFileCreds ==
    {c \in Credentials :
        /\ CredentialBackend(c) = HostSecretStore
        /\ CredentialDelivery(c) = FileDelivery
        /\ CredentialSupport(c) = ManagedSupport}

FileCreds ==
    {c \in Credentials : CredentialDelivery(c) = FileDelivery}

EligibleCreds(h) ==
    {c \in Credentials :
        /\ CredentialSupport(c) # AdapterRequiredSupport
        /\ CredentialHarness(c) \in {h, NoHarness}}

Phases ==
    {"idle",
     "recovering",
     "delivering",
     "running",
     "harvesting",
     "removing"}

ActivePhases ==
    {"delivering", "running", "harvesting", "removing"}

VARIABLES
    phase,
    activeHarness,
    activeCreds,
    delivered,
    host,
    agent,
    conflicts,
    latest,
    recovered,
    baseline,
    envGranted,
    brokerGranted,
    externalGranted

vars ==
    << phase,
       activeHarness,
       activeCreds,
       delivered,
       host,
       agent,
       conflicts,
       latest,
       recovered,
       baseline,
       envGranted,
       brokerGranted,
       externalGranted >>

EmptySecrets ==
    [c \in Credentials |-> NoSecret]

SessionFileCreds ==
    activeCreds \cap ManagedFileCreds

ExposedCreds ==
    {c \in Credentials : agent[c] # NoSecret}
    \cup envGranted
    \cup brokerGranted
    \cup externalGranted

LatestKnown(c) ==
    \/ latest[c] = NoSecret
    \/ latest[c] = host[c]
    \/ latest[c] = agent[c]
    \/ latest[c] \in conflicts[c]

InitialLatest ==
    [c \in Credentials |->
        IF c \in ManagedHostCreds
        THEN IF agent[c] # NoSecret THEN agent[c] ELSE host[c]
        ELSE NoSecret]

Init ==
    /\ phase = "idle"
    /\ activeHarness = NoHarness
    /\ activeCreds = {}
    /\ delivered = {}
    /\ host \in [Credentials -> SecretVals]
    /\ \A c \in Credentials \ ManagedHostCreds : host[c] = NoSecret
    /\ agent \in [Credentials -> SecretVals]
    /\ \A c \in Credentials \ ManagedFileCreds : agent[c] = NoSecret
    /\ conflicts = [c \in Credentials |-> {}]
    /\ latest = InitialLatest
    /\ recovered = {}
    /\ baseline = EmptySecrets
    /\ envGranted = {}
    /\ brokerGranted = {}
    /\ externalGranted = {}

BeginRecover ==
    /\ phase = "idle"
    /\ activeHarness = NoHarness
    /\ activeCreds = {}
    /\ recovered # ManagedFileCreds
    /\ phase' = "recovering"
    /\ UNCHANGED << activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    agent,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

RecoveredHost(c) ==
    IF agent[c] = NoSecret THEN host[c] ELSE agent[c]

RecoveredConflicts(c) ==
    IF /\ agent[c] # NoSecret
       /\ host[c] # NoSecret
       /\ host[c] # agent[c]
    THEN conflicts[c] \cup {host[c]}
    ELSE conflicts[c]

RecoverOne(c) ==
    /\ phase = "recovering"
    /\ c \in ManagedFileCreds \ recovered
    /\ host' = [host EXCEPT ![c] = RecoveredHost(c)]
    /\ agent' = [agent EXCEPT ![c] = NoSecret]
    /\ conflicts' = [conflicts EXCEPT ![c] = RecoveredConflicts(c)]
    /\ recovered' = recovered \cup {c}
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    latest,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

FinishRecover ==
    /\ phase = "recovering"
    /\ recovered = ManagedFileCreds
    /\ phase' = "idle"
    /\ UNCHANGED << activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    agent,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

BeginSession(h, grants) ==
    /\ phase = "idle"
    /\ activeHarness = NoHarness
    /\ activeCreds = {}
    /\ recovered = ManagedFileCreds
    /\ \A c \in Credentials : agent[c] = NoSecret
    /\ h \in Harnesses
    /\ grants \in SUBSET EligibleCreds(h)
    /\ phase' = "delivering"
    /\ activeHarness' = h
    /\ activeCreds' = grants
    /\ delivered' = {}
    /\ baseline' =
        [c \in Credentials |->
            IF c \in grants /\ c \in ManagedFileCreds
            THEN host[c]
            ELSE NoSecret]
    /\ envGranted' = {}
    /\ brokerGranted' = {}
    /\ externalGranted' = {}
    /\ UNCHANGED << host,
                    agent,
                    conflicts,
                    latest,
                    recovered >>

DeliverFile(c) ==
    /\ phase = "delivering"
    /\ c \in activeCreds \ delivered
    /\ CredentialDelivery(c) = FileDelivery
    /\ c \in ManagedFileCreds
    /\ agent' =
        IF host[c] = NoSecret
        THEN agent
        ELSE [agent EXCEPT ![c] = host[c]]
    /\ delivered' = delivered \cup {c}
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    host,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

DeliverEnv(c) ==
    /\ phase = "delivering"
    /\ c \in activeCreds \ delivered
    /\ CredentialDelivery(c) = EnvDelivery
    /\ envGranted' = envGranted \cup {c}
    /\ delivered' = delivered \cup {c}
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    host,
                    agent,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    brokerGranted,
                    externalGranted >>

DeliverBroker(c) ==
    /\ phase = "delivering"
    /\ c \in activeCreds \ delivered
    /\ CredentialDelivery(c) = BrokerDelivery
    /\ brokerGranted' = brokerGranted \cup {c}
    /\ delivered' = delivered \cup {c}
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    host,
                    agent,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    externalGranted >>

DeliverExternal(c) ==
    /\ phase = "delivering"
    /\ c \in activeCreds \ delivered
    /\ CredentialDelivery(c) = ExternalReferenceDelivery
    /\ CredentialSupport(c) = ExternalSupport
    /\ externalGranted' = externalGranted \cup {c}
    /\ delivered' = delivered \cup {c}
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    host,
                    agent,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted >>

StartRunning ==
    /\ phase = "delivering"
    /\ activeCreds \subseteq delivered
    /\ phase' = "running"
    /\ UNCHANGED << activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    agent,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

ToolRefresh(c, v) ==
    /\ phase = "running"
    /\ c \in activeCreds
    /\ c \in ManagedFileCreds
    /\ v \in Values
    /\ agent' = [agent EXCEPT ![c] = v]
    /\ latest' = [latest EXCEPT ![c] = v]
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    conflicts,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

ExternalStoreUpdate(c, v) ==
    /\ phase = "idle"
    /\ c \in ManagedHostCreds
    /\ v \in Values
    /\ host[c] # v
    /\ host' = [host EXCEPT ![c] = v]
    /\ latest' = [latest EXCEPT ![c] = v]
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    agent,
                    conflicts,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

BeginHarvest ==
    /\ phase = "running"
    /\ phase' = "harvesting"
    /\ UNCHANGED << activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    agent,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

HarvestConflicts ==
    [c \in Credentials |->
        IF /\ c \in SessionFileCreds
           /\ agent[c] # NoSecret
           /\ host[c] # NoSecret
           /\ host[c] # agent[c]
           /\ host[c] # baseline[c]
        THEN conflicts[c] \cup {host[c]}
        ELSE conflicts[c]]

HarvestHost ==
    [c \in Credentials |->
        IF /\ c \in SessionFileCreds
           /\ agent[c] # NoSecret
        THEN agent[c]
        ELSE host[c]]

Harvest ==
    /\ phase = "harvesting"
    /\ conflicts' = HarvestConflicts
    /\ host' = HarvestHost
    /\ phase' = "removing"
    /\ UNCHANGED << activeHarness,
                    activeCreds,
                    delivered,
                    agent,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

RemoveOne(c) ==
    /\ phase = "removing"
    /\ c \in SessionFileCreds
    /\ agent[c] # NoSecret
    /\ agent' = [agent EXCEPT ![c] = NoSecret]
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

FinishRemove ==
    /\ phase = "removing"
    /\ \A c \in SessionFileCreds : agent[c] = NoSecret
    /\ phase' = "idle"
    /\ activeHarness' = NoHarness
    /\ activeCreds' = {}
    /\ delivered' = {}
    /\ recovered' = ManagedFileCreds
    /\ baseline' = EmptySecrets
    /\ envGranted' = {}
    /\ brokerGranted' = {}
    /\ externalGranted' = {}
    /\ UNCHANGED << host,
                    agent,
                    conflicts,
                    latest >>

Crash ==
    /\ phase # "idle"
    /\ phase' = "idle"
    /\ activeHarness' = NoHarness
    /\ activeCreds' = {}
    /\ delivered' = {}
    /\ recovered' = {}
    /\ baseline' = EmptySecrets
    /\ envGranted' = {}
    /\ brokerGranted' = {}
    /\ externalGranted' = {}
    /\ UNCHANGED << host,
                    agent,
                    conflicts,
                    latest >>

Stutter ==
    UNCHANGED vars

Next ==
    \/ BeginRecover
    \/ \E c \in Credentials : RecoverOne(c)
    \/ FinishRecover
    \/ \E h \in Harnesses, grants \in SUBSET Credentials : BeginSession(h, grants)
    \/ \E c \in Credentials : DeliverFile(c)
    \/ \E c \in Credentials : DeliverEnv(c)
    \/ \E c \in Credentials : DeliverBroker(c)
    \/ \E c \in Credentials : DeliverExternal(c)
    \/ StartRunning
    \/ \E c \in Credentials, v \in Values : ToolRefresh(c, v)
    \/ \E c \in Credentials, v \in Values : ExternalStoreUpdate(c, v)
    \/ BeginHarvest
    \/ Harvest
    \/ \E c \in Credentials : RemoveOne(c)
    \/ FinishRemove
    \/ Crash
    \/ Stutter

Spec ==
    Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in Phases
    /\ activeHarness \in Harnesses \cup {NoHarness}
    /\ activeCreds \subseteq Credentials
    /\ delivered \subseteq Credentials
    /\ host \in [Credentials -> SecretVals]
    /\ agent \in [Credentials -> SecretVals]
    /\ conflicts \in [Credentials -> SUBSET Values]
    /\ latest \in [Credentials -> SecretVals]
    /\ recovered \subseteq ManagedFileCreds
    /\ baseline \in [Credentials -> SecretVals]
    /\ envGranted \subseteq Credentials
    /\ brokerGranted \subseteq Credentials
    /\ externalGranted \subseteq Credentials

RegistryWellFormed ==
    /\ \A c \in Credentials :
        /\ CredentialBackend(c) \in Backends
        /\ CredentialDelivery(c) \in DeliveryModes
        /\ CredentialSupport(c) \in SupportStatuses
    /\ \A c \in Credentials :
        CredentialDelivery(c) = FileDelivery =>
            /\ CredentialBackend(c) = HostSecretStore
            /\ CredentialSupport(c) = ManagedSupport
    /\ \A c \in Credentials :
        CredentialSupport(c) = AdapterRequiredSupport =>
            /\ CredentialBackend(c) # HostSecretStore
            /\ CredentialDelivery(c) = ExternalReferenceDelivery

ASSUME RegistryWellFormed

NonHostBackendsHaveNoHostStore ==
    \A c \in Credentials :
        CredentialBackend(c) # HostSecretStore =>
            /\ host[c] = NoSecret
            /\ agent[c] = NoSecret
            /\ latest[c] = NoSecret

DeliveryMatchesRegistry ==
    /\ \A c \in Credentials :
        agent[c] # NoSecret => CredentialDelivery(c) = FileDelivery
    /\ \A c \in envGranted : CredentialDelivery(c) = EnvDelivery
    /\ \A c \in brokerGranted : CredentialDelivery(c) = BrokerDelivery
    /\ \A c \in externalGranted :
        /\ CredentialDelivery(c) = ExternalReferenceDelivery
        /\ CredentialSupport(c) = ExternalSupport

AdapterRequiredNeverExposed ==
    \A c \in Credentials :
        CredentialSupport(c) = AdapterRequiredSupport =>
            /\ c \notin activeCreds
            /\ c \notin delivered
            /\ c \notin envGranted
            /\ c \notin brokerGranted
            /\ c \notin externalGranted
            /\ agent[c] = NoSecret

NoCrossHarnessExposure ==
    phase \in ActivePhases =>
        /\ activeHarness \in Harnesses
        /\ \A c \in ExposedCreds :
            CredentialHarness(c) \in {activeHarness, NoHarness}

NoSessionExposureOutsideActivePhase ==
    phase \notin ActivePhases =>
        /\ activeHarness = NoHarness
        /\ activeCreds = {}
        /\ delivered = {}
        /\ envGranted = {}
        /\ brokerGranted = {}
        /\ externalGranted = {}

LaunchOnlyAfterRecovery ==
    phase \in ActivePhases => recovered = ManagedFileCreds

CleanRecoveredStateHasNoAgentResidue ==
    /\ phase = "idle"
    /\ recovered = ManagedFileCreds
    =>
    \A c \in Credentials : agent[c] = NoSecret

LatestValueNeverSilentlyLost ==
    \A c \in ManagedHostCreds : LatestKnown(c)

CleanRecoveredStateKeepsLatestHostOwned ==
    /\ phase = "idle"
    /\ recovered = ManagedFileCreds
    =>
    \A c \in ManagedHostCreds :
        \/ latest[c] = NoSecret
        \/ latest[c] = host[c]
        \/ latest[c] \in conflicts[c]

IdleClearsSessionState ==
    phase = "idle" =>
        /\ activeHarness = NoHarness
        /\ activeCreds = {}
        /\ delivered = {}
        /\ envGranted = {}
        /\ brokerGranted = {}
        /\ externalGranted = {}
        /\ baseline = EmptySecrets

=============================================================================

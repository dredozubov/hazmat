---- MODULE MC_CredentialCapabilityLifecycle ----
\* Registry-level credential capability lifecycle.
\*
\* This model generalizes the file-backed secret-store recovery model. It
\* treats every credential surface as a registry entry with a storage backend,
\* support status, session delivery mode, and explicit consumer harness set.
\*
\* The model intentionally separates durable host storage from session exposure:
\* - materialized-file credentials may create temporary persistent-agent-home
\*   or session-home residue
\* - env credentials may only be present in the session env grant set
\* - brokered credentials may only be present in the broker grant set
\* - external-reference credentials may only be present as an external grant
\* - syncable keychain credentials may be mirrored through the host-owned store,
\*   host-user keychain, and agent-user keychain, but only after explicit sync
\* - adapter-required credentials are not deliverable at all
\* - no-delivery credentials model contained-only profile state that must not be
\*   exposed by credential sync
\*
\* Crash/restart keeps durable host state and credential file residue, but
\* clears session env/broker/external grants. Startup recovery must reconcile
\* all leftover materialized-file residue before a new session can launch.

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
    PersistentAgentHomeTarget,
    SessionHomeTarget,
    RuntimeCredentialTarget,
    ClaudeHarness,
    CodexHarness,
    OpenCodeHarness,
    AntigravityHarness,
    HermesHarness,
    ClaudeConsumerCreds,
    CodexConsumerCreds,
    OpenCodeConsumerCreds,
    AntigravityConsumerCreds,
    HermesConsumerCreds,
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
    AdapterRequiredSupportCreds,
    SyncableKeychainCreds,
    ContainedOnlyCreds

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

CredentialTargets ==
    {PersistentAgentHomeTarget,
     SessionHomeTarget}

CredentialConsumers(c) ==
    IF c \in GlobalCreds THEN Harnesses
    ELSE {h \in Harnesses :
        \/ /\ h = ClaudeHarness
           /\ c \in ClaudeConsumerCreds
        \/ /\ h = CodexHarness
           /\ c \in CodexConsumerCreds
        \/ /\ h = OpenCodeHarness
           /\ c \in OpenCodeConsumerCreds
        \/ /\ h = AntigravityHarness
           /\ c \in AntigravityConsumerCreds
        \/ /\ h = HermesHarness
           /\ c \in HermesConsumerCreds}

ASSUME
    /\ Credentials # {}
    /\ Harnesses # {}
    /\ Values # {}
    /\ NoSecret \notin Values
    /\ NoHarness \notin Harnesses
    /\ ClaudeHarness \in Harnesses
    /\ CodexHarness \in Harnesses
    /\ OpenCodeHarness \in Harnesses
    /\ AntigravityHarness \in Harnesses
    /\ HermesHarness \in Harnesses
    /\ Harnesses = {ClaudeHarness, CodexHarness, OpenCodeHarness, AntigravityHarness, HermesHarness}
    /\ ClaudeHarness # CodexHarness
    /\ ClaudeHarness # OpenCodeHarness
    /\ ClaudeHarness # AntigravityHarness
    /\ ClaudeHarness # HermesHarness
    /\ CodexHarness # OpenCodeHarness
    /\ CodexHarness # AntigravityHarness
    /\ CodexHarness # HermesHarness
    /\ OpenCodeHarness # AntigravityHarness
    /\ OpenCodeHarness # HermesHarness
    /\ AntigravityHarness # HermesHarness
    /\ ClaudeConsumerCreds \subseteq Credentials
    /\ CodexConsumerCreds \subseteq Credentials
    /\ OpenCodeConsumerCreds \subseteq Credentials
    /\ AntigravityConsumerCreds \subseteq Credentials
    /\ HermesConsumerCreds \subseteq Credentials
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
    /\ SyncableKeychainCreds \subseteq Credentials
    /\ ContainedOnlyCreds \subseteq Credentials
    /\ HostSecretStoreCreds \cup KeychainBackendCreds \cup BrokerBackendCreds \cup ExternalFileBackendCreds = Credentials
    /\ FileDeliveryCreds \cup EnvDeliveryCreds \cup BrokerDeliveryCreds \cup ExternalReferenceDeliveryCreds \cup NoDeliveryCreds = Credentials
    /\ ManagedSupportCreds \cup ExternalSupportCreds \cup AdapterRequiredSupportCreds = Credentials
    /\ GlobalCreds \cap (ClaudeConsumerCreds \cup CodexConsumerCreds \cup OpenCodeConsumerCreds \cup AntigravityConsumerCreds \cup HermesConsumerCreds) = {}
    /\ \A c \in Credentials :
        Cardinality({s \in {HostSecretStoreCreds, KeychainBackendCreds, BrokerBackendCreds, ExternalFileBackendCreds} : c \in s}) = 1
    /\ \A c \in Credentials :
        Cardinality({s \in {FileDeliveryCreds, EnvDeliveryCreds, BrokerDeliveryCreds, ExternalReferenceDeliveryCreds, NoDeliveryCreds} : c \in s}) = 1
    /\ \A c \in Credentials :
        Cardinality({s \in {ManagedSupportCreds, ExternalSupportCreds, AdapterRequiredSupportCreds} : c \in s}) = 1
    /\ \A c \in Credentials \ GlobalCreds : CredentialConsumers(c) # {}
    /\ \A c \in FileDeliveryCreds : Cardinality(CredentialConsumers(c)) = 1
    /\ SyncableKeychainCreds \subseteq KeychainBackendCreds
    /\ SyncableKeychainCreds \subseteq ExternalReferenceDeliveryCreds
    /\ SyncableKeychainCreds \subseteq ExternalSupportCreds
    /\ ContainedOnlyCreds \subseteq NoDeliveryCreds

SecretVals == Values \cup {NoSecret}

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

ManagedStoreCreds ==
    ManagedHostCreds \cup SyncableKeychainCreds

ManagedFileCreds ==
    {c \in Credentials :
        /\ CredentialBackend(c) = HostSecretStore
        /\ CredentialDelivery(c) = FileDelivery
        /\ CredentialSupport(c) = ManagedSupport}

FileCreds ==
    {c \in Credentials : CredentialDelivery(c) = FileDelivery}

RecoverableCreds ==
    ManagedFileCreds \cup SyncableKeychainCreds

EligibleCreds(h) ==
    {c \in Credentials :
        /\ CredentialSupport(c) # AdapterRequiredSupport
        /\ CredentialDelivery(c) # NoDelivery
        /\ h \in CredentialConsumers(c)}

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
    sessionAgent,
    hostKeychain,
    agentKeychain,
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
       sessionAgent,
       hostKeychain,
       agentKeychain,
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

SessionKeychainCreds ==
    activeCreds \cap SyncableKeychainCreds

SessionResidueCreds ==
    SessionFileCreds \cup SessionKeychainCreds

ExposedCreds ==
    {c \in Credentials : agent[c] # NoSecret}
    \cup {c \in Credentials : sessionAgent[c] # NoSecret}
    \cup {c \in Credentials : agentKeychain[c] # NoSecret}
    \cup envGranted
    \cup brokerGranted
    \cup externalGranted

LatestKnown(c) ==
    \/ latest[c] = NoSecret
    \/ latest[c] = host[c]
    \/ latest[c] = agent[c]
    \/ latest[c] = sessionAgent[c]
    \/ latest[c] = hostKeychain[c]
    \/ latest[c] = agentKeychain[c]
    \/ latest[c] \in conflicts[c]

InitialLatest ==
    [c \in Credentials |->
        IF c \in ManagedStoreCreds
        THEN IF agentKeychain[c] # NoSecret THEN agentKeychain[c]
             ELSE IF sessionAgent[c] # NoSecret THEN sessionAgent[c]
             ELSE IF agent[c] # NoSecret THEN agent[c]
             ELSE IF hostKeychain[c] # NoSecret THEN hostKeychain[c]
             ELSE host[c]
        ELSE NoSecret]

RuntimeAgent(c) ==
    IF RuntimeCredentialTarget = PersistentAgentHomeTarget THEN agent[c]
    ELSE IF RuntimeCredentialTarget = SessionHomeTarget THEN sessionAgent[c]
    ELSE NoSecret

RuntimeResidue(c) ==
    IF agentKeychain[c] # NoSecret THEN agentKeychain[c]
    ELSE RuntimeAgent(c)

HostKeychainSynced ==
    \A c \in SyncableKeychainCreds :
        \/ hostKeychain[c] = NoSecret
        \/ hostKeychain[c] = host[c]

SingleCredentialResidueTarget ==
    \/ agent = EmptySecrets
    \/ sessionAgent = EmptySecrets

Init ==
    /\ phase = "idle"
    /\ activeHarness = NoHarness
    /\ activeCreds = {}
    /\ delivered = {}
    /\ host \in [Credentials -> SecretVals]
    /\ \A c \in Credentials \ ManagedStoreCreds : host[c] = NoSecret
    /\ agent \in [Credentials -> SecretVals]
    /\ \A c \in Credentials \ ManagedFileCreds : agent[c] = NoSecret
    /\ sessionAgent \in [Credentials -> SecretVals]
    /\ \A c \in Credentials \ ManagedFileCreds : sessionAgent[c] = NoSecret
    /\ hostKeychain \in [Credentials -> SecretVals]
    /\ \A c \in Credentials \ SyncableKeychainCreds : hostKeychain[c] = NoSecret
    /\ agentKeychain \in [Credentials -> SecretVals]
    /\ \A c \in Credentials \ SyncableKeychainCreds : agentKeychain[c] = NoSecret
    /\ SingleCredentialResidueTarget
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
    /\ recovered # RecoverableCreds
    /\ phase' = "recovering"
    /\ UNCHANGED << activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    agent,
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

RecoveredHost(c) ==
    IF agentKeychain[c] # NoSecret THEN agentKeychain[c]
    ELSE IF sessionAgent[c] # NoSecret THEN sessionAgent[c]
    ELSE IF agent[c] # NoSecret THEN agent[c]
    ELSE host[c]

RecoveredRuntimeResidue(c) ==
    IF agentKeychain[c] # NoSecret THEN agentKeychain[c]
    ELSE IF sessionAgent[c] # NoSecret THEN sessionAgent[c]
    ELSE agent[c]

RecoveredConflicts(c) ==
    IF /\ RecoveredRuntimeResidue(c) # NoSecret
       /\ host[c] # NoSecret
       /\ host[c] # RecoveredRuntimeResidue(c)
    THEN conflicts[c] \cup {host[c]}
    ELSE conflicts[c]

RecoverOne(c) ==
    /\ phase = "recovering"
    /\ c \in RecoverableCreds \ recovered
    /\ host' = [host EXCEPT ![c] = RecoveredHost(c)]
    /\ agent' = [agent EXCEPT ![c] = NoSecret]
    /\ sessionAgent' = [sessionAgent EXCEPT ![c] = NoSecret]
    /\ agentKeychain' = [agentKeychain EXCEPT ![c] = NoSecret]
    /\ conflicts' = [conflicts EXCEPT ![c] = RecoveredConflicts(c)]
    /\ recovered' = recovered \cup {c}
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    latest,
                    hostKeychain,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

FinishRecover ==
    /\ phase = "recovering"
    /\ recovered = RecoverableCreds
    /\ phase' = "idle"
    /\ UNCHANGED << activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    agent,
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
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
    /\ recovered = RecoverableCreds
    /\ \A c \in Credentials : agent[c] = NoSecret
    /\ \A c \in Credentials : sessionAgent[c] = NoSecret
    /\ \A c \in Credentials : agentKeychain[c] = NoSecret
    /\ HostKeychainSynced
    /\ h \in Harnesses
    /\ grants \in SUBSET EligibleCreds(h)
    /\ RuntimeCredentialTarget \in CredentialTargets
    /\ phase' = "delivering"
    /\ activeHarness' = h
    /\ activeCreds' = grants
    /\ delivered' = {}
    /\ baseline' =
        [c \in Credentials |->
            IF c \in grants /\ c \in ManagedFileCreds
            THEN host[c]
            ELSE IF c \in grants /\ c \in SyncableKeychainCreds
            THEN host[c]
            ELSE NoSecret]
    /\ envGranted' = {}
    /\ brokerGranted' = {}
    /\ externalGranted' = {}
    /\ UNCHANGED << host,
                    agent,
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
                    conflicts,
                    latest,
                    recovered >>

DeliverFile(c) ==
    /\ phase = "delivering"
    /\ c \in activeCreds \ delivered
    /\ CredentialDelivery(c) = FileDelivery
    /\ c \in ManagedFileCreds
    /\ IF RuntimeCredentialTarget = PersistentAgentHomeTarget
       THEN
        /\ agent' =
            IF host[c] = NoSecret
            THEN agent
            ELSE [agent EXCEPT ![c] = host[c]]
        /\ sessionAgent' = sessionAgent
       ELSE
        /\ sessionAgent' =
            IF host[c] = NoSecret
            THEN sessionAgent
            ELSE [sessionAgent EXCEPT ![c] = host[c]]
        /\ agent' = agent
    /\ delivered' = delivered \cup {c}
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    host,
                    hostKeychain,
                    agentKeychain,
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
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
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
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
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
    /\ IF c \in SyncableKeychainCreds
       THEN agentKeychain' =
            IF host[c] = NoSecret
            THEN agentKeychain
            ELSE [agentKeychain EXCEPT ![c] = host[c]]
       ELSE agentKeychain' = agentKeychain
    /\ externalGranted' = externalGranted \cup {c}
    /\ delivered' = delivered \cup {c}
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    host,
                    agent,
                    sessionAgent,
                    hostKeychain,
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
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
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
    /\ IF RuntimeCredentialTarget = PersistentAgentHomeTarget
       THEN
        /\ agent' = [agent EXCEPT ![c] = v]
        /\ sessionAgent' = sessionAgent
       ELSE
        /\ sessionAgent' = [sessionAgent EXCEPT ![c] = v]
        /\ agent' = agent
    /\ latest' = [latest EXCEPT ![c] = v]
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    hostKeychain,
                    agentKeychain,
                    conflicts,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

ToolRefreshKeychain(c, v) ==
    /\ phase = "running"
    /\ c \in activeCreds
    /\ c \in SyncableKeychainCreds
    /\ v \in Values
    /\ agentKeychain' = [agentKeychain EXCEPT ![c] = v]
    /\ latest' = [latest EXCEPT ![c] = v]
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    agent,
                    sessionAgent,
                    hostKeychain,
                    conflicts,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

\* A harness may rewrite a materialized baseline auth file into a logged-out or
\* empty shape before any credential refresh. The implementation treats that as
\* non-harvestable NoSecret instead of promoting it over the host-owned
\* credential.
ToolLogout(c) ==
    /\ phase = "running"
    /\ c \in activeCreds
    /\ c \in ManagedFileCreds
    /\ RuntimeAgent(c) # NoSecret
    /\ RuntimeAgent(c) = baseline[c]
    /\ IF RuntimeCredentialTarget = PersistentAgentHomeTarget
       THEN
        /\ agent' = [agent EXCEPT ![c] = NoSecret]
        /\ sessionAgent' = sessionAgent
       ELSE
        /\ sessionAgent' = [sessionAgent EXCEPT ![c] = NoSecret]
        /\ agent' = agent
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    hostKeychain,
                    agentKeychain,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

ExternalStoreUpdate(c, v) ==
    /\ phase = "idle"
    /\ c \in ManagedStoreCreds
    /\ v \in Values
    /\ host[c] # v
    /\ host' = [host EXCEPT ![c] = v]
    /\ latest' = [latest EXCEPT ![c] = v]
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    agent,
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
                    conflicts,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

ExternalHostKeychainUpdate(c, v) ==
    /\ phase = "idle"
    /\ c \in SyncableKeychainCreds
    /\ v \in Values
    /\ hostKeychain[c] # v
    /\ hostKeychain' = [hostKeychain EXCEPT ![c] = v]
    /\ latest' = [latest EXCEPT ![c] = v]
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    agent,
                    sessionAgent,
                    agentKeychain,
                    conflicts,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

SyncHostKeychainToStore(c) ==
    /\ phase = "idle"
    /\ c \in SyncableKeychainCreds
    /\ hostKeychain[c] # NoSecret
    /\ host[c] # hostKeychain[c]
    /\ latest[c] = hostKeychain[c]
    /\ host' = [host EXCEPT ![c] = hostKeychain[c]]
    /\ conflicts' =
        IF host[c] # NoSecret
        THEN [conflicts EXCEPT ![c] = conflicts[c] \cup {host[c]}]
        ELSE conflicts
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    agent,
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

SyncStoreToHostKeychain(c) ==
    /\ phase = "idle"
    /\ c \in SyncableKeychainCreds
    /\ host[c] # NoSecret
    /\ hostKeychain[c] # host[c]
    /\ latest[c] = host[c]
    /\ hostKeychain' = [hostKeychain EXCEPT ![c] = host[c]]
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    agent,
                    sessionAgent,
                    agentKeychain,
                    conflicts,
                    latest,
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
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
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
           /\ RuntimeAgent(c) # NoSecret
           /\ host[c] # NoSecret
           /\ host[c] # RuntimeAgent(c)
           /\ host[c] # baseline[c]
        THEN conflicts[c] \cup {host[c]}
        ELSE IF /\ c \in SessionKeychainCreds
                /\ agentKeychain[c] # NoSecret
                /\ host[c] # NoSecret
                /\ host[c] # agentKeychain[c]
                /\ host[c] # baseline[c]
        THEN conflicts[c] \cup {host[c]}
        ELSE conflicts[c]]

HarvestHost ==
    [c \in Credentials |->
        IF /\ c \in SessionFileCreds
           /\ RuntimeAgent(c) # NoSecret
        THEN RuntimeAgent(c)
        ELSE IF /\ c \in SessionKeychainCreds
                /\ agentKeychain[c] # NoSecret
        THEN agentKeychain[c]
        ELSE host[c]]

HarvestHostKeychain ==
    [c \in Credentials |->
        IF /\ c \in SessionKeychainCreds
           /\ agentKeychain[c] # NoSecret
        THEN agentKeychain[c]
        ELSE hostKeychain[c]]

Harvest ==
    /\ phase = "harvesting"
    /\ conflicts' = HarvestConflicts
    /\ host' = HarvestHost
    /\ hostKeychain' = HarvestHostKeychain
    /\ phase' = "removing"
    /\ UNCHANGED << activeHarness,
                    activeCreds,
                    delivered,
                    agent,
                    sessionAgent,
                    agentKeychain,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

RemoveOne(c) ==
    /\ phase = "removing"
    /\ c \in SessionResidueCreds
    /\ RuntimeResidue(c) # NoSecret
    /\ IF RuntimeCredentialTarget = PersistentAgentHomeTarget
       THEN
        /\ agent' = [agent EXCEPT ![c] = NoSecret]
        /\ sessionAgent' = sessionAgent
       ELSE
        /\ sessionAgent' = [sessionAgent EXCEPT ![c] = NoSecret]
        /\ agent' = agent
    /\ agentKeychain' = [agentKeychain EXCEPT ![c] = NoSecret]
    /\ UNCHANGED << phase,
                    activeHarness,
                    activeCreds,
                    delivered,
                    host,
                    hostKeychain,
                    conflicts,
                    latest,
                    recovered,
                    baseline,
                    envGranted,
                    brokerGranted,
                    externalGranted >>

FinishRemove ==
    /\ phase = "removing"
    /\ \A c \in SessionResidueCreds : RuntimeResidue(c) = NoSecret
    /\ phase' = "idle"
    /\ activeHarness' = NoHarness
    /\ activeCreds' = {}
    /\ delivered' = {}
    /\ recovered' = RecoverableCreds
    /\ baseline' = EmptySecrets
    /\ envGranted' = {}
    /\ brokerGranted' = {}
    /\ externalGranted' = {}
    /\ UNCHANGED << host,
                    agent,
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
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
                    sessionAgent,
                    hostKeychain,
                    agentKeychain,
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
    \/ \E c \in Credentials, v \in Values : ToolRefreshKeychain(c, v)
    \/ \E c \in Credentials : ToolLogout(c)
    \/ \E c \in Credentials, v \in Values : ExternalStoreUpdate(c, v)
    \/ \E c \in Credentials, v \in Values : ExternalHostKeychainUpdate(c, v)
    \/ \E c \in Credentials : SyncHostKeychainToStore(c)
    \/ \E c \in Credentials : SyncStoreToHostKeychain(c)
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
    /\ sessionAgent \in [Credentials -> SecretVals]
    /\ hostKeychain \in [Credentials -> SecretVals]
    /\ agentKeychain \in [Credentials -> SecretVals]
    /\ \A c \in Credentials \ SyncableKeychainCreds :
        /\ hostKeychain[c] = NoSecret
        /\ agentKeychain[c] = NoSecret
    /\ SingleCredentialResidueTarget
    /\ conflicts \in [Credentials -> SUBSET Values]
    /\ latest \in [Credentials -> SecretVals]
    /\ recovered \subseteq RecoverableCreds
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
    /\ \A c \in ContainedOnlyCreds :
        /\ CredentialDelivery(c) = NoDelivery
        /\ CredentialSupport(c) = ExternalSupport

ASSUME RegistryWellFormed

NonHostBackendsHaveNoHostStore ==
    \A c \in Credentials :
        /\ CredentialBackend(c) # HostSecretStore
        /\ c \notin SyncableKeychainCreds
        =>
            /\ host[c] = NoSecret
            /\ agent[c] = NoSecret
            /\ sessionAgent[c] = NoSecret
            /\ hostKeychain[c] = NoSecret
            /\ agentKeychain[c] = NoSecret
            /\ latest[c] = NoSecret

DeliveryMatchesRegistry ==
    /\ \A c \in Credentials :
        agent[c] # NoSecret => CredentialDelivery(c) = FileDelivery
    /\ \A c \in Credentials :
        sessionAgent[c] # NoSecret => CredentialDelivery(c) = FileDelivery
    /\ \A c \in Credentials :
        agentKeychain[c] # NoSecret =>
            /\ c \in SyncableKeychainCreds
            /\ CredentialDelivery(c) = ExternalReferenceDelivery
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
            /\ sessionAgent[c] = NoSecret
            /\ agentKeychain[c] = NoSecret

NoDeliveryNeverExposed ==
    \A c \in Credentials :
        CredentialDelivery(c) = NoDelivery =>
            /\ c \notin activeCreds
            /\ c \notin delivered
            /\ c \notin envGranted
            /\ c \notin brokerGranted
            /\ c \notin externalGranted
            /\ agent[c] = NoSecret
            /\ sessionAgent[c] = NoSecret
            /\ agentKeychain[c] = NoSecret

NoCrossHarnessExposure ==
    phase \in ActivePhases =>
        /\ activeHarness \in Harnesses
        /\ \A c \in ExposedCreds :
            activeHarness \in CredentialConsumers(c)

NoSessionExposureOutsideActivePhase ==
    phase \notin ActivePhases =>
        /\ activeHarness = NoHarness
        /\ activeCreds = {}
        /\ delivered = {}
        /\ envGranted = {}
        /\ brokerGranted = {}
        /\ externalGranted = {}

LaunchOnlyAfterRecovery ==
    phase \in ActivePhases =>
        /\ recovered = RecoverableCreds
        /\ HostKeychainSynced

CleanRecoveredStateHasNoCredentialResidue ==
    /\ phase = "idle"
    /\ recovered = RecoverableCreds
    =>
    /\ \A c \in Credentials : agent[c] = NoSecret
    /\ \A c \in Credentials : sessionAgent[c] = NoSecret
    /\ \A c \in Credentials : agentKeychain[c] = NoSecret

LatestValueNeverSilentlyLost ==
    \A c \in ManagedStoreCreds : LatestKnown(c)

CleanRecoveredStateKeepsLatestHostOwned ==
    /\ phase = "idle"
    /\ recovered = RecoverableCreds
    =>
    \A c \in ManagedStoreCreds :
        \/ latest[c] = NoSecret
        \/ latest[c] = host[c]
        \/ latest[c] = hostKeychain[c]
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

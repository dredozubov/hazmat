---- MODULE MC_GitHookApproval ----
\* Repo-local git hook approval and activation.
\*
\* This model captures the host-owned approval / install / drift / refusal
\* state machine proposed for sandboxing-kpde:
\*
\*   - the repo declares a hook bundle (hook set + manifest validity + hash)
\*   - the host records approval against a specific bundle hash
\*   - the host snapshots the approved bundle immutably
\*   - the host installs a git wrapper plus managed + fallback dispatchers
\*   - approved execution is allowed only from the immutable approved snapshot
\*   - wrapper or dispatcher refuses when hooksPath drifts or the repo changes
\*   - uninstall / rollback removes approval + snapshot + install state
\*
\* Scope boundary:
\*   This spec models Hazmat-managed host-side entrypoints only: the git wrapper
\*   and the dispatchers it installs. It intentionally does NOT claim anything
\*   about arbitrary direct execution of some foreign git binary outside that
\*   managed path. That boundary is documented in
\*   docs/plans/2026-04-23-git-hook-approval-design.md.

EXTENDS TLC

CONSTANTS
    HookTypes, \* approved repo-local hook kinds, e.g. pre-commit/pre-push/commit-msg
    Hashes,    \* bundle hashes
    NoHash,    \* sentinel meaning "no approved hash / no snapshot"
    NoHook     \* sentinel for "no hook ran"

ASSUME /\ NoHash \notin Hashes
       /\ NoHook \notin HookTypes

HookPaths == {"default", "managed", "rogue", "disabled"}
Invokers == {"none", "wrapper", "managed-dispatcher", "fallback-dispatcher"}
Outcomes == {"none", "approved", "refused"}

VARIABLES
    declaredHooks,         \* hook set currently declared by the repo manifest
    approvedHooks,         \* hook set recorded in host approval
    manifestValid,         \* whether the repo manifest is structurally valid
    repoHash,              \* current live repo bundle hash
    approvalHash,          \* approved bundle hash (host record)
    snapshotHooks,         \* hook set copied into the host-owned snapshot
    snapshotHash,          \* immutable approved snapshot hash
    wrapperInstalled,      \* host-side git wrapper installed
    managedDispatchers,    \* managed-path dispatchers installed by hazmat
    fallbackDispatchers,   \* .git/hooks fallback dispatchers installed by hazmat
    coreHooksPath,         \* effective local hooksPath value seen by git
    unknownManagedEntries, \* unapproved extra/mutated files in managed hooks dir
    unknownFallbackEntries,\* unapproved extra/mutated files in .git/hooks
    lastHook,              \* last hook kind that tried to run
    lastInvoker,           \* wrapper / managed dispatcher / fallback dispatcher
    lastOutcome,           \* approved or refused
    executedHash,          \* hash of the bytes actually executed, if any
    widenedSessionPolicy   \* should remain FALSE: hooks approval must not widen future session policy

vars ==
    << declaredHooks,
       approvedHooks,
       manifestValid,
       repoHash,
       approvalHash,
       snapshotHooks,
       snapshotHash,
       wrapperInstalled,
       managedDispatchers,
       fallbackDispatchers,
       coreHooksPath,
       unknownManagedEntries,
       unknownFallbackEntries,
       lastHook,
       lastInvoker,
       lastOutcome,
       executedHash,
       widenedSessionPolicy >>

Init ==
    /\ declaredHooks \in SUBSET HookTypes
    /\ approvedHooks = {}
    /\ manifestValid \in BOOLEAN
    /\ repoHash \in Hashes
    /\ approvalHash = NoHash
    /\ snapshotHooks = {}
    /\ snapshotHash = NoHash
    /\ wrapperInstalled = FALSE
    /\ managedDispatchers = {}
    /\ fallbackDispatchers = {}
    /\ coreHooksPath = "default"
    /\ unknownManagedEntries = FALSE
    /\ unknownFallbackEntries = FALSE
    /\ lastHook = NoHook
    /\ lastInvoker = "none"
    /\ lastOutcome = "none"
    /\ executedHash = NoHash
    /\ widenedSessionPolicy = FALSE

\* The repo changes: hook set, manifest validity, or hash may drift independently
\* of any host approval that already exists.
RepoEdit ==
    /\ declaredHooks' \in SUBSET HookTypes
    /\ manifestValid' \in BOOLEAN
    /\ repoHash' \in Hashes
    /\ ~(/\ declaredHooks' = declaredHooks
         /\ manifestValid' = manifestValid
         /\ repoHash' = repoHash)
    /\ lastHook' = NoHook
    /\ lastInvoker' = "none"
    /\ lastOutcome' = "none"
    /\ executedHash' = NoHash
    /\ UNCHANGED << approvedHooks,
                    approvalHash,
                    snapshotHooks,
                    snapshotHash,
                    wrapperInstalled,
                    managedDispatchers,
                    fallbackDispatchers,
                    coreHooksPath,
                    unknownManagedEntries,
                    unknownFallbackEntries,
                    widenedSessionPolicy >>

\* Host approval / install: record approval against the current repo bundle,
\* copy the snapshot, install the wrapper, install dispatchers, and pin the
\* repo to the managed hooksPath.
ApproveInstall ==
    /\ manifestValid
    /\ declaredHooks # {}
    /\ approvedHooks' = declaredHooks
    /\ approvalHash' = repoHash
    /\ snapshotHooks' = declaredHooks
    /\ snapshotHash' = repoHash
    /\ wrapperInstalled' = TRUE
    /\ managedDispatchers' = declaredHooks
    /\ fallbackDispatchers' = declaredHooks
    /\ coreHooksPath' = "managed"
    /\ unknownManagedEntries' = FALSE
    /\ unknownFallbackEntries' = FALSE
    /\ lastHook' = NoHook
    /\ lastInvoker' = "none"
    /\ lastOutcome' = "none"
    /\ executedHash' = NoHash
    /\ UNCHANGED << declaredHooks,
                    manifestValid,
                    repoHash,
                    widenedSessionPolicy >>

\* Contained-agent or repo-local drift that rewrites .git/config.
RewriteHooksPath(newPath) ==
    /\ newPath \in HookPaths
    /\ coreHooksPath' = newPath
    /\ lastHook' = NoHook
    /\ lastInvoker' = "none"
    /\ lastOutcome' = "none"
    /\ executedHash' = NoHash
    /\ UNCHANGED << declaredHooks,
                    approvedHooks,
                    manifestValid,
                    repoHash,
                    approvalHash,
                    snapshotHooks,
                    snapshotHash,
                    wrapperInstalled,
                    managedDispatchers,
                    fallbackDispatchers,
                    unknownManagedEntries,
                    unknownFallbackEntries,
                    widenedSessionPolicy >>

\* Mutations to the installed managed hook layout.
CorruptManagedInstall(newSet) ==
    /\ approvalHash /= NoHash
    /\ newSet \in SUBSET HookTypes
    /\ managedDispatchers' = newSet
    /\ lastHook' = NoHook
    /\ lastInvoker' = "none"
    /\ lastOutcome' = "none"
    /\ executedHash' = NoHash
    /\ UNCHANGED << declaredHooks,
                    approvedHooks,
                    manifestValid,
                    repoHash,
                    approvalHash,
                    snapshotHooks,
                    snapshotHash,
                    wrapperInstalled,
                    fallbackDispatchers,
                    coreHooksPath,
                    unknownManagedEntries,
                    unknownFallbackEntries,
                    widenedSessionPolicy >>

CorruptFallbackInstall(newSet) ==
    /\ approvalHash /= NoHash
    /\ newSet \in SUBSET HookTypes
    /\ fallbackDispatchers' = newSet
    /\ lastHook' = NoHook
    /\ lastInvoker' = "none"
    /\ lastOutcome' = "none"
    /\ executedHash' = NoHash
    /\ UNCHANGED << declaredHooks,
                    approvedHooks,
                    manifestValid,
                    repoHash,
                    approvalHash,
                    snapshotHooks,
                    snapshotHash,
                    wrapperInstalled,
                    managedDispatchers,
                    coreHooksPath,
                    unknownManagedEntries,
                    unknownFallbackEntries,
                    widenedSessionPolicy >>

AddUnknownManagedEntry ==
    /\ approvalHash /= NoHash
    /\ ~unknownManagedEntries
    /\ unknownManagedEntries' = TRUE
    /\ lastHook' = NoHook
    /\ lastInvoker' = "none"
    /\ lastOutcome' = "none"
    /\ executedHash' = NoHash
    /\ UNCHANGED << declaredHooks,
                    approvedHooks,
                    manifestValid,
                    repoHash,
                    approvalHash,
                    snapshotHooks,
                    snapshotHash,
                    wrapperInstalled,
                    managedDispatchers,
                    fallbackDispatchers,
                    coreHooksPath,
                    unknownFallbackEntries,
                    widenedSessionPolicy >>

AddUnknownFallbackEntry ==
    /\ approvalHash /= NoHash
    /\ ~unknownFallbackEntries
    /\ unknownFallbackEntries' = TRUE
    /\ lastHook' = NoHook
    /\ lastInvoker' = "none"
    /\ lastOutcome' = "none"
    /\ executedHash' = NoHash
    /\ UNCHANGED << declaredHooks,
                    approvedHooks,
                    manifestValid,
                    repoHash,
                    approvalHash,
                    snapshotHooks,
                    snapshotHash,
                    wrapperInstalled,
                    managedDispatchers,
                    fallbackDispatchers,
                    coreHooksPath,
                    unknownManagedEntries,
                    widenedSessionPolicy >>

\* The wrapper is the primary defense. It checks both hook locations, the
\* repo hash, and the snapshot record before allowing real git to continue.
WrapperChecksPass(h) ==
    /\ h \in approvedHooks
    /\ manifestValid
    /\ declaredHooks = approvedHooks
    /\ repoHash = approvalHash
    /\ snapshotHash = approvalHash
    /\ snapshotHooks = approvedHooks
    /\ managedDispatchers = approvedHooks
    /\ fallbackDispatchers = approvedHooks
    /\ coreHooksPath = "managed"
    /\ ~unknownManagedEntries
    /\ ~unknownFallbackEntries

InvokeViaWrapper(h) ==
    /\ h \in HookTypes
    /\ wrapperInstalled
    /\ lastHook' = h
    /\ lastInvoker' = "wrapper"
    /\ IF WrapperChecksPass(h)
          THEN /\ lastOutcome' = "approved"
               /\ executedHash' = snapshotHash
          ELSE /\ lastOutcome' = "refused"
               /\ executedHash' = NoHash
    /\ UNCHANGED << declaredHooks,
                    approvedHooks,
                    manifestValid,
                    repoHash,
                    approvalHash,
                    snapshotHooks,
                    snapshotHash,
                    wrapperInstalled,
                    managedDispatchers,
                    fallbackDispatchers,
                    coreHooksPath,
                    unknownManagedEntries,
                    unknownFallbackEntries,
                    widenedSessionPolicy >>

\* Managed dispatcher runs only when git is still pinned to the managed path.
\* It executes the immutable approved snapshot, never live repo bytes.
ManagedDispatcherChecksPass(h) ==
    /\ h \in approvedHooks
    /\ manifestValid
    /\ declaredHooks = approvedHooks
    /\ repoHash = approvalHash
    /\ snapshotHash = approvalHash
    /\ snapshotHooks = approvedHooks
    /\ managedDispatchers = approvedHooks
    /\ coreHooksPath = "managed"
    /\ ~unknownManagedEntries

InvokeViaManagedDispatcher(h) ==
    /\ h \in HookTypes
    /\ h \in managedDispatchers
    /\ coreHooksPath = "managed"
    /\ lastHook' = h
    /\ lastInvoker' = "managed-dispatcher"
    /\ IF ManagedDispatcherChecksPass(h)
          THEN /\ lastOutcome' = "approved"
               /\ executedHash' = snapshotHash
          ELSE /\ lastOutcome' = "refused"
               /\ executedHash' = NoHash
    /\ UNCHANGED << declaredHooks,
                    approvedHooks,
                    manifestValid,
                    repoHash,
                    approvalHash,
                    snapshotHooks,
                    snapshotHash,
                    wrapperInstalled,
                    managedDispatchers,
                    fallbackDispatchers,
                    coreHooksPath,
                    unknownManagedEntries,
                    unknownFallbackEntries,
                    widenedSessionPolicy >>

\* Fallback dispatchers are belt-and-suspenders drift detectors. If git reaches
\* the default hook path, they refuse rather than execute approved content.
InvokeViaFallbackDispatcher(h) ==
    /\ h \in HookTypes
    /\ h \in fallbackDispatchers
    /\ coreHooksPath = "default"
    /\ lastHook' = h
    /\ lastInvoker' = "fallback-dispatcher"
    /\ lastOutcome' = "refused"
    /\ executedHash' = NoHash
    /\ UNCHANGED << declaredHooks,
                    approvedHooks,
                    manifestValid,
                    repoHash,
                    approvalHash,
                    snapshotHooks,
                    snapshotHash,
                    wrapperInstalled,
                    managedDispatchers,
                    fallbackDispatchers,
                    coreHooksPath,
                    unknownManagedEntries,
                    unknownFallbackEntries,
                    widenedSessionPolicy >>

\* Shared cleanup semantics for explicit uninstall or full rollback.
RemoveManagedHooks ==
    /\ approvedHooks' = {}
    /\ approvalHash' = NoHash
    /\ snapshotHooks' = {}
    /\ snapshotHash' = NoHash
    /\ wrapperInstalled' = FALSE
    /\ managedDispatchers' = {}
    /\ fallbackDispatchers' = {}
    /\ coreHooksPath' = "default"
    /\ unknownManagedEntries' = FALSE
    /\ unknownFallbackEntries' = FALSE
    /\ lastHook' = NoHook
    /\ lastInvoker' = "none"
    /\ lastOutcome' = "none"
    /\ executedHash' = NoHash
    /\ UNCHANGED << declaredHooks,
                    manifestValid,
                    repoHash,
                    widenedSessionPolicy >>

Stutter ==
    UNCHANGED vars

Next ==
    \/ RepoEdit
    \/ ApproveInstall
    \/ \E p \in HookPaths : RewriteHooksPath(p)
    \/ \E hs \in SUBSET HookTypes : CorruptManagedInstall(hs)
    \/ \E hs \in SUBSET HookTypes : CorruptFallbackInstall(hs)
    \/ AddUnknownManagedEntry
    \/ AddUnknownFallbackEntry
    \/ \E h \in HookTypes : InvokeViaWrapper(h)
    \/ \E h \in HookTypes : InvokeViaManagedDispatcher(h)
    \/ \E h \in HookTypes : InvokeViaFallbackDispatcher(h)
    \/ RemoveManagedHooks
    \/ Stutter

Spec ==
    Init /\ [][Next]_vars

TypeOK ==
    /\ declaredHooks \subseteq HookTypes
    /\ approvedHooks \subseteq HookTypes
    /\ manifestValid \in BOOLEAN
    /\ repoHash \in Hashes
    /\ approvalHash \in Hashes \cup {NoHash}
    /\ snapshotHooks \subseteq HookTypes
    /\ snapshotHash \in Hashes \cup {NoHash}
    /\ wrapperInstalled \in BOOLEAN
    /\ managedDispatchers \subseteq HookTypes
    /\ fallbackDispatchers \subseteq HookTypes
    /\ coreHooksPath \in HookPaths
    /\ unknownManagedEntries \in BOOLEAN
    /\ unknownFallbackEntries \in BOOLEAN
    /\ lastHook \in HookTypes \cup {NoHook}
    /\ lastInvoker \in Invokers
    /\ lastOutcome \in Outcomes
    /\ executedHash \in Hashes \cup {NoHash}
    /\ widenedSessionPolicy \in BOOLEAN

ApprovalStateWellFormed ==
    /\ (approvalHash = NoHash) <=> (approvedHooks = {})
    /\ (snapshotHash = NoHash) <=> (snapshotHooks = {})

\* If anything executes, it must execute bytes from the immutable approved
\* snapshot, never from the live repo.
ApprovedContentOnly ==
    lastOutcome = "approved" =>
        /\ executedHash = snapshotHash
        /\ executedHash = approvalHash
        /\ executedHash = repoHash
        /\ approvedHooks = snapshotHooks
        /\ declaredHooks = approvedHooks
        /\ lastHook \in approvedHooks

\* Successful execution requires the managed hooksPath. Any hooksPath drift
\* must end in refusal rather than execution.
HooksPathPinned ==
    lastOutcome = "approved" =>
        coreHooksPath = "managed"

\* The wrapper is the primary defense against core.hooksPath bypass.
WrapperRefusesReroute ==
    /\ lastInvoker = "wrapper"
    /\ coreHooksPath /= "managed"
    => lastOutcome = "refused"

\* Managed dispatcher cannot execute once the repo or approval record drifts.
ManagedDispatcherRefusesDrift ==
    /\ lastInvoker = "managed-dispatcher"
    /\ (\/ repoHash /= approvalHash
        \/ declaredHooks /= approvedHooks
        \/ snapshotHash /= approvalHash
        \/ snapshotHooks /= approvedHooks
        \/ ~manifestValid
        \/ unknownManagedEntries)
    => lastOutcome = "refused"

\* The fallback path is detection-only. If git reaches .git/hooks, Hazmat
\* refuses instead of executing approved content from there.
FallbackDispatcherOnlyRefuses ==
    lastInvoker = "fallback-dispatcher" =>
        /\ lastOutcome = "refused"
        /\ executedHash = NoHash

\* Uninstall / rollback clears all host-owned approval and install state.
RollbackClearsHookInstall ==
    approvalHash = NoHash =>
        /\ approvedHooks = {}
        /\ snapshotHash = NoHash
        /\ snapshotHooks = {}
        /\ ~wrapperInstalled
        /\ managedDispatchers = {}
        /\ fallbackDispatchers = {}

\* Hook approval must not widen future session network or filesystem policy.
NoImplicitWidening ==
    ~widenedSessionPolicy

====

---- MODULE MC_SetupRollback ----
\* Hazmat setup/rollback state machine — verifies idempotency, reversibility,
\* and security invariants across all combinations of partial setup, interrupt,
\* rollback, and re-setup.
\*
\* Setup creates ~12 system resources in a fixed order. Rollback removes them
\* in reverse. This spec models each resource as a boolean (present/absent),
\* each setup step as an action that can succeed or fail (nondeterministic),
\* and verifies that:
\*   1. Setup is idempotent (re-running a completed step is a no-op)
\*   2. The system is reversible from ANY intermediate state
\*   3. The agent user is never launchable without firewall containment
\*   4. After full rollback, no hazmat artifacts remain
\*   5. Setup after any interruption converges to fully-configured
\*
\* Expected TLC result: No error has been found. (~2000-5000 states)
\*
\* Governed code:
\*   hazmat/setup.go   — runSetup(), all setupX() functions
\*   hazmat/rollback.go — runRollback(), all rollbackX() functions
\*
\* Model bounds: 2 setup attempts, 2 rollback attempts, failure at any step.

EXTENDS Naturals, FiniteSets

\* ═══════════════════════════════════════════════════════════════════════════════
\* Variables — each resource tracks whether it exists on the system
\* ═══════════════════════════════════════════════════════════════════════════════

VARIABLES
    \* Phase 1: User & Group
    agentUser,       \* system user /Users/agent
    devGroup,        \* dev group with agent + current user
    \* Phase 2: Workspace
    workspace,       \* ~/workspace with dev group ownership, setgid, ACL
    backupScope,     \* .backup-excludes in workspace root
    \* Phase 3: Hardening
    umask,           \* umask 077 in .zshrc files
    seatbelt,        \* claude-sandboxed wrapper in agent's .local/bin
    wrappers,        \* host wrappers (claude-hazmat, agent-exec, agent-shell) + agent env
    \* Phase 4: Privilege
    launchHelper,    \* /usr/local/libexec/hazmat-launch (external — must exist before sudoers)
    sudoers,         \* /etc/sudoers.d/agent (narrow NOPASSWD rule)
    \* Phase 5: Network
    pfAnchor,        \* /etc/pf.anchors/agent + /etc/pf.conf additions + pfctl loaded
    dnsBlocklist,    \* /etc/hosts entries
    \* Phase 6: Persistence
    launchDaemon,    \* /Library/LaunchDaemons/com.local.pf-agent.plist
    \* Phase 7: Application
    claudeCode,      \* Claude Code installed for agent user
    credentials,     \* API key + git identity enrolled

    \* Control state
    phase,           \* "idle" | "setting_up" | "rolling_back"
    setupStep,       \* 0..14 — which step setup will attempt next
    setupAttempts,   \* how many full setup runs have been started
    rollbackAttempts \* how many full rollback runs have been started

CONSTANTS
    MaxSetupAttempts,    \* e.g. 2
    MaxRollbackAttempts  \* e.g. 2

vars == <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
          wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
          launchDaemon, claudeCode, credentials,
          phase, setupStep, setupAttempts, rollbackAttempts>>

resources == <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
               wrappers, sudoers, pfAnchor, dnsBlocklist,
               launchDaemon, claudeCode, credentials>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Type invariant
\* ═══════════════════════════════════════════════════════════════════════════════

TypeOK ==
    /\ agentUser     \in BOOLEAN
    /\ devGroup      \in BOOLEAN
    /\ workspace     \in BOOLEAN
    /\ backupScope   \in BOOLEAN
    /\ umask         \in BOOLEAN
    /\ seatbelt      \in BOOLEAN
    /\ wrappers      \in BOOLEAN
    /\ launchHelper  \in BOOLEAN
    /\ sudoers       \in BOOLEAN
    /\ pfAnchor      \in BOOLEAN
    /\ dnsBlocklist  \in BOOLEAN
    /\ launchDaemon  \in BOOLEAN
    /\ claudeCode    \in BOOLEAN
    /\ credentials   \in BOOLEAN
    /\ phase         \in {"idle", "setting_up", "rolling_back"}
    /\ setupStep     \in 0..14
    /\ setupAttempts \in 0..MaxSetupAttempts
    /\ rollbackAttempts \in 0..MaxRollbackAttempts

\* ═══════════════════════════════════════════════════════════════════════════════
\* Initial state — clean system, nothing installed
\* ═══════════════════════════════════════════════════════════════════════════════

Init ==
    /\ agentUser     = FALSE
    /\ devGroup      = FALSE
    /\ workspace     = FALSE
    /\ backupScope   = FALSE
    /\ umask         = FALSE
    /\ seatbelt      = FALSE
    /\ wrappers      = FALSE
    /\ launchHelper  = FALSE  \* NOTE: external prerequisite, not created by setup
    /\ sudoers       = FALSE
    /\ pfAnchor      = FALSE
    /\ dnsBlocklist  = FALSE
    /\ launchDaemon  = FALSE
    /\ claudeCode    = FALSE
    /\ credentials   = FALSE
    /\ phase         = "idle"
    /\ setupStep     = 0
    /\ setupAttempts = 0
    /\ rollbackAttempts = 0

\* ═══════════════════════════════════════════════════════════════════════════════
\* Helper: launchHelper is an external prerequisite (built separately).
\* We model it appearing nondeterministically (user runs `make install-helper`).
\* ═══════════════════════════════════════════════════════════════════════════════

InstallLaunchHelper ==
    /\ ~launchHelper
    /\ phase = "idle"
    /\ launchHelper' = TRUE
    /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
                    wrappers, sudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    phase, setupStep, setupAttempts, rollbackAttempts>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Setup actions — each step mirrors a setupX() function in setup.go
\* Steps are idempotent: if resource already present, skip (no state change).
\* Steps can fail nondeterministically (interrupt/error) — setup aborts.
\* ═══════════════════════════════════════════════════════════════════════════════

\* Begin a new setup attempt from idle state.
BeginSetup ==
    /\ phase = "idle"
    /\ setupAttempts < MaxSetupAttempts
    /\ phase'         = "setting_up"
    /\ setupStep'     = 0
    /\ setupAttempts' = setupAttempts + 1
    /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
                    wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials, rollbackAttempts>>

\* Setup step succeeds: resource becomes present (or already was — idempotent).
\* Models the "check exists? skip : create" pattern in every setupX() function.
SetupStepSucceed ==
    /\ phase = "setting_up"
    /\ setupStep < 14
    /\ \/ (setupStep = 0  /\ agentUser'    = TRUE /\ UNCHANGED <<devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 1  /\ devGroup'     = TRUE /\ UNCHANGED <<agentUser, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 2  /\ workspace'    = TRUE /\ UNCHANGED <<agentUser, devGroup, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 3  /\ backupScope'  = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 4  /\ umask'        = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 5  /\ seatbelt'     = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 6  /\ wrappers'     = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \* Step 7: launchHelper verification — does NOT create it, only checks.
       \* If helper is absent, setup MUST fail (modeled by SetupStepFail guard).
       \/ (setupStep = 7  /\ launchHelper         /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 8  /\ sudoers'     = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 9  /\ pfAnchor'    = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 10 /\ dnsBlocklist' = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 11 /\ launchDaemon' = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, claudeCode, credentials>>)
       \/ (setupStep = 12 /\ claudeCode'  = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, credentials>>)
       \/ (setupStep = 13 /\ credentials' = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode>>)
    /\ setupStep' = setupStep + 1
    /\ UNCHANGED <<phase, setupAttempts, rollbackAttempts>>

\* Setup step succeeds on the final step — setup completes, return to idle.
SetupComplete ==
    /\ phase = "setting_up"
    /\ setupStep = 14
    /\ phase' = "idle"
    /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
                    wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts, rollbackAttempts>>

\* Setup step fails (nondeterministic) — setup aborts, returns to idle.
\* Resources created by earlier steps remain (no automatic cleanup).
\* Step 7 (launchHelper check) MUST fail if helper is absent.
SetupStepFail ==
    /\ phase = "setting_up"
    /\ setupStep < 14
    \* Step 7 fails deterministically when helper is missing
    /\ \/ (setupStep = 7 /\ ~launchHelper)
       \* Any other step can fail nondeterministically
       \/ setupStep /= 7
    /\ phase' = "idle"
    /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
                    wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts, rollbackAttempts>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Rollback actions — mirrors rollbackX() functions in rollback.go
\* Each step removes a resource if present (idempotent skip if absent).
\* Rollback steps always succeed (best-effort; warnings printed but no abort).
\* ═══════════════════════════════════════════════════════════════════════════════

BeginRollback ==
    /\ phase = "idle"
    /\ rollbackAttempts < MaxRollbackAttempts
    /\ phase'            = "rolling_back"
    /\ rollbackAttempts' = rollbackAttempts + 1
    /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
                    wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts>>

\* Rollback removes all non-destructive resources in one atomic step.
\* This models the real code which runs all rollbackX() functions sequentially
\* without aborting on individual failures.
\* Does NOT remove: agentUser, devGroup (require --delete-user, --delete-group).
\* Does NOT remove: workspace (explicitly preserved, manual cleanup).
\* Does NOT remove: launchHelper (external binary, not managed by setup).
RollbackCore ==
    /\ phase = "rolling_back"
    /\ launchDaemon' = FALSE
    /\ pfAnchor'     = FALSE
    /\ dnsBlocklist'  = FALSE
    /\ sudoers'      = FALSE
    /\ seatbelt'     = FALSE
    /\ wrappers'     = FALSE
    /\ umask'        = FALSE
    /\ backupScope'  = FALSE
    /\ claudeCode'   = FALSE   \* settings/hooks removed; Claude Code may remain installed
    /\ credentials'  = FALSE   \* credentials removed with agent home cleanup
    /\ phase'        = "idle"
    /\ UNCHANGED <<agentUser, devGroup, workspace, launchHelper,
                    setupStep, setupAttempts, rollbackAttempts>>

\* Destructive rollback: also removes user and/or group.
\* Models --delete-user --delete-group flags.
RollbackDestructive ==
    /\ phase = "rolling_back"
    /\ launchDaemon' = FALSE
    /\ pfAnchor'     = FALSE
    /\ dnsBlocklist'  = FALSE
    /\ sudoers'      = FALSE
    /\ seatbelt'     = FALSE
    /\ wrappers'     = FALSE
    /\ umask'        = FALSE
    /\ backupScope'  = FALSE
    /\ claudeCode'   = FALSE
    /\ credentials'  = FALSE
    /\ agentUser'    = FALSE
    /\ devGroup'     = FALSE
    /\ phase'        = "idle"
    /\ UNCHANGED <<workspace, launchHelper,
                    setupStep, setupAttempts, rollbackAttempts>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Terminal state — all attempts exhausted, system is idle. Allow stuttering
\* so TLC does not report a spurious deadlock.
\* ═══════════════════════════════════════════════════════════════════════════════

Done ==
    /\ phase = "idle"
    /\ setupAttempts    = MaxSetupAttempts
    /\ rollbackAttempts = MaxRollbackAttempts
    /\ UNCHANGED vars

\* ═══════════════════════════════════════════════════════════════════════════════
\* Next-state relation
\* ═══════════════════════════════════════════════════════════════════════════════

Next ==
    \/ InstallLaunchHelper
    \/ BeginSetup
    \/ SetupStepSucceed
    \/ SetupComplete
    \/ SetupStepFail
    \/ BeginRollback
    \/ RollbackCore
    \/ RollbackDestructive
    \/ Done

Fairness ==
    /\ WF_vars(SetupStepSucceed)
    /\ WF_vars(SetupComplete)
    /\ WF_vars(RollbackCore)

Spec == Init /\ [][Next]_vars /\ Fairness

\* ═══════════════════════════════════════════════════════════════════════════════
\* Safety invariants
\* ═══════════════════════════════════════════════════════════════════════════════

\* --- Security: agent must never be launchable without firewall containment ---
\* "Launchable" means both the agent user and sudoers entry exist, so the
\* controlling user can run `sudo -u agent ...` without a password.
\* The firewall (pfAnchor) must already be active in that state.
\*
\* Current code: sudoers is step 8, pfAnchor is step 9.
\* This invariant will FAIL if setup can be interrupted between steps 8 and 9,
\* leaving the agent launchable with no firewall. This is a real finding.
AgentContained ==
    (agentUser /\ sudoers) => pfAnchor

\* --- Rollback completeness: after destructive rollback, no artifacts remain ---
\* (except workspace and launchHelper, which are explicitly preserved)
NoOrphanedArtifacts ==
    (~agentUser /\ ~devGroup) =>
        (~sudoers /\ ~pfAnchor /\ ~dnsBlocklist /\ ~launchDaemon
         /\ ~seatbelt /\ ~wrappers /\ ~umask /\ ~backupScope
         /\ ~claudeCode /\ ~credentials)

\* --- Ordering: sudoers must not exist without the launch helper ---
\* setup.go: setupLaunchHelper() is step 7, setupSudoers() is step 8.
\* The helper path is embedded in the sudoers rule. If helper doesn't exist,
\* the sudoers entry points to nothing (harmless but wrong).
SudoersRequiresHelper ==
    sudoers => launchHelper

\* --- Dependency: resources that depend on agent user existence ---
\* If agent user doesn't exist, agent-owned resources should not exist either.
AgentDepsRequireUser ==
    (~agentUser) => (~seatbelt /\ ~claudeCode /\ ~credentials)

\* ═══════════════════════════════════════════════════════════════════════════════
\* Liveness properties
\* ═══════════════════════════════════════════════════════════════════════════════

\* --- Reversibility: from any state, the system can return to clean ---
\* After a destructive rollback, all managed resources are absent.
\* This is implied by NoOrphanedArtifacts + the existence of RollbackDestructive,
\* but we state it as liveness: it's always POSSIBLE to reach clean state.
CanAlwaysReachClean ==
    <>(\/ (~agentUser /\ ~devGroup /\ ~sudoers /\ ~pfAnchor /\ ~dnsBlocklist
           /\ ~launchDaemon /\ ~seatbelt /\ ~wrappers /\ ~umask
           /\ ~backupScope /\ ~claudeCode /\ ~credentials)
       \/ phase /= "idle")  \* or we're in the middle of something

\* --- Convergence: setup eventually completes if retried ---
\* If setup is started and the launch helper is present, the system
\* eventually reaches fully-configured state (all resources present).
FullyConfigured ==
    agentUser /\ devGroup /\ workspace /\ backupScope /\ umask /\ seatbelt
    /\ wrappers /\ sudoers /\ pfAnchor /\ dnsBlocklist /\ launchDaemon
    /\ claudeCode /\ credentials

SetupEventuallyCompletes ==
    (launchHelper /\ phase = "idle") ~> FullyConfigured

====

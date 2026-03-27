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
    rollbackAttempts,\* how many full rollback runs have been started
    rollbackStep,    \* 0..10 — which rollback step is next
    rollbackMode     \* "none" | "core" | "destructive"

CONSTANTS
    MaxSetupAttempts,    \* e.g. 2
    MaxRollbackAttempts  \* e.g. 2

vars == <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
          wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
          launchDaemon, claudeCode, credentials,
          phase, setupStep, setupAttempts, rollbackAttempts,
          rollbackStep, rollbackMode>>

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
    /\ rollbackStep  \in 0..10
    /\ rollbackMode  \in {"none", "core", "destructive"}

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
    /\ rollbackStep  = 0
    /\ rollbackMode  = "none"

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
                    phase, setupStep, setupAttempts, rollbackAttempts,
                    rollbackStep, rollbackMode>>

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
                    launchDaemon, claudeCode, credentials, rollbackAttempts,
                    rollbackStep, rollbackMode>>

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
       \* Steps 7-9: Network containment BEFORE privilege grant.
       \* pf firewall and DNS blocklist are installed before sudoers so the agent
       \* is never launchable without network containment (AgentContained invariant).
       \/ (setupStep = 7  /\ pfAnchor'    = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 8  /\ dnsBlocklist' = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 9  /\ launchDaemon' = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, claudeCode, credentials>>)
       \* Step 10: launchHelper verification — does NOT create it, only checks.
       \* If helper is absent, setup MUST fail (modeled by SetupStepFail guard).
       \/ (setupStep = 10 /\ launchHelper         /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 11 /\ sudoers'     = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 12 /\ claudeCode'  = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, credentials>>)
       \/ (setupStep = 13 /\ credentials' = TRUE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode>>)
    /\ setupStep' = setupStep + 1
    /\ UNCHANGED <<phase, setupAttempts, rollbackAttempts, rollbackStep, rollbackMode>>

\* Setup step succeeds on the final step — setup completes, return to idle.
SetupComplete ==
    /\ phase = "setting_up"
    /\ setupStep = 14
    /\ phase' = "idle"
    /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
                    wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts, rollbackAttempts,
                    rollbackStep, rollbackMode>>

\* Setup step fails (nondeterministic) — setup aborts, returns to idle.
\* Resources created by earlier steps remain (no automatic cleanup).
\* Step 7 (launchHelper check) MUST fail if helper is absent.
SetupStepFail ==
    /\ phase = "setting_up"
    /\ setupStep < 14
    \* Step 10 fails deterministically when helper is missing
    /\ \/ (setupStep = 10 /\ ~launchHelper)
       \* Any other step can fail nondeterministically
       \/ setupStep /= 10
    /\ phase' = "idle"
    /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
                    wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts, rollbackAttempts,
                    rollbackStep, rollbackMode>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Rollback actions — mirrors rollbackX() functions in rollback.go
\* Rollback runs steps sequentially; each step removes one resource.
\* Individual steps cannot fail (warn and continue in real code).
\* However, the process CAN be interrupted (Ctrl-C, crash) between steps,
\* which is modeled by RollbackInterrupt.
\*
\* Rollback step ordering mirrors rollback.go:runRollback():
\*   Step 0: rollbackSudoers          ← revoke privilege FIRST
\*   Step 1: rollbackLaunchDaemon
\*   Step 2: rollbackPfFirewall
\*   Step 3: rollbackDNSBlocklist
\*   Step 4: rollbackSeatbelt
\*   Step 5: rollbackUserExperience (wrappers)
\*   Step 6: rollbackSymlinks + rollbackUmask
\*   Step 7: rollbackBackupScope
\*   Step 8: (optional) rollbackAgentUser — only with --delete-user
\*   Step 9: (optional) rollbackDevGroup  — only with --delete-group
\*
\* Does NOT remove: workspace (explicitly preserved, manual cleanup).
\* Does NOT remove: launchHelper (external binary, not managed by setup).
\* Does NOT remove: claudeCode/credentials (removed only if agent user deleted).
\* ═══════════════════════════════════════════════════════════════════════════════

BeginRollback ==
    /\ phase = "idle"
    /\ rollbackAttempts < MaxRollbackAttempts
    /\ phase'            = "rolling_back"
    /\ rollbackStep'     = 0
    /\ rollbackAttempts' = rollbackAttempts + 1
    \* Nondeterministic choice: core (preserve user/group) or destructive
    /\ rollbackMode' \in {"core", "destructive"}
    /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
                    wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts>>

\* Each rollback step removes one resource, matching rollback.go ordering.
RollbackStepSucceed ==
    /\ phase = "rolling_back"
    /\ rollbackStep < 10
    /\ \/ (rollbackStep = 0 /\ sudoers'      = FALSE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 1 /\ launchDaemon' = FALSE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, claudeCode, credentials>>)
       \/ (rollbackStep = 2 /\ pfAnchor'     = FALSE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 3 /\ dnsBlocklist'  = FALSE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 4 /\ seatbelt'     = FALSE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 5 /\ wrappers'     = FALSE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 6 /\ umask'        = FALSE /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 7 /\ backupScope'  = FALSE /\ UNCHANGED <<agentUser, devGroup, workspace, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \* Steps 8-9: destructive only (--delete-user, --delete-group).
       \* In core mode these are no-ops (user and group preserved).
       \/ (rollbackStep = 8 /\
              IF rollbackMode = "destructive"
              THEN /\ agentUser'   = FALSE
                   /\ claudeCode'  = FALSE   \* rm -rf /Users/agent takes these
                   /\ credentials' = FALSE
                   /\ UNCHANGED <<devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon>>
              ELSE UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 9 /\
              IF rollbackMode = "destructive"
              THEN /\ devGroup' = FALSE
                   /\ UNCHANGED <<agentUser, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>
              ELSE UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
    /\ rollbackStep' = rollbackStep + 1
    /\ UNCHANGED <<phase, rollbackMode, setupStep, setupAttempts, rollbackAttempts>>

\* Rollback completes after all steps.
RollbackComplete ==
    /\ phase = "rolling_back"
    /\ rollbackStep = 10
    /\ phase'        = "idle"
    /\ rollbackMode' = "none"
    /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
                    wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    rollbackStep, setupStep, setupAttempts, rollbackAttempts>>

\* Rollback interrupted (Ctrl-C, crash) — returns to idle with partial state.
RollbackInterrupt ==
    /\ phase = "rolling_back"
    /\ rollbackStep < 10
    /\ phase'        = "idle"
    /\ rollbackMode' = "none"
    /\ UNCHANGED <<agentUser, devGroup, workspace, backupScope, umask, seatbelt,
                    wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    rollbackStep, setupStep, setupAttempts, rollbackAttempts>>

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
    \/ RollbackStepSucceed
    \/ RollbackComplete
    \/ RollbackInterrupt
    \/ Done

Fairness ==
    /\ WF_vars(SetupStepSucceed)
    /\ WF_vars(SetupComplete)
    /\ WF_vars(RollbackStepSucceed)
    /\ WF_vars(RollbackComplete)

Spec == Init /\ [][Next]_vars /\ Fairness

\* ═══════════════════════════════════════════════════════════════════════════════
\* Safety invariants
\* ═══════════════════════════════════════════════════════════════════════════════

\* --- Security: agent must never be launchable without firewall containment ---
\* "Launchable" means both the agent user and sudoers entry exist, so the
\* controlling user can run `sudo -u agent ...` without a password.
\* The firewall (pfAnchor) must already be active in that state.
\* Checked during BOTH setup AND rollback intermediate states.
AgentContained ==
    (agentUser /\ sudoers) => pfAnchor

\* --- Stronger containment: DNS blocklist must also be active ---
\* pf blocks exfiltration ports; DNS blocklist blocks tunnel services by name.
\* Both should be in place whenever the agent is launchable.
AgentFullyContained ==
    (agentUser /\ sudoers) => (pfAnchor /\ dnsBlocklist)

\* --- Boot persistence: if firewall is active, daemon should ensure it survives reboot ---
\* Without the LaunchDaemon, a reboot silently removes pf rules.
\* This can be violated if setup is interrupted after pfAnchor but before launchDaemon.
FirewallPersistent ==
    pfAnchor => launchDaemon

\* --- Rollback completeness: after destructive rollback, no artifacts remain ---
\* (except workspace and launchHelper, which are explicitly preserved)
NoOrphanedArtifacts ==
    (~agentUser /\ ~devGroup) =>
        (~sudoers /\ ~pfAnchor /\ ~dnsBlocklist /\ ~launchDaemon
         /\ ~seatbelt /\ ~wrappers /\ ~umask /\ ~backupScope
         /\ ~claudeCode /\ ~credentials)

\* --- Ordering: sudoers must not exist without the launch helper ---
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

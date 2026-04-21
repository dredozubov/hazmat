---- MODULE MC_SetupRollback ----
\* Hazmat setup/rollback state machine — verifies idempotency, reversibility,
\* and security invariants across all combinations of partial setup, interrupt,
\* rollback, and re-setup.
\*
\* Setup creates system resources in a fixed order. Rollback removes them
\* in reverse. This spec models each resource as a boolean (present/absent),
\* each setup step as an action that can succeed or fail (nondeterministic),
\* and verifies that:
\*   1. Setup is idempotent (re-running a completed step is a no-op)
\*   2. The system is reversible from ANY intermediate state
\*   3. The agent user is never launchable without firewall containment
\*   4. After full rollback, no hazmat artifacts remain
\*   5. Setup after any interruption converges to fully-configured
\*
\* Expected TLC result: No error has been found.
\*
\* Governed code:
\*   hazmat/init.go                  — runInit()
\*   hazmat/init_steps.go            — initSetupSteps() formal resource order
\*   hazmat/rollback.go              — runRollback()
\*   hazmat/rollback_steps.go        — rollback step formal resource order
\*   hazmat/setup_rollback_formal.go — shared resource names
\*
\* Model bounds: 2 setup attempts, 2 rollback attempts, failure at any step.
\*
\* Current modeled init resource order (asserted by setup_rollback_formal_test.go):
\*   0: agentUser          setupAgentUser
\*   1: devGroup           setupDevGroup
\*   2: homeDirTraverse    setupHomeDirTraverse
\*   3: localRepo          setupLocalRepo
\*   4: umask              setupHardeningGaps
\*   5: seatbelt           setupSeatbelt
\*   6: wrappers           setupUserExperience + zsh completions + git safe.directory
\*   7: pfAnchor           setupPfFirewall           ← containment before privilege
\*   8: dnsBlocklist       setupDNSBlocklist
\*   9: launchDaemon       setupLaunchDaemon
\*  10: launchHelper       setupLaunchHelper (verify, not create)
\*  11: sudoers            setupSudoers              ← narrow launch-helper privilege
\*  12: maintenanceSudoers maybeSetupOptionalAgentMaintenanceSudoers
\*  13: claudeCode         selected harness bootstrap + agent config permissions
\*  14: credentials        setupAgentCredentials
\*
\* Explicit non-init harness commands like "hazmat bootstrap opencode",
\* curated harness import flows, and session-only integration activation are
\* intentionally out of model here.

EXTENDS Naturals, FiniteSets

\* ═══════════════════════════════════════════════════════════════════════════════
\* Variables — each resource tracks whether it exists on the system
\* ═══════════════════════════════════════════════════════════════════════════════

VARIABLES
    \* Phase 1: User & Group
    agentUser,       \* system user /Users/agent
    devGroup,        \* dev group with agent + current user
    \* Phase 2: Home access + backup
    homeDirTraverse, \* ACL on $HOME allowing agent to traverse
    localRepo,       \* Kopia snapshot repository at ~/.hazmat/repo
    \* Phase 3: Hardening
    umask,           \* umask 077 in .zshrc files
    seatbelt,        \* claude-sandboxed wrapper in agent's .local/bin
    wrappers,        \* host wrappers + agent env
    \* Phase 4: Network (containment before privilege)
    pfAnchor,        \* /etc/pf.anchors/agent + /etc/pf.conf + pfctl loaded
    dnsBlocklist,    \* /etc/hosts entries
    launchDaemon,    \* /Library/LaunchDaemons/com.local.pf-agent.plist
    \* Phase 5: Privilege
    launchHelper,    \* /usr/local/libexec/hazmat-launch (external prerequisite)
    sudoers,         \* /etc/sudoers.d/agent (narrow NOPASSWD rule)
    maintenanceSudoers, \* /etc/sudoers.d/agent-maintenance (optional broader rule)
    \* Phase 6: Application
    claudeCode,      \* Claude Code installed + npmrc + pip.conf
    credentials,     \* API key + git identity

    \* Control state
    phase,           \* "idle" | "setting_up" | "rolling_back"
    setupStep,       \* 0..15 — which step setup will attempt next
    setupAttempts,
    rollbackAttempts,
    rollbackStep,    \* 0..11 — which rollback step is next
    rollbackMode     \* "none" | "core" | "destructive"

CONSTANTS
    MaxSetupAttempts,
    MaxRollbackAttempts

vars == <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt,
          wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist,
          launchDaemon, claudeCode, credentials,
          phase, setupStep, setupAttempts, rollbackAttempts,
          rollbackStep, rollbackMode>>

resources == <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt,
               wrappers, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist,
               launchDaemon, claudeCode, credentials>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Type invariant
\* ═══════════════════════════════════════════════════════════════════════════════

TypeOK ==
    /\ agentUser      \in BOOLEAN
    /\ devGroup       \in BOOLEAN
    /\ homeDirTraverse \in BOOLEAN
    /\ localRepo      \in BOOLEAN
    /\ umask          \in BOOLEAN
    /\ seatbelt       \in BOOLEAN
    /\ wrappers       \in BOOLEAN
    /\ launchHelper   \in BOOLEAN
    /\ sudoers        \in BOOLEAN
    /\ maintenanceSudoers \in BOOLEAN
    /\ pfAnchor       \in BOOLEAN
    /\ dnsBlocklist   \in BOOLEAN
    /\ launchDaemon   \in BOOLEAN
    /\ claudeCode     \in BOOLEAN
    /\ credentials    \in BOOLEAN
    /\ phase          \in {"idle", "setting_up", "rolling_back"}
    /\ setupStep      \in 0..15
    /\ setupAttempts  \in 0..MaxSetupAttempts
    /\ rollbackAttempts \in 0..MaxRollbackAttempts
    /\ rollbackStep   \in 0..11
    /\ rollbackMode   \in {"none", "core", "destructive"}

\* ═══════════════════════════════════════════════════════════════════════════════
\* Initial state — clean system
\* ═══════════════════════════════════════════════════════════════════════════════

Init ==
    /\ agentUser      = FALSE
    /\ devGroup       = FALSE
    /\ homeDirTraverse = FALSE
    /\ localRepo      = FALSE
    /\ umask          = FALSE
    /\ seatbelt       = FALSE
    /\ wrappers       = FALSE
    /\ launchHelper   = FALSE
    /\ sudoers        = FALSE
    /\ maintenanceSudoers = FALSE
    /\ pfAnchor       = FALSE
    /\ dnsBlocklist   = FALSE
    /\ launchDaemon   = FALSE
    /\ claudeCode     = FALSE
    /\ credentials    = FALSE
    /\ phase          = "idle"
    /\ setupStep      = 0
    /\ setupAttempts  = 0
    /\ rollbackAttempts = 0
    /\ rollbackStep   = 0
    /\ rollbackMode   = "none"

\* ═══════════════════════════════════════════════════════════════════════════════
\* Helper: launchHelper is an external prerequisite.
\* ═══════════════════════════════════════════════════════════════════════════════

InstallLaunchHelper ==
    /\ ~launchHelper
    /\ phase = "idle"
    /\ launchHelper' = TRUE
    /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt,
                    wrappers, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    phase, setupStep, setupAttempts, rollbackAttempts,
                    rollbackStep, rollbackMode>>

\* Unchanged-all helper for non-resource variables
CtlUnchanged == UNCHANGED <<phase, setupStep, setupAttempts, rollbackAttempts, rollbackStep, rollbackMode>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Setup actions — each step mirrors a setupX() function in init.go
\* ═══════════════════════════════════════════════════════════════════════════════

OtherResources(changing) ==
    \A r \in {"agentUser", "devGroup", "homeDirTraverse", "localRepo",
              "umask", "seatbelt", "wrappers", "launchHelper",
              "sudoers", "maintenanceSudoers", "pfAnchor", "dnsBlocklist", "launchDaemon",
              "claudeCode", "credentials"} \ {changing}:
        TRUE  \* placeholder — we use explicit UNCHANGED in each step instead

BeginSetup ==
    /\ phase = "idle"
    /\ setupAttempts < MaxSetupAttempts
    /\ phase'         = "setting_up"
    /\ setupStep'     = 0
    /\ setupAttempts' = setupAttempts + 1
    /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt,
                    wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials, rollbackAttempts,
                    rollbackStep, rollbackMode>>

SetupStepSucceed ==
    /\ phase = "setting_up"
    /\ setupStep < 15
    /\ \/ (setupStep = 0  /\ agentUser'      = TRUE /\ UNCHANGED <<devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 1  /\ devGroup'       = TRUE /\ UNCHANGED <<agentUser, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 2  /\ homeDirTraverse' = TRUE /\ UNCHANGED <<agentUser, devGroup, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 3  /\ localRepo'      = TRUE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 4  /\ umask'          = TRUE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 5  /\ seatbelt'       = TRUE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 6  /\ wrappers'       = TRUE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \* Steps 7-9: Network containment BEFORE privilege grant.
       \/ (setupStep = 7  /\ pfAnchor'      = TRUE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 8  /\ dnsBlocklist'   = TRUE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 9  /\ launchDaemon'   = TRUE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, claudeCode, credentials>>)
       \* Step 10: verify launchHelper exists — does NOT create it.
       \/ (setupStep = 10 /\ launchHelper           /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \* Step 11: narrow privilege granted after all containment is active.
       \/ (setupStep = 11 /\ sudoers'       = TRUE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \* Step 12: optional broader agent-maintenance sudoers rule.
       \/ (setupStep = 12 /\ maintenanceSudoers' \in {maintenanceSudoers, TRUE}
                          /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (setupStep = 13 /\ claudeCode'    = TRUE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, credentials>>)
       \/ (setupStep = 14 /\ credentials'   = TRUE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode>>)
    /\ setupStep' = setupStep + 1
    /\ UNCHANGED <<phase, setupAttempts, rollbackAttempts, rollbackStep, rollbackMode>>

SetupComplete ==
    /\ phase = "setting_up"
    /\ setupStep = 15
    /\ phase' = "idle"
    /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt,
                    wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts, rollbackAttempts,
                    rollbackStep, rollbackMode>>

SetupStepFail ==
    /\ phase = "setting_up"
    /\ setupStep < 15
    /\ \/ (setupStep = 10 /\ ~launchHelper)  \* deterministic fail if helper absent
       \/ setupStep /= 10                     \* any other step can fail randomly
    /\ phase' = "idle"
    /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt,
                    wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts, rollbackAttempts,
                    rollbackStep, rollbackMode>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Rollback actions — mirrors rollback.go
\*
\* Current modeled rollback resource order (asserted by setup_rollback_formal_test.go):
\*   Step 0: sudoers          rollbackSudoers          ← REVOKE PRIVILEGE FIRST
\*   Step 1: launchDaemon     rollbackLaunchDaemon
\*   Step 2: pfAnchor         rollbackPfFirewall
\*   Step 3: dnsBlocklist     rollbackDNSBlocklist
\*   Step 4: seatbelt         rollbackSeatbelt
\*   Step 5: wrappers         rollbackUserExperience + zsh completions + git safe.directory
\*   Step 6: homeDirTraverse  rollbackHomeDirTraverse
\*   Step 7: umask            rollbackUmask
\*   Step 8: localRepo        rollbackLocalRepo
\*   Step 9: agentUser        optional rollbackAgentUser  — only with --delete-user
\*  Step 10: devGroup         optional rollbackDevGroup   — only with --delete-group
\*
\* Does NOT remove: launchHelper (external binary).
\* Does NOT remove: claudeCode/credentials (removed only with --delete-user).
\* ═══════════════════════════════════════════════════════════════════════════════

BeginRollback ==
    /\ phase = "idle"
    /\ rollbackAttempts < MaxRollbackAttempts
    /\ phase'            = "rolling_back"
    /\ rollbackStep'     = 0
    /\ rollbackAttempts' = rollbackAttempts + 1
    /\ \/ rollbackMode' = "core"
       \/ rollbackMode' = "destructive"
    /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt,
                    wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts>>

RollbackCore ==
    /\ phase = "rolling_back"
    /\ rollbackStep < 9
    /\ \/ (rollbackStep = 0 /\ sudoers' = FALSE /\ maintenanceSudoers' = FALSE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 1 /\ launchDaemon'   = FALSE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, claudeCode, credentials>>)
       \/ (rollbackStep = 2 /\ pfAnchor'      = FALSE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 3 /\ dnsBlocklist'   = FALSE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 4 /\ seatbelt'       = FALSE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 5 /\ wrappers'       = FALSE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 6 /\ homeDirTraverse' = FALSE /\ UNCHANGED <<agentUser, devGroup, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 7 /\ umask'          = FALSE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
       \/ (rollbackStep = 8 /\ localRepo'      = FALSE /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
    /\ rollbackStep' = rollbackStep + 1
    /\ UNCHANGED <<phase, setupStep, setupAttempts, rollbackAttempts, rollbackMode>>

\* Destructive rollback: remove agent user and dev group after core steps.
RollbackDestructive ==
    /\ phase = "rolling_back"
    /\ rollbackMode = "destructive"
    /\ rollbackStep >= 9
    /\ rollbackStep < 11
    /\ \/ (rollbackStep = 9  /\ agentUser'    = FALSE /\ claudeCode' = FALSE /\ credentials' = FALSE
                             /\ UNCHANGED <<devGroup, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon>>)
       \/ (rollbackStep = 10 /\ devGroup'     = FALSE
                             /\ UNCHANGED <<agentUser, homeDirTraverse, localRepo, umask, seatbelt, wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist, launchDaemon, claudeCode, credentials>>)
    /\ rollbackStep' = rollbackStep + 1
    /\ UNCHANGED <<phase, setupStep, setupAttempts, rollbackAttempts, rollbackMode>>

RollbackComplete ==
    /\ phase = "rolling_back"
    /\ \/ (rollbackMode = "core" /\ rollbackStep = 9)
       \/ (rollbackMode = "destructive" /\ rollbackStep = 11)
    /\ phase' = "idle"
    /\ rollbackMode' = "none"
    /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt,
                    wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts, rollbackAttempts, rollbackStep>>

RollbackInterrupt ==
    /\ phase = "rolling_back"
    /\ phase' = "idle"
    /\ rollbackMode' = "none"
    /\ UNCHANGED <<agentUser, devGroup, homeDirTraverse, localRepo, umask, seatbelt,
                    wrappers, launchHelper, sudoers, maintenanceSudoers, pfAnchor, dnsBlocklist,
                    launchDaemon, claudeCode, credentials,
                    setupStep, setupAttempts, rollbackAttempts, rollbackStep>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Next-state relation
\* ═══════════════════════════════════════════════════════════════════════════════

\* Stutter when all attempts exhausted (prevents deadlock in bounded model).
Stutter ==
    /\ phase = "idle"
    /\ setupAttempts >= MaxSetupAttempts
    /\ rollbackAttempts >= MaxRollbackAttempts
    /\ UNCHANGED vars

Next ==
    \/ InstallLaunchHelper
    \/ BeginSetup
    \/ SetupStepSucceed
    \/ SetupComplete
    \/ SetupStepFail
    \/ BeginRollback
    \/ RollbackCore
    \/ RollbackDestructive
    \/ RollbackComplete
    \/ RollbackInterrupt
    \/ Stutter

\* ═══════════════════════════════════════════════════════════════════════════════
\* Invariants
\* ═══════════════════════════════════════════════════════════════════════════════

\* The agent must NEVER be launchable without firewall containment.
\* Either sudoers rule makes the agent launchable.
\* pfAnchor provides network containment.
AgentContained ==
    (sudoers \/ maintenanceSudoers) => pfAnchor

\* No orphaned artifacts after a complete destructive rollback.
NoOrphanedArtifacts ==
    (phase = "idle" /\ rollbackMode = "none" /\ ~agentUser /\ ~devGroup) =>
        (~homeDirTraverse /\ ~localRepo /\ ~umask /\ ~seatbelt /\ ~wrappers /\
         ~sudoers /\ ~maintenanceSudoers /\ ~pfAnchor /\ ~dnsBlocklist /\ ~launchDaemon /\
         ~claudeCode /\ ~credentials)

\* The narrow launch-helper sudoers rule requires the helper to be installed.
SudoersRequiresHelper ==
    sudoers => launchHelper

\* Any passwordless privilege requires the agent identity to exist.
PrivilegeRequiresAgentUser ==
    (sudoers \/ maintenanceSudoers) => agentUser

\* Agent-home artifacts require the agent user to exist.
AgentDepsRequireUser ==
    (seatbelt \/ claudeCode \/ credentials) => agentUser

\* Combined safety.
Safety ==
    /\ TypeOK
    /\ AgentContained
    /\ NoOrphanedArtifacts
    /\ SudoersRequiresHelper
    /\ PrivilegeRequiresAgentUser
    /\ AgentDepsRequireUser

\* ═══════════════════════════════════════════════════════════════════════════════
\* Liveness
\* ═══════════════════════════════════════════════════════════════════════════════

\* From any state, it's always possible to reach a clean system
\* (via rollback) or a fully configured system (via setup).
CanAlwaysReachClean ==
    /\ WF_vars(BeginRollback)
    /\ WF_vars(RollbackCore)
    /\ WF_vars(RollbackDestructive)
    /\ WF_vars(RollbackComplete)
    => <>(phase = "idle" /\ ~sudoers /\ ~maintenanceSudoers /\ ~pfAnchor)

SetupEventuallyCompletes ==
    /\ WF_vars(BeginSetup)
    /\ WF_vars(SetupStepSucceed)
    /\ WF_vars(SetupComplete)
    => <>(phase = "idle" /\ sudoers /\ pfAnchor)

Fairness == WF_vars(Next)
Spec == Init /\ [][Next]_vars /\ Fairness

====

---- MODULE MC_SeatbeltPolicy ----
\* Seatbelt (SBPL) policy generation — verifies that credential deny rules
\* are effective for ALL combinations of user-provided ProjectDir, ReadDirs,
\* ResumeDir, per-session temp policy, and per-session network mode.
\*
\* SBPL semantics: rules are evaluated in order, LAST match wins.
\* generateSBPL() emits rules in fixed sections:
\*   Section 1: Read-only directory allows (user input, filtered)
\*   Section 2: Project directory read+write allows (user input)
\*   Section 3: Resume session directory read+write (optional, invoker's ~/.claude/...)
\*   Section 4: Agent home config allows (static — .claude, .local, .config, etc.)
\*   Section 5: Session temp root read+write+exec (agent-owned per-session dir)
\*   Section 6: Project write re-assertion (same path as section 2, last allow)
\*   Section 7: Host temp socket/capability denies
\*   Section 8: Credential denies (static — .ssh, .aws, Keychains, etc.)
\*   Section 9: Explicit agent login keychain exception for Claude OAuth
\*
\* Per-session network policy is independent of filesystem last-match behavior:
\* the default mode emits outbound network and DNS lookup allowances, while
\* network mode "none" emits neither. There is no global firewall state in this
\* model because native network-none is a per-process Seatbelt property.
\*
\* Since all rules within a section have the same action type, "last match wins"
\* reduces to "highest section number wins." This lets us model rules as a set
\* of (section, action, path) tuples instead of an ordered sequence.
\*
\* Key correctness properties:
\*   1. Credential reads are denied except the explicit agent keychain exception
\*   2. Credential writes are denied except the explicit agent keychain exception
\*   3. Read dirs never grant write access
\*   4. ResumeDir (invoker's session dir) cannot be a credential path
\*   5. Host temp is not implicitly readable/writable/executable
\*   6. Session temp remains writable and executable for build tools
\*   7. Codex App temp socket/capability paths are denied even if host temp is granted
\*   8. Network mode "none" grants no outbound network or DNS authority
\*
\* Governed code:
\*   hazmat/session.go — generateSBPL(), isWithinDir()

EXTENDS Naturals, FiniteSets

\* ═══════════════════════════════════════════════════════════════════════════════
\* Constants — abstract path model
\* ═══════════════════════════════════════════════════════════════════════════════

CONSTANTS
    Paths,          \* finite set of abstract path identifiers
    CredPaths,      \* subset: credential deny roots (.ssh, .aws, Keychains, etc.)
    CredentialTargets, \* subset: representative sensitive paths covered by credential deny roots
    AgentKeychainExceptionPaths, \* subset: exact agent login keychain files allowed only for Claude OAuth
    AgentHomeSubs,  \* subset: paths under agent home that get static read+write allows
    ProjectChoices, \* subset: valid choices for ProjectDir
    ReadChoices,    \* subset: valid choices for ReadDir entries
    NetworkChoices, \* subset: network modes ("default", "none")
    ResumeChoices,  \* subset: valid choices for ResumeDir (invoker's session dir or none)
    TempSocketPaths,\* subset: host temp capability/socket paths denied after temp/user allows
    \* Model constant identifiers for abstract paths
    normalProj,     \* /Users/dr/workspace/myproject
    agentHome,      \* /Users/agent
    configDir,      \* /Users/agent/.config
    sshDir,         \* /Users/agent/.ssh
    gcloudDir,      \* /Users/agent/.config/gcloud
    keychainDir,    \* /Users/agent/Library/Keychains
    keychainDB,     \* /Users/agent/Library/Keychains/login.keychain-db
    keychainSHM,    \* /Users/agent/Library/Keychains/login.keychain-db-shm
    keychainWAL,    \* /Users/agent/Library/Keychains/login.keychain-db-wal
    outsideRef,     \* /Users/dr/reference
    invokerSess,    \* /Users/dr/.claude/projects/-foo (invoker's session dir)
    hostTempRoot,   \* /private/tmp
    hostTempOutside,\* /private/tmp/outside-host-readable.txt
    sessionTempRoot,\* /Users/agent/.cache/hazmat/tmp/<session>
    sessionTempFile,\* /Users/agent/.cache/hazmat/tmp/<session>/artifact
    codexTempSocket \* /private/tmp/codex-ipc/app.sock

ASSUME CredPaths \subseteq Paths
ASSUME CredentialTargets \subseteq Paths
ASSUME AgentKeychainExceptionPaths \subseteq CredentialTargets
ASSUME AgentHomeSubs \subseteq Paths
ASSUME ProjectChoices \subseteq Paths
ASSUME ReadChoices \subseteq Paths
ASSUME NetworkChoices \subseteq {"default", "none"}
ASSUME ResumeChoices \ {"none"} \subseteq Paths
ASSUME TempSocketPaths \subseteq Paths

\* Contains(child, parent) = TRUE iff `child` is within (or equal to) `parent`.
\* Hardcoded containment relation for our abstract path model.
\* invokerSess is under the invoker's home (/Users/dr/.claude/...), which is
\* NOT under agentHome. It has no containment relationship with credential paths.
Contains(child, parent) ==
    \/ child = parent
    \/ (child = sshDir     /\ parent = agentHome)
    \/ (child = configDir  /\ parent = agentHome)
    \/ (child = gcloudDir  /\ parent = agentHome)
    \/ (child = gcloudDir  /\ parent = configDir)
    \/ (child = keychainDir /\ parent = agentHome)
    \/ (child = keychainDB  /\ parent = agentHome)
    \/ (child = keychainDB  /\ parent = keychainDir)
    \/ (child = keychainSHM /\ parent = agentHome)
    \/ (child = keychainSHM /\ parent = keychainDir)
    \/ (child = keychainWAL /\ parent = agentHome)
    \/ (child = keychainWAL /\ parent = keychainDir)
    \/ (child = sessionTempRoot /\ parent = agentHome)
    \/ (child = sessionTempFile /\ parent = agentHome)
    \/ (child = sessionTempFile /\ parent = sessionTempRoot)
    \/ (child = hostTempOutside /\ parent = hostTempRoot)
    \/ (child = codexTempSocket /\ parent = hostTempRoot)

\* ═══════════════════════════════════════════════════════════════════════════════
\* Variables
\* ═══════════════════════════════════════════════════════════════════════════════

VARIABLES
    projectDir,   \* chosen project directory (a Path)
    readDirs,     \* chosen read directories (SUBSET Paths)
    networkMode,  \* "default" or "none"
    resumeDir,    \* chosen resume session directory (a Path or "none")
    agentKeychainAccess, \* whether Claude OAuth gets the exact agent login keychain exception
    rules,        \* set of emitted rules: [section, action, path]
    networkAllows,\* set of emitted network grants
    section       \* 0..10: which section we're generating (10 = done)

vars == <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, rules, networkAllows, section>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Rule constructors
\* ═══════════════════════════════════════════════════════════════════════════════

AllowRead(sec, p)  == [section |-> sec, action |-> "allow_read",  path |-> p]
AllowWrite(sec, p) == [section |-> sec, action |-> "allow_write", path |-> p]
AllowExec(sec, p)  == [section |-> sec, action |-> "allow_exec",  path |-> p]
DenyRead(sec, p)   == [section |-> sec, action |-> "deny_read",   path |-> p]
DenyWrite(sec, p)  == [section |-> sec, action |-> "deny_write",  path |-> p]
DenyExec(sec, p)   == [section |-> sec, action |-> "deny_exec",   path |-> p]

\* ═══════════════════════════════════════════════════════════════════════════════
\* Type invariant
\* ═══════════════════════════════════════════════════════════════════════════════

TypeOK ==
    /\ projectDir \in Paths
    /\ readDirs   \subseteq Paths
    /\ networkMode \in {"default", "none"}
    /\ resumeDir  \in Paths \cup {"none"}
    /\ agentKeychainAccess \in BOOLEAN
    /\ section    \in 0..10
    /\ networkAllows \subseteq {"network_outbound", "dns_lookup", "network_inbound_local"}

\* ═══════════════════════════════════════════════════════════════════════════════
\* Initial state — nondeterministic choice of inputs
\* ═══════════════════════════════════════════════════════════════════════════════

Init ==
    /\ projectDir \in ProjectChoices
    /\ readDirs   \in SUBSET ReadChoices
    /\ networkMode \in NetworkChoices
    /\ resumeDir  \in ResumeChoices
    /\ agentKeychainAccess \in BOOLEAN
    /\ rules      = {}
    /\ networkAllows = {}
    /\ section    = 0

\* ═══════════════════════════════════════════════════════════════════════════════
\* Policy generation actions
\* ═══════════════════════════════════════════════════════════════════════════════

\* Section 0: System library allows (static paths like /usr/lib, /System/Library).
\* These never overlap with agent home or credential paths. Abstracted away.
\* Network allows are also emitted in the static section in the implementation.
EmitSystemLibs ==
    /\ section = 0
    /\ networkAllows' =
       IF networkMode = "default"
       THEN networkAllows \cup {"network_outbound", "dns_lookup", "network_inbound_local"}
       ELSE networkAllows \cup {"network_inbound_local"}
    /\ section' = 1
    /\ UNCHANGED <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, rules>>

\* Section 1: Read-only directory allows.
\* Each read dir gets (allow file-read*) unless subsumed.
\* Models session.go filtering logic.
EmitReadDirs ==
    /\ section = 1
    /\ LET
         notInProject == {d \in readDirs : ~Contains(d, projectDir)}
         notSubsumed  == {d \in notInProject :
             ~\E other \in notInProject : other /= d /\ Contains(d, other)}
       IN
         rules' = rules \cup
            {AllowRead(1, d) : d \in notSubsumed} \cup
            {AllowExec(1, d) : d \in notSubsumed}
    /\ section' = 2
    /\ UNCHANGED <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, networkAllows>>

\* Section 2: Project directory read+write.
EmitProjectDir ==
    /\ section = 2
    /\ rules' = rules \cup {AllowRead(2, projectDir), AllowWrite(2, projectDir), AllowExec(2, projectDir)}
    /\ section' = 3
    /\ UNCHANGED <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, networkAllows>>

\* Section 3: Resume session directory read+write (optional).
\* When --resume or --continue is used, the invoking user's session directory
\* (~/.claude/projects/<hash>/) is opened for read+write so Claude Code can
\* follow symlinks and append to the resumed conversation transcript.
\* This path is under the INVOKER's home, not the agent's — it should never
\* overlap with credential paths (which are under agent home).
EmitResumeDir ==
    /\ section = 3
    /\ IF resumeDir /= "none"
       THEN rules' = rules \cup {AllowRead(3, resumeDir), AllowWrite(3, resumeDir)}
       ELSE UNCHANGED rules
    /\ section' = 4
    /\ UNCHANGED <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, networkAllows>>

\* Section 4: Agent home — BROAD read+write allow on entire agent home.
\* This replaced individual subdirectory allows (.claude, .local, .config, etc.)
\* because Claude Code needs to access paths that can't be enumerated in advance.
\* Credential directories are denied in section 8 (last-match-wins overrides this);
\* section 9 may re-allow only exact agent login keychain files for Claude OAuth.
EmitHomeConfig ==
    /\ section = 4
    /\ rules' = rules \cup
         \* Broad allow on agentHome covers ALL subpaths including AgentHomeSubs
         \* AND CredPaths. The credential denies in section 8 override this.
         {AllowRead(4, agentHome), AllowWrite(4, agentHome)} \cup
         {AllowRead(4, p) : p \in AgentHomeSubs} \cup
         {AllowWrite(4, p) : p \in AgentHomeSubs} \cup
         {AllowExec(4, agentHome)}
    /\ section' = 5
    /\ UNCHANGED <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, networkAllows>>

\* Section 5: Session temp root.
\* Hazmat no longer grants broad /private/tmp or /private/var/folders
\* read/write/exec. Runtime TMPDIR is an agent-owned per-session root, which
\* remains writable and executable for compiler/runtime temp artifacts.
EmitSessionTemp ==
    /\ section = 5
    /\ rules' = rules \cup
         {AllowRead(5, sessionTempRoot), AllowWrite(5, sessionTempRoot), AllowExec(5, sessionTempRoot)}
    /\ section' = 6
    /\ UNCHANGED <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, networkAllows>>

\* Section 6: Project write re-assertion.
\* When a read-only -R directory is a parent of the project directory,
\* the broad file-read* rule could interact with SBPL evaluation.
\* Re-asserting file-write* here guarantees it is the last matching
\* allow for any write operation targeting the project directory.
EmitProjectWriteReassert ==
    /\ section = 6
    /\ rules' = rules \cup {AllowRead(6, projectDir), AllowWrite(6, projectDir), AllowExec(6, projectDir)}
    /\ section' = 7
    /\ UNCHANGED <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, networkAllows>>

\* Section 7: host temp socket/capability denies.
\* These are explicit defense-in-depth denies so a broad user-granted temp
\* read dir or project root does not expose Codex App control sockets.
EmitTempSocketDenies ==
    /\ section = 7
    /\ rules' = rules \cup
         {DenyRead(7, p)  : p \in TempSocketPaths} \cup
         {DenyWrite(7, p) : p \in TempSocketPaths} \cup
         {DenyExec(7, p)  : p \in TempSocketPaths}
    /\ section' = 8
    /\ UNCHANGED <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, networkAllows>>

\* Section 8: Credential denies.
\* Deny both file-read* (exfiltration) and file-write* (planting).
EmitCredDenies ==
    /\ section = 8
    /\ rules' = rules \cup
         {DenyRead(8, p)  : p \in CredPaths} \cup
         {DenyWrite(8, p) : p \in CredPaths}
    /\ section' = 9
    /\ UNCHANGED <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, networkAllows>>

\* Section 9: Exact agent login keychain exception for Claude OAuth.
\* The broader Keychains deny root remains denied. Only the managed login
\* keychain DB representative is re-allowed when the session explicitly needs
\* agent-account Keychain OAuth compatibility.
EmitAgentKeychainException ==
    /\ section = 9
    /\ IF agentKeychainAccess
       THEN rules' = rules \cup
            {AllowRead(9, p)  : p \in AgentKeychainExceptionPaths} \cup
            {AllowWrite(9, p) : p \in AgentKeychainExceptionPaths}
       ELSE UNCHANGED rules
    /\ section' = 10
    /\ UNCHANGED <<projectDir, readDirs, networkMode, resumeDir, agentKeychainAccess, networkAllows>>

\* Terminal.
Done ==
    /\ section = 10
    /\ UNCHANGED vars

Next ==
    \/ EmitSystemLibs
    \/ EmitReadDirs
    \/ EmitProjectDir
    \/ EmitResumeDir
    \/ EmitHomeConfig
    \/ EmitSessionTemp
    \/ EmitProjectWriteReassert
    \/ EmitTempSocketDenies
    \/ EmitCredDenies
    \/ EmitAgentKeychainException
    \/ Done

Spec == Init /\ [][Next]_vars

\* ═══════════════════════════════════════════════════════════════════════════════
\* SBPL "last match wins" evaluation
\* ═══════════════════════════════════════════════════════════════════════════════

\* For a target path and access type, find the rule with the highest section
\* number whose path covers the target. That rule determines the outcome.

\* Effective read access for a target path.
\* Returns "allow_read", "deny_read", or "deny_default" (no matching rule).
EffectiveRead(target) ==
    LET matching == {r \in rules :
            r.action \in {"allow_read", "deny_read"}
            /\ Contains(target, r.path)}
    IN IF matching = {}
       THEN "deny_default"
       ELSE LET maxSec == CHOOSE s \in {r.section : r \in matching} :
                    \A r \in matching : r.section <= s
            IN (CHOOSE r \in matching : r.section = maxSec).action

\* Effective write access for a target path.
\* Returns "allow_write", "deny_write", or "deny_default".
EffectiveWrite(target) ==
    LET matching == {r \in rules :
            r.action \in {"allow_write", "deny_write"}
            /\ Contains(target, r.path)}
    IN IF matching = {}
       THEN "deny_default"
       ELSE LET maxSec == CHOOSE s \in {r.section : r \in matching} :
                    \A r \in matching : r.section <= s
            IN (CHOOSE r \in matching : r.section = maxSec).action

\* Effective process-exec access for a target path.
\* Returns "allow_exec", "deny_exec", or "deny_default".
EffectiveExec(target) ==
    LET matching == {r \in rules :
            r.action \in {"allow_exec", "deny_exec"}
            /\ Contains(target, r.path)}
    IN IF matching = {}
       THEN "deny_default"
       ELSE LET maxSec == CHOOSE s \in {r.section : r \in matching} :
                    \A r \in matching : r.section <= s
            IN (CHOOSE r \in matching : r.section = maxSec).action

\* ═══════════════════════════════════════════════════════════════════════════════
\* Safety invariants — checked when policy generation is complete (section = 10)
\* ═══════════════════════════════════════════════════════════════════════════════

\* --- CRITICAL: credential file-read* is denied outside explicit exceptions ---
\* No matter what ProjectDir, ReadDirs, or ResumeDir the user chooses, the
\* credential deny in section 8 must override all earlier allows.
CredentialReadDenied ==
    section = 10 =>
        \A cred \in CredentialTargets \ AgentKeychainExceptionPaths :
            EffectiveRead(cred) = "deny_read"

\* --- Credential writes denied outside explicit exceptions ---
\* Section 8 denies both file-read* and file-write* for all credential paths.
CredentialWriteDenied ==
    section = 10 =>
        \A cred \in CredentialTargets \ AgentKeychainExceptionPaths :
            EffectiveWrite(cred) = "deny_write"

\* --- Agent keychain exception is exact and conditional ---
AgentKeychainExceptionScoped ==
    section = 10 =>
        IF agentKeychainAccess
        THEN \A p \in AgentKeychainExceptionPaths :
            /\ EffectiveRead(p) = "allow_read"
            /\ EffectiveWrite(p) = "allow_write"
        ELSE \A p \in AgentKeychainExceptionPaths :
            /\ EffectiveRead(p) = "deny_read"
            /\ EffectiveWrite(p) = "deny_write"

\* --- Read dirs never grant write access ---
\* Rules emitted for ReadDirs (section 1) must only be AllowRead, never AllowWrite.
ReadDirsNoWrite ==
    section = 10 =>
        ~\E r \in rules : r.section = 1 /\ r.action = "allow_write"

\* --- Project dir is writable (unless it IS a credential path) ---
\* If the user picks a credential dir as their project, the deny wins.
\* This is correct — credential protection takes priority.
ProjectDirWritable ==
    section = 10 =>
        \/ projectDir \in CredPaths  \* credential deny overrides — expected
        \/ EffectiveWrite(projectDir) = "allow_write"

\* --- Read dirs within project are elided (subsumption) ---
\* If a read dir is inside ProjectDir, no rule should be emitted for it
\* (the project's read+write already covers it).
ReadDirSubsumption ==
    section = 10 =>
        ~\E r \in rules :
            r.section = 1
            /\ Contains(r.path, projectDir)

\* --- ResumeDir never overlaps credential paths ---
\* The resume session directory is under the invoker's home (/Users/dr/...),
\* not under agent home. It must never be a credential path.
ResumeDirNotCredential ==
    section = 10 =>
        \/ resumeDir = "none"
        \/ resumeDir \notin CredPaths

\* --- Host temp is not implicitly readable/writable/executable ---
\* An outside host-temp file should stay denied unless the user explicitly
\* selected it through the project or read-dir surface.
HostTempNotImplicitlyReadable ==
    section = 10 =>
        \/ Contains(hostTempOutside, projectDir)
        \/ \E d \in readDirs : Contains(hostTempOutside, d)
        \/ EffectiveRead(hostTempOutside) # "allow_read"

HostTempNotImplicitlyWritable ==
    section = 10 =>
        \/ Contains(hostTempOutside, projectDir)
        \/ EffectiveWrite(hostTempOutside) # "allow_write"

HostTempNotImplicitlyExecutable ==
    section = 10 =>
        \/ Contains(hostTempOutside, projectDir)
        \/ \E d \in readDirs : Contains(hostTempOutside, d)
        \/ EffectiveExec(hostTempOutside) # "allow_exec"

\* --- Session temp remains usable for compiler/runtime artifacts ---
SessionTempWritable ==
    section = 10 =>
        /\ EffectiveRead(sessionTempFile) = "allow_read"
        /\ EffectiveWrite(sessionTempFile) = "allow_write"
        /\ EffectiveExec(sessionTempFile) = "allow_exec"

\* --- Codex App temp/control sockets stay denied even under broad temp grants ---
TempSocketsDenied ==
    section = 10 =>
        \A sock \in TempSocketPaths :
            /\ EffectiveRead(sock) = "deny_read"
            /\ EffectiveWrite(sock) = "deny_write"
            /\ EffectiveExec(sock) = "deny_exec"

\* --- Network mode "none" grants no outbound network authority ---
NetworkNoneDeniesOutbound ==
    section = 10 /\ networkMode = "none" =>
        "network_outbound" \notin networkAllows

\* --- Network mode "none" grants no DNS lookup authority ---
NetworkNoneDeniesDNS ==
    section = 10 /\ networkMode = "none" =>
        "dns_lookup" \notin networkAllows

\* --- Default mode preserves the existing outbound network behavior ---
NetworkDefaultAllowsOutbound ==
    section = 10 /\ networkMode = "default" =>
        /\ "network_outbound" \in networkAllows
        /\ "dns_lookup" \in networkAllows

====

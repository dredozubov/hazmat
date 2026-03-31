---- MODULE MC_SeatbeltPolicy ----
\* Seatbelt (SBPL) policy generation — verifies that credential deny rules
\* are effective for ALL combinations of user-provided ProjectDir, ReadDirs,
\* and ResumeDir.
\*
\* SBPL semantics: rules are evaluated in order, LAST match wins.
\* generateSBPL() emits rules in fixed sections:
\*   Section 1: Read-only directory allows (user input, filtered)
\*   Section 2: Project directory read+write allows (user input)
\*   Section 3: Resume session directory read+write (optional, invoker's ~/.claude/...)
\*   Section 4: Agent home config allows (static — .claude, .local, .config, etc.)
\*   Section 5: Project write re-assertion (same path as section 2, last allow)
\*   Section 6: Credential denies (static — .ssh, .aws, .config/gcloud, etc.)
\*
\* Since all rules within a section have the same action type, "last match wins"
\* reduces to "highest section number wins." This lets us model rules as a set
\* of (section, action, path) tuples instead of an ordered sequence.
\*
\* Key correctness properties:
\*   1. Credential reads are ALWAYS denied (section 6 deny overrides all allows)
\*   2. Credential writes are ALWAYS denied (section 6 deny overrides all allows)
\*   3. Read dirs never grant write access
\*   4. ResumeDir (invoker's session dir) cannot be a credential path
\*
\* Governed code:
\*   hazmat/session.go — generateSBPL(), isWithinDir()

EXTENDS Naturals, FiniteSets

\* ═══════════════════════════════════════════════════════════════════════════════
\* Constants — abstract path model
\* ═══════════════════════════════════════════════════════════════════════════════

CONSTANTS
    Paths,          \* finite set of abstract path identifiers
    CredPaths,      \* subset: credential directories (.ssh, .aws, .config/gcloud)
    AgentHomeSubs,  \* subset: paths under agent home that get static read+write allows
    ProjectChoices, \* subset: valid choices for ProjectDir
    ReadChoices,    \* subset: valid choices for ReadDir entries
    ResumeChoices,  \* subset: valid choices for ResumeDir (invoker's session dir or none)
    \* Model constant identifiers for abstract paths
    normalProj,     \* /Users/dr/workspace/myproject
    agentHome,      \* /Users/agent
    configDir,      \* /Users/agent/.config
    sshDir,         \* /Users/agent/.ssh
    gcloudDir,      \* /Users/agent/.config/gcloud
    outsideRef,     \* /tmp/reference
    invokerSess     \* /Users/dr/.claude/projects/-foo (invoker's session dir)

ASSUME CredPaths \subseteq Paths
ASSUME AgentHomeSubs \subseteq Paths
ASSUME ProjectChoices \subseteq Paths
ASSUME ReadChoices \subseteq Paths
ASSUME ResumeChoices \ {"none"} \subseteq Paths

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

\* ═══════════════════════════════════════════════════════════════════════════════
\* Variables
\* ═══════════════════════════════════════════════════════════════════════════════

VARIABLES
    projectDir,   \* chosen project directory (a Path)
    readDirs,     \* chosen read directories (SUBSET Paths)
    resumeDir,    \* chosen resume session directory (a Path or "none")
    rules,        \* set of emitted rules: [section, action, path]
    section       \* 0..7: which section we're generating (7 = done)

vars == <<projectDir, readDirs, resumeDir, rules, section>>

\* ═══════════════════════════════════════════════════════════════════════════════
\* Rule constructors
\* ═══════════════════════════════════════════════════════════════════════════════

AllowRead(sec, p)  == [section |-> sec, action |-> "allow_read",  path |-> p]
AllowWrite(sec, p) == [section |-> sec, action |-> "allow_write", path |-> p]
DenyRead(sec, p)   == [section |-> sec, action |-> "deny_read",   path |-> p]
DenyWrite(sec, p)  == [section |-> sec, action |-> "deny_write",  path |-> p]

\* ═══════════════════════════════════════════════════════════════════════════════
\* Type invariant
\* ═══════════════════════════════════════════════════════════════════════════════

TypeOK ==
    /\ projectDir \in Paths
    /\ readDirs   \subseteq Paths
    /\ resumeDir  \in Paths \cup {"none"}
    /\ section    \in 0..7

\* ═══════════════════════════════════════════════════════════════════════════════
\* Initial state — nondeterministic choice of inputs
\* ═══════════════════════════════════════════════════════════════════════════════

Init ==
    /\ projectDir \in ProjectChoices
    /\ readDirs   \in SUBSET ReadChoices
    /\ resumeDir  \in ResumeChoices
    /\ rules      = {}
    /\ section    = 0

\* ═══════════════════════════════════════════════════════════════════════════════
\* Policy generation actions
\* ═══════════════════════════════════════════════════════════════════════════════

\* Section 0: System library allows (static paths like /usr/lib, /System/Library).
\* These never overlap with agent home or credential paths. Abstracted away.
EmitSystemLibs ==
    /\ section = 0
    /\ section' = 1
    /\ UNCHANGED <<projectDir, readDirs, resumeDir, rules>>

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
         rules' = rules \cup {AllowRead(1, d) : d \in notSubsumed}
    /\ section' = 2
    /\ UNCHANGED <<projectDir, readDirs, resumeDir>>

\* Section 2: Project directory read+write.
EmitProjectDir ==
    /\ section = 2
    /\ rules' = rules \cup {AllowRead(2, projectDir), AllowWrite(2, projectDir)}
    /\ section' = 3
    /\ UNCHANGED <<projectDir, readDirs, resumeDir>>

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
    /\ UNCHANGED <<projectDir, readDirs, resumeDir>>

\* Section 4: Agent home — BROAD read+write allow on entire agent home.
\* This replaced individual subdirectory allows (.claude, .local, .config, etc.)
\* because Claude Code needs to access paths that can't be enumerated in advance.
\* Credential directories are denied in section 6 (last-match-wins overrides this).
EmitHomeConfig ==
    /\ section = 4
    /\ rules' = rules \cup
         \* Broad allow on agentHome covers ALL subpaths including AgentHomeSubs
         \* AND CredPaths. The credential denies in section 6 override this.
         {AllowRead(4, agentHome), AllowWrite(4, agentHome)} \cup
         {AllowRead(4, p) : p \in AgentHomeSubs} \cup
         {AllowWrite(4, p) : p \in AgentHomeSubs}
    /\ section' = 5
    /\ UNCHANGED <<projectDir, readDirs, resumeDir>>

\* Section 5: Project write re-assertion.
\* When a read-only -R directory is a parent of the project directory,
\* the broad file-read* rule could interact with SBPL evaluation.
\* Re-asserting file-write* here guarantees it is the last matching
\* allow for any write operation targeting the project directory.
EmitProjectWriteReassert ==
    /\ section = 5
    /\ rules' = rules \cup {AllowRead(5, projectDir), AllowWrite(5, projectDir)}
    /\ section' = 6
    /\ UNCHANGED <<projectDir, readDirs, resumeDir>>

\* Section 6: Credential denies (static, ALWAYS LAST).
\* Deny both file-read* (exfiltration) and file-write* (planting).
EmitCredDenies ==
    /\ section = 6
    /\ rules' = rules \cup
         {DenyRead(6, p)  : p \in CredPaths} \cup
         {DenyWrite(6, p) : p \in CredPaths}
    /\ section' = 7
    /\ UNCHANGED <<projectDir, readDirs, resumeDir>>

\* Terminal.
Done ==
    /\ section = 7
    /\ UNCHANGED vars

Next ==
    \/ EmitSystemLibs
    \/ EmitReadDirs
    \/ EmitProjectDir
    \/ EmitResumeDir
    \/ EmitHomeConfig
    \/ EmitProjectWriteReassert
    \/ EmitCredDenies
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

\* ═══════════════════════════════════════════════════════════════════════════════
\* Safety invariants — checked when policy generation is complete (section = 7)
\* ═══════════════════════════════════════════════════════════════════════════════

\* --- CRITICAL: credential file-read* is always denied ---
\* No matter what ProjectDir, ReadDirs, or ResumeDir the user chooses, the
\* credential deny in section 6 must override all earlier allows.
CredentialReadDenied ==
    section = 7 =>
        \A cred \in CredPaths : EffectiveRead(cred) = "deny_read"

\* --- Credential writes denied ---
\* Section 6 denies both file-read* and file-write* for all credential paths.
CredentialWriteDenied ==
    section = 7 =>
        \A cred \in CredPaths : EffectiveWrite(cred) = "deny_write"

\* --- Read dirs never grant write access ---
\* Rules emitted for ReadDirs (section 1) must only be AllowRead, never AllowWrite.
ReadDirsNoWrite ==
    section = 7 =>
        ~\E r \in rules : r.section = 1 /\ r.action = "allow_write"

\* --- Project dir is writable (unless it IS a credential path) ---
\* If the user picks a credential dir as their project, the deny wins.
\* This is correct — credential protection takes priority.
ProjectDirWritable ==
    section = 7 =>
        \/ projectDir \in CredPaths  \* credential deny overrides — expected
        \/ EffectiveWrite(projectDir) = "allow_write"

\* --- Read dirs within project are elided (subsumption) ---
\* If a read dir is inside ProjectDir, no rule should be emitted for it
\* (the project's read+write already covers it).
ReadDirSubsumption ==
    section = 7 =>
        ~\E r \in rules :
            r.section = 1
            /\ Contains(r.path, projectDir)

\* --- ResumeDir never overlaps credential paths ---
\* The resume session directory is under the invoker's home (/Users/dr/...),
\* not under agent home. It must never be a credential path.
ResumeDirNotCredential ==
    section = 7 =>
        \/ resumeDir = "none"
        \/ resumeDir \notin CredPaths

====

# Running TLA+ Specs — Hazmat

## Tooling

| Tool | Purpose | Install |
|------|---------|---------|
| `tla2tools.jar` | TLC model checker — finds invariant violations by exhaustive state exploration | `curl -L https://github.com/tlaplus/tlaplus/releases/download/v1.8.0/tla2tools.jar -o ~/workspace/tla2tools.jar` |
| Java 17+ | Runtime for TLC | `brew install --cask temurin` (or download from adoptium.net) |

**Installed location:** `~/workspace/tla2tools.jar`

Verify:
```bash
cd tla/
./run_tlc.sh -help | head -3
```

`run_tlc.sh` resolves a real Java binary before invoking TLC. This matters on
macOS hosts where `java` or `/usr/bin/java` may be the Apple launcher stub
instead of an installed JDK.

If your shell exports `JAVA_HOME=/usr`, that is the same launcher stub, not a
usable JDK for TLC. `run_tlc.sh` ignores it and keeps searching.

If auto-detection misses your JDK, set one of:

```bash
JAVA_BIN=/path/to/java ./run_tlc.sh -help
JAVA_HOME=/path/to/jdk ./run_tlc.sh -help
TLA2TOOLS_JAR=/path/to/tla2tools.jar ./run_tlc.sh -help
```

---

## Spec File Structure

Each problem description maps to two files:

```
tla/
  MC_SetupRollback.tla    # the spec (variables, actions, properties)
  MC_SetupRollback.cfg    # model configuration (bounds, what to check)
```

---

## Running TLC

### Check invariants (safety)
```bash
cd tla/
./run_tlc.sh \
  -workers auto \
  -config MC_SetupRollback.cfg \
  MC_SetupRollback.tla
```

### Check liveness properties (temporal)
```bash
cd tla/
./run_tlc.sh \
  -workers auto \
  -lncheck final \
  -config MC_SetupRollback.cfg \
  MC_SetupRollback.tla
```

### Key flags

| Flag | Meaning |
|------|---------|
| `-workers auto` | Use all CPU cores |
| `-deadlock` | Check for deadlock (no enabled actions) — on by default |
| `-lncheck final` | Check liveness at the end of each trace (faster) |
| `-config <file>` | Model configuration |
| `-metadir /tmp/tlc` | Write state files to a temp dir (keep working dir clean) |
| `-dump dot <file>` | Export state graph as Graphviz dot |
| `-terse` | Less verbose output |

---

## Reading TLC Output

### Success
```
Model checking completed. No error has been found.
  Estimates of the probability that TLC did not check all reachable states ...
X states generated, Y distinct states found, Z states left on queue.
```

### Invariant violation (safety)
```
Error: Invariant AgentContained is violated.

The behavior up to this point is:
1: <Initial predicate>
   agentUser = FALSE, sudoers = FALSE, pfAnchor = FALSE ...

2: <BeginSetup line ...>
   phase = "setting_up", setupStep = 0 ...

3: ...
```

The numbered trace is the **counterexample**. Read bottom-up from the violation.

### Deadlock
```
Error: Deadlock reached.
```
All variables are in a state where no action in `Next` is enabled. Usually
means the spec is incomplete (forgot an action or exhausted attempt bounds).

---

## Problem-Specific Configs

### 01 — Setup/Rollback State Machine
```cfg
SPECIFICATION Spec
CONSTANTS
    MaxSetupAttempts    = 2
    MaxRollbackAttempts = 2
INVARIANTS
    TypeOK
    NoOrphanedArtifacts
    SudoersRequiresHelper
    AgentDepsRequireUser
\* INVARIANTS
\*   AgentContained          \* KNOWN VIOLATION — uncomment to see counterexample
```
Expected: No error has been found (1887 distinct states, <1s).

---

## Agentic Workflow

TLC exits `0` on success and non-zero on any error or violation.

```bash
if ./run_tlc.sh \
      -workers auto -terse \
      -config MC.cfg MC.tla 2>&1 | tee /tmp/tlc_out.txt; then
  echo "PASS: no violations found"
else
  echo "FAIL: see /tmp/tlc_out.txt for counterexample"
  grep -A 30 "Error:" /tmp/tlc_out.txt
fi
```

---

## State Space Sizes (Reference)

| Problem | Model Bounds | Expected States | Runtime |
|---------|-------------|-----------------|---------|
| 01 Setup/Rollback | 2 setup, 2 rollback | 26,905 distinct | <1s |
| 02 Seatbelt Policy | 6 paths, 4 project choices | 192 distinct | <1s |
| 03 Backup Safety | 3 snapshots, 2 sessions, 2 restores | 395 distinct | <1s |
| 05 Tier 3 Launch Containment | 8 paths, 4 project choices, 5 read choices, 5 launch-gate booleans | 23,580 distinct | ~1s |
| 06 Tier 2 vs Tier 3 Policy Equivalence | 11 paths, 5 project choices, 6 read choices, 4 write choices, 5 launch-gate booleans | 163,840 distinct | ~15s |

If TLC runs for more than 60 seconds, reduce your model bounds.

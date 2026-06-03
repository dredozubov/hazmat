# Modular Architecture — Implementor Handoff

**Date:** 2026-06-02
**For:** whoever implements `sandboxing-zr7t` and the beads behind it
**Controlling design:** [2026-06-02-modular-architecture-direction.md](2026-06-02-modular-architecture-direction.md)
**Prior context:** [2026-05-28-reusable-library-decomposition.md](2026-05-28-reusable-library-decomposition.md), [2026-05-28-linux-ready-backend-architecture.md](2026-05-28-linux-ready-backend-architecture.md)
**Status:** Phase 0 complete (direction frozen, audited, golden baseline landed). Phase 1 ready to start.

---

## 1. The one rule that dominates everything

**Spec-first, always.** `tla/VERIFIED.md` is the authoritative governance table. If a bead touches a governed function, modeled ordering, modeled field, or modeled invariant, you **update the `.tla` spec first, run TLC to "No error has been found", then write Go**. This is not optional and it is not negotiable (CLAUDE.md, top of repo).

Pure code movement with **unchanged behavior** does not require a model update — but it requires equivalence tests that prove byte-for-byte or exact structural sameness where the spec depends on ordering or presence. "Structurally equivalent" is **not** good enough for SBPL section ordering; spec 2 requires exact order.

Running TLC:
```bash
cd tla && bash check_suite.sh           # full suite
cd tla && ./run_tlc.sh -workers auto -config MC_SeatbeltPolicy.cfg MC_SeatbeltPolicy.tla   # single spec
```

---

## 2. Where things stand right now

- Branch: `create-modular-architecture-plan` (tracking origin).
- The architecture direction doc is **audited and frozen** (audit bead `sandboxing-zm9m` closed). It carries every Phase-0-exit section: non-omittable deny floor, DTO→validated-type rule, remote threat model, schema versioning, telemetry classification, error/gap taxonomy, TLA+ phase map, golden baseline requirement, doc obligations, invariant ownership/CI.
- **Golden equivalence baseline is in** (`4041cbc`): 17 baselines under `hazmat/testdata/golden/` (SBPL matrix, explain JSON, backend capability-gap JSON, integration merge/errors, launch metadata) + a determinism fix (`sort.Strings` in `Contract.AncestorMetadataDirs`). `951bcb3` completed the Cursor Agent harness registry metadata. Both audited clean — they land **outside** the spec-change path and required no `VERIFIED.md` edit.
- The four target subpackages **already exist** as real packages under `hazmat/`: `pathpolicy/`, `sessioncontract/`, `containment/` (+ `containment/linux/`), `sessionbackend/`. Phase 1 *strengthens* them; it does not create them.

The golden baseline is your safety net. Before any Phase 1 change, `go test ./... -run TestGolden` must be green; after, the **only** golden diffs you accept are ones you can explain line-for-line. Refresh with `go test ./... -update-golden` from `hazmat/` **only after reviewing the diff**.

---

## 3. The two open invariant gaps the early beads must close

These are the substantive findings from the audit. They are *design holes the type-state work is meant to fill*, not bugs in current launch behavior (the live path enforces both correctly today via `package main`).

### F1 — `containment.Contract` fails open (owned by `ip8g`)
`hazmat/containment/contract.go`: `Contract` is an all-public-fields struct with **no constructor** (`grep "^func New|^func Build" containment/contract.go` → nothing). `CredentialDenies []CredentialDeny` is a caller-supplied slice. Any caller can write `containment.Contract{Project: ...}` with an empty deny slice and get a contract with **no credential floor**. The architecture rule (doc §"Illegal States"): *"Credential-deny floors should not be caller-supplied slices. They should be derived by validated constructors and checked by backend compilers."* `ip8g` must make the floor structural — non-omittable by construction, and re-asserted by each backend compiler so an under-populated contract fails closed.

### F2 — deny-zone input rejection is not yet non-bypassable (owned by `zr7t`)
The rejection logic lives in `package main`: `hazmat/path_policy.go:23 isCredentialDenyPath`, `:27 isHostStateDenyPath`, enforced in `session.go` `resolveSessionConfig` (lines ~1180–1208, project/read/write each checked). It works, but it's a *convention* — a function call a future caller can forget. `zr7t` must move this behind validated path/request constructors so an `AbsolutePath`/`ReadOnlyGrant`/`ProjectRoot` **cannot be constructed** pointing at a credential or host-state deny zone. Rejection becomes a property of the type, not a step.

---

## 4. Bead sequence (do them in order)

Each bead names: **goal · governing spec(s) · files · definition of done**. The doc's "Recommended First Implementation Beads" is the source of truth; this expands it with code anchors.

### `sandboxing-zr7t` — validated path/request constructors + non-bypassable deny-zone rejection
- **Goal:** wrap existing `sessioncontract` + `pathpolicy` behavior in validated constructors (`AbsolutePath`, `ExistingDir`, `CanonicalDir`, `ProjectRoot`, `ReadOnlyGrant`, `ReadWriteGrant`) so deny-zone inputs are rejected at construction, not by a separate check. Closes **F2**.
- **Governing specs:** **Spec 2** `MC_SeatbeltPolicy` (credential denies), **Spec 6** `MC_TierPolicyEquivalence` (deny-zone input rejection + backend equivalence). Model-first: if you change *what* counts as a deny zone or *when* rejection happens, the spec changes first. If you only relocate the existing predicate behind a constructor with identical semantics, it's pure movement + equivalence tests — but the rejection-on-construct behavior is exactly the kind of thing spec 6 models, so read `MC_TierPolicyEquivalence.tla` `CredentialInputsRejectedInBoth` before touching it and confirm your design still satisfies it.
- **Files:** `pathpolicy/pathpolicy.go`, `sessioncontract/sessioncontract.go`, source predicate `hazmat/path_policy.go` (`isCredentialDenyPath`, `isHostStateDenyPath`), call site `hazmat/session.go` `resolveSessionConfig`. Existing tests: `session_test.go:535+` (`TestResolveSessionConfigRejects{Project,Read,Write}CredentialDenyPath`), `integration_manifest_test.go:45+`.
- **Done when:** constructors reject every deny-zone input the current `resolveSessionConfig` rejects (port those table tests to the package); `package main` keeps a compatibility shim so launch behavior is unchanged; goldens unchanged; `go test ./...` green; `MC_TierPolicyEquivalence` + `MC_SeatbeltPolicy` re-run green (belt-and-suspenders even if you believe it's pure movement).

### `sandboxing-ip8g` — structural credential-deny floor + typed path-grant variants
- **Goal:** typed/constructor-backed containment path grants, adapters that preserve current JSON, and a **structural** credential-deny floor. Closes **F1**.
- **Governing specs:** **Spec 2** (deny floor is the last broad credential boundary), **Spec 6** (comparable core contract across tiers).
- **Files:** `containment/contract.go` (add constructor; make `CredentialDenies` derived, not free-set), the sole floor assembler `hazmat/native_session_policy.go` `newNativeSessionPolicy()` (lines ~62–99 — today the only place the floor is injected), backend compilers `hazmat/session_policy_sbpl.go` `compileDarwinSBPL`.
- **Watch the SBPL ordering:** the deny floor MUST stay the final broad credential boundary (CLAUDE.md: *"Do not reorder the sections in generateSBPL()"*). The documented post-deny keychain re-allow overrides (visible in `testdata/golden/sbpl/claude-keychain.sbpl` tail) are intentional and must survive. Any reorder shows up as a golden diff — if you see one you didn't author, stop.
- **Done when:** `containment.Contract` cannot be constructed without a floor; backend compilers re-assert/verify it (fail closed on an under-populated contract); JSON adapters keep `backend/*.json` and `explain/*.json` goldens byte-identical; `MC_SeatbeltPolicy` green.

### `sandboxing-slu6` — side-effect-free `sessionplanner` facade
- **Goal:** extract a planner that reproduces **both** launch-time planning and explain/preview planning, with `package main` launch behavior unchanged.
- **Governing specs:** **Spec 6** (`resolveSessionConfig` behavior + both Tier 2/Tier 3 planning); **Spec 7** `MC_SessionPermissionRepairs` *if* host-mutation preview/repair planning moves; **Specs 12/13** *if* credential descriptors/delivery move (avoid moving those in this bead — see §6).
- **Files:** `hazmat/session.go` `resolveSessionConfig`/`generateSBPL`, `hazmat/sandbox.go` `buildSandboxLaunchSpec` (Tier 3 path, spec-5 governed — consumes raw `sessionConfig`, not `Contract`), explain path `buildExplainJSON`.
- **Done when:** planner is pure (no FS/process/network side effects), both paths produce identical goldens, and the launch path still routes through it transparently.
- **Coverage gap to close here (from the audit):** the golden net is darwin-heavy. It pins the SBPL matrix and the backend capability-*gap* JSON, but **not** the actual Tier-3 Docker sandbox launch spec or a linux launch spec. Add Docker/linux launch-spec goldens as part of this bead before the compiler split, so the equivalence net covers the code being moved (spec 5 / future spec 14 govern those compilers).

### `sandboxing-jx71` — backend artifact variant types
- **Goal:** typed prepared-launch artifacts (`DarwinSeatbelt`, `LinuxLaunchSpec`, `DockerSandboxSpec`, `RemoteEnvelope`) — `PreparedLaunch` carries exactly one. **Do not move launch execution.** Keep `RemoteEnvelope` experimental until `nmqn` settles DTO validation/integrity.
- **Governing specs:** **Spec 2** (darwin SBPL), **Spec 5** `MC_Tier3LaunchContainment` (Docker), **Spec 6** (comparable contract), **Spec 14** `MC_LinuxNativeLaunch` (linux specs — Design Proved, Implementation Pending).
- **Done when:** `PreparedLaunch` is constructible only with one artifact variant and only when capability gaps are empty or a typed `AcceptedGap` was deliberately accepted (doc §"Illegal States"); no execution moved; goldens green.

### `sandboxing-nmqn` — remote launch envelope schema (plan-only)
- **Goal:** a **document**, not code. Draft the remote envelope schema + worker admission checklist covering integrity, replay, worker identity, threat model, credential lifecycle, cleanup, telemetry.
- **Governing specs:** **new model required before any remote execution**; map to Specs 12/13 if credential handles cross the wire. No runner implementation in this bead.
- **Done when:** the plan-only doc exists and the DTO→`ParseAndValidate`→ValidatedType rule (doc §"Wire Types Are Not Authority") is specified for the envelope. Also update `docs/design-assumptions.md` and `docs/cve-audit.md` (remote plane changes Hazmat's local-machine trust model — this is a documentation obligation, not optional).

---

## 5. Code anchors (quick map)

| Concern | Location |
| --- | --- |
| Backend-neutral contract (F1) | `hazmat/containment/contract.go` — `Contract` struct (~74–85), no constructor |
| Deny-zone predicates (F2) | `hazmat/path_policy.go:23` `isCredentialDenyPath`, `:27` `isHostStateDenyPath` |
| Deny-zone rejection call site | `hazmat/session.go` `resolveSessionConfig` (~1175, checks at 1180–1208) |
| Sole credential-floor assembler | `hazmat/native_session_policy.go` `newNativeSessionPolicy()` (~62–99) |
| Darwin SBPL compiler | `hazmat/session_policy_sbpl.go` `compileDarwinSBPL` (~22); ancestor-metadata emit (~90); deny emit (~255–260) |
| Tier-3 (Docker) launch spec | `hazmat/sandbox.go` `buildSandboxLaunchSpec` (~1167) — raw `sessionConfig`, not `Contract` |
| Explain/preview JSON | `hazmat/session.go` `buildExplainJSON`, `generateSBPL` (~2195) |
| Existing subpackages | `hazmat/{pathpolicy,sessioncontract,containment,sessionbackend}/` (+`containment/linux/`) |
| Golden harness | `hazmat/golden_baseline_test.go` (stubs `lookupAgentUser`, `-update-golden` flag) |
| Golden fixtures | `hazmat/testdata/golden/{sbpl,explain,backend,integrations,metadata}/` |

---

## 6. What NOT to touch in these beads (doc §"What Not To Move First")

Leave these alone until a later, separately-modeled bead — they are verified, security-sensitive, or under product discovery:

- `init.go` / `init_steps.go` / setup ordering / rollback (Spec 1 `MC_SetupRollback`, Spec 4 `MC_Migration`)
- live credential store internals / broker secret materialization (Specs 12/13)
- seatbelt **ordering semantics** (Spec 2 — you may move code, never reorder sections)
- launch helper fd isolation (Spec 9 `MC_LaunchFDIsolation`)
- native account + sudoers mutation
- destructive harness lifecycle (Spec 8 `MC_HarnessLifecycle`)
- desktop app attach/app-server experiment code

Integrations stay **pure data, self-enforcing**: unsafe env keys / read dirs are rejected by reusable integration logic itself, not by callbacks injected from `package main` (doc §"Invariant Ownership And CI"). `integrations` is the model every other validated package should follow.

---

## 7. Gates every bead must pass before landing

```bash
# from hazmat/
go test ./...                          # all package + compat tests, incl. TestGolden
go test ./... -run TestGolden          # equivalence net (must be green; review any diff)
# from tla/  — for any bead touching a governed area
bash check_suite.sh                    # or the single affected spec, green
```
Plus the full pre-push hook (secrets scan, `go vet`, full tests, lint, Linux compile, CLI smoke matrix). Commit on a branch (never master); end commit messages with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`; commit/push only when asked.

**Invariant ownership** (doc requires each bead to name owner + test + CI gate):
- deny-zone rejection → validated request/path package (`zr7t`)
- credential-deny floor → `containment` + backend compilers (`ip8g`)
- safe integration env → `integrations`, not caller callbacks
- zero-gap prepared launch → backend artifact package (`jx71`)
- remote DTO validation → remote envelope package (`nmqn`)

---

## 8. Documentation obligations (don't skip)

Update when the change reaches the reader's trust model or user-visible behavior (doc §"Documentation Obligations"):
- `tla/VERIFIED.md` + the spec design note — when modeled behavior or governed-code ownership changes
- `docs/design-assumptions.md` — when remote/worker/multi-party trust changes the local-machine assumption (`nmqn`)
- `docs/cve-audit.md` + threat docs — when a new backend or remote plane changes attack surface
- `README.md`, `docs/overview.md`, user docs — when command surface, JSON output, backend support, or setup changes
- supersede the older 2026-05-28 plans' package names if they drift from what you ship

---

## TL;DR for the implementor

Start with `zr7t`. Read `MC_TierPolicyEquivalence.tla` and `MC_SeatbeltPolicy.tla` first. Move the existing deny-zone rejection (`path_policy.go` + `resolveSessionConfig`) behind validated path/request constructors so a deny-zone path **can't be constructed**, keep a `package main` shim, port the rejection table tests into the package, keep all 17 goldens byte-identical, re-run both specs green. Then `ip8g` makes the `containment.Contract` floor structural. Don't move setup/rollback/credentials/fd-isolation/seatbelt-ordering. The golden net + TLC are your proof that you changed structure without changing behavior.

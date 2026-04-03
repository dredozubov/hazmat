# Problem 6 — Tier 2 vs Tier 3 Effective Policy Equivalence

## Problem Statement

The user-visible question is simple: are Tier 2 native containment and Tier 3
Docker Sandbox sessions "the same policy"?

The codebase already suggests the answer is "not exactly":

- Tier 3 rejects integration env passthrough in `hazmat/sandbox.go`
- Tier 3 keeps `--resume` / `--continue` history sandbox-local instead of using
  the host transcript sync used by Tier 2
- Tier 3 may rewrite ancestor read/write inputs into sibling mounts because
  Docker workspace mounts cannot exactly mirror Seatbelt path rules

At the same time, Hazmat still wants one coherent contract for the important
part of session authority:

- the project directory is read-write
- explicit extra write directories are read-write
- explicit extra read directories are read-only
- credential-deny paths are rejected or blocked

This spec therefore answers two separate questions:

1. Can Tier 2 and Tier 3 be proved **exactly identical** as backend policies?
2. If not, can they still be proved equivalent on a narrower,
   backend-neutral **core containment contract**?

## Code Location

| File | Functions |
|------|-----------|
| `hazmat/session.go` | `resolveSessionConfig()`, `generateSBPL()`, `agentEnvPairs()` |
| `hazmat/sandbox.go` | `prepareSandboxLaunch()`, `buildSandboxLaunchSpec()` |
| `hazmat/integration_manifest.go` | `isCredentialDenyPath()` |

## Status

Proved, with one real code bug fixed during the analysis.

## Current Conclusion

### Exact identity: reasoned false

The code and the drafted model both show that exact Tier 2/Tier 3 identity is
too strong for the current product, even after fixing a real bug in Tier 2
path validation.

The drafted model encodes three persistent exact-identity gaps:

| Invariant | Meaning |
|-----------|---------|
| `IntegrationEnvBreaksExactIdentity` | Tier 2 can launch with integration env passthrough, Tier 3 currently rejects it |
| `ResumeBreaksExactIdentity` | Tier 2 uses host transcript sync, Tier 3 keeps resume history sandbox-local |
| `AncestorRewriteBreaksExactIdentity` | Tier 3 must rewrite some ancestor paths into sibling mounts instead of preserving the exact root set |

So the answer to "are the policies identical?" is **no**.

### Core containment equivalence: proved

The narrower claim is:

> If we exclude backend-specific env/resume behavior, require Tier 3 launch
> gates to be satisfied, and avoid ancestor-overlap shapes that require mount
> rewriting, then Tier 2 and Tier 3 implement the same core containment
> contract.

That claim is captured by `CanonicalCoreContainmentEquivalent`, and TLC passes
with the current model bounds.

## Bug Found and Fixed

This work found a real mismatch, not just an intentional product difference:

- Tier 3 already rejected project/read/write paths that resolved to credential
  deny zones using `isCredentialDenyPath()`
- Tier 2 native sessions did **not** reject those paths during
  `resolveSessionConfig()`

That meant native sessions could accept explicit roots such as the invoker's
home directory, even though Docker Sandbox mode already treated the same input
as unsafe because it is a parent of `~/.ssh`, `~/.aws`, and similar credential
paths.

The fix was to add the same credential-deny validation to native session
resolution in `resolveSessionConfig()`.

## TLA+ Model

### Abstract Path Model

Eleven abstract paths:

| Path | Represents | Contains |
|------|-----------|----------|
| `workspaceRoot` | broad parent of project | `projectRoot`, `projectSub` |
| `projectRoot` | project root | `projectSub` |
| `projectSub` | nested project dir | (nothing) |
| `readRoot` | explicit read-only reference root | `readChild` |
| `readChild` | nested read-only path | (nothing) |
| `writeRoot` | explicit write extension root | `writeChild` |
| `writeChild` | nested write path | (nothing) |
| `invokerHome` | invoker home | `sshDir`, `awsDir`, `resumeHost` |
| `sshDir` | invoker `~/.ssh` | (nothing) |
| `awsDir` | invoker `~/.aws` | (nothing) |
| `resumeHost` | host transcript/resume directory | (nothing) |

Credential-deny paths are modeled as `{sshDir, awsDir}`. A path is unsafe if
it is a credential path itself or a parent of one.

### Nondeterministic Inputs

- `ProjectDir`
- `ReadDirs`
- `WriteDirs`
- `IntegrationEnvRequested`
- `ResumeRequested`
- Tier 3 launch gates:
  - backend ready
  - approval granted
  - extra mount support present or absent

### Policy Operators

The spec derives:

- Tier 2 normalized read roots
- Tier 2 normalized write roots
- Tier 3 normalized read roots
- Tier 3 normalized write roots
- exact backend policy records
- narrower core containment policy records

Tier 3 also tracks whether ancestor rewrite would be required. The spec does
not model the full rewritten sibling set; it only needs to prove that exact
identity is impossible once that rewrite behavior enters.

## What TLC Checks

| Invariant | Meaning |
|-----------|---------|
| `CredentialInputsRejectedInBoth` | Both tiers reject credential-overlapping project/read/write roots |
| `IntegrationEnvBreaksExactIdentity` | Integration env passthrough makes exact identity false |
| `ResumeBreaksExactIdentity` | Host resume sync makes exact identity false |
| `AncestorRewriteBreaksExactIdentity` | Ancestor rewrite makes exact identity false |
| `CanonicalCoreContainmentEquivalent` | Under canonical comparable inputs, both tiers have the same core containment policy |

## TLC Result

Run:

```bash
cd tla/
./run_tlc.sh -workers 1 \
  -config MC_TierPolicyEquivalence.cfg \
  MC_TierPolicyEquivalence.tla
```

Observed result:

- `Model checking completed. No error has been found.`
- `327680 states generated`
- `163840 distinct states found`
- `depth 1`
- `Finished in 13s`

## Interpretation

The useful product conclusion is:

- Hazmat should not claim Tier 2 and Tier 3 are identical policies
- Hazmat **can** claim they share one core containment contract when the
  session fits the canonical comparable subset
- any future work that aims for stronger equivalence has to remove the known
  product differences first:
  - integration env support in Tier 3
  - host transcript resume parity
  - a more explicit cross-backend story for ancestor path handling

## Model Bounds

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Paths | 11 | project roots, read/write roots, invoker credential roots, resume root |
| ProjectChoices | 5 | safe project roots plus unsafe broad/credential parents |
| ReadChoices | 6 | safe roots, nested roots, overlapping roots, unsafe home parent |
| WriteChoices | 4 | safe write roots plus unsafe broad parent |
| Launch gate booleans | 5 | env, resume, backend readiness, approval, extra-mount capability |

# TLC Trace Artifact Policy

Hazmat treats raw TLC trace and state files as local generated output, not proof
source. The promoted proof base is the checked `MC_*.tla` / `MC_*.cfg` suite,
the companion design notes, `VERIFIED.md`, and the proof ownership ledger.

## Default Policy

- `tla/*_TTrace_*.tla`, `tla/*_TTrace_*.bin`, and `tla/states/` are ignored by
  Git and may be deleted at any time.
- Raw TLC traces are useful while debugging a failing model, but no proof claim
  should depend on an undocumented local trace file.
- CI proof artifacts should retain logs and parsed metrics, not root-level raw
  Toolbox trace modules.
- If a counterexample must be preserved, reduce it to a documented reproducer:
  a focused `.md` note, a smaller promoted model, or a checked test case linked
  to the owning bead. Do not commit raw `_TTrace_` files without an explicit
  policy exception.

## Current Local State

The repository may contain ignored local `_TTrace_` files from previous TLC or
Toolbox debugging sessions. They are not tracked, not part of the proof audit
baseline, and not required to reproduce the current verified results.

Use:

```bash
cd tla/
bash trace_artifact_check.sh
```

The check fails if generated TLC traces or state files become tracked.

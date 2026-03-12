# Step 10: Snapshots Enhancement

Date: 2026-03-12
Plan: docs/plans/2026-03-12-snapshots-enhancement.md

## What Was Implemented

Added explicit snapshot capture/restore with version validation, enabling point-in-time export/import of workflow instance state. `Engine.Snapshot()` captures the complete `WorkflowInstance` runtime state plus `DefinitionHash` for validation. `Engine.Restore()` creates a new instance from a snapshot (reject if exists), validating both `WorkflowVersion` and `DefinitionHash` against the registered compiled machine.

## Files Created

- `types/snapshot.go` — `Snapshot` struct with JSON tags, mirrors all runtime-relevant `WorkflowInstance` fields plus `DefinitionHash` and `CapturedAt`
- `types/snapshot_test.go` — compile-time field mapping test (reflect-based), JSON round-trip tests, nil map handling
- `internal/runtime/snapshot.go` — `Engine.Snapshot()` capture logic and `Engine.Restore()` with 3-step validation (version → hash → existence)
- `snapshot_test.go` — 20 integration tests in `flowstep_test` package: capture, restore, error paths, round-trips across flat/hierarchical/parallel/stuck workflows

## Files Modified

- `errors.go` — added `ErrSnapshotDefinitionMismatch`, `ErrSnapshotInstanceExists` sentinel errors
- `internal/runtime/runtime.go` — added `ErrSnapshotDefinitionMismatch`, `ErrSnapshotInstanceExists` to `Deps` struct
- `internal/runtime/helpers.go` — added `copyTimePtr()` helper for deep-copying `*time.Time` (prevents pointer aliasing)
- `engine.go` — added `Snapshot()` and `Restore()` public methods delegating to internal; wired new sentinel errors
- `aliases.go` — re-exported `types.Snapshot` as `flowstep.Snapshot`

## Decisions Made

- **Snapshot is a value type** — callers copy and modify fields (e.g., `AggregateID`) before passing to `Restore`. This is the intended mechanism for "restore to different aggregate" / clone scenarios.
- **Restore is create-only** — rejects with `ErrSnapshotInstanceExists` if an instance already exists. No overwrite semantics.
- **Double validation** — both `WorkflowVersion` and `DefinitionHash` must match. Version resolves the compiled machine; hash proves the definition content is identical.
- **TOCTOU race documented** — the Get-then-Create existence check is best-effort. Production stores should enforce uniqueness via database UNIQUE constraint.
- **SnapshotStore deferred** — no persistence adapter for snapshots in this task. Deferred to Task 9 (replay).
- **PendingTimers deferred** — timer state excluded until Task 6 (timers) is implemented.
- **`StuckAt *time.Time` deep-copied** — `copyTimePtr()` helper prevents pointer aliasing between snapshot and stored instance.

## Deviations from Plan

None — all 4 tasks implemented as planned. Review findings (StuckAt pointer aliasing, missing deep-history test) were fixed during verification.

# Snapshots Enhancement Implementation Plan

Created: 2026-03-12
Status: VERIFIED
Approved: Yes
Iterations: 3
Worktree: No
Type: Feature

## Summary

**Goal:** Add explicit snapshot capture/restore with version validation, enabling point-in-time export/import of workflow instance state.

**Architecture:** `types.Snapshot` as a pure domain struct in `types/`, with `Engine.Snapshot()` and `Engine.Restore()` as public API methods. Snapshot captures the complete `WorkflowInstance` runtime state plus `DefinitionHash` for validation. Restore creates a new instance (reject if exists). No SnapshotStore, no periodic automation — those are deferred to Task 9 (replay).

**Tech Stack:** Go, flowstep Clean Architecture (types/, internal/runtime/, root package)

## Scope

### In Scope
- `types.Snapshot` struct capturing full WorkflowInstance runtime state + DefinitionHash
- JSON serialization/deserialization of Snapshot
- `Engine.Snapshot(ctx, aggregateType, aggregateID)` — capture current state
- `Engine.Restore(ctx, snapshot)` — create instance from snapshot (reject if exists)
- DefinitionHash + WorkflowVersion validation on restore
- SnapshotRestored domain event for audit trail
- Sentinel errors: `ErrSnapshotDefinitionMismatch`, `ErrSnapshotInstanceExists`
- Round-trip tests for flat, hierarchical, parallel, and stuck workflows

### Out of Scope
- SnapshotStore interface (deferred to Task 9 — replay)
- Periodic/automatic snapshotting (deferred to Task 9)
- Snapshot migration hooks (deferred to versioning work)
- Overwrite/force-restore semantics (future admin API if needed)
- PendingTimers in snapshot (deferred until Task 6 — timers)
- Snapshot retention/pruning policy
- Loading snapshot + replaying tail events

## Context for Implementer

> Write for an implementer who has never seen the codebase.

- **Patterns to follow:**
  - Public API wrapper pattern: `engine.go` delegates to `internal/runtime`. See `engine.go:91-98` (Transition) for the delegation pattern.
  - Functional options: `options.go` defines `WithXxx` helpers. Not needed here (no new engine options), but follow the naming conventions.
  - Optional interfaces via type assertion: `SavepointProvider` (`types/interfaces.go:72-78`) shows how optional capabilities are detected. Not needed here, but good context for the pattern.
  - Domain event construction: `executor.go:250-264` shows how to build a `DomainEvent` from instance state.
  - Instance creation in transaction: `executor.go:402-443` (`createInstance`) shows the create-in-tx pattern.
  - Re-export aliases: `aliases.go` and `flowstep.go` re-export types for the public API surface.

- **Conventions:**
  - Sentinel errors in `errors.go` with `flowstep:` prefix
  - All public methods take `context.Context` as first argument
  - Test harness: `testutil.NewTestEngine(t)` or `newTestHarness(t)` in `engine_test.go`
  - Black-box integration tests in `flowstep_test` package (root `*_test.go` files)
  - White-box unit tests inside `internal/runtime/` package
  - IDs generated via `generateID()` in `internal/runtime/helpers.go`

- **Key files:**
  - `types/instance.go` — WorkflowInstance struct (16 fields, all must be in Snapshot)
  - `types/event.go` — DomainEvent struct
  - `types/interfaces.go` — All store interfaces
  - `internal/graph/compiled.go:42-44` — DefinitionHash on CompiledMachine
  - `internal/runtime/executor.go:402-443` — createInstance (pattern for Restore)
  - `internal/runtime/runtime.go:155-171` — compiledFor (resolve compiled machine by version)
  - `engine.go` — public Engine wrapper
  - `errors.go` — sentinel errors
  - `aliases.go` — type re-exports

- **Gotchas:**
  - `LastReadUpdatedAt` is `json:"-"` and internal-only — exclude from Snapshot, set fresh on restore
  - `StuckAt` is `*time.Time` (pointer) — handle nil in JSON serialization
  - `ShallowHistory`, `DeepHistory`, `ActiveInParallel` may be nil vs empty map — Snapshot should normalize (always non-nil maps)
  - `ParallelClock` is meaningful only when `ActiveInParallel` is non-nil
  - Instance identity (AggregateType + AggregateID) in Snapshot determines WHERE it gets restored — these are the composite key
  - `types.Snapshot` is a value type (not pointer) — callers may copy and modify fields (including AggregateID, CorrelationID) before passing to Restore. This is the intended mechanism for "restore to different aggregate" / clone scenarios. Restore creates an instance using the snapshot's fields as-is.
  - `InstanceStore.Create` takes a `tx any` parameter — must wrap in transaction
  - The `memstore.InstanceStore.Get` returns a copy (not pointer into map) — safe for testing
  - The "reject if exists" check is best-effort under the current interface: Get-then-Create has a TOCTOU race window. Production stores should enforce uniqueness via database constraints (e.g., UNIQUE index on aggregate_type + aggregate_id). The memstore is single-goroutine-safe due to its mutex but does not enforce duplicate detection in Create. Document this limitation.

- **Domain context:**
  - A Snapshot is a point-in-time export of a workflow instance's complete runtime state
  - The DefinitionHash proves the snapshot was taken against a specific workflow definition graph
  - Restoring a snapshot means creating a new instance row from the captured state — it's conceptually like "importing a backup"
  - The WorkflowVersion + DefinitionHash double-check prevents restoring against a different or modified definition

## Assumptions

- Snapshot captures WorkflowInstance fields only, not related entities (tasks, children, activities) — supported by user decision "Reject if exists" + simple scope — Tasks 1-4 depend on this
- DefinitionHash is always available on CompiledMachine after Register() — supported by `compiled.go:136` (`cm.DefinitionHash = computeHash(def)`) — Task 2 depends on this
- No concurrent modification risk during snapshot capture (read-only, no locking needed) — supported by `InstanceStore.Get` returning a copy — Task 2 depends on this
- Restored instance gets the captured CreatedAt (preserves original timeline) and UpdatedAt set to restoration time — Task 3 depends on this
- SnapshotRestored event uses the snapshot's CorrelationID (whatever value is in the snapshot at restore time). For same-aggregate restore this maintains the audit chain. For cross-aggregate clone scenarios (caller modifies AggregateID before restore), the caller is responsible for also deciding whether to keep or change the CorrelationID — Task 3 depends on this

## Testing Strategy

- **Unit tests (types/):** Snapshot JSON round-trip, field completeness assertion
- **Integration tests (flowstep_test):** Full Engine.Snapshot/Restore round-trips across workflow types (flat, hierarchical, parallel, stuck), error cases (mismatch, already exists)
- **Patterns:** Table-driven tests with `t.Run()` subtests. Use `testutil.NewTestEngine(t)` for engine setup.

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Snapshot struct drifts from WorkflowInstance when new fields are added | Medium | High — silent data loss on round-trip | Task 1 includes a compile-time field completeness test that fails when WorkflowInstance gets new fields not present in Snapshot |
| Restored instance has stale CorrelationID that collides with existing events | Low | Medium — confusing event queries | Restore uses snapshot's CorrelationID (preserves chain); if instance was deleted, old events still exist with that correlation — this is correct behavior |
| Map fields (StateData, History) have reference sharing after restore | Medium | High — mutations leak between snapshot and instance | Snapshot capture and restore both deep-copy all map fields |
| TOCTOU race on "reject if exists" check (Get-then-Create) | Low | Medium — duplicate instance in concurrent scenario | Best-effort under current interface; document limitation; production stores should use UNIQUE constraint on (aggregate_type, aggregate_id) |

## Pre-Mortem

*Assume this plan failed. Most likely internal reasons:*

1. **Snapshot struct misses a WorkflowInstance field added by a future task** (Task 1) — Trigger: the compile-time field-count assertion in `types/snapshot_test.go` fails after a WorkflowInstance change. Recovery: add the new field to Snapshot and update mapping functions.
2. **Restore creates an instance but the engine can't load it because compiledFor() can't find the version** (Task 3) — Trigger: Restore succeeds but the first Transition after restore fails with "no workflow version N registered." Recovery: Restore must validate that the definition is registered BEFORE creating the instance, not just check the hash.

## Goal Verification

### Truths
1. `Engine.Snapshot()` returns a Snapshot that contains every runtime-relevant field from WorkflowInstance
2. `Engine.Restore()` creates a new instance from a Snapshot that can immediately participate in transitions
3. Restore rejects snapshots with mismatched DefinitionHash with `ErrSnapshotDefinitionMismatch`
4. Restore rejects snapshots with mismatched WorkflowVersion with `ErrSnapshotDefinitionMismatch`
5. Restore rejects when an instance already exists with `ErrSnapshotInstanceExists`
6. Snapshot round-trips through JSON without data loss
7. Snapshot/Restore works correctly for flat, hierarchical (with history), and parallel workflows

### Artifacts
1. `types/snapshot.go` — Snapshot struct with JSON tags
2. `types/snapshot_test.go` — JSON round-trip, field completeness
3. `internal/runtime/snapshot.go` — capture and restore logic
4. `engine.go` — Snapshot() and Restore() public methods
5. `errors.go` — sentinel errors
6. `snapshot_test.go` — integration tests (flowstep_test package)

### Key Links
- `types.Snapshot` ↔ `types.WorkflowInstance` (field mapping)
- `Engine.Snapshot()` → `internal/runtime.Engine.Snapshot()` → `InstanceStore.Get` + `compiledFor`
- `Engine.Restore()` → `internal/runtime.Engine.Restore()` → `compiledFor` + `InstanceStore.Create`
- `CompiledMachine.DefinitionHash` → `Snapshot.DefinitionHash` (validation key)

## Progress Tracking

- [x] Task 1: Snapshot type + sentinel errors + field completeness test
- [x] Task 2: Engine.Snapshot() — capture runtime state
- [x] Task 3: Engine.Restore() — restore from snapshot with validation
- [x] Task 4: Round-trip integration tests across workflow types

**Total Tasks:** 4 | **Completed:** 4 | **Remaining:** 0

## Implementation Tasks

### Task 1: Snapshot Type Definition

**Objective:** Define the `types.Snapshot` struct with all fields needed to round-trip a `WorkflowInstance`, plus sentinel errors and a compile-time field completeness guard.

**Dependencies:** None

**Files:**
- Create: `types/snapshot.go`
- Create: `types/snapshot_test.go`
- Modify: `errors.go`

**Key Decisions / Notes:**
- Snapshot struct mirrors all runtime-relevant WorkflowInstance fields (see `types/instance.go:6-38`)
- Adds `DefinitionHash` (from `CompiledMachine`) and `CapturedAt` (metadata)
- Includes `CreatedAt` from WorkflowInstance (restored instance preserves original creation time)
- Excludes `LastReadUpdatedAt` (internal, json:"-") and `UpdatedAt` (set to restoration time on restore)
- All map fields use `omitempty` for parallel-specific fields only; history maps always present (even if empty)
- JSON tags on every field for explicit serialization control
- Sentinel errors: `ErrSnapshotDefinitionMismatch` and `ErrSnapshotInstanceExists` in `errors.go`
- The `ID` field from WorkflowInstance is intentionally renamed to `InstanceID` in Snapshot for clarity in serialized JSON. The capture function maps `instance.ID → snap.InstanceID` and restore maps `snap.InstanceID → instance.ID`.
- Field completeness test: define an explicit mapping table of (WorkflowInstance field name → Snapshot field name) covering all fields except the excluded set (UpdatedAt, LastReadUpdatedAt). Use `reflect` to iterate both structs' fields and verify: (a) every WorkflowInstance field outside the exclusion list has a corresponding Snapshot field in the mapping, (b) every Snapshot field that maps to a WorkflowInstance field actually exists on the struct, (c) if a new field is added to WorkflowInstance and not mapped, the test fails. This is stronger than a count-based assertion because it catches renamed, missing, or mis-mapped fields.

**Snapshot struct:**
```go
type Snapshot struct {
    InstanceID       string            `json:"instance_id"`
    WorkflowType     string            `json:"workflow_type"`
    WorkflowVersion  int               `json:"workflow_version"`
    AggregateType    string            `json:"aggregate_type"`
    AggregateID      string            `json:"aggregate_id"`
    CurrentState     string            `json:"current_state"`
    StateData        map[string]any    `json:"state_data"`
    CorrelationID    string            `json:"correlation_id"`
    IsStuck          bool              `json:"is_stuck"`
    StuckReason      string            `json:"stuck_reason,omitempty"`
    StuckAt          *time.Time        `json:"stuck_at,omitempty"`
    RetryCount       int               `json:"retry_count"`
    ShallowHistory   map[string]string `json:"shallow_history"`
    DeepHistory      map[string]string `json:"deep_history"`
    ActiveInParallel map[string]string `json:"active_in_parallel,omitempty"`
    ParallelClock    int64             `json:"parallel_clock,omitempty"`
    CreatedAt        time.Time         `json:"created_at"`
    DefinitionHash   string            `json:"definition_hash"`
    CapturedAt       time.Time         `json:"captured_at"`
}
```

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] `types.Snapshot` struct defined with JSON tags matching all runtime-relevant WorkflowInstance fields
- [ ] `ErrSnapshotDefinitionMismatch` and `ErrSnapshotInstanceExists` sentinel errors defined
- [ ] JSON marshal/unmarshal round-trip test passes for a fully populated Snapshot
- [ ] Field mapping test uses reflect + explicit mapping table to verify Snapshot covers all WorkflowInstance fields (minus excluded: UpdatedAt, LastReadUpdatedAt), catching renames and omissions

**Verify:**
```bash
go test ./types/ -run TestSnapshot -v
go test ./... -run TestSnapshot -count=1
go vet ./...
```

---

### Task 2: Engine.Snapshot() — Capture Runtime State

**Objective:** Implement `Engine.Snapshot()` that reads the current instance state and returns a complete `types.Snapshot` with the `DefinitionHash` from the registered compiled machine.

**Dependencies:** Task 1

**Files:**
- Modify: `engine.go` (add `Snapshot` method)
- Create: `internal/runtime/snapshot.go` (capture logic)
- Modify: `aliases.go` (re-export `Snapshot` type)
- Create: `snapshot_test.go` (integration tests in `flowstep_test` package)

**Key Decisions / Notes:**
- Public API: `Engine.Snapshot(ctx context.Context, aggregateType, aggregateID string) (*types.Snapshot, error)`
- Delegates to `internal/runtime.Engine.Snapshot()` following `engine.go` delegation pattern
- Implementation: `InstanceStore.Get` → `compiledFor(aggregateType, instance.WorkflowVersion)` → build Snapshot from instance fields + `cm.DefinitionHash`
- `CapturedAt` set from `deps.Clock.Now()` (testable via FakeClock)
- All map fields deep-copied (prevent aliasing between returned Snapshot and stored instance)
- Returns `ErrInstanceNotFound` if no instance exists
- Returns error if no compiled machine found for the instance's version
- Read-only operation — no transaction needed
- Follows shutdown guard pattern: `checkShutdown()` + `wg.Add(1)/Done()`

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] `engine.Snapshot()` returns a complete Snapshot for an existing instance
- [ ] Snapshot's DefinitionHash matches the registered definition's hash
- [ ] Snapshot's WorkflowVersion matches the instance's version
- [ ] All map fields are deep copies (mutation test)
- [ ] Returns ErrInstanceNotFound for non-existent aggregate

**Verify:**
```bash
go test ./... -run TestSnapshot -v -count=1
go vet ./...
```

---

### Task 3: Engine.Restore() — Restore From Snapshot With Validation

**Objective:** Implement `Engine.Restore()` that validates a snapshot against the registered definition and creates a new workflow instance from it.

**Dependencies:** Task 1, Task 2

**Files:**
- Modify: `engine.go` (add `Restore` method)
- Modify: `internal/runtime/snapshot.go` (restore logic)
- Modify: `snapshot_test.go` (add restore tests)

**Key Decisions / Notes:**
- Public API: `Engine.Restore(ctx context.Context, snap types.Snapshot) error`
- Validation sequence (fail-fast):
  1. `compiledFor(snap.AggregateType, snap.WorkflowVersion)` — version must be registered
  2. Compare `snap.DefinitionHash` with `cm.DefinitionHash` — must match exactly
  3. `InstanceStore.Get(snap.AggregateType, snap.AggregateID)` — check with `errors.Is(err, ErrInstanceNotFound)`: if this sentinel is returned, the instance does not exist and restore may proceed; if Get returns nil error (instance found), return `ErrSnapshotInstanceExists`; if Get returns any other error, propagate it as-is (do not interpret DB errors as "instance found")
- Creates a new `WorkflowInstance` from Snapshot fields:
  - `CreatedAt` = `snap.CreatedAt` (preserves original instance creation time)
  - `UpdatedAt` = `deps.Clock.Now()` (restoration time)
  - `LastReadUpdatedAt` = zero (new instance, no prior read)
  - All other fields copied from Snapshot
- Persists via `InstanceStore.Create` inside a transaction
- Emits a `SnapshotRestored` domain event via `EventStore.Append` in the same transaction
  - EventType: `"SnapshotRestored"`, TransitionName: `"_snapshot_restore"`
  - Payload includes `{"_snapshot_captured_at": snap.CapturedAt}`
- Emits event via EventBus post-commit (same pattern as ForceState, `executor.go:531-534`)
- **TOCTOU note:** The Get-then-Create existence check has a race window under concurrent access. This is best-effort; production stores should enforce uniqueness via database constraints. Document this in a code comment on the Restore method.
- Follows shutdown guard pattern

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] Restore creates a working instance from a valid Snapshot
- [ ] Restore rejects with `ErrSnapshotDefinitionMismatch` when DefinitionHash doesn't match
- [ ] Restore rejects with `ErrSnapshotDefinitionMismatch` when WorkflowVersion doesn't match (version not registered)
- [ ] Restore rejects with `ErrSnapshotInstanceExists` when instance already exists
- [ ] Restored instance can immediately accept transitions
- [ ] SnapshotRestored event is persisted and emitted

**Verify:**
```bash
go test ./... -run "TestSnapshot|TestRestore" -v -count=1
go vet ./...
```

---

### Task 4: Round-Trip Integration Tests

**Objective:** Comprehensive integration tests proving Snapshot/Restore works correctly across all workflow types: flat, hierarchical (with history), parallel (with regions), and stuck instances.

**Dependencies:** Task 2, Task 3

**Files:**
- Modify: `snapshot_test.go` (add round-trip integration subtests)

**Key Decisions / Notes:**
- Each subtest: define workflow → register → create instance → transition to interesting state → snapshot → copy snapshot with modified AggregateID → restore copy → verify state matches → verify restored instance can transition
- Snapshot is a value type — tests copy and modify `snap.AggregateID` (e.g., `"order-1"` → `"order-1-restored"`) before calling Restore. This is the documented mechanism for restoring to a different aggregate.
- Test scenarios:
  1. **Flat workflow round-trip:** Simple A→B→C, snapshot at B, modify AggregateID, restore, verify CurrentState=B, transition to C works
  2. **Hierarchical with shallow history:** Enter compound state → transition within → exit → snapshot → modify AggregateID → restore → verify ShallowHistory preserved
  3. **Hierarchical with deep history:** Same pattern with deep history, verify DeepHistory maps
  4. **Parallel state round-trip:** Enter parallel state → verify ActiveInParallel + ParallelClock captured → modify AggregateID → restore → verify parallel state functional
  5. **Stuck instance round-trip:** Force instance to stuck state → snapshot → modify AggregateID → restore → verify IsStuck, StuckReason, RetryCount preserved
  6. **JSON serialization round-trip:** Snapshot → `json.Marshal` → `json.Unmarshal` → modify AggregateID → Restore → verify no data loss
  7. **StateData preservation:** Instance with non-trivial StateData → snapshot → modify AggregateID → restore → verify map contents match
- Use `testutil.NewTestEngine` for full adapter setup

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] Flat workflow round-trip succeeds
- [ ] Hierarchical workflow round-trip preserves history
- [ ] Parallel workflow round-trip preserves regions and clock
- [ ] Stuck instance round-trip preserves stuck state
- [ ] JSON serialization round-trip preserves all data
- [ ] Restored instances can continue normal transitions

**Verify:**
```bash
go test ./... -run TestSnapshotRoundTrip -v -count=1
go test ./... -count=1
go vet ./...
```

## Open Questions

None — all design decisions resolved.

### Deferred Ideas

- **SnapshotStore interface** — optional persistence adapter for snapshots (Task 9 scope)
- **Periodic/automatic snapshots** — background scheduler for snapshot cadence (Task 9 scope)
- **Snapshot migration hooks** — transform snapshots across definition versions (future versioning work)
- **ForceRestore / WithOverwrite** — admin API to overwrite existing instances from snapshots
- **PendingTimers in snapshot** — capture timer state once Task 6 (timers) is implemented
- **Related entity snapshot** — capture tasks, children, activities alongside instance state

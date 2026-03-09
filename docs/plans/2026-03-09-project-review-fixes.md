# Project Review & Critical-to-Medium Fixes Implementation Plan

Created: 2026-03-09
Status: VERIFIED
Approved: Yes
Iterations: 1
Worktree: No
Type: Feature

## Summary

**Goal:** Fix all critical-to-medium severity issues found during a comprehensive review of the flowstate Go library codebase.

**Architecture:** The fixes touch the internal engine (thread safety, error handling, dead code), all store adapters (optimistic locking), the migration schema, the Mermaid generator, and test coverage. No structural changes to the Clean Architecture layout.

**Tech Stack:** Go, pgx, database/sql, DynamoDB SDK

## Scope

### In Scope
- Critical: Engine thread safety (Register race condition)
- Critical: Silently swallowed post-commit errors
- Critical: Missing optimistic locking in pgxstore/sqlitestore/dynamostore
- High: Dead code cleanup in Transition method
- High: Missing `next_retry_at` column in migration
- High: pgxstore TaskStore.Get not returning sentinel errors
- Medium: `ErrEngineShutdown` missing from sentinel error test
- Medium: Shallow `copyMap` for event state snapshots
- Medium: Mermaid generator not rendering routed transitions
- Medium: memstore using `time.Now()` instead of Clock interface
- Test coverage improvements for all changes

### Out of Scope
- Adding new features or changing the public API shape
- Performance optimization
- Adding tests for adapters that require external infrastructure (Redis, NATS, DynamoDB, PostgreSQL)
- `ChildrenDef.InputsFn` always called with nil (would require API change)
- SQLite store migrations file (no existing migration file to fix)

## Context for Implementer

> Write for an implementer who has never seen the codebase.

- **Patterns to follow:** Functional Options (`options.go`), sentinel errors with wrapping (`errors.go`), interface mirroring in internal packages (`internal/engine/engine.go:42-112`)
- **Conventions:** All interfaces in root package, implementations in `adapters/`, business logic in `internal/engine/`, pure types in `types/`
- **Key files:**
  - `internal/engine/engine.go` — Core state machine logic (869 lines, main fix target)
  - `errors.go` — All sentinel errors
  - `hooks.go` — Hook interface (needs new `OnPostCommitError` method)
  - `adapters/pgxstore/instance_store.go` — PostgreSQL instance store (needs optimistic locking)
  - `adapters/pgxstore/migrations/001_flowstate.sql` — Schema definition
  - `adapters/memstore/task_store.go` — In-memory task store (uses `time.Now()` directly)
- **Gotchas:**
  - The internal engine mirrors all root interfaces to avoid import cycles. Changes to `Hooks` interface must be mirrored in `internal/engine/engine.go`.
  - `NoopHooks` in `hooks.go` must implement any new methods.
  - pgxstore tests require `FLOWSTATE_TEST_DB` env var (real PostgreSQL). Without it, tests are silently skipped. Verify optimistic locking against a local PostgreSQL if available.
  - Adding a method to the `Hooks` interface is a compile-time breaking change for external implementors — triggers a minor version bump per CLAUDE.md Section 6B.

## Progress Tracking

- [x] Task 1: Add RWMutex to engine for thread-safe Register
- [x] Task 2: Add OnPostCommitError hook and surface post-commit failures
- [x] Task 3: Fix optimistic locking in pgxstore InstanceStore.Update
- [x] Task 4: Fix optimistic locking in sqlitestore and dynamostore InstanceStore.Update
- [x] Task 5: Fix migration schema, sentinel errors in pgxstore, and shallow copyMap
- [x] Task 6: Fix Mermaid routed transitions and memstore Clock usage
- [x] Task 7: Fix ErrEngineShutdown missing from test + dead code cleanup

**Total Tasks:** 7 | **Completed:** 7 | **Remaining:** 0

## Implementation Tasks

### Task 1: Add RWMutex to Engine for Thread-Safe Register

**Objective:** Prevent data races when `Register` is called concurrently with `Transition`/`Signal`/etc.

**Dependencies:** None

**Files:**
- Modify: `internal/engine/engine.go`
- Test: `engine_test.go` (add concurrent registration test)

**Key Decisions / Notes:**
- Add `sync.RWMutex` field `mu` to `Engine` struct at `internal/engine/engine.go:115`
- `Register()` takes `e.mu.Lock()` / `e.mu.Unlock()`
- `definitionFor()` takes `e.mu.RLock()` / `e.mu.RUnlock()` **internally** — this automatically protects all callers (Transition, Signal, ForceState, ChildCompleted)
- Keep the existing `atomic.Bool` for shutdown — it's correct for that use case
- Add `e.wg.Add(1)` / `defer e.wg.Done()` to `Signal`, `ForceState`, and `ChildCompleted` — currently only `Transition` tracks in-flight operations via WaitGroup, meaning `Shutdown` can return while Signal/ForceState are still running

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] `go test -race ./...` passes with concurrent Register+Transition test
- [ ] No data race warnings
- [ ] Signal, ForceState, and ChildCompleted are tracked by WaitGroup and return ErrEngineShutdown if called after shutdown

**Verify:**
- `go test ./... -race -count=1`

---

### Task 2: Add OnPostCommitError Hook and Surface Post-Commit Failures

**Objective:** Stop silently swallowing errors from post-commit operations (EventBus.Emit, ActivityRunner.Dispatch, TaskStore.Create, ChildStore.Create). Report them via a new hook method and collect warnings in TransitionResult.

**Dependencies:** None

**Files:**
- Modify: `hooks.go` (add `OnPostCommitError` method to `Hooks` interface and `NoopHooks`)
- Modify: `internal/engine/engine.go` (mirror interface, replace `_ =` with hook calls, collect warnings)
- Modify: `types/result.go` (add `Warnings []string` field to `TransitionResult`)
- Test: `engine_test.go` (test that hook fires on post-commit failures)

**Key Decisions / Notes:**
- New method signature: `OnPostCommitError(ctx context.Context, operation string, err error)` where operation is e.g. "EventBus.Emit", "ActivityRunner.Dispatch"
- Mirror the updated `Hooks` interface in `internal/engine/engine.go:104-112`
- `NoopHooks` gets an empty implementation
- In `Transition()`, replace each `_ = e.deps.X.Method(...)` with: call, check error, if err: call hook + append to warnings slice
- **Also replace `_ = e.deps.EventBus.Emit` in `ForceState` (engine.go ~line 755) with the same hook+warning pattern** — ForceState has identical silent error swallowing
- Use `Warnings []PostCommitWarning` with `PostCommitWarning struct { Operation string; Err error }` in TransitionResult — preserves error types for programmatic inspection via `errors.Is`/`errors.As`
- This is a new interface method — triggers a minor version bump (v0.X.0 → v0.(X+1).0) per CLAUDE.md Section 6B. Update CHANGELOG accordingly.

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] Post-commit error from a failing EventBus triggers OnPostCommitError hook in both Transition and ForceState
- [ ] TransitionResult.Warnings contains structured PostCommitWarning entries

**Verify:**
- `go test ./... -count=1`

---

### Task 3: Fix Optimistic Locking in pgxstore InstanceStore.Update

**Objective:** Add `WHERE updated_at = $old` clause to pgxstore's Update query so it properly implements the optimistic locking contract.

**Dependencies:** None

**Files:**
- Modify: `types/instance.go` (add `LastReadUpdatedAt` field)
- Modify: `internal/engine/engine.go` (set `LastReadUpdatedAt` before modifying `UpdatedAt`)
- Modify: `adapters/pgxstore/instance_store.go` (change UPDATE query to use `LastReadUpdatedAt`)
- Test: `adapters/pgxstore/instance_store_test.go` (uncomment and enable existing optimistic locking test)

**Key Decisions / Notes:**
- **Chosen approach: LastReadUpdatedAt field**
  1. Add `LastReadUpdatedAt time.Time \`json:"-"\`` to `types/instance.go` WorkflowInstance struct
  2. In `internal/engine/engine.go` Transition method, at line 275 (where `oldUpdatedAt := instance.UpdatedAt`), set `instance.LastReadUpdatedAt = instance.UpdatedAt` BEFORE setting `instance.UpdatedAt = now`
  3. Also set `LastReadUpdatedAt` in `ForceState` before modifying `UpdatedAt`
  4. pgxstore `Update` query changes to: `WHERE aggregate_type = $7 AND aggregate_id = $8 AND updated_at = $9` where `$9 = instance.LastReadUpdatedAt`
  5. If `RowsAffected() == 0`, return `flowstate.ErrConcurrentModification`
- Uncomment and enable the existing optimistic locking test block at `instance_store_test.go` (lines ~82-98). Update the stale instance to use the new `LastReadUpdatedAt` field pattern.

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] pgxstore Update returns `ErrConcurrentModification` when row's updated_at doesn't match
- [ ] The previously-commented optimistic locking test section passes

**Verify:**
- `go test ./adapters/pgxstore/... -count=1`

---

### Task 4: Fix Optimistic Locking in sqlitestore and dynamostore InstanceStore.Update

**Objective:** Apply the same optimistic locking pattern from Task 3 to sqlitestore and dynamostore.

**Dependencies:** Task 3 (same pattern)

**Files:**
- Modify: `adapters/sqlitestore/store.go` (add WHERE updated_at clause)
- Modify: `adapters/dynamostore/store.go` (add ConditionExpression)
- Modify: `types/instance.go` (add `LastReadUpdatedAt` field if not done in Task 3)

**Key Decisions / Notes:**
- sqlitestore: Add `AND updated_at = ?` to UPDATE WHERE clause. Change `_, err := sqlTx.ExecContext(...)` to `result, err := sqlTx.ExecContext(...)`. After error check, add: `if n, _ := result.RowsAffected(); n == 0 { return flowstate.ErrConcurrentModification }`. The `updated_at` value comes from `instance.LastReadUpdatedAt` (added in Task 3).
- dynamostore: `Update` currently delegates to `Create` (PutItem upsert) — split into a dedicated Update path. Read `instance.LastReadUpdatedAt` as the old value. Add `ConditionExpression: "UpdatedAt = :oldUpdatedAt"` with `ExpressionAttributeValues` to the PutItem call. On `ConditionalCheckFailedException`, return `flowstate.ErrConcurrentModification`.
- Both need to return `flowstate.ErrConcurrentModification` on conflict

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] sqlitestore and dynamostore Update methods include optimistic locking

**Verify:**
- `go test ./... -count=1`

---

### Task 5: Fix Migration Schema, Sentinel Errors in pgxstore, and Shallow copyMap

**Objective:** Fix the missing `next_retry_at` column in 001 migration, make pgxstore TaskStore.Get return sentinel errors, and make `copyMap` deep.

**Dependencies:** None

**Files:**
- Modify: `adapters/pgxstore/migrations/001_flowstate.sql` (add `next_retry_at` column)
- Modify: `adapters/pgxstore/task_store.go` (return `ErrTaskNotFound` sentinel)
- Modify: `adapters/pgxstore/activity_store.go` (return sentinel error for not-found)
- Modify: `internal/engine/engine.go` (make `copyMap` recursive for nested maps)
- Test: `adapters/pgxstore/task_store_test.go`, `adapters/pgxstore/activity_store_test.go`

**Key Decisions / Notes:**
- Add `next_retry_at TIMESTAMPTZ` to `flowstate_activities` table in 001_flowstate.sql
- pgxstore `TaskStore` needs to accept an `errNotFound` parameter like `InstanceStore` does, and return it instead of `fmt.Errorf("pgxstore: task not found")`
- Add `ErrActivityNotFound = errors.New("flowstate: activity invocation not found")` to `errors.go`. Add it to the sentinel distinctness test in `errors_test.go`. The pgxstore `ActivityStore` constructor should accept an `errNotFound` parameter (same pattern as InstanceStore and TaskStore) and return it from `Get` on `pgx.ErrNoRows`.
- `copyMap`: recursively copy `map[string]any` values. For non-map values, shallow copy is fine (strings, numbers are immutable).

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] `next_retry_at` column exists in migration
- [ ] pgxstore TaskStore.Get wraps the sentinel error

**Verify:**
- `go test ./... -count=1`

---

### Task 6: Fix Mermaid Routed Transitions and memstore Clock Usage

**Objective:** Make Mermaid generator render routed transitions, and fix memstore stores that use `time.Now()` directly instead of the engine's Clock.

**Dependencies:** None

**Files:**
- Modify: `types/mermaid.go` (render routes when `tr.Target == ""`)
- Modify: `adapters/memstore/task_store.go` (remove `time.Now()` calls — use caller-provided time)
- Modify: `adapters/memstore/child_store.go` (remove `time.Now()` calls)
- Test: `types/mermaid_test.go`, `adapters/memstore/event_store_test.go` or `adapters/memstore/instance_store_test.go`

**Key Decisions / Notes:**
- Mermaid: When `tr.Target == ""` and `len(tr.Routes) > 0`, render each route as `src --> route.Target : name`
- memstore TaskStore.Complete (line 65-71): uses `time.Now()` — but the store doesn't have access to Clock. The issue is the store interface doesn't pass a timestamp. Since this is the in-memory store for testing, the pragmatic fix is to accept that the memstore Complete timestamps won't be deterministic. The core correctness isn't affected since the engine uses its own Clock for event timestamps. Document this as a known limitation rather than changing the interface.
- memstore ChildStore.Complete (line 71-74): Same issue. Document rather than change interface.

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] Mermaid output for a workflow with one routed transition includes an edge for each route target, verified by mermaid_test.go
- [ ] A code comment is added to memstore TaskStore.Complete and ChildStore.Complete explaining that CompletedAt uses wall clock (time.Now) because the store interface does not accept a timestamp

**Verify:**
- `go test ./... -count=1`

---

### Task 7: Fix ErrEngineShutdown Missing from Test + Dead Code Cleanup

**Objective:** Add `ErrEngineShutdown` to the sentinel distinctness test, remove dead code block in Transition method, and general cleanup.

**Dependencies:** None

**Files:**
- Modify: `errors_test.go` (add `ErrEngineShutdown` to sentinel list)
- Modify: `internal/engine/engine.go` (remove dead if-block at lines 291-293)
- Test: `errors_test.go`

**Key Decisions / Notes:**
- `errors_test.go:26`: Add `ErrEngineShutdown` to the sentinels slice
- `internal/engine/engine.go:291-293`: Remove the empty if-block:
  ```go
  if previousState == def.InitialState && oldUpdatedAt.Equal(instance.CreatedAt) {
      // This was just created by loadOrCreate — update it
  }
  ```
  This is dead code that does nothing. The `Update` call below it always runs regardless.

**Definition of Done:**
- [ ] All tests pass
- [ ] No diagnostics errors
- [ ] `ErrEngineShutdown` tested for distinctness
- [ ] No dead code blocks remain

**Verify:**
- `go test ./... -count=1`

## Assumptions

- pgxstore, sqlitestore, dynamostore are all pre-v1.0 and have no production deployments — supported by go.mod showing `v0.x` development and CLAUDE.md stating v1.0 is post-Phase 22. All tasks depend on this.
- The `WorkflowInstance` struct can be extended with a new field for optimistic locking without breaking existing serialization — supported by `types/instance.go` using plain struct (no custom marshaling). Tasks 3, 4 depend on this.
- Existing tests cover the happy path thoroughly (89.8% root coverage) — supported by test run. All tasks depend on this for regression safety.

## Testing Strategy

- **Unit tests:** For each fix — guard failure hook, concurrent registration, Mermaid route rendering
- **Integration tests:** The existing integration test suite (`integration_test.go`) validates end-to-end behavior through the full engine
- **Race detection:** `go test -race ./...` for the thread safety fix
- **Manual verification:** `go vet ./...` clean, all tests pass

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Adding `Warnings` to TransitionResult breaks downstream consumers | Low | Medium | Field is additive — existing code that doesn't check it is unaffected |
| Optimistic locking changes break pgxstore tests | Medium | Low | pgxstore tests require FLOWSTATE_TEST_DB env var (real PostgreSQL). In CI without this var, tests are silently skipped. Verify against a local PostgreSQL if available: `FLOWSTATE_TEST_DB=<conn> go test ./adapters/pgxstore/...` |
| Hooks interface change breaks external implementors | Low | High | Add `OnPostCommitError` as a new method with NoopHooks default — compile error is intentional to surface missing implementations |

## Pre-Mortem

*Assume this plan failed. Most likely internal reasons:*

1. **Optimistic locking needs old UpdatedAt but engine doesn't pass it through** (Tasks 3, 4) → Trigger: pgxstore Update can't access the pre-modification `UpdatedAt` because the engine overwrites it before calling Update. If this happens, add a `LastReadUpdatedAt` field to WorkflowInstance that the engine sets before modifying UpdatedAt.
2. **Hooks interface change cascades wider than expected** (Task 2) → Trigger: More than hooks.go and internal/engine need updating — check for any other Hooks implementors in tests or adapters.

## Goal Verification

### Truths
1. `go test -race ./...` passes with zero failures and zero race warnings
2. A post-commit EventBus.Emit failure triggers `OnPostCommitError` hook
3. pgxstore InstanceStore.Update returns `ErrConcurrentModification` when the row was modified since last read
4. Mermaid output for a workflow with routed transitions includes all route targets
5. `ErrEngineShutdown` is tested for distinctness from all other sentinel errors
6. No `_ =` silently swallowing store/bus errors in `internal/engine/engine.go` post-commit section

### Artifacts
- `internal/engine/engine.go` — engine thread safety + error handling
- `hooks.go` — new `OnPostCommitError` method
- `types/result.go` — `Warnings` field
- `adapters/pgxstore/instance_store.go` — optimistic locking
- `adapters/pgxstore/migrations/001_flowstate.sql` — schema fix

### Key Links
- `hooks.go:Hooks` ↔ `internal/engine/engine.go:Hooks` (mirrored interfaces)
- `engine.go:NewEngine` → `internal/engine/engine.go:Deps` (sentinel error passing)
- `types/instance.go:WorkflowInstance` ↔ all store adapters (struct shape)

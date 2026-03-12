# Parallel States Implementation Plan

Created: 2026-03-12
Status: VERIFIED
Approved: Yes
Iterations: 3
Worktree: No
Type: Feature

## Summary

**Goal:** Add orthogonal regions (parallel states) within a single workflow instance, enabling multiple independent state machines to run simultaneously inside one workflow.

**Architecture:** A parallel state is a special compound state whose children are regions (each a compound state). When a workflow enters a parallel state, `CurrentState` is set to the parallel state name and `ActiveInParallel` tracks each region's active leaf. Transitions within parallel context are dispatched by checking all active leaves, resolving conflicts centrally (structural > specificity > priority > declaration order), and committing one atomic `DomainEvent` with delta-based Payload encoding. `Priority` is added as a general transition property (useful beyond parallel conflict resolution).

**Tech Stack:** Go, flowstep's existing Clean Architecture (types/, internal/builder/, internal/graph/, internal/runtime/, adapters/)

## Scope

### In Scope
- `IsParallel bool` on StateDef; `Priority int` on TransitionDef
- `ActiveInParallel map[string]string` on WorkflowInstance
- Nested fluent builder DSL: `ParallelState("name").Region("r1", states...).Region("r2", states...).Done()`
- `Priority(int)` as a general TransitionOption
- Region containment validation (all children of parallel must be compound)
- Graph compilation: populate `RegionIndex` in `CompiledMachine`
- Engine: entering/leaving parallel states (populate/clear `ActiveInParallel`)
- Engine: intra-parallel transition dispatch with conflict resolution
- Atomic parallel transition events via structured `DomainEvent.Payload` keys
- Delta-based model with `RegionSequenceMap` (per-instance `ParallelClock`)
- Entry/exit activities for parallel states and regions
- History recording on parallel state exit
- `ForceState` parallel-awareness (clear `ActiveInParallel`, populate on parallel target)
- Persistence: pgxstore + sqlitestore migration + Go adapter updates for `active_in_parallel` and `parallel_clock` columns (dynamostore marshals full struct — no changes needed)
- Example program, step documentation, CHANGELOG update

### Out of Scope
- Mermaid diagram rendering for parallel states (defer to separate task)
- Cross-parallel-boundary transitions between regions (regions are independent)
- Nested parallel states (parallel within parallel) — validate and reject for now

## Context for Implementer

> Write for an implementer who has never seen the codebase.

- **Patterns to follow:**
  - Functional options: `flowstep.go:27-61` re-exports builder functions
  - Builder pattern: `internal/builder/builder.go:276-396` — `DefBuilder.States()`, `Build()` collects states and wires Parent/Children
  - Sentinel errors: `errors.go:10-60` — all errors defined at root, injected via `graph.Sentinels` struct
  - CompiledMachine: `internal/graph/compiled.go:24-48` — precomputed graph data for O(1) lookups
  - Engine decomposition: `internal/runtime/executor.go` (transition execution), `helpers.go` (pure functions), `dispatcher.go` (post-commit), `scheduler.go` (signal/task/child dispatch)

- **Conventions:**
  - Interfaces in `types/interfaces.go`, pure data in `types/`
  - Validation in `internal/graph/validate.go`, compilation in `internal/graph/compiled.go`
  - Engine logic in `internal/runtime/`, adapters in `adapters/`
  - Tests use internal stubs (`runtime_test.go:13-80`): `noopTx`, `noopEventStore`, `memInstanceStore`, `fixedClock`

- **Key files:**
  - `types/definition.go:52-66` — StateDef struct (add IsParallel here)
  - `types/definition.go:84-100` — TransitionDef struct (add Priority here)
  - `types/instance.go:6-31` — WorkflowInstance struct (add ActiveInParallel, ParallelClock here)
  - `internal/builder/builder.go:218-270` — StateOption, Initial/Terminal/State/WaitState/CompoundState builders
  - `internal/builder/builder.go:322-396` — Build() method that wires Children from Parent
  - `internal/graph/validate.go:28-61` — Validate() dispatcher calls all check functions
  - `internal/graph/validate.go:80-101` — checkCompoundStates (must exempt parallel)
  - `internal/graph/validate.go:233-286` — checkReachability (must handle parallel enqueue)
  - `internal/graph/compiled.go:74-131` — Compile() (must populate RegionIndex)
  - `internal/graph/compiled.go:38-40` — RegionIndex field (already declared, empty)
  - `internal/runtime/executor.go:69-103` — Transition() entry point
  - `internal/runtime/executor.go:132-200` — validateTransition (must handle parallel context)
  - `internal/runtime/executor.go:204-379` — commitTransition (must handle parallel enter/exit)
  - `internal/runtime/helpers.go:53-85` — computeExitSequence/computeEntrySequence
  - `internal/runtime/helpers.go:137-171` — recordHistory
  - `flowstep.go:27-61` — re-exported builder functions (add ParallelState, Priority)
  - `errors.go` — sentinel errors (add parallel-specific errors)

- **Gotchas:**
  - `StateOption` is a value type wrapping `types.StateDef` (`builder.go:218-226`). The nested builder must extend it with a `nested []StateOption` field to carry region children through `States()` and `Build()`.
  - `Build()` wires Children from Parent (`builder.go:343-356`). For parallel states, the nested builder sets Parent on region states, so Build()'s existing wiring works. But the nested states must be flattened from `StateOption.nested` first.
  - `checkCompoundStates` (`validate.go:82`) requires InitialChild for compound states. Parallel states are compound but do NOT have InitialChild (they activate all children). Must exempt `IsParallel` states from this check.
  - `checkReachability` (`validate.go:233`) enqueues InitialChild for compound states. For parallel states, must enqueue ALL children (regions).
  - `computeHash` (`compiled.go:208-258`) includes all StateDef fields. Must add `IsParallel` and `Priority` to the hash.
  - `CurrentState` is a single string. In parallel mode it holds the parallel state name, with `ActiveInParallel` holding the per-region active leaves. All transition matching must check ActiveInParallel values when non-empty.
  - memstore stores `WorkflowInstance` as a value copy — new fields are automatically persisted. No Go code changes needed for memstore.
  - `ForceState` (`executor.go:427-526`) clears history maps on force. Must also clear `ActiveInParallel` and handle parallel targets.

- **Domain context:**
  - Parallel states = orthogonal regions within ONE workflow instance. NOT the same as child workflows (separate instances via SpawnChildren).
  - Each region runs independently: events are dispatched to all active leaves, each region may or may not transition.
  - One atomic DomainEvent per dispatch — never separate events per region. Preserves deterministic replay.
  - Priority is a deterministic tie-breaker for conflict resolution, NOT a processing order. Structural rules always take precedence.

## Design Decisions

### D1: Builder DSL — Nested Fluent Builder
Regions are first-class structural concepts, not ordinary children with Parent() links. The DSL reflects this explicitly:
```go
flowstep.ParallelState("editing").
    Region("bold_region",
        flowstep.State("bold_off"),
        flowstep.State("bold_on"),
    ).
    Region("italic_region",
        flowstep.State("italic_off"),
        flowstep.State("italic_on"),
    ).
    Done()
```
The first state in each Region() call is the region's InitialChild (convention, like Initial() for the workflow).

### D2: Engine API — Extend Existing Transition()
No new public API method. `Transition()` auto-detects parallel context via `len(instance.ActiveInParallel) > 0` and dispatches accordingly. Callers don't need to know about parallel internals.

**`Signal()`, `CompleteTask()`, and `ChildCompleted()` require parallel-aware updates in `scheduler.go`.** These methods preselect candidate transitions by checking `slices.Contains(tr.Sources, instance.CurrentState)`. In parallel mode `CurrentState` = the parallel parent, so trigger-based transitions sourced from active leaf states will not match. Each must be updated to also check `ActiveInParallel` values (and their ancestors) when `len(instance.ActiveInParallel) > 0`. This is addressed in Task 6.

### D3: Priority — General Transition Property
`Priority int` on TransitionDef, exposed via `Priority(int)` TransitionOption. Default is 0. Structural resolution rules take precedence; priority is a deterministic tie-breaker. Higher priority wins. Within same priority, alphabetical sort by transition name as the final deterministic fallback. Go maps have no iteration order guarantee — implementers must use sorted transition names, not map iteration order.

### D4: CurrentState = Parallel State Name
When entering a parallel state:
- `CurrentState` = parallel state name (e.g., "editing")
- `ActiveInParallel` = `{"bold_region": "bold_off", "italic_region": "italic_off"}`

When leaving:
- `ActiveInParallel` = cleared (empty map)
- `CurrentState` = destination leaf state

### D5: Event Encoding — Structured Payload Keys
Parallel transition data encoded in `DomainEvent.Payload` using reserved underscore-prefixed keys:
```go
Payload: map[string]any{
    "_parallel_regions": map[string]any{
        "bold_region": map[string]any{"from": "bold_off", "to": "bold_on"},
    },
    "_region_sequence_map": map[string]any{
        "bold_region":   int64(5),  // this event's parallel clock
        "italic_region": int64(3),  // last event that changed italic
    },
    // User params also present
}
```
EventType is `"ParallelTransition"` for intra-parallel events.

### D6: ParallelClock — Per-Instance Logical Counter
`WorkflowInstance.ParallelClock int64` — increments on each parallel transition. `RegionSequenceMap` maps region name to the `ParallelClock` value when that region last changed. Self-contained, no dependency on EventStore sequencing.

### D7: Conflict Resolution Order
**Scope:** `Transition()` targets a single transition by name. Conflict resolution applies when that single named transition matches in multiple regions (same transition name sourced from states in different regions). For `Signal()`/`CompleteTask()`/`ChildCompleted()`, which discover candidate transitions by trigger type, resolution applies when multiple trigger-matched transitions exist across active leaves.

Resolution order:
1. **Structural rules** — transitions within different regions never conflict (they're independent). A transition that exits the parallel state entirely takes precedence over intra-region transitions.
2. **Specificity** — a transition sourced from a leaf state is more specific than one sourced from an ancestor compound state.
3. **Priority** — higher `Priority` value wins as a deterministic tie-breaker.
4. **Name sort** — alphabetical sort by transition name as final deterministic fallback.

## Assumptions

- Hierarchical states (Task 3) are fully implemented and stable — supported by: `types/definition.go:59-65` has Parent/Children/InitialChild/IsCompound fields, `internal/graph/compiled.go:74-131` computes ancestry/depth/LCA. Tasks 1-8 all depend on this.
- `RegionIndex` field already exists on `CompiledMachine` but is unpopulated — supported by: `compiled.go:38-40`. Task 4 depends on this.
- memstore stores `WorkflowInstance` as a value copy, so new fields are automatically persisted — supported by: `adapters/memstore/instance_store.go:43-44`. Task 7 depends on this (only pgxstore needs migration).
- Go map iteration order is non-deterministic — supported by: Go spec. Conflict resolution must NOT depend on map iteration order. Tasks 6 depends on this.
- `IsParallel` states are a subset of compound states (`IsParallel` implies `IsCompound`) — supported by: parallel states have children (regions). Tasks 3, 4, 5 depend on this.
- Nested parallel states (parallel within parallel) are not needed in the initial implementation — user has not requested this. Task 3 (validation) rejects them.
- Entry/exit activities are idempotent — supported by: documented in parent plan `2026-03-10-statekit-feature-comparison.md:257`. Task 5 depends on this.

## Testing Strategy

- **Unit tests** for each task: builder output verification, validation accept/reject, compilation correctness, engine state changes
- **Integration tests** in `internal/runtime/executor_test.go`: full parallel workflow scenarios using in-memory stubs
- **Existing tests must remain green** — all changes are additive; flat/hierarchical workflows unaffected
- **Table-driven tests** for conflict resolution scenarios (multiple priority/specificity combinations)
- **Coverage target:** 80%+ for new code

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Parallel enter/exit breaks existing hierarchical workflows | Medium | Critical | IsParallel-specific code paths only activate when IsParallel=true. All existing hierarchy tests must pass unchanged. |
| Conflict resolution produces non-deterministic results | Medium | Critical | Resolution uses 4-tier deterministic ordering (structural > specificity > priority > name sort). No map iteration dependency. Table-driven test suite for all conflict scenarios. |
| `checkCompoundStates` validation breaks parallel states | High | Medium | Exempt IsParallel from InitialChild requirement in the check function. Test both compound and parallel validation. |
| StateOption.nested changes break existing builder usage | Low | High | `nested` field defaults to nil. Existing StateOption creation (Initial, State, Terminal, etc.) is unchanged. Only ParallelState().Done() populates nested. |
| ActiveInParallel map aliasing in memstore | Medium | Medium | Use copy-on-write pattern (same as ShallowHistory/DeepHistory in `helpers.go:155-161`). |
| Parallel transition Payload keys collide with user params | Low | Medium | Use underscore-prefixed reserved keys (`_parallel_regions`, `_region_sequence_map`). Document that `_`-prefixed payload keys are reserved by flowstep. |
| pgxstore adapter silently drops parallel state data | High | Critical | pgxstore Go adapter update is included in Task 7 (migration + Go code are tightly coupled). Without Go changes, ActiveInParallel is lost on save/reload, causing silent state corruption. |

## Pre-Mortem

*Assume this plan failed after full execution. Most likely internal reasons:*

1. **Exit sequence computation incomplete for parallel states** (Task 5) — The existing `computeExitSequence` walks from source leaf to LCA. When exiting a parallel state, ALL active region leaves must exit, not just the triggering region's leaf. If we miss this, only one region exits and the others silently retain their state. Trigger: test "exit parallel state" shows leftover entries in `ActiveInParallel` after the workflow leaves the parallel state.

2. **Transition matching finds zero candidates in parallel context** (Task 6) — When `CurrentState` = parallel state name, the existing `validateTransition` checks `slices.Contains(tr.Sources, instance.CurrentState)`. If the transition sources reference leaf states (not the parallel state), no match is found. The parallel dispatch path must check `ActiveInParallel` values AND their ancestors, not `CurrentState`. Trigger: `Transition()` returns `ErrInvalidTransition` for a transition that should match an active leaf.

3. **Builder flattening loses Parent wiring** (Task 2) — `Build()` currently wires Children from Parent references (`builder.go:343-356`). If `Done()` sets Parent on nested states but `Build()` processes them before the Parent wiring loop, or if nested states aren't inserted into `def.States`, the hierarchy is broken. Trigger: validation error "state X has Parent Y which does not exist" for a state inside a region.

## Goal Verification

### Truths
1. A workflow definition with parallel states builds successfully via the nested fluent builder DSL
2. Entering a parallel state sets `CurrentState` to the parallel state name and populates `ActiveInParallel` with each region's initial leaf
3. Transitions within a parallel region update only the affected region in `ActiveInParallel`
4. Each parallel dispatch produces exactly one `DomainEvent` with delta-based Payload encoding
5. Conflict resolution between regions is deterministic (structural > specificity > priority > name)
6. Exiting a parallel state clears `ActiveInParallel` and records history for the parallel state
7. Existing flat and hierarchical workflows are unaffected (all prior tests pass)

### Artifacts
- `types/definition.go` — StateDef.IsParallel, TransitionDef.Priority
- `types/instance.go` — WorkflowInstance.ActiveInParallel, ParallelClock
- `internal/builder/builder.go` — ParallelStateBuilder, Region(), Done(), Priority()
- `internal/graph/validate.go` — checkParallelStates, updated checkCompoundStates, checkReachability
- `internal/graph/compiled.go` — RegionIndex population
- `internal/runtime/executor.go` — parallel-aware Transition/commitTransition
- `internal/runtime/parallel.go` — conflict resolution, candidate collection
- `errors.go` — parallel sentinel errors
- `adapters/pgxstore/migrations/003_parallel_states.sql`
- `examples/07-parallel-states/main.go`

### Key Links
- `types/definition.go` StateDef.IsParallel <-> validation (checkParallelStates), compilation (RegionIndex), engine (parallel detection)
- `types/instance.go` ActiveInParallel <-> engine enter/exit, transition dispatch, persistence
- `internal/builder/builder.go` StateOption.nested <-> Build() flattening, ParallelStateBuilder
- `internal/graph/compiled.go` RegionIndex <-> engine parallel dispatch (region iteration)
- `internal/runtime/executor.go` commitTransition <-> parallel enter/exit, ActiveInParallel mutation
- `internal/runtime/parallel.go` conflict resolution <-> executor validateTransition, DomainEvent Payload

## Progress Tracking

- [x] Task 1: Type Extensions + Priority TransitionOption
- [x] Task 2: ParallelState Fluent Builder
- [x] Task 3: Validation — Region Containment + Parallel Rules
- [x] Task 4: Graph Compilation — RegionIndex
- [x] Task 5: Engine — Entering and Leaving Parallel States
- [x] Task 6: Engine — Intra-Parallel Transition Dispatch
- [x] Task 7: Persistence Migration + Example + Documentation

**Total Tasks:** 7 | **Completed:** 7 | **Remaining:** 0

## Implementation Tasks

### Task 1: Type Extensions + Priority TransitionOption

**Objective:** Add `IsParallel` to StateDef, `Priority` to TransitionDef, `ActiveInParallel`/`ParallelClock` to WorkflowInstance, `Priority()` builder option, and update `computeHash`.

**Dependencies:** None

**Files:**
- Modify: `types/definition.go` — add `IsParallel bool` to StateDef (after IsCompound:63), add `Priority int` to TransitionDef (after HistoryMode:99)
- Modify: `types/instance.go` — add `ActiveInParallel map[string]string` and `ParallelClock int64` to WorkflowInstance (after DeepHistory:26)
- Modify: `internal/builder/builder.go` — add `priority int` to transitionBuilder:10, add `Priority(int) TransitionOption` function, wire priority in Build():369-384
- Modify: `internal/graph/compiled.go` — update `computeHash` to include `st.IsParallel` in state hash line:225 and `tr.Priority` in transition hash line:236
- Modify: `flowstep.go` — re-export `Priority = builder.Priority`

**Key Decisions / Notes:**
- `IsParallel` implies `IsCompound` — the builder will set both. But `IsParallel` is the new flag; `IsCompound` is set by the existing Children-wiring logic in Build().
- `Priority` default is 0. Higher value = higher priority. It's a general TransitionDef property, not parallel-specific.
- `ActiveInParallel` and `ParallelClock` are initialized to nil/zero — populated only when entering a parallel state.
- Follow existing `computeHash` pattern: deterministic formatting, sorted map keys.

**Definition of Done:**
- [ ] `StateDef` has `IsParallel bool` field
- [ ] `TransitionDef` has `Priority int` field
- [ ] `WorkflowInstance` has `ActiveInParallel map[string]string` and `ParallelClock int64`
- [ ] `Priority(5)` builder option sets priority on transition — verified by calling `Build()`, reading the resulting `TransitionDef` from `Definition.Transitions`, and asserting `Priority == 5`
- [ ] `computeHash` includes IsParallel and Priority — two definitions differing only in these fields produce different hashes
- [ ] All existing tests pass

**Verify:**
```bash
go test ./types/... ./internal/builder/... ./internal/graph/... -count=1
```

---

### Task 2: ParallelState Fluent Builder

**Objective:** Create the nested fluent builder DSL for parallel states: `ParallelState("name").Region("r", states...).Done()`.

**Dependencies:** Task 1

**Files:**
- Modify: `internal/builder/builder.go` — add `nested []StateOption` field to StateOption:219, add `ParallelStateBuilder` struct, `ParallelState()` function, `Region()` method, `Done()` method. Update `Build()`:323 to flatten nested StateOptions before processing.
- Modify: `flowstep.go` — re-export `ParallelState = builder.ParallelState`

**Key Decisions / Notes:**
- `StateOption` gets a `nested []StateOption` field (nil for all existing state types — backward-compatible).
- `ParallelStateBuilder` holds the parallel state name and accumulated regions.
- `Region(name string, states ...StateOption)` creates a compound state for the region with `Parent = parallel_state_name`, `InitialChild = first_state.Name`, `IsCompound = true`. Sets `Parent = region_name` on each state that has no Parent set.
- `Done()` returns a single `StateOption` with `def = parallel StateDef` and `nested = all region + child StateOptions`.
- **Build() flatten algorithm:** Before the state collection loop (`builder.go:332-341`), recursively expand `b.states` into a flat `[]StateOption`. For each `StateOption` where `nested != nil`: emit the StateOption itself (the parallel/region state), then recursively flatten each entry in `nested`. This ensures ALL states from Region() calls appear in `def.States` before the Parent→Children wiring loop processes them. Without this step, region and leaf states never enter `def.States` and the Parent wiring silently skips them.
- The parallel StateDef has `IsParallel = true`. `IsCompound` will be auto-set by Build()'s wiring loop when children reference it as Parent.
- **Orphan-parent check:** `checkOrphanedChildren` in `validate.go:64-78` already verifies that every state with a Parent references an existing state. This catches any flattening bugs where a child references a parent that wasn't inserted into `def.States`.

**Definition of Done:**
- [ ] `ParallelState("editing").Region("bold", State("off"), State("on")).Region("italic", State("off2"), State("on2")).Done()` produces correct StateDef entries
- [ ] All states defined inside Region() calls appear in `def.States` after `Build()` (flatten verification)
- [ ] Parallel state has `IsParallel=true`, `IsCompound=true`
- [ ] Region states have `IsCompound=true`, `Parent=parallel_name`, `InitialChild=first_child`
- [ ] Leaf states have `Parent=region_name`
- [ ] Priority on transitions works in parallel workflow definition
- [ ] All existing tests pass

**Verify:**
```bash
go test ./internal/builder/... -count=1 -run TestParallel
```

---

### Task 3: Validation — Region Containment + Parallel Rules

**Objective:** Add validation rules ensuring parallel state children are all compound (regions), preventing nested parallels, and updating reachability for parallel states.

**Dependencies:** Task 1, Task 2

**Files:**
- Modify: `errors.go` — add `ErrParallelStateNoRegions`, `ErrParallelRegionNotCompound`, `ErrNestedParallelState`
- Modify: `internal/graph/validate.go` — add `checkParallelStates()` function, update `checkCompoundStates()` to exempt IsParallel from InitialChild requirement, update `checkReachability()` to enqueue all children for parallel states, add new sentinel fields to `Sentinels` struct:147-162, add `checkParallelStates` call to `Validate()`:30-61
- Modify: `flowstep.go` — pass new sentinels in Define():66-79
- Modify: `engine.go` — pass new sentinels in NewEngine() Sentinels block:63-73 (Engine.Register() validates through this sentinel set, not the one in flowstep.go)

**Key Decisions / Notes:**
- `checkParallelStates`: for each `IsParallel` state, verify (a) it has children (len > 0), (b) all children are compound states (regions), (c) no child is itself IsParallel (no nested parallel).
- `checkCompoundStates`: change `if !st.IsCompound` to `if !st.IsCompound || st.IsParallel` to skip InitialChild check for parallel states. Parallel states don't have InitialChild — they activate all children.
- `checkReachability.enqueue`: add `if st.IsParallel { for _, child := range st.Children { enqueue(child) } }` before the existing `if st.IsCompound && st.InitialChild != ""` branch.
- `checkDeadEnds`: parallel states are compound → already excluded by `if st.IsCompound { continue }`. No changes needed.

**Definition of Done:**
- [ ] Parallel state with no regions → `ErrParallelStateNoRegions`
- [ ] Parallel state with non-compound child → `ErrParallelRegionNotCompound`
- [ ] Nested parallel (parallel child of parallel) → `ErrNestedParallelState`
- [ ] Valid parallel state passes validation
- [ ] Reachability check reaches all states inside parallel regions
- [ ] All existing tests pass

**Verify:**
```bash
go test ./internal/graph/... -count=1 -run TestParallel
```

---

### Task 4: Graph Compilation — RegionIndex

**Objective:** Populate `CompiledMachine.RegionIndex` during compilation so the engine can efficiently discover regions of a parallel state at runtime.

**Dependencies:** Task 1, Task 2, Task 3

**Files:**
- Modify: `internal/graph/compiled.go` — add RegionIndex population in `Compile()`:77-131 (after ancestry computation, before hash). Also handle parallel states in InitialLeafMap: parallel states should NOT be in InitialLeafMap (they don't resolve to a single leaf).

**Key Decisions / Notes:**
- After the ancestry loop (`compiled.go:89-99`), iterate states: for each `IsParallel` state, set `cm.RegionIndex[name] = st.Children` (the children are the region names).
- InitialLeafMap: the existing loop (`compiled.go:103-107`) processes `st.IsCompound && st.InitialChild != ""`. Parallel states have no InitialChild, so they are naturally excluded. No change needed, but verify with a test.
- Each region (a compound state) IS in InitialLeafMap (it has InitialChild). The engine will use this to resolve each region's initial leaf.

**Definition of Done:**
- [ ] `CompiledMachine.RegionIndex["editing"]` = `["bold_region", "italic_region"]` for a parallel state "editing"
- [ ] Each region has an entry in `InitialLeafMap` mapping to its initial leaf
- [ ] Parallel state itself is NOT in `InitialLeafMap`
- [ ] `LCA()` works correctly for states within parallel regions
- [ ] All existing tests pass

**Verify:**
```bash
go test ./internal/graph/... -count=1 -run TestCompile
```

---

### Task 5: Engine — Entering and Leaving Parallel States

**Objective:** Handle parallel state entry (populate `ActiveInParallel`, run entry activities for all regions) and exit (clear `ActiveInParallel`, run exit activities, record history).

**Dependencies:** Task 1, Task 4

**Files:**
- Modify: `internal/runtime/executor.go` — update `commitTransition()`:204-379 to detect when target is a parallel state (check `cm.RegionIndex`) and handle entry; detect when source is inside a parallel state and target is outside, handle exit. Update `createInstance()`:382-423 for rare case of initial state inside parallel. Update `ForceState()`:427-526 to clear/populate `ActiveInParallel`.
- Modify: `internal/runtime/helpers.go` — add `enterParallelState()` helper (populates ActiveInParallel from RegionIndex + InitialLeafMap), add `exitParallelState()` helper (computes full exit sequence across all regions), add `computeParallelExitSequence()`.
- Test: `internal/runtime/executor_test.go` — new test cases for parallel enter/exit

**Key Decisions / Notes:**
- **Entering parallel:** After resolving target state, if target is in `cm.RegionIndex` (i.e., IsParallel), set `instance.CurrentState = target`, populate `instance.ActiveInParallel` with region→InitialLeaf for each region in RegionIndex. Run entry activities: parallel state entry → each region entry → each region's initial leaf entry.
- **Exiting parallel:** When transitioning FROM a state inside a parallel hierarchy TO a state outside it, all regions must exit. The exit sequence = for each active leaf in ActiveInParallel: compute exit from leaf up to (but not including) the parallel state, then add the parallel state itself. Clear `ActiveInParallel`. Record history: the parallel state's ShallowHistory and DeepHistory should capture each region's last active state.
- **ForceState:** Clear `ActiveInParallel` (reset parallel context). If target is a parallel state, populate `ActiveInParallel` with initial leaves.
- The existing `resolvedTarget` logic in commitTransition (history-aware target resolution) should skip for parallel targets — parallel states don't resolve to a single leaf.
- Use copy-on-write for `ActiveInParallel` (same pattern as `recordHistory` in helpers.go:155-161).

**Definition of Done:**
- [ ] Transition INTO a parallel state sets `CurrentState` to parallel state name
- [ ] `ActiveInParallel` is populated with each region's initial leaf
- [ ] Entry activities fire for parallel state, each region, and each initial leaf (in order)
- [ ] Transition OUT of a parallel state clears `ActiveInParallel`
- [ ] Exit activities fire for all active leaves and regions (leaf→region→parallel order)
- [ ] History is recorded for each **region** (compound state) on exit — not the parallel state itself. Each region gets its own ShallowHistory/DeepHistory entry via the existing `recordHistory` helper (regions appear in the exit sequence as compound states). On re-entry with `WithHistory`, each region restores from its own history entry instead of using InitialChild.
- [ ] ForceState clears ActiveInParallel; ForceState to parallel target populates it
- [ ] All existing tests pass

**Verify:**
```bash
go test ./internal/runtime/... -count=1 -run TestParallel
```

---

### Task 6: Engine — Intra-Parallel Transition Dispatch

**Objective:** When the workflow is inside a parallel state, dispatch transitions by checking all active leaves, resolving conflicts, and committing an atomic event with delta-based Payload.

**Dependencies:** Task 5

**Files:**
- Create: `internal/runtime/parallel.go` — `collectParallelCandidates()`, `resolveParallelConflicts()`, `buildParallelPayload()`
- Modify: `internal/runtime/executor.go` — update `Transition()`:69-103 to detect parallel context and route to `parallelTransition()`. Extract `runGuardsForTransition()` and `resolveRouteForTransition()` as standalone helpers from `validateTransition()`. Add `parallelTransition()` method that orchestrates: candidate collection → conflict resolution → per-region exit/entry → atomic commit.
- Modify: `internal/runtime/scheduler.go` — update `Signal()`, `CompleteTask()`, `ChildCompleted()` to check `ActiveInParallel` values (and their ancestors) when `len(instance.ActiveInParallel) > 0`, not just `instance.CurrentState`
- Modify: `internal/runtime/dispatcher.go` — update `runPostCommit()` to include parallel Payload in TransitionResult if applicable
- Test: `internal/runtime/executor_test.go` — parallel transition tests (single-region, multi-region, conflict resolution, exit-from-parallel, signal-in-parallel)

**Key Decisions / Notes:**
- **Parallel detection:** In `Transition()`, after loading instance, check `len(instance.ActiveInParallel) > 0`. If true, route to `parallelTransition()` instead of normal `validateTransition→commitTransition` path.
- **Architecture: `parallelTransition()` is a peer code path to `validateTransition()`, NOT a consumer of it.** `validateTransition()` returns `(*CompiledTransition, string, error)` — a single transition result. Parallel dispatch produces multiple candidates (one per region). To avoid duplicating guard/route logic, extract `runGuardsForTransition(ctx, cm, ct, instance, params) error` and `resolveRouteForTransition(ctx, tr, instance, params) (string, error)` as standalone helpers from the existing `validateTransition` body. `collectParallelCandidates()` calls these helpers per candidate. `validateTransition()` is refactored to also call these helpers (no behavior change for non-parallel path).
- **Candidate collection (`collectParallelCandidates`):**
  1. Get all active leaf states from `instance.ActiveInParallel` values
  2. For each leaf, find matching transitions by name (including ancestor bubbling UP TO the parallel state boundary — do not bubble past the parallel state)
  3. Also check the parallel state itself for matching transitions (for transitions that exit the parallel state)
  4. Return: `[]parallelCandidate{region, leafState, compiledTransition, specificity}`
- **Conflict resolution (`resolveParallelConflicts`):**
  1. If any candidate exits the parallel state (target outside parallel hierarchy), it takes precedence → exit parallel
  2. Group remaining candidates by region
  3. Within each region, pick the most specific match (leaf > compound ancestor)
  4. Between regions with conflicting targets (targeting same state), use Priority then name sort
  5. Result: set of non-conflicting transitions, one per affected region
- **Atomic commit:**
  1. For each affected region: compute exit/entry sequences within the region
  2. Run exit activities → entry activities
  3. Update `ActiveInParallel` with new leaf states (copy-on-write)
  4. Increment `ParallelClock`
  5. Build `DomainEvent` with `EventType = "ParallelTransition"` and structured Payload:
     - `_parallel_regions`: delta map (only changed regions)
     - `_region_sequence_map`: full map (all regions → their last ParallelClock value)
  6. Persist event + updated instance in one transaction
- **Transition NOT matching any active leaf:** Return `ErrInvalidTransition` (same as non-parallel behavior).

**Definition of Done:**
- [ ] Transition within one region updates only that region in `ActiveInParallel`
- [ ] Transition matching multiple regions updates all matched regions atomically
- [ ] Conflict resolution follows structural > specificity > priority > name order
- [ ] Each parallel dispatch produces exactly one DomainEvent with `EventType = "ParallelTransition"`
- [ ] Payload contains `_parallel_regions` (delta) and `_region_sequence_map` (full)
- [ ] `ParallelClock` increments on each parallel transition
- [ ] Transition that exits the parallel state entirely works (exits all regions, clears parallel context)
- [ ] Guards are evaluated per-candidate before conflict resolution
- [ ] Post-commit hooks (observers, event bus) fire with the atomic event
- [ ] All existing tests pass

**Verify:**
```bash
go test ./internal/runtime/... -count=1 -run TestParallel
```

---

### Task 7: Persistence Migration + Example + Documentation

**Objective:** Add pgxstore migration for parallel state fields, create an example program, write step documentation, and update CHANGELOG.

**Dependencies:** Task 6

**Files:**
- Create: `adapters/pgxstore/migrations/003_parallel_states.sql`
- Modify: `adapters/pgxstore/instance_store.go` — add `active_in_parallel` and `parallel_clock` to INSERT/UPDATE/SELECT queries and scan targets
- Create: `adapters/sqlitestore/migrations/003_parallel_states.sql`
- Modify: `adapters/sqlitestore/store.go` — add `active_in_parallel` and `parallel_clock` to INSERT/UPDATE/SELECT queries and scan targets (same pattern as pgxstore)
- Create: `examples/07-parallel-states/main.go`
- Create: `docs/steps/impl/2026-03-12-08-parallel-states.md`
- Modify: `CHANGELOG.md`

**Key Decisions / Notes:**
- **Migration:** `ALTER TABLE flowstep_instances ADD COLUMN IF NOT EXISTS active_in_parallel JSONB DEFAULT '{}'; ALTER TABLE flowstep_instances ADD COLUMN IF NOT EXISTS parallel_clock BIGINT DEFAULT 0;`
- **pgxstore Go adapter:** The migration and Go code are tightly coupled — without the Go changes, the adapter silently drops `ActiveInParallel` on save and returns empty on load, causing silent parallel state data loss. Read the existing `instance_store.go` to find the INSERT, UPDATE, and SELECT queries. Add `active_in_parallel` and `parallel_clock` columns to each. Use `json.Marshal`/`json.Unmarshal` for the JSONB `ActiveInParallel` field (same pattern as `state_data`).
- **Example:** Text editor with bold/italic parallel regions. Define workflow, run transitions (enter editing → toggle bold → toggle italic → exit editing), print state at each step.
- **Step doc:** Record what was implemented, files modified, design decisions, deviations.
- **CHANGELOG:** Add entry under next version for parallel states feature.

**Definition of Done:**
- [ ] SQL migration file creates `active_in_parallel` and `parallel_clock` columns
- [ ] pgxstore `instance_store.go` reads/writes `ActiveInParallel` and `ParallelClock`
- [ ] sqlitestore migration + `store.go` reads/writes `ActiveInParallel` and `ParallelClock`
- [ ] Example program compiles and runs: `go run ./examples/07-parallel-states/`
- [ ] Step documentation in `docs/steps/impl/`
- [ ] CHANGELOG updated
- [ ] All tests pass: `go test ./... -count=1`

**Verify:**
```bash
go build ./examples/07-parallel-states/ && go test ./... -count=1
```

## Deferred Ideas

- **Mermaid rendering for parallel states** — use `--` notation for regions in stateDiagram-v2. Requires changes to `types/mermaid.go`.
- **Nested parallel states** (parallel within parallel) — validate and reject for now. Could be added later if use cases emerge.
- **Cross-region transitions** — a transition that moves state from one region to another within the same parallel state. Complex semantics, unclear use case.
- **Parallel state join condition** — auto-transition when all regions reach terminal states (similar to child workflow JoinPolicy). Could be useful but adds complexity.

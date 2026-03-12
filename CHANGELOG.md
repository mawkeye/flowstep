# Changelog

All notable changes to this project will be documented in this file.
Format based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

## [v0.11.0] - 2026-03-12

### Added
- **Parallel states (orthogonal regions)** — multiple independent state machines running simultaneously inside a single workflow instance. When a workflow enters a parallel state, `CurrentState` is set to the parallel state name and each region tracks its own active leaf via `ActiveInParallel`. (Tasks 1-7: Parallel States)
- `StateDef.IsParallel bool` — marks a state as a parallel (orthogonal) container
- `TransitionDef.Priority int` — general transition priority; higher value wins in parallel conflict resolution and can be used in any workflow
- `WorkflowInstance.ActiveInParallel map[string]string` — maps region name → current active leaf within that region
- `WorkflowInstance.ParallelClock int64` — monotonically incrementing per-instance counter for ordering parallel transitions
- Builder DSL: `ParallelState(name).Region(regionName, states...).Done()` nested fluent builder; first state in each `Region()` call becomes the region's initial child
- `Priority(int)` `TransitionOption` re-exported from `flowstep` root package
- Parallel validation: `ErrParallelStateNoRegions`, `ErrParallelRegionNotCompound`, `ErrNestedParallelState` sentinel errors; `checkParallelStates()` validates region structure and rejects nested parallel states (order-independent detection)
- `CompiledMachine.RegionIndex map[string][]string` — maps parallel state name → region child names for O(1) lookup at runtime
- Intra-parallel dispatch: `parallelTransition()` routes transitions within active regions, runs per-region exit/entry activities, and commits one atomic `DomainEvent` with `EventType="ParallelTransition"`
- Delta-based event payload: `_parallel_regions` (changed regions only) and `_region_sequence_map` (all regions with last `ParallelClock` value)
- Parallel-exit support: transition sourced from the parallel state name exits all active regions atomically and clears `ActiveInParallel`
- Scheduler parallel-awareness: `Signal()`, `CompleteTask()`, `ChildCompleted()` check active leaf states via `activeStates()` helper when matching trigger transitions
- `pgxstore` migration `003_parallel_states.sql` — adds `active_in_parallel` JSONB and `parallel_clock` BIGINT columns
- `sqlitestore` migration `003_parallel_states.sql` — adds `active_in_parallel` TEXT and `parallel_clock` INTEGER columns
- Example `07-parallel-states` — text editor with independent bold/italic parallel regions

### Changed
- `computeHash()` includes `StateDef.IsParallel` and `TransitionDef.Priority` for dedup correctness
- `checkCompoundStates()` exempts parallel states from `InitialChild` requirement (parallel states activate all children simultaneously)
- `checkReachability()` enqueues all children of parallel states (not just `InitialChild`)
- `InitialLeafMap` excludes parallel states (they have no single initial leaf)
- `Transition()` routes to `parallelTransition()` when `ActiveInParallel` is non-empty
- `commitTransition()` detects parallel targets and populates `ActiveInParallel`; detects non-parallel targets and clears `ActiveInParallel`
- `ForceState()` clears `ActiveInParallel`; repopulates it when target is a parallel state
- `pgxstore` Create/Get/Update/ListStuck handle `active_in_parallel` and `parallel_clock` columns
- `sqlitestore` Create/Get/Update/ListStuck handle `active_in_parallel` and `parallel_clock` columns

## [v0.10.0] - 2026-03-11

### Added
- **History states** — shallow and deep history for resuming previous state within compound states. When re-entering a compound state via a history-aware transition, the engine resolves to the last-visited child (shallow `[H]`) or leaf (deep `[H*]`) instead of the default `InitialChild`. (Task 4: History States)
- `HistoryMode` type in `types/` with `HistoryShallow` and `HistoryDeep` constants
- `WithHistory(mode HistoryMode)` transition option in builder DSL
- `WorkflowInstance.ShallowHistory` and `DeepHistory` map fields for persisting history across transitions
- Auto-recording of history on every compound state exit — no opt-in required
- Fallback to `InitialChild` when no history is recorded (backward-compatible)
- `ForceState()` clears both history maps (admin recovery resets from scratch)
- Mermaid history annotation: `[H]` suffix for shallow, `[H*]` for deep history transitions
- `pgxstore` migration `002_history_states.sql` — adds `shallow_history` and `deep_history` JSONB columns
- `sqlitestore` migration `002_history_states.sql` — adds `shallow_history` and `deep_history` TEXT columns
- Root package exports: `HistoryMode`, `HistoryShallow`, `HistoryDeep`, `WithHistory`

### Changed
- `computeHash()` includes `TransitionDef.HistoryMode` for dedup correctness
- `pgxstore` Create/Get/Update/ListStuck handle history columns
- `sqlitestore` Create/Get/Update/ListStuck handle history columns
- `dynamostore` requires no changes — `attributevalue.MarshalMap` handles new struct fields automatically

## [v0.9.0] - 2026-03-11

### Added
- **Hierarchical/nested states** — compound states with parent-child containment, entry/exit activities, event bubbling, and LCA-based transition resolution. Backward-compatible with flat workflows. (Tasks 1-10: Hierarchical States)
- `StateDef` hierarchy fields: `Parent`, `Children`, `InitialChild`, `IsCompound`, `EntryActivity`, `ExitActivity`
- `ActivityInput` causation metadata: `Transition`, `SourceState`, `EventID` fields for entry/exit activity context
- Builder DSL: `CompoundState()` helper, `Parent()`, `InitialChild()`, `EntryActivityOpt()`, `ExitActivityOpt()` state modifiers. All state factories (`State`, `WaitState`, `Terminal`) accept `...StateModifier` variadic options.
- `SavepointProvider` optional interface on `TxProvider` for rollback-to-stable on entry activity failures
- `TaskInvalidator` optional interface on `TaskStore` for atomic dangling task cleanup when exiting hierarchical subtrees
- `ActivityResolver` optional interface on `ActivityRunner` for synchronous entry/exit activity resolution by name
- `memstore.TaskStore` implements `TaskInvalidator` with `InvalidateByStates()`
- `memrunner` implements `ActivityResolver` with `Resolve(name)`
- Mermaid compound state rendering using `state X { ... }` nesting syntax with scoped internal transitions
- Event bubbling: unhandled transitions propagate from leaf state through ancestor chain (first match wins)
- Engine resolves compound state targets to initial leaf states recursively via `InitialLeafMap`
- Sentinel errors: `ErrCompoundStateNoInitialChild`, `ErrOrphanedChild`, `ErrCircularHierarchy`
- Validation rules: compound states must declare `InitialChild`, children reference existing states, `IsWait` leaf-only, circular hierarchy detection
- `Compile()` populates `Ancestry`, `DepthMap`, `InitialLeafMap` for hierarchical definitions
- Full hierarchical integration test: compound states, entry/exit activities, event bubbling, Mermaid rendering

### Changed
- `commitTransition()` now computes LCA-based exit/entry sequences with synchronous entry/exit activity execution and savepoint support
- `validateTransition()` walks ancestor chain for event bubbling when no direct transition match exists
- `createInstance()` resolves compound initial states to their initial leaf via `InitialLeafMap`
- `ForceState()` resolves compound targets to leaf states
- `computeHash()` includes new `StateDef` hierarchy fields for dedup correctness

## [v0.8.0] - 2026-03-11

### Added
- Graph Compilation: `CompiledMachine` intermediate representation in `internal/graph/compiled.go` — precomputes ancestry, depth map, transition-by-state index, definition hash (SHA-256), and guard names at registration time (Task 1: CompiledMachine)
- `NamedGuard` optional interface (`Guard` + `Name() string`) — guards can provide human-readable names for observer events; unimplemented guards fall back to `fmt.Sprintf("%T")` (Task 2: NamedGuard)
- Cross-workflow spawn cycle detection — `Register()` validates the spawn graph (ChildDef/ChildrenDef) across all registered definitions and returns `ErrSpawnCycle` on cycles (Task 3: Spawn Cycles)
- `ErrSpawnCycle` sentinel error for cross-workflow spawn cycle detection
- `NamedGuard` type alias exported from root package

### Changed
- `Register()` now validates, compiles, checks for spawn cycles, and stores a `CompiledMachine` alongside the raw definition. Hash-based dedup skips recompilation for identical definitions. (Task 4: Register Integration)
- All runtime methods (`Transition`, `Signal`, `ChildCompleted`, `ForceState`, `CompleteTask`) now operate on `CompiledMachine` instead of raw `*Definition` — uses precomputed transition index and guard names (Task 5: Runtime Migration)
- Guard failure observer events now report `NamedGuard.Name()` when available instead of Go type name

### Fixed
- `Register()` no longer commits definitions to the registry before spawn-cycle validation — rejected definitions cannot corrupt engine state

## [v0.7.0] - 2026-03-10

### Added
- Task 12: `adapters/slogadapter` — structured logging observer using Go's `log/slog`. Implements all four observer interfaces (`TransitionObserver`, `GuardObserver`, `ActivityObserver`, `InfrastructureObserver`). Logs each engine event with structured attributes at configurable levels. Register via `flowstep.WithObservers(slogadapter.New(logger))`.
- Seven functional options for per-event log level overrides: `WithTransitionLevel`, `WithGuardFailedLevel`, `WithActivityDispatchedLevel`, `WithActivityCompletedLevel`, `WithActivityFailedLevel`, `WithStuckLevel`, `WithPostCommitErrorLevel`.
- Default levels: Info (transitions, activity dispatched/completed), Warn (guard failures), Error (stuck, activity failed, post-commit errors).

## [v0.6.0] - 2026-03-10

### Added
- Task 7: `WithObservers(o ...Observer) Option` — register one or more typed observer adapters with the engine. Replaces `WithHooks`.
- `TransitionObserver`, `GuardObserver`, `ActivityObserver`, `InfrastructureObserver` — four focused observer interfaces. Adapters implement only the interfaces they need.
- Structured event structs: `TransitionEvent`, `GuardFailureEvent`, `ActivityDispatchedEvent`, `ActivityCompletedEvent`, `ActivityFailedEvent`, `StuckEvent`, `PostCommitErrorEvent` — all re-exported from the root package for adapter implementors.
- Empty observer registry (no observers registered) is a safe no-op — equivalent to the previous `NoopHooks` default.

### Removed
- `Hooks` interface (7-method monolithic contract) — replaced by the four focused observer interfaces above.
- `NoopHooks` struct — no longer needed; an empty observer registry produces the same no-op behavior.
- `WithHooks(h Hooks) Option` — replaced by `WithObservers(o ...Observer)`.

## [v0.5.0] - 2026-03-10

### Added
- Task 16: `engine.SideEffect(ctx, aggregateType, aggregateID, name string, fn func() (any, error)) (any, error)` — execute-once-persist-result pattern for non-deterministic operations (UUID generation, timestamps, random numbers). Runs `fn`, persists the result as a `SideEffect` DomainEvent to the EventStore, and returns the result. Enables replay-safe workflows (Task 9 will short-circuit re-execution using the stored event).
- `types.EventTypeSideEffect` and `types.EventTypeActivityOutcome` constants for event type identification
- `types.NewSideEffectEvent`, `types.ParseSideEffect` — helper functions for building and reading SideEffect DomainEvents
- `types.NewActivityOutcomeEvent`, `types.ParseActivityOutcome` — helper functions for activity outcome recording (ActivityID, ActivityName, Result, TransitionPath); used by the scheduler when OnSuccess/OnFailure transitions are wired (future task)
- Payload key constants (`PayloadKeySideEffectName`, `PayloadKeySideEffectResult`, `PayloadKeyActivityID`, `PayloadKeyActivityName`, `PayloadKeyActivityResult`, `PayloadKeyTransitionPath`)

## [v0.4.2] - 2026-03-10

### Changed
- Task 1: Decomposed `internal/engine/engine.go` (895 lines) into `internal/runtime/` sublayers — `runtime.go` (coordinator), `executor.go` (transition pipeline), `dispatcher.go` (post-commit), `scheduler.go` (trigger matching), `helpers.go` (utilities) (b9b842e)
- All files remain under 300 lines; internal test suite preserved across 4 test files
- No API or behavior changes — root `flowstep.Engine` interface unchanged

## [v0.4.1] - 2026-03-10

### Changed
- Task 0: Consolidated 9 root type alias forwarding files (`activityrunner.go`, `activitystore.go`, `childstore.go`, `eventbus.go`, `clock.go`, `txprovider.go`, `hooks.go`, `taskstore.go`, `store.go`) into a single `aliases.go` (d964b6f)
- Root package reduced from 14 to 6 production files
- No API or behavior changes — all exported symbols preserved as type aliases

## [v0.4.0] - 2026-03-10

### Changed
- README update and workflow examples

## [v0.3.0] - 2026-03-10

### Changed
- Codebase refactoring

## [v0.2.0] - 2026-03-10

### Fixed
- Project review and critical-to-medium fixes implementation

## [v0.1.0] - 2026-03-10

### Added
- flowstep first implementation

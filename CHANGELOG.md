# Changelog

All notable changes to this project will be documented in this file.
Format based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

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

 ---
  Batch 6 Complete: Tasks 20-25

  Task 20-22: Child Workflows (SpawnChild, OnChildCompleted)
  - Wired ChildStore into root engine deps
  - Exposed ChildCompleted() on public Engine type
  - TestEngineSpawnChild passes — full parent→child lifecycle works

  Task 23-24: Parallel Children (SpawnChildren, JoinAll/Any/N)
  - Added Join field to ChildrenDef type
  - Engine spawns multiple children with shared GroupID when ChildrenDef is set
  - ChildCompleted evaluates join policies (ALL, ANY, N) for grouped children
  - Fires OnChildrenJoined only when join policy is satisfied
  - TestEngineSpawnChildren_JoinAll passes — 3 children spawn, first 2 completions are rejected, 3rd triggers parent transition

  Task 25: Workflow Versioning
  - Engine now stores map[aggregateType]map[version]*Definition instead of single definition per aggregate
  - Register() tracks latest version per aggregate type
  - Transition(), Signal(), CompleteTask(), ChildCompleted() all resolve the correct definition version from the instance's WorkflowVersion
  - New instances use latest version, existing instances stay pinned to their creation version
  - TestEngineVersionCoexistence passes — v1 instance completes with v1 transitions even after v2 is registered

  ---
  Batch 7 Complete: Tasks 26-28

  Task 26: Observability — Hooks & Logging
  - Updated internal Hooks interface to include OnActivityDispatched, OnActivityCompleted, OnActivityFailed
  - runGuards() now calls OnGuardFailed with guard type name on failure
  - Activity dispatch calls OnActivityDispatched after each dispatch
  - Tests: TestEngineHooksCalledOnTransition, TestEngineHooksCalledOnGuardFailed, TestEngineHooksCalledOnActivityDispatched

  Task 27: Admin Recovery — ForceState
  - Added ForceState() to bypass normal transition rules, guards, and terminal state restrictions
  - Creates StateForced audit event with reason, from/to state
  - Clears stuck flags on forced instances
  - Tests: TestEngineForceState, TestEngineForceStateFromTerminal

  Task 28: Concurrency & Shutdown
  - Added ErrEngineShutdown sentinel error
  - Engine tracks in-flight operations with sync.WaitGroup
  - Shutdown() sets atomic flag and waits for in-flight ops
  - All public methods (Transition, Signal, CompleteTask, ChildCompleted, ForceState) check shutdown flag before starting
  - Test: TestEngineShutdown verifies post-shutdown transitions return ErrEngineShutdown

  ---
  Batch 8 Complete: Tasks 29-32

  Task 29: testutil Package
  - NewTestEngine(t) — pre-wired with all in-memory adapters + FakeClock
  - FakeClock with Now(), Advance(d), Set(t) for time control
  - AssertState, AssertEventCount, AssertEventChain assertion helpers
  - OrderWorkflow and ApprovalWorkflow fixture definitions
  - All 5 testutil tests pass

  Task 30-31: pgxstore (PostgreSQL)
  - Complete pgx/v5 implementation: EventStore, InstanceStore, TaskStore, ChildStore, ActivityStore, TxProvider
  - Embedded SQL migration (001_flowstate.sql) with all tables and indexes
  - JSONB for state data, payload, options; TIMESTAMPTZ for timestamps
  - Package compiles successfully

  Task 32: sqlitestore
  - database/sql implementation — driver-agnostic (users import their own SQLite driver)
  - EventStore, InstanceStore, TxProvider with SQLite-compatible types (TEXT instead of JSONB, INTEGER for booleans)
  - Embedded migration with SQLite schema
  - Package compiles successfully

  ---
  Batch 9 Complete: Tasks 33-35

  Task 33: dynamostore
  - Single-table DynamoDB design with PK/SK patterns and GSI for correlation queries
  - TransactWriteItems for atomic transaction support
  - EventStore + InstanceStore implemented, compiles cleanly

  Task 34: redisbus
  - Redis Streams EventBus: XADD on Emit, XREADGROUP with consumer groups for durable subscriptions
  - Events stored with indexed fields (event_type, aggregate_type, correlation_id) plus full JSON data

  Task 35: natsbus
  - NATS JetStream EventBus: Publish on Emit, durable consumer subscriptions
  - Manual ACK/NAK for reliable delivery

 ---
  Batch 11 Summary — Task 37:

  - Created integration_test.go with 13 integration tests covering the complete booking workflow
  - Tests implemented:
    - TestIntegration_BookingHappyPath — full flow: payment → confirm → check-in → complete
    - TestIntegration_BookingPaymentFailed — payment failure signal → CANCELLED
    - TestIntegration_BookingPaymentTimedOut — payment timeout signal → CANCELLED
    - TestIntegration_BookingUserCancellation — cancel from PENDING_PAYMENT and CONFIRMED
    - TestIntegration_BookingCancellationGuardRejects — cancellation policy guard blocks late cancellation
    - TestIntegration_BookingCheckInGuardRejects — time window guard blocks early/late check-in
    - TestIntegration_BookingTrainerHoldRelease — trainer wait state + task → release to public
    - TestIntegration_BookingTrainerHold — trainer wait state → hold → trainer confirm back
    - TestIntegration_BookingNoShow — mark no-show from CONFIRMED
    - TestIntegration_BookingEventChainCorrelation — verify all events share correlation ID
    - TestIntegration_BookingMermaidExport — verify Mermaid diagram contains all edges
    - TestIntegration_BookingAdminForceState — admin force-cancel stuck workflow
    - TestIntegration_BookingMultipleConcurrentWorkflows — independent workflow instances
  - Fix encountered: CompleteTask disambiguates by transition name (not task option label), so task options must match transition names

  All 37 tasks are complete. Per the executing-plans skill, I now need to use the superpowers:finishing-a-development-branch skill to complete this work. 

 ---
  Merge complete.
  - feature/flowstate-implementation (29 commits, 68 files, ~8,094 lines) fast-forward merged into main
  - All tests pass on main
  - Feature branch deleted
  - Worktree cleaned up

  The full flowstate library is now on main with:
  - Clean Architecture: types/, internal/engine, internal/builder, internal/graph
  - Fluent builder DSL with graph validation
  - State machine engine with guards, conditions, signals, tasks, child workflows, version coexistence
  - 7 adapter packages: memstore, pgxstore, sqlitestore, dynamostore, redisbus, natsbus, asynqrunner, goroutinerunner
  - testutil package with FakeClock, assertions, and fixtures
  - Observability hooks, admin recovery, graceful shutdown
  - 13 integration tests covering the complete booking workflow

  ---
  The README.md has been written with the following sections:

  1. Title + tagline — one-liner describing the library
  2. Features — 13 capability bullets
  3. Installation — go get command
  4. Quick Start — complete working example (define workflow, create engine, execute transitions)
  5. Core Concepts — Guards, Conditional Routing, Signals, Wait States & Tasks, Child Workflows (single + parallel with join policies), Activities (fire-and-forget +
  await)
  6. Engine Configuration — functional options with required vs optional callouts
  7. Adapters — three tables (Storage, Event Bus, Activity Runner) plus setup examples for PostgreSQL and Asynq
  8. Mermaid Diagrams — generation example with rendered output
  9. Testing — testutil.NewTestEngine, FakeClock, assertions, fixture workflows
  10. Error Handling — errors.Is / errors.As patterns with a sentinel error reference table
  11. License — MIT placeholder

  All examples are drawn from the actual codebase (integration tests, testutil fixtures, adapter constructors).
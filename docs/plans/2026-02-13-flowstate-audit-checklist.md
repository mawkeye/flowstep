# Flowstate Design Audit Checklist

**Date:** 2026-02-13
**Purpose:** Comprehensive checklist for auditing the Court Command workflow engine (flowstate) against core software engineering principles
**Scope:** Workflow engine package (`internal/workflow/`), its integration with domain modules, and the surrounding event/plugin infrastructure

---

## How to Use This Checklist

For each item: mark **PASS**, **PARTIAL**, or **FAIL**, then note the specific file/line and observation. Items marked N/A should include a justification for why the principle does not apply.

---

## 1. SOLID Principles (Robert C. Martin)

### 1.1 Single Responsibility Principle (SRP)
> A module should have one, and only one, reason to change.

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 1.1.1 | Engine has a single concern | `engine.go` handles only state machine execution (load state, run guards, transact, run effects, emit). It does NOT contain domain logic, persistence details, or serialization. | | |
| 1.1.2 | Definition is separate from execution | `definition.go` defines workflow structure (states, transitions). `engine.go` executes them. Neither knows about the other's internals. | | |
| 1.1.3 | Guards own only precondition logic | Each Guard implementation (e.g., `PaymentCompleteGuard`) checks exactly one precondition. It does not mutate state, emit events, or call external services. | | |
| 1.1.4 | Effects own only side-effect logic | Each Effect implementation (e.g., `ScheduleLighting`) performs exactly one post-commit action. It does not check preconditions or modify the aggregate. | | |
| 1.1.5 | Registry is separate from engine | `registry.go` handles workflow registration and lookup. Engine does not manage its own registry. | | |
| 1.1.6 | Mermaid export is isolated | `mermaid.go` only converts definitions to Mermaid syntax. It does not depend on engine runtime or database state. | | |
| 1.1.7 | Domain modules own their workflow definitions | `booking/workflow.go` defines the booking workflow. `payment/workflow.go` defines the payment workflow. The workflow package itself defines no domain-specific workflows. | | |

### 1.2 Open/Closed Principle (OCP)
> Software entities should be open for extension but closed for modification.

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 1.2.1 | New workflows without engine changes | Adding a new workflow (e.g., `TournamentMatchWorkflow`) requires only a new `Define()` call in a domain module, not changes to `engine.go`. | | |
| 1.2.2 | New guards without engine changes | Adding a new guard requires only implementing the `Guard` interface. No switch statements or type assertions in the engine. | | |
| 1.2.3 | New effects without engine changes | Adding a new effect requires only implementing the `Effect` interface. Engine iterates over effects generically. | | |
| 1.2.4 | New states and transitions are additive | Adding states to a workflow is a matter of adding `.State()` and `.Transition()` calls, not modifying existing transition logic. | | |
| 1.2.5 | Builder pattern preserves immutability | `workflow.Define()...Build()` returns an immutable definition. After `Build()`, the definition cannot be modified at runtime. | | |

### 1.3 Liskov Substitution Principle (LSP)
> Subtypes must be substitutable for their base types without altering correctness.

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 1.3.1 | All Guard implementations are interchangeable | The engine calls `guard.Check(ctx, aggregate, params)` uniformly. No guard requires special handling or type-specific casting by the engine. | | |
| 1.3.2 | All Effect implementations are interchangeable | The engine calls `effect.Execute(ctx, aggregate, event)` uniformly. No effect requires the engine to know its concrete type. | | |
| 1.3.3 | Aggregate parameter is truly generic | The `any` type for aggregates does not force type assertions inside the engine. Guards and effects perform their own type assertions internally. | | |
| 1.3.4 | Plugin interface is substitutable | All `Plugin` implementations (LightingPlugin, AccessPlugin, AuditLogger) can be swapped without changing event routing logic. | | |

### 1.4 Interface Segregation Principle (ISP)
> No client should be forced to depend on methods it does not use.

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 1.4.1 | Guard interface is minimal | `Guard` has only `Check()`. It does not force implementors to provide lifecycle hooks, names, or metadata they do not need. | | |
| 1.4.2 | Effect interface is minimal | `Effect` has only `Execute()`. It does not force implementors to provide rollback, retry policy, or health check methods. | | |
| 1.4.3 | Plugin interface is focused | `Plugin` has `Name()`, `Subscriptions()`, and `Handle()`. Consider whether `Name()` should be separate or if it creates unnecessary coupling. | | |
| 1.4.4 | DriverProtocol is not too wide | `DriverProtocol` has 6 methods. Check if drivers that don't support polling (`PollState`) or callbacks (`ParseFeedback`, `ValidateCallback`) are forced to implement no-op stubs. Consider splitting into `Commander`, `FeedbackReceiver`, `HealthChecker`. | | |
| 1.4.5 | Engine does not expose internal state | The engine's public API exposes only `Transition()`, `GetState()`, and similar. It does not leak transaction management, event storage, or internal data structures. | | |

### 1.5 Dependency Inversion Principle (DIP)
> High-level modules should not depend on low-level modules. Both should depend on abstractions.

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 1.5.1 | Engine depends on interfaces, not implementations | The engine accepts `Guard` and `Effect` interfaces, not concrete types like `PaymentCompleteGuard`. | | |
| 1.5.2 | Engine does not depend on database package | The engine does not import `internal/database` directly. Persistence is injected via an interface (e.g., `WorkflowStore` or `EventStore`). | | |
| 1.5.3 | Engine does not depend on Redis | The engine does not import `go-redis`. Lock management is handled externally or via an injected `Locker` interface. | | |
| 1.5.4 | Event emission is abstracted | The engine emits events through an `EventBus` interface, not directly through Redis Streams. | | |
| 1.5.5 | Domain modules depend on workflow interfaces | `booking/workflow.go` imports `internal/workflow` types, not the reverse. The workflow package has zero imports from `internal/modules/*`. | | |

---

## 2. Clean Code (Robert C. Martin)

### 2.1 Meaningful Names

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 2.1.1 | Types reveal intent | `WorkflowDefinition`, `State`, `Transition`, `Guard`, `Effect` -- names describe what they are, not implementation details. No `Manager`, `Handler`, `Helper` god-names. | | |
| 2.1.2 | Method names describe actions | `engine.Transition()` not `engine.Process()`. `guard.Check()` not `guard.Do()`. `effect.Execute()` not `effect.Run()`. | | |
| 2.1.3 | State names use domain language | States are named `PENDING_PAYMENT`, `CONFIRMED`, `CHECKED_IN` -- domain terms that non-developers understand. Not `STATE_1`, `STEP_2`. | | |
| 2.1.4 | Transition names are verbs | Transitions are named `confirm`, `cancel_user`, `check_in`, `expire` -- actions. Not `toConfirmed` or `state2to3`. | | |
| 2.1.5 | No ambiguous abbreviations | No `wf`, `tx`, `evt` in public APIs. Internal short names are acceptable if scoped tightly. | | |

### 2.2 Small Functions

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 2.2.1 | Transition method is decomposed | `engine.Transition()` delegates to sub-steps: `validateTransition()`, `runGuards()`, `executeTransaction()`, `runEffects()`, `emitEvent()`. No single method >40 lines. | | |
| 2.2.2 | Guards are single-purpose | Each guard's `Check()` method is <20 lines. Complex validation is broken into helper functions. | | |
| 2.2.3 | Effects are single-purpose | Each effect's `Execute()` method performs one external call or one state mutation. No effect does both. | | |
| 2.2.4 | Builder methods are chainable and small | Each builder method (`.State()`, `.Transition()`, `.Guards()`) does one thing: append to internal state. | | |

### 2.3 Error Handling

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 2.3.1 | Guard errors are descriptive | Guard failures return errors with context: which guard failed, what condition was not met, what the current state was. Not just `"guard failed"`. | | |
| 2.3.2 | Transition errors distinguish error types | The engine distinguishes: invalid transition (no path from current state), guard failure (precondition not met), persistence failure (database error), effect failure (post-commit). | | |
| 2.3.3 | Effect failures do not roll back transitions | A confirmed transition is never undone because a post-commit effect failed. The workflow is marked `is_stuck` instead. | | |
| 2.3.4 | Errors are wrapped with context | All errors use `fmt.Errorf("workflow %s transition %s: %w", ...)` pattern for stack tracing. | | |
| 2.3.5 | Sentinel errors for programmatic handling | The package defines sentinel errors like `ErrInvalidTransition`, `ErrGuardFailed`, `ErrWorkflowStuck` that callers can check with `errors.Is()`. | | |
| 2.3.6 | Panics are never used for control flow | The engine never panics on invalid input. Invalid workflow definitions are caught at `Build()` time with clear error returns. | | |

### 2.4 Boundaries

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 2.4.1 | Clear package boundary | `internal/workflow/` exposes a clean public API. Internal types (builder state, validation helpers) are unexported. | | |
| 2.4.2 | No leaky abstractions | The workflow package does not expose `pgx.Tx`, `redis.Client`, or any infrastructure types in its public API. | | |
| 2.4.3 | Domain types do not leak into workflow | The workflow package does not import `booking.Booking` or `payment.Transaction`. It works with `any` aggregates and `map[string]any` params. | | |
| 2.4.4 | Third-party dependencies are wrapped | If the workflow package uses external libraries (e.g., for deep copy), they are wrapped in internal functions, not exposed. | | |

---

## 3. A Philosophy of Software Design (John Ousterhout)

### 3.1 Deep vs. Shallow Modules

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 3.1.1 | Engine is a deep module | The engine provides a simple public API (`Transition`, `GetState`, `ListTransitions`) but internally handles guard evaluation, transactional persistence, event chain writes, effect execution, stuck detection, and event bus emission. High functionality-to-interface ratio. | | |
| 3.1.2 | Guards are appropriately shallow | Guards have a 1-method interface. This is acceptable because they are leaf nodes -- simple predicate functions. Their shallowness is intentional, not a design flaw. | | |
| 3.1.3 | Effects are appropriately shallow | Same reasoning as guards. Effects are side-effect leaves. One method is correct. | | |
| 3.1.4 | Builder is a deep module | The builder's public API is a chain of simple method calls, but internally it validates state graph connectivity, detects unreachable states, verifies terminal states exist, and prevents duplicate transition names. | | |

### 3.2 Information Hiding

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 3.2.1 | Transaction management is hidden | Callers of `engine.Transition()` do not know about `BEGIN`, `COMMIT`, `ROLLBACK`. The engine manages the full transactional lifecycle internally. | | |
| 3.2.2 | Event chain writing is hidden | The engine automatically creates `domain_events` entries with `state_before` and `state_after` snapshots. Callers do not manually construct events. | | |
| 3.2.3 | Stuck detection is hidden | The engine internally marks workflows as stuck when effects fail. Callers receive an error but do not manage the `is_stuck` flag. | | |
| 3.2.4 | State serialization is hidden | How aggregate state is serialized to `JSONB` for `state_data` is an internal detail. Callers pass Go structs. | | |
| 3.2.5 | Workflow instance lifecycle is hidden | Creating, updating, and querying `workflow_instances` rows is internal. Callers work with aggregate IDs, not workflow instance IDs. | | |

### 3.3 Complexity Management

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 3.3.1 | Complexity is pushed down, not out | The engine absorbs complexity (transaction management, event sourcing, retry tracking) so that domain code (booking module) remains simple declarative definitions. | | |
| 3.3.2 | No "pass-through" methods | The engine does not have methods that simply delegate to a child object without adding value. Each public method adds meaningful logic. | | |
| 3.3.3 | Cognitive load per transition is low | A developer defining a new transition writes ~5-8 lines of builder code. They do not need to understand event storage, transaction isolation, or Redis Streams. | | |
| 3.3.4 | No configuration explosion | The engine does not have dozens of optional config parameters. Sensible defaults (e.g., retry count) are built in, with few override points. | | |

---

## 4. The Pragmatic Programmer (Hunt & Thomas)

### 4.1 DRY (Don't Repeat Yourself)

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 4.1.1 | Transition logic is not duplicated | The engine handles all transitions uniformly. There is no copy-paste between booking transitions, payment transitions, and age transitions. | | |
| 4.1.2 | Event chain creation is centralized | `domain_events` rows are created by the engine, not by each domain module independently. No duplicate event-writing code across modules. | | |
| 4.1.3 | Guard/Effect registration is uniform | All workflows use the same `workflow.Guards()` and `workflow.Effects()` builder methods. No workflow has a special registration path. | | |
| 4.1.4 | State validation is not duplicated | The `current_state` field is updated by the engine in one place. Domain modules do not independently update aggregate state columns. | | |
| 4.1.5 | Stuck workflow handling is centralized | Retry, force, and rewind operations are engine-level operations, not reimplemented per domain. | | |

### 4.2 Orthogonality

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 4.2.1 | Guards are independent of each other | Changing `PaymentCompleteGuard` does not affect `CancellationPolicyGuard`. They share no mutable state. | | |
| 4.2.2 | Effects are independent of each other | `ScheduleLighting` and `GenerateCredential` can fail independently. One effect's failure does not prevent others from running. | | |
| 4.2.3 | Workflow definitions are independent | Changing the booking workflow does not affect the payment workflow or age transition workflow. | | |
| 4.2.4 | Engine is orthogonal to event bus | The engine works correctly even if the event bus is down. Event emission is a post-commit non-blocking step. | | |
| 4.2.5 | Engine is orthogonal to plugin system | Plugins react to events after the engine has completed its work. Plugin failures do not affect engine correctness. | | |

### 4.3 Decoupling

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 4.3.1 | Engine-to-database decoupling | The engine can be tested with an in-memory store. It does not require PostgreSQL to function. | | |
| 4.3.2 | Engine-to-domain decoupling | The engine knows nothing about bookings, payments, or users. It works with generic aggregates and string-based state names. | | |
| 4.3.3 | Guard-to-infrastructure decoupling | Guards that need external data (e.g., checking Stripe payment status) receive dependencies via constructor injection, not global variables. | | |
| 4.3.4 | Effect-to-infrastructure decoupling | Effects that call external APIs (e.g., Salto KS, Shelly Cloud) receive HTTP clients or service interfaces via constructor injection. | | |
| 4.3.5 | Temporal decoupling of effects | Effects run after the transaction commits. There is no temporal coupling between the database commit and effect execution. | | |

### 4.4 Tracer Bullets

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 4.4.1 | Correlation ID traces full lifecycle | Every transition, event, guard failure, effect execution, and plugin reaction shares the same `correlation_id`. A single query reconstructs the entire story. | | |
| 4.4.2 | Causation chain is maintained | Each event records `causation_id` pointing to the event that triggered it. Effect-generated events point back to the transition event. | | |
| 4.4.3 | State snapshots enable replay | `state_before` and `state_after` in `domain_events` allow reconstruction of any historical state without replaying all events from genesis. | | |
| 4.4.4 | Mermaid export enables visual tracing | The auto-generated Mermaid diagram matches the runtime behavior. Guards are annotated on transitions so the diagram serves as living documentation. | | |

---

## 5. Domain-Driven Design (Eric Evans)

### 5.1 Bounded Contexts

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 5.1.1 | Workflow engine is its own bounded context | `internal/workflow/` does not import any domain module. It is a pure infrastructure concern. | | |
| 5.1.2 | Domain modules are bounded contexts | `booking/`, `payment/`, `identity/`, `pricing/` each own their models, events, and workflow definitions. They do not share database tables or internal types. | | |
| 5.1.3 | Cross-context communication is via events | When a booking confirmation triggers a payment, it happens through the event bus, not a direct function call from `booking/` to `payment/`. | | |
| 5.1.4 | Context boundaries are enforced by Go packages | Go's package system enforces that `internal/modules/booking/` cannot access unexported types in `internal/modules/payment/`. | | |

### 5.2 Aggregates

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 5.2.1 | Each workflow operates on one aggregate | The booking workflow operates on a `Booking` aggregate. The payment workflow operates on a `Payment` aggregate. No workflow spans multiple aggregate roots. | | |
| 5.2.2 | Aggregate boundaries are clear | The `Booking` aggregate includes the booking record and its workflow instance. It does not include the user profile, the resource, or the price rules. | | |
| 5.2.3 | Workflow instance is part of the aggregate | `workflow_instances` has a unique constraint on `(aggregate_type, aggregate_id)` -- one workflow instance per aggregate. The workflow instance is subordinate to the aggregate, not independent. | | |
| 5.2.4 | Aggregate invariants are protected | Guards enforce aggregate invariants (e.g., "a booking can only be confirmed if payment is complete"). These invariants are checked before any state change. | | |

### 5.3 Value Objects

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 5.3.1 | States are value objects | A `State` is defined by its name and type (Initial, Normal, Terminal). Two states with the same name are equal. States are immutable after definition. | | |
| 5.3.2 | Transitions are value objects | A `Transition` is defined by its name, source states, target state, event name, guards, and effects. Transitions are immutable after `Build()`. | | |
| 5.3.3 | Domain events are value objects | `DomainEvent` structs are immutable once created. They are stored in an append-only table. No event is ever modified. | | |
| 5.3.4 | Workflow definitions are value objects | After `Build()`, a `WorkflowDefinition` is immutable. It can be shared across goroutines safely. | | |

### 5.4 Ubiquitous Language

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 5.4.1 | Code uses domain terms | Code says `Transition`, `Guard`, `Effect`, `State` -- terms that domain experts would understand. Not `FSM`, `Node`, `Edge`, `Predicate`. | | |
| 5.4.2 | State names match business language | `PENDING_PAYMENT`, `CONFIRMED`, `CHECKED_IN`, `NO_SHOW` are terms the facility staff uses daily. | | |
| 5.4.3 | Guard names describe business rules | `PaymentCompleteGuard`, `CancellationPolicyGuard`, `WithinTimeWindowGuard` -- names a facility manager can read and validate. | | |
| 5.4.4 | Event names describe what happened | `BookingConfirmed`, `BookingCancelled`, `PaymentInitiated` -- past-tense domain events that tell a story. | | |
| 5.4.5 | No technical jargon in domain layer | Workflow definitions in `booking/workflow.go` read like a business specification, not like infrastructure code. | | |

---

## 6. 12-Factor App

### 6.1 Configuration

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 6.1.1 | No hardcoded timeouts | Effect timeouts, retry limits, stuck detection thresholds are configurable via environment variables or config, not hardcoded constants. | | |
| 6.1.2 | No hardcoded connection strings | Database URLs, Redis addresses, and event bus endpoints are injected via configuration. | | |
| 6.1.3 | Workflow definitions are code, not config | Workflow definitions are Go code (compile-time verified), not YAML/JSON that could silently break at runtime. This is an intentional design decision, not a 12-factor violation. | | |

### 6.2 Dependencies

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 6.2.1 | All dependencies are explicit in go.mod | No vendored copies, no implicit system dependencies. `go mod tidy` produces a clean result. | | |
| 6.2.2 | Workflow package has minimal dependencies | `internal/workflow/` imports only stdlib, context, and its own interfaces. It does not pull in pgx, go-redis, or other infrastructure directly. | | |

### 6.3 Backing Services

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 6.3.1 | Database is a replaceable backing service | The engine uses an injected store interface. Swapping PostgreSQL for another database requires implementing the interface, not modifying the engine. | | |
| 6.3.2 | Event bus is a replaceable backing service | The engine emits events through an interface. Swapping Redis Streams for NATS, Kafka, or an in-memory bus requires implementing the interface. | | |
| 6.3.3 | Graceful degradation without Redis | The architecture document states the system degrades gracefully if Redis is down (locks bypass to PostgreSQL). Verify the engine itself does not hard-depend on Redis. | | |

### 6.4 Disposability

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 6.4.1 | Engine handles graceful shutdown | The engine can drain in-flight transitions on SIGTERM. Effects that have not started are skipped cleanly. | | |
| 6.4.2 | Incomplete effects are recoverable | If the process dies between commit and effect execution, the `is_stuck` mechanism allows retry on restart. No data is lost. | | |
| 6.4.3 | Engine startup is fast | The engine initializes by loading workflow definitions from the registry. No database migrations, no heavy bootstrapping in the engine itself. | | |

---

## 7. Go-Specific Principles

### 7.1 Effective Go

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 7.1.1 | Errors are values, not exceptions | The engine returns `error` from all operations. No panics for expected conditions. | | |
| 7.1.2 | Context is threaded through | All public methods accept `context.Context` as the first parameter. Guards and effects receive context for cancellation and deadline propagation. | | |
| 7.1.3 | Goroutines are managed | Post-commit effects run in goroutines with proper error collection (e.g., `errgroup`). No fire-and-forget goroutines that can leak. | | |
| 7.1.4 | Package naming follows conventions | `workflow` not `workflow_engine`. `guard` not `guard_interface`. Short, lowercase, no underscores. | | |
| 7.1.5 | Documentation on exported types | All exported types (`WorkflowDefinition`, `Guard`, `Effect`, `Engine`) have godoc comments explaining their purpose and usage. | | |

### 7.2 Go Proverbs (Rob Pike)

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 7.2.1 | "Don't communicate by sharing memory; share memory by communicating" | Effects and plugins communicate via events (channels/event bus), not via shared mutable state. | | |
| 7.2.2 | "The bigger the interface, the weaker the abstraction" | `Guard` (1 method) and `Effect` (1 method) are strong abstractions. `DriverProtocol` (6 methods) is a potential concern. | | |
| 7.2.3 | "Make the zero value useful" | A zero-value `WorkflowDefinition` is either explicitly invalid (caught at `Build()`) or safely inert. It does not cause nil pointer panics. | | |
| 7.2.4 | "A little copying is better than a little dependency" | The workflow package does not import a utility library for trivial operations (deep copy, string helpers). It copies small utilities inline if needed. | | |
| 7.2.5 | "Clear is better than clever" | The builder API uses explicit method names (`workflow.From()`, `workflow.To()`, `workflow.Guards()`) not clever operator overloading or reflection magic. | | |
| 7.2.6 | "Errors are values" | The engine defines typed errors (`ErrInvalidTransition`, `ErrGuardFailed`) that can be programmatically inspected, not just string-matched. | | |
| 7.2.7 | "Don't just check errors, handle them gracefully" | Guard failures produce user-facing error messages. Effect failures trigger stuck-workflow recovery. Database errors trigger transaction rollback. Each error type has a defined recovery path. | | |

### 7.3 Accept Interfaces, Return Structs

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 7.3.1 | Engine constructor accepts interfaces | `NewEngine(store WorkflowStore, bus EventBus, logger *slog.Logger)` -- accepts interfaces for store and bus, concrete type for logger (stdlib). | | |
| 7.3.2 | Engine methods return concrete types | `engine.Transition()` returns `(*TransitionResult, error)`, not an interface. Callers get full type information. | | |
| 7.3.3 | Builder returns concrete definition | `workflow.Define()...Build()` returns `*WorkflowDefinition`, not an interface. | | |
| 7.3.4 | Interfaces are defined by consumers | The `WorkflowStore` interface is defined in `internal/workflow/`, not in `internal/database/`. The consumer defines what it needs. | | |

### 7.4 Make the Zero Value Useful

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 7.4.1 | Empty definition is safely invalid | A `WorkflowDefinition{}` with no states or transitions is caught at `Build()` with a clear error, not at runtime with a nil pointer dereference. | | |
| 7.4.2 | Default state_data is `{}` | `workflow_instances.state_data` defaults to `'{}'::jsonb`. An empty workflow instance is valid and queryable. | | |
| 7.4.3 | Default retry_count is 0 | `workflow_instances.retry_count` defaults to 0. A new workflow instance is not stuck by default. | | |

---

## 8. Temporal/Cadence Workflow Engine Patterns

### 8.1 Deterministic Workflows

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 8.1.1 | Transitions are deterministic | Given the same current state, transition name, and guard inputs, the engine always produces the same result. No randomness, no time-dependent logic in the engine itself. | | |
| 8.1.2 | Guards are pure functions of input | Guards evaluate based on the aggregate state and params passed to them. They do not read wall-clock time, random values, or external state that could change between retries. (Time-dependent guards like `WithinTimeWindowGuard` receive time as a parameter, not via `time.Now()`.) | | |
| 8.1.3 | State snapshots enable replay | The `state_before` + `state_after` pattern in `domain_events` means any state can be reconstructed without replaying from genesis. This is the "Rewind" capability. | | |
| 8.1.4 | No side effects during guard evaluation | Guards only check conditions. They do not write to the database, call external APIs, or emit events. All side effects happen in the Effect phase. | | |

### 8.2 Activity Patterns

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 8.2.1 | Effects are the equivalent of Activities | Effects (post-commit actions) map to Temporal's Activity concept: potentially non-deterministic, potentially slow, potentially failing operations that happen outside the workflow transaction. | | |
| 8.2.2 | Effects have retry semantics | Failed effects can be retried via the "Retry" admin action. The `retry_count` field tracks attempts. Consider whether automatic retry with backoff is supported. | | |
| 8.2.3 | Effects have timeout handling | Effects that call external APIs (Stripe, Salto, Shelly) have context-based timeouts. A stuck effect does not block the engine indefinitely. | | |
| 8.2.4 | Effects are idempotent | Effects must be safe to retry. `ScheduleLighting` called twice for the same booking produces the same result (idempotent IoT command). `GenerateCredential` does not create duplicate credentials. | | |
| 8.2.5 | Effect results are recorded | The engine records which effects succeeded and which failed, enabling selective retry of only the failed effects. | | |

### 8.3 Signal Handling

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 8.3.1 | External events can trigger transitions | External signals (Stripe webhook: payment succeeded, Salto callback: door opened, Timer: booking expired) can trigger workflow transitions. The engine does not only support user-initiated transitions. | | |
| 8.3.2 | Signals are processed via the same engine path | A timer-triggered `expire` transition goes through the same guard/effect pipeline as a user-triggered `cancel_user`. No separate code path for automated transitions. | | |
| 8.3.3 | IoT feedback triggers transitions | Device callbacks (ENTRY_EVENT from Salto) can trigger `check_in` transitions. This is the bidirectional feedback pipeline described in the architecture. | | |
| 8.3.4 | Timer-based transitions are supported | The `expire` transition for `PENDING_PAYMENT` bookings implies a timer mechanism. Verify this is implemented via asynq scheduled tasks, not busy-wait polling. | | |

### 8.4 Child Workflows

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 8.4.1 | Booking workflow can spawn payment workflow | When a booking transitions to `PENDING_PAYMENT`, a payment workflow instance is created. The two workflows are linked via `correlation_id`. | | |
| 8.4.2 | Split payment workflow is a child of payment | The `SplitPaymentWorkflow` instances are children of the main `PaymentWorkflow`. Guest charge events bubble up to the parent. | | |
| 8.4.3 | Child workflow completion triggers parent transition | When all split payment shares are collected, the parent payment workflow transitions to `COMPLETED`, which in turn triggers the booking workflow's `confirm` transition. | | |
| 8.4.4 | Child workflow failure is handled | If a split payment `FALLOUT_CHARGE_FAILED`, the parent payment workflow transitions to `FAILED`. The booking workflow's `expire` or admin `force` handles the cascade. | | |
| 8.4.5 | Correlation ID links parent and children | All workflows in a booking lifecycle share the same `correlation_id`. A single query retrieves the full tree: booking + payment + split payments + IoT commands. | | |

### 8.5 Workflow Versioning

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 8.5.1 | Workflow type is stored per instance | `workflow_instances.workflow_type` and `bookings.workflow_type` store the version/variant. An existing `standard_booking` instance is not broken when `tournament_match` is added. | | |
| 8.5.2 | Multiple workflow versions can coexist | Active bookings on v1 of a workflow continue to use v1 definitions. New bookings use v2. No forced migration of in-flight workflows. | | |
| 8.5.3 | Workflow changes are backward compatible | Adding a new state or transition to a workflow does not invalidate existing instances. Removing a state requires a migration plan for active instances. | | |

---

## 9. Cross-Cutting Concerns

### 9.1 Testability

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 9.1.1 | Engine is testable without infrastructure | The engine can be instantiated with mock `WorkflowStore` and `EventBus` implementations. Full transition lifecycle can be tested in-memory. | | |
| 9.1.2 | Guards are independently testable | Each guard can be unit-tested by constructing the appropriate aggregate state and params. No database or network needed. | | |
| 9.1.3 | Effects are independently testable | Each effect can be tested with a mock aggregate and event. External dependencies (Stripe, Salto) are injected interfaces. | | |
| 9.1.4 | Workflow definitions are testable | A definition can be validated for correctness: all states reachable, terminal states exist, no dead-end non-terminal states, no duplicate transition names. | | |
| 9.1.5 | Integration tests cover the full path | At least one test exercises: define workflow -> create instance -> run guards -> transition -> write event -> run effects -> emit to bus. | | |

### 9.2 Observability

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 9.2.1 | Structured logging for transitions | Every transition logs: workflow type, aggregate ID, from state, to state, transition name, actor ID, duration, success/failure. Uses `slog` structured fields. | | |
| 9.2.2 | Metrics for workflow health | Track: transitions per second, guard failure rate, effect failure rate, stuck workflow count, average transition duration. | | |
| 9.2.3 | Stuck workflow alerting | When `is_stuck` is set, an alert mechanism exists (log, metric, notification). Stuck workflows do not silently accumulate. | | |
| 9.2.4 | Event chain is queryable | The `domain_events` table with `correlation_id` and `sequence_num` indexes supports efficient debugging queries. | | |

### 9.3 Concurrency Safety

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 9.3.1 | Workflow definitions are read-only after Build() | `WorkflowDefinition` is safe to share across goroutines. No mutex needed because it is immutable. | | |
| 9.3.2 | Engine is safe for concurrent use | Multiple goroutines can call `engine.Transition()` concurrently. The engine uses per-transition transactions, not global locks. | | |
| 9.3.3 | Aggregate-level serialization | Two concurrent transitions on the same aggregate are serialized by the database (row-level locks or `UNIQUE` constraint on workflow_instances). | | |
| 9.3.4 | Effects run with isolated state | Effects receive copies of aggregate data, not references to shared mutable state. Concurrent effect goroutines cannot interfere with each other. | | |

### 9.4 Security

| # | Check Item | What to Look For | Status | Notes |
|---|-----------|-------------------|--------|-------|
| 9.4.1 | Actor ID is always recorded | Every transition records who initiated it. The `actor_id` field in `domain_events` is NOT NULL. System-initiated transitions use a well-known system actor ID. | | |
| 9.4.2 | Admin overrides are audited | Force transitions and rewinds write dedicated event types (`WorkflowForceTransitioned`, `WorkflowRewound`) with the admin's actor ID and a reason field. | | |
| 9.4.3 | Domain events are immutable | The `domain_events` table has INSERT + SELECT permissions only. No UPDATE or DELETE. The application code never modifies existing events. | | |
| 9.4.4 | State cannot be backdated | Transitions always move forward. The `sequence_num` (BIGSERIAL) ensures ordering. There is no API to insert an event at a past sequence number. | | |

---

## Summary Scoring Template

| Category | Total Items | Pass | Partial | Fail | N/A | Score |
|----------|------------|------|---------|------|-----|-------|
| 1. SOLID Principles | 20 | | | | | /20 |
| 2. Clean Code | 17 | | | | | /17 |
| 3. Philosophy of Software Design | 11 | | | | | /11 |
| 4. Pragmatic Programmer | 17 | | | | | /17 |
| 5. Domain-Driven Design | 16 | | | | | /16 |
| 6. 12-Factor App | 10 | | | | | /10 |
| 7. Go-Specific | 15 | | | | | /15 |
| 8. Temporal/Cadence Patterns | 17 | | | | | /17 |
| 9. Cross-Cutting Concerns | 15 | | | | | /15 |
| **TOTAL** | **138** | | | | | **/138** |

---

## Critical Path Items (Must-Pass)

The following items represent non-negotiable quality gates. A FAIL on any of these should block release:

1. **1.5.5** -- Workflow package has zero imports from domain modules (DIP)
2. **2.3.3** -- Effect failures do not roll back transitions (error handling correctness)
3. **4.1.2** -- Event chain creation is centralized (DRY for audit integrity)
4. **5.2.1** -- Each workflow operates on one aggregate (aggregate boundaries)
5. **7.1.2** -- Context is threaded through all public methods (cancellation/timeouts)
6. **8.1.1** -- Transitions are deterministic (workflow correctness)
7. **8.2.4** -- Effects are idempotent (at-least-once delivery safety)
8. **8.4.5** -- Correlation ID links all related workflows (debuggability)
9. **9.3.3** -- Aggregate-level serialization prevents concurrent corruption
10. **9.4.3** -- Domain events are immutable (audit integrity)

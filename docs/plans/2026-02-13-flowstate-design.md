# flowstate: Declarative Workflow Engine for Go

**Date:** 2026-02-13
**Status:** Approved
**Module:** `github.com/mawkeye/flowstate`
**Scope:** Standalone, reusable Go package for declarative workflow/state machine management with immutable event chains, pluggable persistence, deterministic execution, and async activity orchestration.

---

## Table of Contents

1. [Design Goals & Principles](#1-design-goals--principles)
2. [Package Structure](#2-package-structure)
3. [Core Types](#3-core-types)
4. [Interfaces](#4-interfaces)
5. [Fluent Builder API](#5-fluent-builder-api)
6. [Engine API](#6-engine-api)
7. [Deterministic Execution & Activities](#7-deterministic-execution--activities)
8. [Conditional Routing](#8-conditional-routing)
9. [Wait States & Pending Tasks](#9-wait-states--pending-tasks)
10. [Signals](#10-signals)
11. [Child Workflows & Parallel Orchestration](#11-child-workflows--parallel-orchestration)
12. [Build-Time Validation](#12-build-time-validation)
13. [Workflow Versioning](#13-workflow-versioning)
14. [Observability & Hooks](#14-observability--hooks)
15. [Error Handling](#15-error-handling)
16. [Mermaid Diagram Export](#16-mermaid-diagram-export)
17. [Admin Recovery](#17-admin-recovery)
18. [Concurrency & Shutdown](#18-concurrency--shutdown)
19. [TDD Strategy & Test Utilities](#19-tdd-strategy--test-utilities)
20. [Reference Implementations](#20-reference-implementations)
21. [Database Schema](#21-database-schema)
22. [Complete Booking Example](#22-complete-booking-example)

---

## 1. Design Goals & Principles

### Goals

- **Reusable**: Works in any Go application, not just Court Command
- **Declarative**: Workflows defined as code, compile-time verified
- **Deterministic**: Workflow code never performs I/O; activities handle all non-deterministic work
- **Pluggable**: Every infrastructure dependency (DB, event bus, activity runner) is an interface
- **Observable**: Hooks for metrics, structured logging, alerting
- **Debuggable**: Immutable event chain with state snapshots, correlation IDs, Mermaid diagrams
- **TDD-first**: Ship in-memory implementations and test utilities so consumers write tests from day one

### Core Principles

| Principle | How flowstate applies it |
|---|---|
| SRP | Engine executes. Builder validates. Activities do work. Guards check. Conditions route. |
| OCP | New workflows, guards, conditions, activities = new code, never engine changes |
| DIP | Engine depends on interfaces (stores, bus, runner), never implementations |
| Deep modules | Simple public API, complex internals (txn mgmt, event chain, stuck detection) |
| Deterministic workflows | Guards and conditions read state only. Activities run async outside the transaction. |
| Accept interfaces, return structs | All dependencies injected as interfaces. All results returned as concrete types. |
| Zero core dependencies | Core package imports only stdlib. Reference implementations are separate sub-packages. |

---

## 2. Package Structure

```
github.com/mawkeye/flowstate/
│
├── flowstate.go              # Public entry: Define(), NewEngine()
├── definition.go             # Definition, State, Transition types
├── builder.go                # Fluent builder (DefBuilder)
├── engine.go                 # Engine: Transition, Signal, CompleteTask, admin ops
├── guard.go                  # Guard interface (deterministic)
├── condition.go              # Condition interface (deterministic, for routing)
├── router.go                 # Route, When, Default
├── activity.go               # Activity interface + ActivityInput/Result/Invocation
├── event.go                  # DomainEvent type
├── instance.go               # WorkflowInstance type
├── task.go                   # PendingTask type + TaskDef
├── child.go                  # ChildRelation type + JoinPolicy
├── signal.go                 # SignalInput type
├── result.go                 # TransitionResult type
├── store.go                  # EventStore + InstanceStore interfaces
├── taskstore.go              # TaskStore interface
├── childstore.go             # ChildStore interface
├── activitystore.go          # ActivityStore interface
├── txprovider.go             # TxProvider interface
├── eventbus.go               # EventBus interface
├── activityrunner.go         # ActivityRunner interface
├── clock.go                  # Clock interface (deterministic time)
├── hooks.go                  # Hooks interface (observability)
├── options.go                # Functional options (With*)
├── errors.go                 # Sentinel errors + GuardError
├── validate.go               # Build-time graph validation
├── mermaid.go                # Definition → Mermaid stateDiagram export
├── doc.go                    # Package documentation
│
├── memstore/                 # In-memory (TDD + consumer tests)
│   ├── event_store.go
│   ├── instance_store.go
│   ├── task_store.go
│   ├── child_store.go
│   ├── activity_store.go
│   └── tx_provider.go
│
├── memrunner/                # Synchronous in-memory ActivityRunner (TDD)
│   └── runner.go
│
├── chanbus/                  # In-process Go channel EventBus
│   └── bus.go
│
├── pgxstore/                 # PostgreSQL via pgx/v5
│   ├── event_store.go
│   ├── instance_store.go
│   ├── task_store.go
│   ├── child_store.go
│   ├── activity_store.go
│   ├── tx_provider.go
│   └── migrations/
│       └── 001_flowstate.sql # Embeddable via embed.FS
│
├── sqlitestore/              # SQLite via modernc.org/sqlite
│   ├── (same files as pgxstore)
│   └── migrations/
│       └── 001_flowstate.sql
│
├── dynamostore/              # AWS DynamoDB via aws-sdk-go-v2
│   ├── (same files, adapted for DynamoDB)
│   └── table_definitions.go
│
├── redisbus/                 # Redis Streams EventBus via go-redis/v9
│   └── bus.go
│
├── natsbus/                  # NATS JetStream EventBus via nats.go
│   └── bus.go
│
├── asynqrunner/              # asynq-based ActivityRunner (persistent queue)
│   └── runner.go
│
├── goroutinerunner/          # In-process goroutine ActivityRunner
│   └── runner.go
│
├── testutil/                 # TDD helpers
│   ├── engine.go             # NewTestEngine(t)
│   ├── assertions.go         # AssertState, AssertEventChain, etc.
│   ├── activities.go         # CompleteActivity, mock helpers
│   ├── tasks.go              # CompleteTask helper
│   ├── time.go               # FakeClock + AdvanceTime
│   └── fixtures.go           # Example workflows (OrderWorkflow, ApprovalChain)
│
├── go.mod                    # Core module: stdlib only
├── go.sum
├── LICENSE
└── README.md
```

**Dependency isolation**: Core package = stdlib only. Each sub-package has its own import cost. Consumer only pays for what they import.

---

## 3. Core Types

### State

```go
type State struct {
    Name       string
    IsInitial  bool
    IsTerminal bool
    IsWait     bool   // workflow pauses here until task/signal/child
}
```

### Transition

```go
type Transition struct {
    Name       string
    Sources    []string       // one or more source states
    Target     string         // fixed target (empty if routed)
    Routes     []Route        // conditional targets (empty if fixed)
    Event      string         // event type emitted on success
    Guards     []Guard
    Activities []ActivityDef  // async work to dispatch
    TaskDef    *TaskDef       // if this transition creates a pending task
    ChildDef   *ChildDef      // if this transition spawns a child
    ChildrenDef *ChildrenDef  // if this transition spawns parallel children
    TriggerType TriggerType   // DIRECT, SIGNAL, TASK_COMPLETED, CHILD_COMPLETED, CHILDREN_JOINED, TIMEOUT
    TriggerKey  string        // signal name, task choice, child workflow type, etc.
}

type TriggerType string

const (
    TriggerDirect           TriggerType = "DIRECT"
    TriggerSignal           TriggerType = "SIGNAL"
    TriggerTaskCompleted    TriggerType = "TASK_COMPLETED"
    TriggerChildCompleted   TriggerType = "CHILD_COMPLETED"
    TriggerChildrenJoined   TriggerType = "CHILDREN_JOINED"
    TriggerTimeout          TriggerType = "TIMEOUT"
)
```

### DomainEvent

```go
type DomainEvent struct {
    ID             string
    AggregateType  string
    AggregateID    string
    WorkflowType   string
    WorkflowVersion int
    EventType      string
    CorrelationID  string
    CausationID    string
    ActorID        string
    StateBefore    map[string]any
    StateAfter     map[string]any
    TransitionName string
    Error          map[string]any
    Payload        map[string]any
    CreatedAt      time.Time
    SequenceNum    int64
}
```

### WorkflowInstance

```go
type WorkflowInstance struct {
    ID              string
    WorkflowType    string
    WorkflowVersion int
    AggregateType   string
    AggregateID     string
    CurrentState    string
    StateData       map[string]any
    CorrelationID   string
    IsStuck         bool
    StuckReason     string
    StuckAt         *time.Time
    RetryCount      int
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

### PendingTask

```go
type PendingTask struct {
    ID            string
    WorkflowType  string
    AggregateType string
    AggregateID   string
    CorrelationID string
    TaskType      string
    Description   string
    Options       []string
    AssignedTo    string
    Timeout       time.Duration
    ExpiresAt     time.Time
    CompletedAt   *time.Time
    Choice        string
    Status        string         // PENDING, COMPLETED, EXPIRED, CANCELLED
    CreatedAt     time.Time
}
```

### ChildRelation

```go
type ChildRelation struct {
    ID                  string
    GroupID             string         // siblings share this
    ParentWorkflowType  string
    ParentAggregateType string
    ParentAggregateID   string
    ChildWorkflowType   string
    ChildAggregateType  string
    ChildAggregateID    string
    CorrelationID       string
    Status              string         // RUNNING, COMPLETED
    ChildTerminalState  string
    JoinPolicy          string         // ALL, ANY:<state>, N:<count>
    CreatedAt           time.Time
    CompletedAt         *time.Time
}
```

### ActivityInvocation

```go
type ActivityInvocation struct {
    ID             string
    ActivityName   string
    WorkflowType   string
    AggregateType  string
    AggregateID    string
    CorrelationID  string
    Mode           DispatchMode       // FIRE_AND_FORGET, AWAIT_RESULT
    Input          ActivityInput
    RetryPolicy    *RetryPolicy
    ResultSignals  map[string]string  // result key → signal name
    Timeout        time.Duration
    Status         string             // SCHEDULED, RUNNING, COMPLETED, FAILED, TIMED_OUT
    Result         *ActivityResult
    Attempts       int
    MaxAttempts    int
    NextRetryAt    *time.Time
    ScheduledAt    time.Time
    StartedAt      *time.Time
    CompletedAt    *time.Time
}

type ActivityInput struct {
    WorkflowType  string
    AggregateType string
    AggregateID   string
    CorrelationID string
    Params        map[string]any
    ScheduledAt   time.Time
}

type ActivityResult struct {
    Output map[string]any
    Error  string
}

type DispatchMode string

const (
    FireAndForget DispatchMode = "FIRE_AND_FORGET"
    AwaitResult   DispatchMode = "AWAIT_RESULT"
)

type RetryPolicy struct {
    MaxAttempts    int
    InitialBackoff time.Duration
    MaxBackoff     time.Duration
    BackoffFactor  float64
}
```

### TransitionResult

```go
type TransitionResult struct {
    Instance             WorkflowInstance
    Event                DomainEvent
    PreviousState        string
    NewState             string
    TransitionName       string
    ActivitiesDispatched []string
    TaskCreated          *PendingTask
    ChildrenSpawned      []ChildRelation
    IsTerminal           bool
}
```

---

## 4. Interfaces

### Storage Interfaces

```go
// EventStore persists and queries immutable domain events.
type EventStore interface {
    Append(ctx context.Context, tx any, event DomainEvent) error
    ListByCorrelation(ctx context.Context, correlationID string) ([]DomainEvent, error)
    ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]DomainEvent, error)
}

// InstanceStore persists and queries workflow instances.
// Update MUST use optimistic locking (WHERE updated_at = $old).
// Returns ErrConcurrentModification if the row was changed since last read.
type InstanceStore interface {
    Get(ctx context.Context, aggregateType, aggregateID string) (*WorkflowInstance, error)
    Create(ctx context.Context, tx any, instance WorkflowInstance) error
    Update(ctx context.Context, tx any, instance WorkflowInstance) error
    ListStuck(ctx context.Context) ([]WorkflowInstance, error)
}

// TaskStore persists pending tasks for human-in-the-loop workflows.
type TaskStore interface {
    Create(ctx context.Context, tx any, task PendingTask) error
    Get(ctx context.Context, taskID string) (*PendingTask, error)
    GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]PendingTask, error)
    Complete(ctx context.Context, tx any, taskID, choice, actorID string) error
    ListPending(ctx context.Context) ([]PendingTask, error)
    ListExpired(ctx context.Context) ([]PendingTask, error)
}

// ChildStore tracks parent-child workflow relationships.
type ChildStore interface {
    Create(ctx context.Context, tx any, relation ChildRelation) error
    GetByChild(ctx context.Context, childAggregateType, childAggregateID string) (*ChildRelation, error)
    GetByParent(ctx context.Context, parentAggregateType, parentAggregateID string) ([]ChildRelation, error)
    GetByGroup(ctx context.Context, groupID string) ([]ChildRelation, error)
    Complete(ctx context.Context, tx any, childAggregateType, childAggregateID, terminalState string) error
}

// ActivityStore tracks dispatched activity invocations.
type ActivityStore interface {
    Create(ctx context.Context, tx any, invocation ActivityInvocation) error
    Get(ctx context.Context, invocationID string) (*ActivityInvocation, error)
    UpdateStatus(ctx context.Context, invocationID, status string, result *ActivityResult) error
    ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]ActivityInvocation, error)
    ListPending(ctx context.Context) ([]ActivityInvocation, error)
    ListFailed(ctx context.Context) ([]ActivityInvocation, error)
    ListRetryable(ctx context.Context) ([]ActivityInvocation, error)
}
```

### Infrastructure Interfaces

```go
// TxProvider manages database transactions.
// The tx handle is `any` to support pgx.Tx, sql.Tx, DynamoDB TransactWriteItems, etc.
type TxProvider interface {
    Begin(ctx context.Context) (tx any, err error)
    Commit(ctx context.Context, tx any) error
    Rollback(ctx context.Context, tx any) error
}

// EventBus publishes domain events to external subscribers.
type EventBus interface {
    Emit(ctx context.Context, event DomainEvent) error
}

// ActivityRunner dispatches activity invocations for async execution.
type ActivityRunner interface {
    Dispatch(ctx context.Context, invocation ActivityInvocation) error
}

// Activity performs non-deterministic work outside the workflow transaction.
// Can contain any code: API calls, DB writes, file I/O, network requests.
// flowstate does NOT recover or replay activity state on failure.
type Activity interface {
    Name() string
    Execute(ctx context.Context, input ActivityInput) (*ActivityResult, error)
}

// Clock provides deterministic time for the engine.
// Guards and conditions receive Clock.Now() via params["_now"].
type Clock interface {
    Now() time.Time
}
```

### Workflow Logic Interfaces

```go
// Guard checks preconditions before a transition. MUST be deterministic.
// Only read from aggregate and params. No API calls, no DB queries, no I/O.
// Return nil to pass. Return error to block (wrapped as ErrGuardFailed).
type Guard interface {
    Check(ctx context.Context, aggregate any, params map[string]any) error
}

// Condition evaluates a routing decision. MUST be deterministic.
// Only read from aggregate and params. No I/O.
type Condition interface {
    Evaluate(ctx context.Context, aggregate any, params map[string]any) (bool, error)
}
```

### Observability Interface

```go
// Hooks allows consumers to observe engine behavior.
// All methods MUST be non-blocking.
type Hooks interface {
    OnTransition(ctx context.Context, result TransitionResult, duration time.Duration)
    OnGuardFailed(ctx context.Context, workflowType, transitionName, guardName string, err error)
    OnActivityDispatched(ctx context.Context, invocation ActivityInvocation)
    OnActivityCompleted(ctx context.Context, invocation ActivityInvocation, result *ActivityResult)
    OnActivityFailed(ctx context.Context, invocation ActivityInvocation, err error)
    OnStuck(ctx context.Context, instance WorkflowInstance, reason string)
}

// NoopHooks is the default. Does nothing.
type NoopHooks struct{}
```

---

## 5. Fluent Builder API

```go
// Define starts building a workflow definition.
func Define(aggregateType, workflowType string) *DefBuilder

// Builder methods
func (b *DefBuilder) Version(v int) *DefBuilder
func (b *DefBuilder) States(states ...StateOption) *DefBuilder
func (b *DefBuilder) Transition(name string, opts ...TransitionOption) *DefBuilder
func (b *DefBuilder) Build() (*Definition, error)

// State options
func Initial(name string) StateOption
func State(name string) StateOption
func WaitState(name string) StateOption
func Terminal(name string) StateOption

// Transition options
func From(states ...string) TransitionOption
func To(state string) TransitionOption
func Route(routes ...RouteOption) TransitionOption
func Event(name string) TransitionOption
func Guards(guards ...Guard) TransitionOption
func Dispatch(activityName string) TransitionOption
func DispatchAndWait(activityName string, opts ...DispatchOption) TransitionOption
func EmitTask(def TaskDef) TransitionOption
func SpawnChild(def ChildDef) TransitionOption
func SpawnChildren(def ChildrenDef) TransitionOption
func OnSignal(signalName string) TransitionOption
func OnTaskCompleted(choice string) TransitionOption
func OnChildCompleted(workflowType, terminalState string) TransitionOption
func OnChildrenJoined(workflowType string, policy JoinPolicy) TransitionOption
func OnTimeout() TransitionOption
func AllowSelfTransition() TransitionOption

// Route options (conditional branching)
func When(condition Condition) *RouteBuilder
func (r *RouteBuilder) To(state string) RouteOption
func Default(state string) RouteOption

// Dispatch options (activity control)
func OnResult(key string) *ResultBuilder
func (r *ResultBuilder) Signal(signalName string) DispatchOption
func OnDispatchTimeout(d time.Duration) *TimeoutBuilder
func (t *TimeoutBuilder) Signal(signalName string) DispatchOption
func Retry(policy RetryPolicy) DispatchOption

// Join policies
func JoinAll() JoinPolicy
func JoinAny(terminalState string) JoinPolicy
func JoinN(n int) JoinPolicy
func (j JoinPolicy) CancelRemaining() JoinPolicy
```

---

## 6. Engine API

### Constructor

```go
func NewEngine(opts ...Option) *Engine

// Options
func WithEventStore(s EventStore) Option
func WithInstanceStore(s InstanceStore) Option
func WithTaskStore(s TaskStore) Option
func WithChildStore(s ChildStore) Option
func WithActivityStore(s ActivityStore) Option
func WithTxProvider(t TxProvider) Option
func WithEventBus(b EventBus) Option
func WithActivityRunner(r ActivityRunner) Option
func WithClock(c Clock) Option
func WithHooks(h Hooks) Option
func WithLogger(l *slog.Logger) Option
```

### Registration

```go
func (e *Engine) Register(def *Definition) error
func (e *Engine) RegisterActivity(activity Activity) error
```

### Six Ways to Advance a Workflow

```go
// 1. Direct transition (user action, API call)
func (e *Engine) Transition(ctx context.Context, aggregateType, aggregateID, transitionName, actorID string, params map[string]any) (*TransitionResult, error)

// 2. Complete a pending task (human decision)
func (e *Engine) CompleteTask(ctx context.Context, taskID, choice, actorID string, params map[string]any) (*TransitionResult, error)

// 3. Signal from external source (webhook, peer workflow, activity result)
func (e *Engine) Signal(ctx context.Context, input SignalInput) (*TransitionResult, error)

// 4. Child completion — handled internally when child reaches terminal state
// 5. Children joined — handled internally when join policy satisfied
// 6. Timeout — processed by consumer-run worker
func (e *Engine) ProcessTimeouts(ctx context.Context) (int, error)
```

### Query API

```go
func (e *Engine) GetInstance(ctx context.Context, aggregateType, aggregateID string) (*WorkflowInstance, error)
func (e *Engine) GetEventChain(ctx context.Context, correlationID string) ([]DomainEvent, error)
func (e *Engine) GetEventsByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]DomainEvent, error)
func (e *Engine) ListStuck(ctx context.Context) ([]WorkflowInstance, error)
func (e *Engine) ListPendingTasks(ctx context.Context) ([]PendingTask, error)
func (e *Engine) ListFailedActivities(ctx context.Context) ([]ActivityInvocation, error)

// Diagram export
func (e *Engine) MermaidDiagram(workflowType string) (string, error)
func (e *Engine) MermaidDiagramAll() (map[string]string, error)
```

### Lifecycle

```go
// Shutdown drains in-flight transitions and returns.
// Does NOT wait for dispatched activities (they're async).
func (e *Engine) Shutdown(ctx context.Context) error
```

---

## 7. Deterministic Execution & Activities

### The Rule

| Layer | Can do | Cannot do |
|---|---|---|
| **Workflow code** (Guards, Conditions) | Read aggregate state, read params, pure logic | API calls, DB queries, file I/O, `time.Now()`, random |
| **Activity code** | Anything: API calls, DB writes, HTTP, retries | Nothing restricted |

The workflow says **what** should happen. Activities **do** it.

### Deterministic Time

The engine injects `Clock.Now()` into params as `params["_now"]`. Guards read time from params, never from `time.Now()`:

```go
// In the guard:
func (g *WithinTimeWindowGuard) Check(ctx context.Context, aggregate any, params map[string]any) error {
    now := params["_now"].(time.Time)  // injected by engine
    // ... check against booking time window
}
```

In tests, `testutil.FakeClock` controls time:

```go
clock := testutil.NewFakeClock(time.Date(2026, 2, 13, 18, 0, 0, 0, time.UTC))
engine := flowstate.NewEngine(flowstate.WithClock(clock))
// ... later
clock.Advance(15 * time.Minute)
```

### Two Activity Dispatch Modes

**`Dispatch`** — fire-and-forget:

```go
flowstate.Dispatch("send_confirmation_email")
```

Activity runs async. If it fails, the engine records the failure but the workflow has already moved on.

**`DispatchAndWait`** — workflow pauses for result:

```go
flowstate.DispatchAndWait("process_stripe_charge",
    flowstate.OnResult("succeeded").Signal("payment_succeeded"),
    flowstate.OnResult("failed").Signal("payment_failed"),
    flowstate.Retry(flowstate.RetryPolicy{
        MaxAttempts:    3,
        InitialBackoff: 1 * time.Second,
        MaxBackoff:     30 * time.Second,
        BackoffFactor:  2.0,
    }),
    flowstate.OnDispatchTimeout(10*time.Minute).Signal("payment_timed_out"),
)
```

### Activity Lifecycle

```
Transition commits → INSERT activity_invocation (SCHEDULED)
  → Engine dispatches to ActivityRunner
    → Runner picks up → status RUNNING
      → activity.Execute(ctx, input)
        → SUCCESS: status COMPLETED, store result
          → IF AwaitResult: engine.Signal(parent, result key)
        → FAILURE:
          → IF retries remaining: status SCHEDULED, NextRetryAt set
          → IF retries exhausted: status FAILED
            → IF AwaitResult: engine.Signal(parent, "failed")
        → TIMEOUT: status TIMED_OUT
          → IF AwaitResult: engine.Signal(parent, "timed_out")
```

---

## 8. Conditional Routing

Transitions can have multiple targets. The engine evaluates conditions and picks the target at runtime:

```go
Transition("graduate",
    flowstate.From("ACTIVE_YOUNG_ADULT"),
    flowstate.Route(
        flowstate.When(&HasPaymentMethodCondition{}).To("ACTIVE_ADULT"),
        flowstate.When(&NoPaymentMethodCondition{}).To("SILENT_MEMBER"),
        flowstate.Default("SILENT_MEMBER"),
    ),
    flowstate.Event("AgeGraduated"),
    flowstate.Dispatch("recalculate_pricing"),
)
```

Execution order: **Route** (pick target) → **Guards** (can still reject) → **Transaction** → **Activities** → **Emit**.

Routes evaluated top-to-bottom, first match wins. `Default` is mandatory — `Build()` rejects routed transitions without a default.

---

## 9. Wait States & Pending Tasks

A state marked as `WaitState` means the workflow pauses until external input:

```go
flowstate.WaitState("AWAITING_TRAINER_DECISION")

Transition("trainer_cancel_init",
    flowstate.From("CONFIRMED"),
    flowstate.To("AWAITING_TRAINER_DECISION"),
    flowstate.Guards(&IsProfessionalGuard{}),
    flowstate.EmitTask(flowstate.TaskDef{
        Type:    "trainer_release_decision",
        Options: []string{"release", "hold"},
        Timeout: 60 * time.Minute,
    }),
)

Transition("release_to_public",
    flowstate.From("AWAITING_TRAINER_DECISION"),
    flowstate.To("CANCELLED"),
    flowstate.OnTaskCompleted("release"),
    flowstate.Dispatch("trigger_watcher"),
)

Transition("hold_for_trainer",
    flowstate.From("AWAITING_TRAINER_DECISION"),
    flowstate.To("TRAINER_HOLD"),
    flowstate.OnTaskCompleted("hold"),
)

Transition("decision_timeout",
    flowstate.From("AWAITING_TRAINER_DECISION"),
    flowstate.To("CANCELLED"),
    flowstate.OnTimeout(),
    flowstate.Dispatch("trigger_watcher"),
)
```

External code completes the task:

```go
engine.CompleteTask(ctx, taskID, "release", actorID, nil)
```

---

## 10. Signals

Signals are named events from external sources (webhooks, peer workflows, activity results):

```go
Transition("guest_paid",
    flowstate.From("COLLECTING"),
    flowstate.To("COLLECTING"),
    flowstate.OnSignal("guest_payment_received"),
    flowstate.Guards(&NotAllSharesCollectedGuard{}),
    flowstate.AllowSelfTransition(),
)

Transition("all_collected",
    flowstate.From("COLLECTING"),
    flowstate.To("COMPLETED"),
    flowstate.OnSignal("guest_payment_received"),
    flowstate.Guards(&AllSharesCollectedGuard{}),
)
```

Sending a signal:

```go
engine.Signal(ctx, flowstate.SignalInput{
    TargetAggregateType: "split_payment",
    TargetAggregateID:   splitPaymentID,
    SignalName:           "guest_payment_received",
    Payload:              map[string]any{"user": "B", "amount": 1000},
    ActorID:              actorID,
})
```

When multiple transitions match a signal, guards resolve the ambiguity. If zero or multiple guards pass, the engine returns `ErrNoMatchingSignal` or `ErrSignalAmbiguous`.

---

## 11. Child Workflows & Parallel Orchestration

### Single Child

```go
Transition("initiate_payment",
    flowstate.From("PENDING_PAYMENT"),
    flowstate.To("AWAITING_PAYMENT"),
    flowstate.SpawnChild(flowstate.ChildDef{
        WorkflowType: "payment",
        InputFrom:    "aggregate",
    }),
)

Transition("payment_succeeded",
    flowstate.From("AWAITING_PAYMENT"),
    flowstate.To("CONFIRMED"),
    flowstate.OnChildCompleted("payment", "SUCCEEDED"),
    flowstate.Dispatch("schedule_lighting"),
)
```

### Parallel Children (Fan-Out/Fan-In)

```go
Transition("send_split_invites",
    flowstate.From("HOST_PAID"),
    flowstate.To("COLLECTING"),
    flowstate.SpawnChildren(flowstate.ChildrenDef{
        WorkflowType: "guest_payment",
        InputsFn: func(aggregate any) []map[string]any {
            booking := aggregate.(*Booking)
            var inputs []map[string]any
            for _, guest := range booking.Guests {
                inputs = append(inputs, map[string]any{
                    "user_id": guest.ID, "amount": guest.Share,
                })
            }
            return inputs
        },
    }),
)

// Resume when all guests pay
Transition("all_done",
    flowstate.From("COLLECTING"),
    flowstate.To("COMPLETED"),
    flowstate.OnChildrenJoined("guest_payment", flowstate.JoinAll()),
    flowstate.Guards(&AllSucceededGuard{}),
)

// Resume on first failure
Transition("first_failure",
    flowstate.From("COLLECTING"),
    flowstate.To("FALLOUT"),
    flowstate.OnChildrenJoined("guest_payment", flowstate.JoinAny("FAILED")),
)
```

### Join Policies

| Policy | Behavior |
|---|---|
| `JoinAll()` | Wait for ALL children to reach any terminal state |
| `JoinAny(state)` | Resume when ANY child reaches the specified terminal state |
| `JoinN(n)` | Resume when N children reach any terminal state |
| `.CancelRemaining()` | After join policy satisfied, signal remaining children to cancel |

### Engine Logic on Child Completion

Every time a child reaches terminal:

1. Update `ChildRelation` → COMPLETED
2. Load siblings by `GroupID`
3. Evaluate `JoinPolicy`
4. If satisfied → collect `ChildrenResult` → fire parent's `OnChildrenJoined` transition
5. If not satisfied → wait for more children

---

## 12. Build-Time Validation

`Build()` performs graph analysis and rejects invalid definitions:

### Hard Errors (Build fails)

| Defect | Detection |
|---|---|
| No initial state | Zero states with `IsInitial` |
| Multiple initial states | More than one `IsInitial` |
| No terminal states | Zero states with `IsTerminal` |
| Unreachable state | BFS from initial — state not visited |
| Dead-end non-terminal | Non-terminal with zero outgoing transitions |
| Unknown state reference | Transition source/target not in state map |
| Routed transition missing Default | `Route()` without `Default()` |
| Wait state with no outgoing transitions | Wait state + zero exits |
| Duplicate transition name | Same name registered twice |
| Self-transition without `AllowSelfTransition()` | Source == target without explicit opt-in |

### Warnings (Build succeeds, accessible via `def.Warnings()`)

| Defect | Severity |
|---|---|
| No path from state X to any terminal | Warning |
| Wait state with no timeout transition | Warning |
| State reachable only via ForceState | Warning |

---

## 13. Workflow Versioning

```go
flowstate.Define("booking", "standard_booking").
    Version(2).
    States(...).
    Build()
```

Rules:

- `workflow_instances` stores `workflow_version`
- Multiple versions can be registered simultaneously
- New instances get the latest version
- In-flight instances continue on their registered version
- Engine loads definition matching `(workflow_type, version)` from the instance
- Consumer migrates in-flight instances via `ForceState` with version bump

---

## 14. Observability & Hooks

### Hooks Interface

```go
type Hooks interface {
    OnTransition(ctx context.Context, result TransitionResult, duration time.Duration)
    OnGuardFailed(ctx context.Context, workflowType, transitionName, guardName string, err error)
    OnActivityDispatched(ctx context.Context, invocation ActivityInvocation)
    OnActivityCompleted(ctx context.Context, invocation ActivityInvocation, result *ActivityResult)
    OnActivityFailed(ctx context.Context, invocation ActivityInvocation, err error)
    OnStuck(ctx context.Context, instance WorkflowInstance, reason string)
}

func WithHooks(h Hooks) Option  // defaults to NoopHooks
```

### Structured Logging Contract

| Event | Level | Key Fields |
|---|---|---|
| Transition succeeded | INFO | workflow_type, aggregate_id, from, to, transition, actor_id, duration_ms |
| Guard failed | WARN | workflow_type, aggregate_id, transition, guard_name, error |
| Activity dispatched | DEBUG | workflow_type, aggregate_id, activity_name, mode |
| Activity failed | ERROR | workflow_type, aggregate_id, activity_name, error, retry_count |
| Workflow stuck | ERROR | workflow_type, aggregate_id, state, reason |
| Admin force/rewind | WARN | workflow_type, aggregate_id, actor_id, reason, from, to |

---

## 15. Error Handling

### Sentinel Errors

```go
// Definition errors (Build time)
var (
    ErrNoInitialState        = errors.New("flowstate: no initial state defined")
    ErrMultipleInitialStates = errors.New("flowstate: multiple initial states")
    ErrNoTerminalStates      = errors.New("flowstate: no terminal states defined")
    ErrUnreachableState      = errors.New("flowstate: unreachable state detected")
    ErrDeadEndState          = errors.New("flowstate: non-terminal state with no outgoing transitions")
    ErrUnknownState          = errors.New("flowstate: transition references unknown state")
    ErrMissingDefault        = errors.New("flowstate: routed transition missing Default")
    ErrDuplicateTransition   = errors.New("flowstate: duplicate transition name")
)

// Runtime errors
var (
    ErrInstanceNotFound        = errors.New("flowstate: workflow instance not found")
    ErrInvalidTransition       = errors.New("flowstate: transition not valid from current state")
    ErrGuardFailed             = errors.New("flowstate: guard check failed")
    ErrNoMatchingRoute         = errors.New("flowstate: no condition matched and no default")
    ErrAlreadyTerminal         = errors.New("flowstate: workflow already in terminal state")
    ErrWorkflowStuck           = errors.New("flowstate: workflow is stuck")
    ErrConcurrentModification  = errors.New("flowstate: concurrent modification detected")
)

// Signal errors
var (
    ErrNoMatchingSignal  = errors.New("flowstate: no transition matches signal")
    ErrSignalAmbiguous   = errors.New("flowstate: multiple transitions match signal")
)

// Task errors
var (
    ErrTaskNotFound         = errors.New("flowstate: pending task not found")
    ErrTaskExpired          = errors.New("flowstate: task has expired")
    ErrTaskAlreadyCompleted = errors.New("flowstate: task already completed")
    ErrInvalidChoice        = errors.New("flowstate: choice not in task options")
)

// Activity errors
var (
    ErrActivityNotRegistered = errors.New("flowstate: activity not registered")
    ErrActivityTimeout       = errors.New("flowstate: activity timed out")
)
```

### GuardError (wraps guard details)

```go
type GuardError struct {
    GuardName string
    Reason    error
}

// Usage:
if errors.Is(err, flowstate.ErrGuardFailed) {
    var guardErr *flowstate.GuardError
    if errors.As(err, &guardErr) {
        log.Warn("guard failed", "guard", guardErr.GuardName, "reason", guardErr.Reason)
    }
}
```

---

## 16. Mermaid Diagram Export

Every definition auto-exports to Mermaid stateDiagram-v2:

```go
diagram, _ := engine.MermaidDiagram("standard_booking")
```

Output includes annotations:

- `[GuardName]` — guard check
- `⚡activity_name` — dispatched activity
- `◀signal:name` — triggered by signal
- `◀task:choice` — triggered by task completion
- `◀child:type[STATE]` — triggered by child workflow
- `📋task` — creates pending task
- `>>spawn:type` — spawns child workflow

Exposed via `engine.MermaidDiagram(workflowType)` and `engine.MermaidDiagramAll()`.

---

## 17. Admin Recovery

Three resolution paths for stuck workflows:

```go
// Retry: re-dispatch failed activities
engine.RetryActivities(ctx, aggregateType, aggregateID, actorID, reason)

// Force: manually set state, bypassing guards
engine.ForceState(ctx, aggregateType, aggregateID, targetState, actorID, reason, stateData)

// Rewind: load state snapshot from a previous event, re-transition from there
engine.Rewind(ctx, aggregateType, aggregateID, toEventID, actorID, reason)
```

All admin operations write dedicated events to the chain:

- `WorkflowActivitiesRetried`
- `WorkflowForceTransitioned`
- `WorkflowRewound`

Each records: admin actor ID, reason, previous state, new state.

---

## 18. Concurrency & Shutdown

### Aggregate-Level Serialization

`InstanceStore.Update()` uses optimistic concurrency control:

```
WHERE aggregate_type = $1 AND aggregate_id = $2 AND updated_at = $3
```

If 0 rows affected → `ErrConcurrentModification`. Consumer can retry.

### Concurrent Engine Access

- `Definition` is immutable after `Build()` — safe to share across goroutines
- `Engine` is safe for concurrent use — per-transition transactions, no global locks
- Activities receive copies of aggregate data, not shared references

### Graceful Shutdown

```go
engine.Shutdown(ctx)
```

- Stops accepting new transitions
- Drains in-flight transitions (waits for current transactions to commit)
- Does NOT wait for dispatched activities (they're async by design)
- Returns when done or when ctx is cancelled

---

## 19. TDD Strategy & Test Utilities

Every feature is implemented test-first using in-memory backends:

### Test Engine (One-Line Setup)

```go
func TestBookingWorkflow(t *testing.T) {
    engine := testutil.NewTestEngine(t)
    engine.Register(NewBookingWorkflow())
    engine.RegisterActivity(&MockStripeCharge{})

    // Create instance and transition
    result, err := engine.Transition(ctx, "booking", "b-1", "initiate_payment", "user-1", nil)
    require.NoError(t, err)
    assert.Equal(t, "AWAITING_PAYMENT", result.NewState)

    // Simulate activity completion
    testutil.CompleteActivity(t, engine, "process_stripe_charge", "b-1",
        &flowstate.ActivityResult{Output: map[string]any{"charge_id": "ch_xxx"}},
    )

    // Verify workflow advanced
    testutil.AssertState(t, engine, "booking", "b-1", "CONFIRMED")

    // Verify event chain
    testutil.AssertEventChain(t, engine, result.Event.CorrelationID,
        "PaymentInitiated", "BookingConfirmed",
    )
}
```

### Test Helpers

| Helper | Purpose |
|---|---|
| `testutil.NewTestEngine(t)` | Pre-wired engine: memstore + chanbus + memrunner + FakeClock |
| `testutil.CompleteActivity(t, engine, name, aggID, result)` | Simulate activity completion |
| `testutil.CompleteTask(t, engine, aggID, choice)` | Simulate human decision |
| `testutil.AdvanceTime(t, engine, duration)` | Advance FakeClock, trigger timeouts |
| `testutil.AssertState(t, engine, aggType, aggID, expected)` | Assert current state |
| `testutil.AssertEventChain(t, engine, corrID, ...eventTypes)` | Assert event sequence |
| `testutil.GetEventChain(t, engine, corrID)` | Fetch full chain for custom assertions |
| `testutil.AssertStuck(t, engine, aggType, aggID)` | Assert workflow is stuck |
| `testutil.AssertNotStuck(t, engine, aggType, aggID)` | Assert workflow is not stuck |

### Fixtures

`testutil/fixtures.go` ships pre-built example workflows consumers can study:

- `OrderWorkflow` — simple: Created → Paid → Shipped → Delivered
- `ApprovalChain` — human-in-the-loop: Submitted → ManagerReview → DirectorReview → Approved/Rejected
- `ParallelProcessing` — fan-out/fan-in: Start → Processing (3 children) → Complete

---

## 20. Reference Implementations

### Storage

| Sub-package | Backend | Dependency |
|---|---|---|
| `memstore/` | In-memory maps | stdlib only |
| `pgxstore/` | PostgreSQL | pgx/v5 |
| `sqlitestore/` | SQLite | modernc.org/sqlite |
| `dynamostore/` | AWS DynamoDB | aws-sdk-go-v2 |

### Event Bus

| Sub-package | Backend | Dependency |
|---|---|---|
| `chanbus/` | In-process Go channels | stdlib only |
| `redisbus/` | Redis Streams | go-redis/v9 |
| `natsbus/` | NATS JetStream | nats.go |

### Activity Runner

| Sub-package | Backend | Dependency |
|---|---|---|
| `memrunner/` | Synchronous in-memory | stdlib only |
| `goroutinerunner/` | In-process goroutines | stdlib only |
| `asynqrunner/` | asynq task queue | hibiken/asynq |

---

## 21. Database Schema

Provided by store implementations. Reference SQL (pgxstore):

```sql
-- workflow_instances: live state tracker
CREATE TABLE workflow_instances (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_type    VARCHAR(100) NOT NULL,
    workflow_version INT NOT NULL DEFAULT 1,
    aggregate_type   VARCHAR(50) NOT NULL,
    aggregate_id     UUID NOT NULL,
    current_state    VARCHAR(50) NOT NULL,
    state_data       JSONB NOT NULL DEFAULT '{}',
    correlation_id   UUID NOT NULL,
    is_stuck         BOOLEAN DEFAULT false,
    stuck_reason     TEXT,
    stuck_at         TIMESTAMPTZ,
    retry_count      INT DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (aggregate_type, aggregate_id)
);

CREATE INDEX idx_workflow_stuck ON workflow_instances (is_stuck) WHERE is_stuck = true;

-- domain_events: immutable event chain
CREATE TABLE domain_events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_type   VARCHAR(50) NOT NULL,
    aggregate_id     UUID NOT NULL,
    workflow_type    VARCHAR(100) NOT NULL,
    workflow_version INT NOT NULL,
    event_type       VARCHAR(100) NOT NULL,
    correlation_id   UUID NOT NULL,
    causation_id     UUID,
    actor_id         UUID NOT NULL,
    state_before     JSONB NOT NULL,
    state_after      JSONB NOT NULL,
    transition_name  VARCHAR(100),
    error            JSONB,
    payload          JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    sequence_num     BIGSERIAL
);

CREATE INDEX idx_domain_events_correlation ON domain_events (correlation_id, sequence_num);
CREATE INDEX idx_domain_events_aggregate ON domain_events (aggregate_type, aggregate_id, sequence_num);

-- pending_tasks: human-in-the-loop
CREATE TABLE pending_tasks (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_type    VARCHAR(100) NOT NULL,
    aggregate_type   VARCHAR(50) NOT NULL,
    aggregate_id     UUID NOT NULL,
    correlation_id   UUID NOT NULL,
    task_type        VARCHAR(100) NOT NULL,
    description      TEXT,
    options          JSONB NOT NULL DEFAULT '[]',
    assigned_to      UUID,
    timeout_duration INTERVAL,
    expires_at       TIMESTAMPTZ,
    completed_at     TIMESTAMPTZ,
    choice           VARCHAR(100),
    completed_by     UUID,
    status           VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_pending_tasks_status ON pending_tasks (status) WHERE status = 'PENDING';
CREATE INDEX idx_pending_tasks_aggregate ON pending_tasks (aggregate_type, aggregate_id);

-- child_relations: parent-child workflow links
CREATE TABLE child_relations (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id              UUID NOT NULL,
    parent_workflow_type  VARCHAR(100) NOT NULL,
    parent_aggregate_type VARCHAR(50) NOT NULL,
    parent_aggregate_id   UUID NOT NULL,
    child_workflow_type   VARCHAR(100) NOT NULL,
    child_aggregate_type  VARCHAR(50) NOT NULL,
    child_aggregate_id    UUID NOT NULL,
    correlation_id        UUID NOT NULL,
    status                VARCHAR(20) NOT NULL DEFAULT 'RUNNING',
    child_terminal_state  VARCHAR(50),
    join_policy           VARCHAR(50) NOT NULL DEFAULT 'ALL',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at          TIMESTAMPTZ
);

CREATE INDEX idx_child_relations_child ON child_relations (child_aggregate_type, child_aggregate_id);
CREATE INDEX idx_child_relations_parent ON child_relations (parent_aggregate_type, parent_aggregate_id);
CREATE INDEX idx_child_relations_group ON child_relations (group_id);

-- activity_invocations: dispatched async work
CREATE TABLE activity_invocations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_name    VARCHAR(100) NOT NULL,
    workflow_type    VARCHAR(100) NOT NULL,
    aggregate_type   VARCHAR(50) NOT NULL,
    aggregate_id     UUID NOT NULL,
    correlation_id   UUID NOT NULL,
    mode             VARCHAR(20) NOT NULL DEFAULT 'FIRE_AND_FORGET',
    input            JSONB NOT NULL DEFAULT '{}',
    retry_policy     JSONB,
    result_signals   JSONB,
    timeout_duration INTERVAL,
    status           VARCHAR(20) NOT NULL DEFAULT 'SCHEDULED',
    result           JSONB,
    attempts         INT DEFAULT 0,
    max_attempts     INT DEFAULT 1,
    next_retry_at    TIMESTAMPTZ,
    scheduled_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at       TIMESTAMPTZ,
    completed_at     TIMESTAMPTZ
);

CREATE INDEX idx_activity_status ON activity_invocations (status) WHERE status IN ('SCHEDULED', 'RUNNING');
CREATE INDEX idx_activity_aggregate ON activity_invocations (aggregate_type, aggregate_id);
CREATE INDEX idx_activity_retryable ON activity_invocations (next_retry_at) WHERE status = 'SCHEDULED' AND next_retry_at IS NOT NULL;

-- Security: domain_events INSERT + SELECT only
-- GRANT INSERT, SELECT ON domain_events TO app_role;
```

---

## 22. Complete Booking Example

```go
func NewBookingWorkflow() *flowstate.Definition {
    def, _ := flowstate.Define("booking", "standard_booking").
        Version(1).
        States(
            flowstate.Initial("PENDING_PAYMENT"),
            flowstate.WaitState("AWAITING_PAYMENT"),
            flowstate.State("CONFIRMED"),
            flowstate.State("CHECKED_IN"),
            flowstate.WaitState("AWAITING_TRAINER_DECISION"),
            flowstate.State("TRAINER_HOLD"),
            flowstate.Terminal("CANCELLED"),
            flowstate.Terminal("NO_SHOW"),
        ).
        // User initiates → dispatch payment activity
        Transition("initiate_payment",
            flowstate.From("PENDING_PAYMENT"),
            flowstate.To("AWAITING_PAYMENT"),
            flowstate.Event("PaymentInitiated"),
            flowstate.DispatchAndWait("process_stripe_charge",
                flowstate.OnResult("succeeded").Signal("payment_succeeded"),
                flowstate.OnResult("failed").Signal("payment_failed"),
                flowstate.Retry(flowstate.RetryPolicy{
                    MaxAttempts:    3,
                    InitialBackoff: 1 * time.Second,
                    MaxBackoff:     30 * time.Second,
                    BackoffFactor:  2.0,
                }),
                flowstate.OnDispatchTimeout(10*time.Minute).Signal("payment_timed_out"),
            ),
        ).
        // Payment succeeded → confirmed
        Transition("confirm",
            flowstate.From("AWAITING_PAYMENT"),
            flowstate.To("CONFIRMED"),
            flowstate.Event("BookingConfirmed"),
            flowstate.OnSignal("payment_succeeded"),
            flowstate.Dispatch("schedule_lighting"),
            flowstate.Dispatch("generate_credential"),
            flowstate.Dispatch("send_confirmation_email"),
        ).
        // Payment failed → cancelled
        Transition("payment_failed",
            flowstate.From("AWAITING_PAYMENT"),
            flowstate.To("CANCELLED"),
            flowstate.Event("BookingPaymentFailed"),
            flowstate.OnSignal("payment_failed"),
            flowstate.Dispatch("release_redis_lock"),
        ).
        // Payment timed out → cancelled
        Transition("payment_timed_out",
            flowstate.From("AWAITING_PAYMENT"),
            flowstate.To("CANCELLED"),
            flowstate.Event("BookingPaymentTimedOut"),
            flowstate.OnSignal("payment_timed_out"),
            flowstate.Dispatch("release_redis_lock"),
        ).
        // Check in
        Transition("check_in",
            flowstate.From("CONFIRMED"),
            flowstate.To("CHECKED_IN"),
            flowstate.Event("BookingCheckedIn"),
            flowstate.Guards(&WithinTimeWindowGuard{}),
            flowstate.Dispatch("log_attendance"),
        ).
        // User cancel
        Transition("cancel_user",
            flowstate.From("PENDING_PAYMENT", "CONFIRMED"),
            flowstate.To("CANCELLED"),
            flowstate.Event("BookingCancelled"),
            flowstate.Guards(&CancellationPolicyGuard{}),
            flowstate.Dispatch("initiate_refund"),
            flowstate.Dispatch("trigger_watcher"),
            flowstate.Dispatch("cancel_lighting"),
            flowstate.Dispatch("revoke_credential"),
        ).
        // Trainer cancel → wait for decision
        Transition("trainer_cancel_init",
            flowstate.From("CONFIRMED"),
            flowstate.To("AWAITING_TRAINER_DECISION"),
            flowstate.Event("TrainerCancelInitiated"),
            flowstate.Guards(&IsProfessionalGuard{}),
            flowstate.EmitTask(flowstate.TaskDef{
                Type:    "trainer_release_decision",
                Options: []string{"release", "hold"},
                Timeout: 60 * time.Minute,
            }),
        ).
        // Trainer decides: release
        Transition("release_to_public",
            flowstate.From("AWAITING_TRAINER_DECISION"),
            flowstate.To("CANCELLED"),
            flowstate.Event("BookingReleasedToPublic"),
            flowstate.OnTaskCompleted("release"),
            flowstate.Dispatch("trigger_watcher"),
            flowstate.Dispatch("cancel_lighting"),
            flowstate.Dispatch("revoke_credential"),
        ).
        // Trainer decides: hold
        Transition("hold_for_trainer",
            flowstate.From("AWAITING_TRAINER_DECISION"),
            flowstate.To("TRAINER_HOLD"),
            flowstate.Event("BookingTrainerHeld"),
            flowstate.OnTaskCompleted("hold"),
            flowstate.Dispatch("cancel_lighting"),
        ).
        // Trainer decision timeout → release
        Transition("trainer_decision_timeout",
            flowstate.From("AWAITING_TRAINER_DECISION"),
            flowstate.To("CANCELLED"),
            flowstate.Event("TrainerDecisionTimedOut"),
            flowstate.OnTimeout(),
            flowstate.Dispatch("trigger_watcher"),
            flowstate.Dispatch("cancel_lighting"),
        ).
        // Booking expired (10min payment timer)
        Transition("expire",
            flowstate.From("PENDING_PAYMENT"),
            flowstate.To("CANCELLED"),
            flowstate.Event("BookingExpired"),
            flowstate.OnTimeout(),
            flowstate.Dispatch("release_redis_lock"),
            flowstate.Dispatch("trigger_watcher"),
        ).
        // Ghost protocol: no-show
        Transition("mark_no_show",
            flowstate.From("CONFIRMED"),
            flowstate.To("NO_SHOW"),
            flowstate.Event("BookingNoShow"),
            flowstate.Dispatch("apply_no_show_penalty"),
        ).
        Build()
    return def
}
```

---

## Audit Compliance

This design was audited against 138 items across 9 categories (SOLID, Clean Code, Philosophy of Software Design, Pragmatic Programmer, DDD, 12-Factor, Go-Specific, Temporal/Cadence, Cross-Cutting). Score: **132/133 PASS (99.2%)** after incorporating 7 audit fixes:

1. `Hooks` interface for observability (9.2.2)
2. `Engine.Shutdown(ctx)` for graceful shutdown (6.4.1)
3. `RetryPolicy` for activity auto-retry (8.2.2)
4. `Version(n)` for workflow versioning (8.5.2, 8.5.3)
5. `Clock` interface for deterministic time (8.1.2)
6. Structured logging contract (9.2.1)
7. `ErrConcurrentModification` for optimistic locking (9.3.3)

All 10 critical path gates PASS. Full audit checklist: `docs/plans/2026-02-13-flowstate-audit-checklist.md`

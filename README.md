# flowstep <!-- omit in toc -->

A declarative workflow engine for Go. Define state machines with a fluent builder API, execute transitions with guards, signals, activities, and human-in-the-loop tasks. Core package has zero external dependencies.

The package requires a minimum version of Go 1.25.  

## Table of Content <!-- omit in toc -->
- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Core Concepts](#core-concepts)
  - [States](#states)
  - [Hierarchical States](#hierarchical-states)
  - [History States](#history-states)
  - [Transitions](#transitions)
  - [Guards](#guards)
  - [Conditional Routing](#conditional-routing)
  - [Signals](#signals)
  - [Wait States \& Tasks](#wait-states--tasks)
  - [Child Workflows](#child-workflows)
  - [Activities](#activities)
  - [Snapshot / Restore](#snapshot--restore)
- [Engine Configuration](#engine-configuration)
- [Adapters](#adapters)
  - [Storage](#storage)
  - [Event Bus](#event-bus)
  - [Activity Runner](#activity-runner)
  - [PostgreSQL Setup](#postgresql-setup)
  - [Asynq Activity Runner](#asynq-activity-runner)
- [Mermaid Diagrams](#mermaid-diagrams)
- [Testing](#testing)
  - [Fixture Workflows](#fixture-workflows)
- [Error Handling](#error-handling)
  - [Sentinel Errors](#sentinel-errors)
- [Examples](#examples)
- [License](#license)


## Features

- **Fluent builder DSL** for defining workflows as state machines
- **Hierarchical states** — compound/nested states with entry/exit activities, event bubbling, and LCA-based transitions
- **History states** — shallow (`[H]`) and deep (`[H*]`) history for resuming previous state within compound states
- **Guards** — deterministic precondition checks (no I/O)
- **Conditional routing** — branch transitions based on runtime conditions
- **Signals** — trigger transitions from external events (payments, webhooks)
- **Wait states & tasks** — human-in-the-loop decisions with timeouts
- **Child workflows** — single or parallel fan-out with join policies (ALL/ANY/N)
- **Activities** — async side-effects (fire-and-forget or await-result)
- **Entry/exit activities** — synchronous activities that run within the transition transaction when entering/exiting states
- **Deterministic side effects** — `engine.SideEffect()` executes a function once and persists its result as an event, enabling replay-safe UUID/timestamp/random generation
- **Immutable event chain** — every transition appends a domain event
- **Version coexistence** — multiple workflow versions run side by side
- **Observability hooks** — instrument transitions, guard failures, activity lifecycle
- **Snapshot/Restore** — point-in-time capture and restore of workflow instance state with definition hash validation
- **Admin recovery** — `ForceState` bypasses all rules for operational fixes
- **Mermaid export** — generate state diagrams from definitions, including nested compound states
- **Pluggable adapters** — swap storage, event bus, and activity runner implementations

## Installation

```bash
go get github.com/mawkeye/flowstep
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/mawkeye/flowstep"
    "github.com/mawkeye/flowstep/adapters/memstore"
)

func main() {
    // 1. Define a workflow
    def, err := flowstep.Define("order", "order_workflow").
        Version(1).
        States(
            flowstep.Initial("CREATED"),
            flowstep.State("PROCESSING"),
            flowstep.Terminal("DONE"),
            flowstep.Terminal("CANCELLED"),
        ).
        Transition("start_processing",
            flowstep.From("CREATED"),
            flowstep.To("PROCESSING"),
            flowstep.Event("OrderStarted"),
        ).
        Transition("complete",
            flowstep.From("PROCESSING"),
            flowstep.To("DONE"),
            flowstep.Event("OrderCompleted"),
        ).
        Transition("cancel",
            flowstep.From("CREATED", "PROCESSING"),
            flowstep.To("CANCELLED"),
            flowstep.Event("OrderCancelled"),
        ).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    // 2. Create an engine with in-memory adapters
    engine, err := flowstep.NewEngine(
        flowstep.WithEventStore(memstore.NewEventStore()),
        flowstep.WithInstanceStore(memstore.NewInstanceStore()),
        flowstep.WithTxProvider(memstore.NewTxProvider()),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer engine.Shutdown(context.Background())

    if err := engine.Register(def); err != nil {
        log.Fatal(err)
    }

    // 3. Execute transitions
    ctx := context.Background()

    result, err := engine.Transition(ctx, "order", "order-1", "start_processing", "user-1", nil)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("State: %s -> %s\n", result.PreviousState, result.NewState)
    // Output: State: CREATED -> PROCESSING

    result, err = engine.Transition(ctx, "order", "order-1", "complete", "user-1", nil)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("State: %s (terminal: %v)\n", result.NewState, result.IsTerminal)
    // Output: State: DONE (terminal: true)
}
```

## Core Concepts

### States

Every workflow has exactly one **initial** state and at least one **terminal** state. Intermediate states are declared with `State`. States that wait for human input use `WaitState`. All state factories accept optional `StateModifier` arguments for hierarchy and entry/exit activities.

```go
flowstep.Initial("SUBMITTED")
flowstep.State("IN_REVIEW")
flowstep.WaitState("PENDING_APPROVAL")  // pauses for a task
flowstep.Terminal("APPROVED")
flowstep.Terminal("REJECTED")
```

### Hierarchical States

Compound states model containment — a parent state contains child states. The workflow is always in a leaf state; entering a compound state means entering its initial child (recursively for deep hierarchies).

```go
def, _ := flowstep.Define("order", "order_workflow").
    Version(1).
    States(
        flowstep.Initial("CREATED"),
        // Compound state with two children
        flowstep.CompoundState("PROCESSING",
            flowstep.InitialChild("REVIEWING"),
            flowstep.EntryActivityOpt("start_processing"),
            flowstep.ExitActivityOpt("cleanup_processing"),
        ),
        flowstep.State("REVIEWING", flowstep.Parent("PROCESSING")),
        flowstep.State("APPROVED", flowstep.Parent("PROCESSING")),
        flowstep.Terminal("DONE"),
    ).
    // Transitions work with hierarchy
    Transition("begin", flowstep.From("CREATED"), flowstep.To("PROCESSING")).
    Transition("approve", flowstep.From("REVIEWING"), flowstep.To("APPROVED")).
    Transition("finish", flowstep.From("APPROVED"), flowstep.To("DONE")).
    Build()
```

Key behaviors:

- **Compound target resolution:** Transitioning to `PROCESSING` automatically enters `REVIEWING` (the initial child).
- **Entry/exit activities:** When entering a compound state subtree, entry activities run from ancestor to leaf. When exiting, exit activities run from leaf to ancestor. All execute synchronously within the transaction.
- **Event bubbling:** If no transition matches at the current leaf state, the engine checks ancestor states upward. First match wins.
- **LCA-based transitions:** The engine computes the Lowest Common Ancestor to determine which states to exit and enter. Only states between the source and LCA are exited, and states between the LCA and target are entered.
- **Dangling task cleanup:** Pending tasks for states in an exited subtree are automatically invalidated (requires `TaskInvalidator` on your `TaskStore`).
- **Savepoint support:** Entry activity failures roll back to a savepoint, preserving the pre-transition state (requires `SavepointProvider` on your `TxProvider`). Without savepoints, the instance is marked STUCK.

### History States

History states allow a compound state to "remember" which substate was last active. When re-entering with history, the engine resumes from the recorded state instead of the default `InitialChild`.

```go
// Shallow history: remembers the last direct child of the compound state
flowstep.Transition("resume",
    flowstep.From("IDLE"),
    flowstep.To("PROCESSING"),
    flowstep.WithHistory(flowstep.HistoryShallow),
)

// Deep history: remembers the last leaf (innermost active state)
flowstep.Transition("resume_deep",
    flowstep.From("IDLE"),
    flowstep.To("PROCESSING"),
    flowstep.WithHistory(flowstep.HistoryDeep),
)
```

Key behaviors:

- **Automatic recording:** History is recorded on every compound state exit — no opt-in per state needed.
- **Shallow (`[H]`):** Records the last direct child of the compound state. If that child is itself compound, its `InitialChild` is used (secondary resolution).
- **Deep (`[H*]`):** Records the innermost leaf state. Used directly as the re-entry target.
- **Fallback:** When no history is recorded (first entry or after `ForceState`), the engine falls back to `InitialChild` — identical to transitions without history.
- **`ForceState` clears history:** Admin recovery resets both shallow and deep history maps.
- **Mermaid annotation:** History-aware transitions are annotated with `[H]` (shallow) or `[H*]` (deep) in Mermaid diagrams.

### Transitions

A transition moves a workflow from one or more source states to a target state, optionally emitting a named event.

```go
flowstep.Transition("approve",
    flowstep.From("IN_REVIEW"),
    flowstep.To("APPROVED"),
    flowstep.Event("RequestApproved"),
)
```

A transition can originate from multiple states:

```go
flowstep.Transition("cancel",
    flowstep.From("SUBMITTED", "IN_REVIEW"),
    flowstep.To("CANCELLED"),
    flowstep.Event("RequestCancelled"),
)
```

### Guards

Guards are deterministic precondition checks that run before any I/O. If a guard returns an error, the transition is blocked and no event is emitted.

```go
type minimumAmountGuard struct {
    minimum float64
}

func (g *minimumAmountGuard) Check(ctx context.Context, aggregate any, params map[string]any) error {
    amount, ok := params["amount"].(float64)
    if !ok || amount < g.minimum {
        return fmt.Errorf("amount must be at least %.2f", g.minimum)
    }
    return nil
}

// Use in a transition:
flowstep.Transition("charge",
    flowstep.From("PENDING"),
    flowstep.To("CHARGED"),
    flowstep.Guards(&minimumAmountGuard{minimum: 1.00}),
)
```

Guard failures are wrapped as `flowstep.ErrGuardFailed` and can be checked with `errors.Is`.

Guards can optionally implement `NamedGuard` to provide human-readable names for observer events:

```go
type NamedGuard interface {
    Guard
    Name() string
}

// Example:
func (g *minimumAmountGuard) Name() string { return "minimum-amount" }
```

Guards that don't implement `NamedGuard` are identified by their Go type name in observer events.

### Conditional Routing

Route a transition to different target states based on runtime conditions:

```go
flowstep.Transition("evaluate",
    flowstep.From("SUBMITTED"),
    flowstep.Route(
        flowstep.When(&highValueCondition{}),
        flowstep.To("MANUAL_REVIEW"),
    ),
    flowstep.Route(
        flowstep.Default(),
        flowstep.To("AUTO_APPROVED"),
    ),
)
```

A `Condition` implements `Evaluate(ctx, aggregate, params) (bool, error)`. Routed transitions must include a `Default` branch.

### Signals

Signal-triggered transitions fire when an external system sends a named signal. This is useful for payment callbacks, webhook events, or any async notification.

```go
// Define signal-triggered transitions
flowstep.Transition("payment_ok",
    flowstep.From("AWAITING_PAYMENT"),
    flowstep.To("CONFIRMED"),
    flowstep.OnSignal("payment_succeeded"),
)

flowstep.Transition("payment_fail",
    flowstep.From("AWAITING_PAYMENT"),
    flowstep.To("CANCELLED"),
    flowstep.OnSignal("payment_failed"),
)

// Send a signal at runtime
result, err := engine.Signal(ctx, types.SignalInput{
    TargetAggregateType: "booking",
    TargetAggregateID:   "b-1",
    SignalName:          "payment_succeeded",
    ActorID:             "payment-system",
})
```

### Wait States & Tasks

Wait states pause a workflow until a human (or external system) completes a task. The task defines a set of choices; the chosen option determines which transition fires.

```go
// Emit a task when entering a wait state
flowstep.Transition("request_approval",
    flowstep.From("SUBMITTED"),
    flowstep.To("PENDING_APPROVAL"),
    flowstep.EmitTask(types.TaskDef{
        Type:        "approval",
        Description: "Approve or reject the request",
        Options:     []string{"approve", "reject"},
        Timeout:     24 * time.Hour,
    }),
)

// Task-completion-triggered transitions (one per option)
flowstep.Transition("approve",
    flowstep.From("PENDING_APPROVAL"),
    flowstep.To("APPROVED"),
    flowstep.OnTaskCompleted("approval"),
)
flowstep.Transition("reject",
    flowstep.From("PENDING_APPROVAL"),
    flowstep.To("REJECTED"),
    flowstep.OnTaskCompleted("approval"),
)

// Complete the task at runtime (choice matches a transition name)
result, err := engine.CompleteTask(ctx, taskID, "approve", "manager-1")
```

### Child Workflows

Spawn a single child workflow:

```go
flowstep.Transition("start_sub",
    flowstep.From("PROCESSING"),
    flowstep.To("WAITING_FOR_SUB"),
    flowstep.SpawnChild(types.ChildDef{
        WorkflowType: "sub_workflow",
        InputFrom:    "sub_input",
    }),
)

flowstep.Transition("sub_done",
    flowstep.From("WAITING_FOR_SUB"),
    flowstep.To("COMPLETED"),
    flowstep.OnChildCompleted("sub_workflow"),
)
```

Spawn parallel children with a join policy:

```go
flowstep.Transition("fan_out",
    flowstep.From("PROCESSING"),
    flowstep.To("WAITING_FOR_ALL"),
    flowstep.SpawnChildren(types.ChildrenDef{
        WorkflowType: "item_workflow",
        InputsFn:     func(agg any) []map[string]any { /* return one map per child */ },
        Join:         types.JoinAll(),             // wait for all children
        // Or: types.JoinAny("COMPLETED")          // resume when any child reaches COMPLETED
        // Or: types.JoinN(3)                      // resume when 3 children complete
    }),
)

flowstep.Transition("all_done",
    flowstep.From("WAITING_FOR_ALL"),
    flowstep.To("COMPLETED"),
    flowstep.OnChildrenJoined(),
)
```

### Activities

Activities perform async side-effects (API calls, emails, I/O). Two dispatch modes:

```go
// Fire-and-forget: transition completes immediately
flowstep.Dispatch("send_email")

// Await result: engine waits for the activity to finish
flowstep.DispatchAndWait("process_payment")
```

Implement the `Activity` interface:

```go
type sendEmailActivity struct{}

func (a *sendEmailActivity) Name() string { return "send_email" }
func (a *sendEmailActivity) Execute(ctx context.Context, input types.ActivityInput) (*types.ActivityResult, error) {
    // input.AggregateID, input.Params, etc.
    return &types.ActivityResult{Output: map[string]any{"sent": true}}, nil
}
```

Register activities with an `ActivityRunner`:

```go
runner := memrunner.New()
runner.Register(&sendEmailActivity{})

engine, _ := flowstep.NewEngine(
    // ...
    flowstep.WithActivityRunner(runner),
    flowstep.WithActivityStore(memstore.NewActivityStore()),
)
```

### Snapshot / Restore

Capture the complete runtime state of a workflow instance and restore it later — useful for backups, cloning, and cross-environment migration.

```go
ctx := context.Background()

// Capture a snapshot
snap, err := engine.Snapshot(ctx, "order", "order-1")
if err != nil {
    log.Fatal(err)
}

// Serialize for storage/transport
data, _ := json.Marshal(snap)

// Later: deserialize and restore to a different aggregate
var restored flowstep.Snapshot
json.Unmarshal(data, &restored)
restored.AggregateID = "order-1-clone"

if err := engine.Restore(ctx, restored); err != nil {
    log.Fatal(err)
}
// The restored instance can immediately participate in transitions
```

Key behaviors:

- **Complete state capture:** All runtime fields including `StateData`, `ShallowHistory`, `DeepHistory`, `ActiveInParallel`, `ParallelClock`, stuck state, and `CorrelationID`.
- **Definition validation:** Restore validates both `WorkflowVersion` and `DefinitionHash` against the registered compiled machine. Mismatches return `ErrSnapshotDefinitionMismatch`.
- **Create-only:** Restore rejects if an instance already exists for the target aggregate key (`ErrSnapshotInstanceExists`).
- **Value type:** `Snapshot` is a value type — copy and modify fields (e.g., `AggregateID`) before passing to `Restore` for clone scenarios.
- **Audit trail:** A `SnapshotRestored` domain event is persisted and emitted on successful restore.
- **Deep copy isolation:** All map and pointer fields are deep-copied — mutations to a snapshot never affect stored instances.

## Engine Configuration

The engine is configured with functional options. Three stores are required; everything else is optional.

```go
engine, err := flowstep.NewEngine(
    // Required
    flowstep.WithEventStore(eventStore),
    flowstep.WithInstanceStore(instanceStore),
    flowstep.WithTxProvider(txProvider),

    // Optional — enable features as needed
    flowstep.WithTaskStore(taskStore),           // for wait states & tasks
    flowstep.WithChildStore(childStore),         // for child workflows
    flowstep.WithActivityStore(activityStore),   // for activity tracking
    flowstep.WithEventBus(eventBus),             // for event publishing
    flowstep.WithActivityRunner(activityRunner), // for activity dispatch
    flowstep.WithClock(clock),                   // default: RealClock
    flowstep.WithObservers(observer),            // register observer adapters (optional)
)
```

## Adapters

### Storage

| Package | Backend | Interfaces |
|---------|---------|------------|
| `adapters/memstore` | In-memory | `EventStore`, `InstanceStore`, `TaskStore`, `ChildStore`, `ActivityStore`, `TxProvider` |
| `adapters/pgxstore` | PostgreSQL (pgx/v5) | `EventStore`, `InstanceStore`, `TaskStore`, `ChildStore`, `ActivityStore`, `TxProvider` |
| `adapters/sqlitestore` | SQLite | `EventStore`, `InstanceStore`, `TxProvider` |
| `adapters/dynamostore` | AWS DynamoDB | `EventStore`, `InstanceStore`, `TaskStore`, `ChildStore`, `ActivityStore`, `TxProvider` |

### Event Bus

| Package | Backend | Interface |
|---------|---------|-----------|
| `adapters/chanbus` | Go channels (in-memory) | `EventBus` |
| `adapters/redisbus` | Redis Streams | `EventBus` |
| `adapters/natsbus` | NATS JetStream | `EventBus` |

### Activity Runner

| Package | Backend | Interface |
|---------|---------|-----------|
| `adapters/memrunner` | Synchronous in-memory | `ActivityRunner` |
| `adapters/goroutinerunner` | Goroutine-per-activity | `ActivityRunner` |
| `adapters/asynqrunner` | Asynq (Redis task queue) | `ActivityRunner` |

### Observability

| Package | Description | Interfaces |
|---------|-------------|------------|
| `adapters/slogadapter` | Structured logging via `log/slog` | `TransitionObserver`, `GuardObserver`, `ActivityObserver`, `InfrastructureObserver` |

```go
import (
    "log/slog"
    "github.com/mawkeye/flowstep/adapters/slogadapter"
)

logger := slog.Default()
engine, _ := flowstep.NewEngine(
    // ... storage options ...
    flowstep.WithObservers(slogadapter.New(logger)),
)
```

### PostgreSQL Setup

```go
import (
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/mawkeye/flowstep/adapters/pgxstore"
)

pool, _ := pgxpool.New(ctx, "postgres://user:pass@localhost/mydb")

engine, _ := flowstep.NewEngine(
    flowstep.WithEventStore(pgxstore.NewEventStore(pool)),
    flowstep.WithInstanceStore(pgxstore.NewInstanceStore(pool, flowstep.ErrInstanceNotFound)),
    flowstep.WithTaskStore(pgxstore.NewTaskStore(pool, flowstep.ErrTaskNotFound)),
    flowstep.WithChildStore(pgxstore.NewChildStore(pool)),
    flowstep.WithActivityStore(pgxstore.NewActivityStore(pool)),
    flowstep.WithTxProvider(pgxstore.NewTxProvider(pool)),
)
```

The `pgxstore` package includes embedded SQL migrations (`pgxstore.Migrations`).

### Asynq Activity Runner

```go
import (
    "github.com/hibiken/asynq"
    "github.com/mawkeye/flowstep/adapters/asynqrunner"
)

// Producer side
client := asynq.NewClient(asynq.RedisClientOpt{Addr: "localhost:6379"})
runner := asynqrunner.New(client)

engine, _ := flowstep.NewEngine(
    // ...
    flowstep.WithActivityRunner(runner),
)

// Worker side
srv := asynq.NewServer(asynq.RedisClientOpt{Addr: "localhost:6379"}, asynq.Config{})
mux := asynq.NewServeMux()
mux.HandleFunc(
    asynqrunner.TaskType("send_email"),
    asynqrunner.HandleFunc(&sendEmailActivity{}),
)
srv.Run(mux)
```

## Mermaid Diagrams

Generate a Mermaid state diagram from any definition:

```go
import "github.com/mawkeye/flowstep/types"

def, _ := flowstep.Define("order", "order_workflow").
    // ... states and transitions ...
    Build()

fmt.Println(types.Mermaid(def))
```

Output:

```mermaid
stateDiagram-v2
    [*] --> CREATED
    CREATED --> PROCESSING : start_processing
    PROCESSING --> DONE : complete
    CREATED --> CANCELLED : cancel
    PROCESSING --> CANCELLED : cancel
    DONE --> [*]
    CANCELLED --> [*]
```

For hierarchical workflows, compound states are rendered with `state X { ... }` nesting:

```mermaid
stateDiagram-v2
    [*] --> CREATED
    CREATED --> PROCESSING : begin
    state PROCESSING {
        [*] --> REVIEWING
        REVIEWING --> APPROVED : approve
    }
    PROCESSING --> DONE : finish
    DONE --> [*]
```

## Testing

The `testutil` package provides a fully wired in-memory engine, a fake clock, and assertion helpers.

```go
import (
    "testing"
    "github.com/mawkeye/flowstep/testutil"
)

func TestMyWorkflow(t *testing.T) {
    te := testutil.NewTestEngine(t)

    def := buildMyWorkflow(t)
    if err := te.Engine.Register(def); err != nil {
        t.Fatal(err)
    }

    ctx := context.Background()

    // Execute transitions
    _, err := te.Engine.Transition(ctx, "order", "o-1", "start_processing", "user-1", nil)
    if err != nil {
        t.Fatal(err)
    }

    // Assert final state
    testutil.AssertState(t, te, "order", "o-1", "PROCESSING")

    // Assert event count
    testutil.AssertEventCount(t, te, "order", "o-1", 1)

    // Assert full event chain by correlation ID
    testutil.AssertEventChain(t, te, result.Event.CorrelationID,
        "OrderStarted",
    )

    // Manipulate time
    te.Clock.Advance(2 * time.Hour)
}
```

### Fixture Workflows

`testutil` includes pre-built workflows for common test scenarios:

```go
def := testutil.OrderWorkflow(t)       // CREATED -> PROCESSING -> DONE/CANCELLED
def := testutil.ApprovalWorkflow(t)    // SUBMITTED -> PENDING_APPROVAL -> APPROVED/REJECTED
```

## Error Handling

All errors are defined as sentinel values in the root package. Use `errors.Is` for matching:

```go
result, err := engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
if err != nil {
    switch {
    case errors.Is(err, flowstep.ErrGuardFailed):
        // Precondition not met — guard failure details are reported via
        // GuardObserver.OnGuardFailed notification is delivered to registered observers.
    case errors.Is(err, flowstep.ErrInvalidTransition):
        // Transition not valid from the current state
    case errors.Is(err, flowstep.ErrAlreadyTerminal):
        // Workflow has already reached a terminal state
    case errors.Is(err, flowstep.ErrConcurrentModification):
        // Optimistic lock conflict — retry
    case errors.Is(err, flowstep.ErrInstanceNotFound):
        // No workflow instance exists for this aggregate
    }
}
```

### Sentinel Errors

| Error | When |
|-------|------|
| `ErrInstanceNotFound` | No workflow instance for the given aggregate |
| `ErrInvalidTransition` | Transition not valid from current state |
| `ErrGuardFailed` | A guard precondition check failed |
| `ErrNoMatchingRoute` | No condition matched and no default route |
| `ErrAlreadyTerminal` | Workflow already in a terminal state |
| `ErrWorkflowStuck` | Workflow is stuck and cannot proceed |
| `ErrConcurrentModification` | Optimistic lock conflict |
| `ErrNoMatchingSignal` | No transition matches the signal |
| `ErrSignalAmbiguous` | Multiple transitions match the same signal |
| `ErrTaskNotFound` | Pending task does not exist |
| `ErrTaskExpired` | Task has passed its timeout |
| `ErrTaskAlreadyCompleted` | Task was already completed |
| `ErrInvalidChoice` | Choice not in the task's options list |
| `ErrActivityNotRegistered` | Activity name not registered with the runner |
| `ErrActivityTimeout` | Activity timed out before completing |
| `ErrActivityNotFound` | Activity invocation not found |
| `ErrSpawnCycle` | Cross-workflow spawn cycle detected during `Register()` |
| `ErrCompoundStateNoInitialChild` | Compound state has no `InitialChild` declared |
| `ErrOrphanedChild` | State references a non-existent parent |
| `ErrCircularHierarchy` | Circular parent-child hierarchy detected |
| `ErrSnapshotDefinitionMismatch` | Snapshot's definition hash or version doesn't match registered |
| `ErrSnapshotInstanceExists` | Instance already exists for the snapshot's aggregate key |
| `ErrEngineShutdown` | Engine has been shut down |


## Examples

The `examples/` folder contains standalone runnable programs covering all features from simple to complex:

| Example | Features |
|---------|----------|
| [`01-basic-linear`](examples/01-basic-linear/main.go) | Workflow definition, engine setup, basic transitions |
| [`02-guards-and-routing`](examples/02-guards-and-routing/main.go) | Guard preconditions, conditional routing with `When`/`Default` |
| [`03-signals`](examples/03-signals/main.go) | External signal triggers (`OnSignal`, `engine.Signal`) |
| [`04-wait-states-tasks`](examples/04-wait-states-tasks/main.go) | Human-in-the-loop approval with wait states and tasks |
| [`05-child-workflow`](examples/05-child-workflow/main.go) | Parent/child workflows with `SpawnChild` and `OnChildCompleted` |
| [`06-parallel-children-signals`](examples/06-parallel-children-signals/main.go) | Parallel fan-out with `JoinAll`, join policy, and signal-based confirmation |

Run any example from the module root:

```bash
go run ./examples/01-basic-linear/
go run ./examples/06-parallel-children-signals/
```

## License

This project is licensed under the [GNU General Public License v3.0](LICENSE).

```
flowstep - A declarative workflow engine for Go
Copyright (C) 2026

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU General Public License for more details.
```
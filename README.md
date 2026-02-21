# flowstate <!-- omit in toc -->

A declarative workflow engine for Go. Define state machines with a fluent builder API, execute transitions with guards, signals, activities, and human-in-the-loop tasks. Core package has zero external dependencies.

The package requires a minimum version of Go 1.25.  

## Table of Content <!-- omit in toc -->
- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Core Concepts](#core-concepts)
  - [States](#states)
  - [Transitions](#transitions)
  - [Guards](#guards)
  - [Conditional Routing](#conditional-routing)
  - [Signals](#signals)
  - [Wait States \& Tasks](#wait-states--tasks)
  - [Child Workflows](#child-workflows)
  - [Activities](#activities)
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
- [License](#license)


## Features

- **Fluent builder DSL** for defining workflows as state machines
- **Guards** — deterministic precondition checks (no I/O)
- **Conditional routing** — branch transitions based on runtime conditions
- **Signals** — trigger transitions from external events (payments, webhooks)
- **Wait states & tasks** — human-in-the-loop decisions with timeouts
- **Child workflows** — single or parallel fan-out with join policies (ALL/ANY/N)
- **Activities** — async side-effects (fire-and-forget or await-result)
- **Immutable event chain** — every transition appends a domain event
- **Version coexistence** — multiple workflow versions run side by side
- **Observability hooks** — instrument transitions, guard failures, activity lifecycle
- **Admin recovery** — `ForceState` bypasses all rules for operational fixes
- **Mermaid export** — generate state diagrams from definitions
- **Pluggable adapters** — swap storage, event bus, and activity runner implementations

## Installation

```bash
go get github.com/mawkeye/flowstate
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/mawkeye/flowstate"
    "github.com/mawkeye/flowstate/adapters/memstore"
)

func main() {
    // 1. Define a workflow
    def, err := flowstate.Define("order", "order_workflow").
        Version(1).
        States(
            flowstate.Initial("CREATED"),
            flowstate.State("PROCESSING"),
            flowstate.Terminal("DONE"),
            flowstate.Terminal("CANCELLED"),
        ).
        Transition("start_processing",
            flowstate.From("CREATED"),
            flowstate.To("PROCESSING"),
            flowstate.Event("OrderStarted"),
        ).
        Transition("complete",
            flowstate.From("PROCESSING"),
            flowstate.To("DONE"),
            flowstate.Event("OrderCompleted"),
        ).
        Transition("cancel",
            flowstate.From("CREATED", "PROCESSING"),
            flowstate.To("CANCELLED"),
            flowstate.Event("OrderCancelled"),
        ).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    // 2. Create an engine with in-memory adapters
    engine, err := flowstate.NewEngine(
        flowstate.WithEventStore(memstore.NewEventStore()),
        flowstate.WithInstanceStore(memstore.NewInstanceStore()),
        flowstate.WithTxProvider(memstore.NewTxProvider()),
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

Every workflow has exactly one **initial** state and at least one **terminal** state. Intermediate states are declared with `State`. States that wait for human input use `WaitState`.

```go
flowstate.Initial("SUBMITTED")
flowstate.State("IN_REVIEW")
flowstate.WaitState("PENDING_APPROVAL")  // pauses for a task
flowstate.Terminal("APPROVED")
flowstate.Terminal("REJECTED")
```

### Transitions

A transition moves a workflow from one or more source states to a target state, optionally emitting a named event.

```go
flowstate.Transition("approve",
    flowstate.From("IN_REVIEW"),
    flowstate.To("APPROVED"),
    flowstate.Event("RequestApproved"),
)
```

A transition can originate from multiple states:

```go
flowstate.Transition("cancel",
    flowstate.From("SUBMITTED", "IN_REVIEW"),
    flowstate.To("CANCELLED"),
    flowstate.Event("RequestCancelled"),
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
flowstate.Transition("charge",
    flowstate.From("PENDING"),
    flowstate.To("CHARGED"),
    flowstate.Guards(&minimumAmountGuard{minimum: 1.00}),
)
```

Guard failures are wrapped as `flowstate.ErrGuardFailed` and can be checked with `errors.Is`.

### Conditional Routing

Route a transition to different target states based on runtime conditions:

```go
flowstate.Transition("evaluate",
    flowstate.From("SUBMITTED"),
    flowstate.Route(
        flowstate.When(&highValueCondition{}),
        flowstate.To("MANUAL_REVIEW"),
    ),
    flowstate.Route(
        flowstate.Default(),
        flowstate.To("AUTO_APPROVED"),
    ),
)
```

A `Condition` implements `Evaluate(ctx, aggregate, params) (bool, error)`. Routed transitions must include a `Default` branch.

### Signals

Signal-triggered transitions fire when an external system sends a named signal. This is useful for payment callbacks, webhook events, or any async notification.

```go
// Define signal-triggered transitions
flowstate.Transition("payment_ok",
    flowstate.From("AWAITING_PAYMENT"),
    flowstate.To("CONFIRMED"),
    flowstate.OnSignal("payment_succeeded"),
)

flowstate.Transition("payment_fail",
    flowstate.From("AWAITING_PAYMENT"),
    flowstate.To("CANCELLED"),
    flowstate.OnSignal("payment_failed"),
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
flowstate.Transition("request_approval",
    flowstate.From("SUBMITTED"),
    flowstate.To("PENDING_APPROVAL"),
    flowstate.EmitTask(types.TaskDef{
        Type:        "approval",
        Description: "Approve or reject the request",
        Options:     []string{"approve", "reject"},
        Timeout:     24 * time.Hour,
    }),
)

// Task-completion-triggered transitions (one per option)
flowstate.Transition("approve",
    flowstate.From("PENDING_APPROVAL"),
    flowstate.To("APPROVED"),
    flowstate.OnTaskCompleted("approval"),
)
flowstate.Transition("reject",
    flowstate.From("PENDING_APPROVAL"),
    flowstate.To("REJECTED"),
    flowstate.OnTaskCompleted("approval"),
)

// Complete the task at runtime (choice matches a transition name)
result, err := engine.CompleteTask(ctx, taskID, "approve", "manager-1")
```

### Child Workflows

Spawn a single child workflow:

```go
flowstate.Transition("start_sub",
    flowstate.From("PROCESSING"),
    flowstate.To("WAITING_FOR_SUB"),
    flowstate.SpawnChild(types.ChildDef{
        WorkflowType: "sub_workflow",
        InputFrom:    "sub_input",
    }),
)

flowstate.Transition("sub_done",
    flowstate.From("WAITING_FOR_SUB"),
    flowstate.To("COMPLETED"),
    flowstate.OnChildCompleted("sub_workflow"),
)
```

Spawn parallel children with a join policy:

```go
flowstate.Transition("fan_out",
    flowstate.From("PROCESSING"),
    flowstate.To("WAITING_FOR_ALL"),
    flowstate.SpawnChildren(types.ChildrenDef{
        WorkflowType: "item_workflow",
        InputsFn:     func(agg any) []map[string]any { /* return one map per child */ },
        Join:         types.JoinAll(),             // wait for all children
        // Or: types.JoinAny("COMPLETED")          // resume when any child reaches COMPLETED
        // Or: types.JoinN(3)                      // resume when 3 children complete
    }),
)

flowstate.Transition("all_done",
    flowstate.From("WAITING_FOR_ALL"),
    flowstate.To("COMPLETED"),
    flowstate.OnChildrenJoined(),
)
```

### Activities

Activities perform async side-effects (API calls, emails, I/O). Two dispatch modes:

```go
// Fire-and-forget: transition completes immediately
flowstate.Dispatch("send_email")

// Await result: engine waits for the activity to finish
flowstate.DispatchAndWait("process_payment")
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

engine, _ := flowstate.NewEngine(
    // ...
    flowstate.WithActivityRunner(runner),
    flowstate.WithActivityStore(memstore.NewActivityStore()),
)
```

## Engine Configuration

The engine is configured with functional options. Three stores are required; everything else is optional.

```go
engine, err := flowstate.NewEngine(
    // Required
    flowstate.WithEventStore(eventStore),
    flowstate.WithInstanceStore(instanceStore),
    flowstate.WithTxProvider(txProvider),

    // Optional — enable features as needed
    flowstate.WithTaskStore(taskStore),           // for wait states & tasks
    flowstate.WithChildStore(childStore),         // for child workflows
    flowstate.WithActivityStore(activityStore),   // for activity tracking
    flowstate.WithEventBus(eventBus),             // for event publishing
    flowstate.WithActivityRunner(activityRunner), // for activity dispatch
    flowstate.WithClock(clock),                   // default: RealClock
    flowstate.WithHooks(hooks),                   // default: NoopHooks
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

### PostgreSQL Setup

```go
import (
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/mawkeye/flowstate/adapters/pgxstore"
)

pool, _ := pgxpool.New(ctx, "postgres://user:pass@localhost/mydb")

engine, _ := flowstate.NewEngine(
    flowstate.WithEventStore(pgxstore.NewEventStore(pool)),
    flowstate.WithInstanceStore(pgxstore.NewInstanceStore(pool, flowstate.ErrInstanceNotFound)),
    flowstate.WithTaskStore(pgxstore.NewTaskStore(pool, flowstate.ErrTaskNotFound)),
    flowstate.WithChildStore(pgxstore.NewChildStore(pool)),
    flowstate.WithActivityStore(pgxstore.NewActivityStore(pool)),
    flowstate.WithTxProvider(pgxstore.NewTxProvider(pool)),
)
```

The `pgxstore` package includes embedded SQL migrations (`pgxstore.Migrations`).

### Asynq Activity Runner

```go
import (
    "github.com/hibiken/asynq"
    "github.com/mawkeye/flowstate/adapters/asynqrunner"
)

// Producer side
client := asynq.NewClient(asynq.RedisClientOpt{Addr: "localhost:6379"})
runner := asynqrunner.New(client)

engine, _ := flowstate.NewEngine(
    // ...
    flowstate.WithActivityRunner(runner),
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
def, _ := flowstate.Define("order", "order_workflow").
    // ... states and transitions ...
    Build()

fmt.Println(def.Mermaid())
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

## Testing

The `testutil` package provides a fully wired in-memory engine, a fake clock, and assertion helpers.

```go
import (
    "testing"
    "github.com/mawkeye/flowstate/testutil"
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
    case errors.Is(err, flowstate.ErrGuardFailed):
        // Precondition not met — inspect the GuardError for details
        var ge *flowstate.GuardError
        if errors.As(err, &ge) {
            fmt.Printf("Guard %q failed: %v\n", ge.GuardName, ge.Reason)
        }
    case errors.Is(err, flowstate.ErrInvalidTransition):
        // Transition not valid from the current state
    case errors.Is(err, flowstate.ErrAlreadyTerminal):
        // Workflow has already reached a terminal state
    case errors.Is(err, flowstate.ErrConcurrentModification):
        // Optimistic lock conflict — retry
    case errors.Is(err, flowstate.ErrInstanceNotFound):
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
| `ErrConcurrentModification` | Optimistic lock conflict |
| `ErrNoMatchingSignal` | No transition matches the signal |
| `ErrTaskNotFound` | Pending task does not exist |
| `ErrTaskExpired` | Task has passed its timeout |
| `ErrTaskAlreadyCompleted` | Task was already completed |
| `ErrInvalidChoice` | Choice not in the task's options list |
| `ErrActivityNotRegistered` | Activity name not registered with the runner |
| `ErrEngineShutdown` | Engine has been shut down |


## License

This project is licensed under the [GNU General Public License v3.0](LICENSE).

```
Court Command - Multi-tenant sports facility management platform
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
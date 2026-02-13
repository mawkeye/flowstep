# flowstate Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build `github.com/mawkeye/flowstate` — a standalone, reusable Go workflow engine package with deterministic execution, pluggable persistence, async activities, child workflows, and TDD-first test utilities.

**Architecture:** Fluent builder for declarative workflow definitions. Engine with pluggable stores (EventStore, InstanceStore, TaskStore, ChildStore, ActivityStore), event bus, activity runner, clock, and hooks. Core package has zero external dependencies (stdlib only). Reference implementations in sub-packages (pgxstore, sqlitestore, dynamostore, redisbus, natsbus, asynqrunner).

**Tech Stack:** Go 1.23+, pgx/v5, modernc.org/sqlite, aws-sdk-go-v2, go-redis/v9, nats.go, hibiken/asynq. TDD with testify.

**Design Doc:** `docs/plans/2026-02-13-flowstate-design.md` (1470 lines, 22 sections)

**Phases:**
1. Repository Setup & Core Types (Tasks 1-3)
2. Builder & Validation (Tasks 4-6)
3. Engine — Basic Transitions (Tasks 7-10)
4. Mermaid Export (Task 11)
5. Activities & Retry (Tasks 12-15)
6. Signals (Task 16)
7. Wait States & Pending Tasks (Tasks 17-18)
8. Conditional Routing (Task 19)
9. Child Workflows (Tasks 20-22)
10. Parallel Children & Join Policies (Tasks 23-24)
11. Workflow Versioning (Task 25)
12. Observability — Hooks & Logging (Task 26)
13. Admin Recovery (Task 27)
14. Concurrency & Shutdown (Task 28)
15. Test Utilities (Task 29)
16. Reference Implementations — pgxstore (Tasks 30-31)
17. Reference Implementations — sqlitestore (Task 32)
18. Reference Implementations — dynamostore (Task 33)
19. Reference Implementations — redisbus (Task 34)
20. Reference Implementations — natsbus (Task 35)
21. Reference Implementations — asynqrunner & goroutinerunner (Task 36)
22. Integration Tests & Fixtures (Task 37)

---

## Phase 1: Repository Setup & Core Types

### Task 1: Initialize Repository

**Files:**
- Create: `~/workspace/github.com/mawkeye/flowstate/go.mod`
- Create: `~/workspace/github.com/mawkeye/flowstate/doc.go`
- Create: `~/workspace/github.com/mawkeye/flowstate/.gitignore`
- Create: `~/workspace/github.com/mawkeye/flowstate/LICENSE`

**Step 1: Create directory and initialize Go module**

```bash
mkdir -p ~/workspace/github.com/mawkeye/flowstate
cd ~/workspace/github.com/mawkeye/flowstate
git init
go mod init github.com/mawkeye/flowstate
```

**Step 2: Create doc.go**

```go
// Package flowstate provides a declarative workflow engine for Go.
//
// Workflows are defined as state machines using a fluent builder API.
// The engine handles state transitions, guard evaluation, activity dispatch,
// and immutable event chain persistence.
//
// Core package has zero external dependencies (stdlib only).
// Persistence, event bus, and activity runner are pluggable via interfaces.
// Reference implementations are provided in sub-packages.
//
// Key concepts:
//   - Definition: immutable workflow structure (states, transitions)
//   - Guard: deterministic precondition check (no I/O)
//   - Activity: async non-deterministic work (API calls, I/O allowed)
//   - DomainEvent: immutable record of a state transition
//   - WorkflowInstance: live state tracker for a workflow
//
// Example:
//
//	def, _ := flowstate.Define("order", "standard_order").
//	    Version(1).
//	    States(
//	        flowstate.Initial("CREATED"),
//	        flowstate.State("PAID"),
//	        flowstate.Terminal("SHIPPED"),
//	    ).
//	    Transition("pay",
//	        flowstate.From("CREATED"),
//	        flowstate.To("PAID"),
//	        flowstate.Event("OrderPaid"),
//	    ).
//	    Transition("ship",
//	        flowstate.From("PAID"),
//	        flowstate.To("SHIPPED"),
//	        flowstate.Event("OrderShipped"),
//	    ).
//	    Build()
package flowstate
```

**Step 3: Create .gitignore**

```
# Binaries
*.exe
*.dll
*.so
*.dylib

# Test
*.test
*.out
coverage.html

# IDE
.idea/
.vscode/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db
```

**Step 4: Commit**

```bash
git add -A
git commit -m "chore: initialize flowstate Go module"
```

---

### Task 2: Core Types — Events, Instances, Errors

**Files:**
- Create: `event.go`
- Create: `instance.go`
- Create: `result.go`
- Create: `errors.go`
- Test: `errors_test.go`

**Step 1: Write the failing test for error types**

```go
// errors_test.go
package flowstate

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	// Verify sentinel errors are distinct
	sentinels := []error{
		ErrNoInitialState,
		ErrMultipleInitialStates,
		ErrNoTerminalStates,
		ErrUnreachableState,
		ErrDeadEndState,
		ErrUnknownState,
		ErrMissingDefault,
		ErrDuplicateTransition,
		ErrInstanceNotFound,
		ErrInvalidTransition,
		ErrGuardFailed,
		ErrNoMatchingRoute,
		ErrAlreadyTerminal,
		ErrWorkflowStuck,
		ErrConcurrentModification,
		ErrNoMatchingSignal,
		ErrSignalAmbiguous,
		ErrTaskNotFound,
		ErrTaskExpired,
		ErrTaskAlreadyCompleted,
		ErrInvalidChoice,
		ErrActivityNotRegistered,
		ErrActivityTimeout,
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel errors %d and %d should be distinct", i, j)
			}
		}
	}
}

func TestGuardError(t *testing.T) {
	inner := fmt.Errorf("payment not complete")
	err := &GuardError{GuardName: "PaymentCompleteGuard", Reason: inner}

	if !errors.Is(err, ErrGuardFailed) {
		t.Error("GuardError should match ErrGuardFailed via errors.Is")
	}

	var guardErr *GuardError
	if !errors.As(err, &guardErr) {
		t.Error("should unwrap to *GuardError")
	}
	if guardErr.GuardName != "PaymentCompleteGuard" {
		t.Errorf("expected PaymentCompleteGuard, got %s", guardErr.GuardName)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./... -run TestSentinelErrors -v
```

Expected: FAIL — types not defined.

**Step 3: Implement errors.go**

```go
// errors.go
package flowstate

import "errors"

// Definition errors (Build time).
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

// Runtime errors.
var (
	ErrInstanceNotFound       = errors.New("flowstate: workflow instance not found")
	ErrInvalidTransition      = errors.New("flowstate: transition not valid from current state")
	ErrGuardFailed            = errors.New("flowstate: guard check failed")
	ErrNoMatchingRoute        = errors.New("flowstate: no condition matched and no default")
	ErrAlreadyTerminal        = errors.New("flowstate: workflow already in terminal state")
	ErrWorkflowStuck          = errors.New("flowstate: workflow is stuck")
	ErrConcurrentModification = errors.New("flowstate: concurrent modification detected")
)

// Signal errors.
var (
	ErrNoMatchingSignal = errors.New("flowstate: no transition matches signal")
	ErrSignalAmbiguous  = errors.New("flowstate: multiple transitions match signal")
)

// Task errors.
var (
	ErrTaskNotFound         = errors.New("flowstate: pending task not found")
	ErrTaskExpired          = errors.New("flowstate: task has expired")
	ErrTaskAlreadyCompleted = errors.New("flowstate: task already completed")
	ErrInvalidChoice        = errors.New("flowstate: choice not in task options")
)

// Activity errors.
var (
	ErrActivityNotRegistered = errors.New("flowstate: activity not registered")
	ErrActivityTimeout       = errors.New("flowstate: activity timed out")
)

// GuardError wraps a guard failure with the guard name and reason.
type GuardError struct {
	GuardName string
	Reason    error
}

func (e *GuardError) Error() string {
	return "flowstate: guard " + e.GuardName + " failed: " + e.Reason.Error()
}

func (e *GuardError) Is(target error) bool {
	return target == ErrGuardFailed
}

func (e *GuardError) Unwrap() error {
	return e.Reason
}
```

**Step 4: Implement event.go**

```go
// event.go
package flowstate

import "time"

// DomainEvent is the immutable record of a state transition.
type DomainEvent struct {
	ID              string
	AggregateType   string
	AggregateID     string
	WorkflowType    string
	WorkflowVersion int
	EventType       string
	CorrelationID   string
	CausationID     string
	ActorID         string
	StateBefore     map[string]any
	StateAfter      map[string]any
	TransitionName  string
	Error           map[string]any
	Payload         map[string]any
	CreatedAt       time.Time
	SequenceNum     int64
}
```

**Step 5: Implement instance.go**

```go
// instance.go
package flowstate

import "time"

// WorkflowInstance tracks the live state of a workflow.
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

**Step 6: Implement result.go**

```go
// result.go
package flowstate

// TransitionResult is returned by every successful state advancement.
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

**Step 7: Run tests**

```bash
go test ./... -v
```

Expected: PASS (result.go will have compile errors for PendingTask/ChildRelation — add forward declarations or stubs).

Note: `PendingTask` and `ChildRelation` don't exist yet. Add minimal stub types to result.go or create task.go and child.go with just the type declarations. We'll flesh them out later.

**Step 8: Commit**

```bash
git add -A
git commit -m "feat: add core types — DomainEvent, WorkflowInstance, TransitionResult, sentinel errors"
```

---

### Task 3: Core Types — Tasks, Children, Activities, Signals

**Files:**
- Create: `task.go`
- Create: `child.go`
- Create: `activity.go`
- Create: `signal.go`

**Step 1: Implement task.go**

```go
// task.go
package flowstate

import "time"

// TaskDef defines a pending task created when entering a wait state.
type TaskDef struct {
	Type        string
	Description string
	Options     []string
	Timeout     time.Duration
}

// PendingTask is a human-in-the-loop decision awaiting completion.
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
	CompletedBy   string
	Status        string
	CreatedAt     time.Time
}

// Task statuses.
const (
	TaskStatusPending   = "PENDING"
	TaskStatusCompleted = "COMPLETED"
	TaskStatusExpired   = "EXPIRED"
	TaskStatusCancelled = "CANCELLED"
)
```

**Step 2: Implement child.go**

```go
// child.go
package flowstate

import "time"

// ChildDef defines a single child workflow to spawn.
type ChildDef struct {
	WorkflowType string
	InputFrom    string
}

// ChildrenDef defines parallel child workflows to spawn.
type ChildrenDef struct {
	WorkflowType string
	InputsFn     func(aggregate any) []map[string]any
}

// ChildRelation tracks a parent-child workflow relationship.
type ChildRelation struct {
	ID                  string
	GroupID             string
	ParentWorkflowType  string
	ParentAggregateType string
	ParentAggregateID   string
	ChildWorkflowType   string
	ChildAggregateType  string
	ChildAggregateID    string
	CorrelationID       string
	Status              string
	ChildTerminalState  string
	JoinPolicy          string
	CreatedAt           time.Time
	CompletedAt         *time.Time
}

// JoinPolicy defines when a parent should resume after spawning parallel children.
type JoinPolicy struct {
	Mode            string // ALL, ANY, N
	TerminalState   string // for ANY mode
	Count           int    // for N mode
	CancelSiblings  bool
}

// JoinAll waits for ALL children to reach any terminal state.
func JoinAll() JoinPolicy {
	return JoinPolicy{Mode: "ALL"}
}

// JoinAny resumes when ANY child reaches the specified terminal state.
func JoinAny(terminalState string) JoinPolicy {
	return JoinPolicy{Mode: "ANY", TerminalState: terminalState}
}

// JoinN resumes when N children reach any terminal state.
func JoinN(n int) JoinPolicy {
	return JoinPolicy{Mode: "N", Count: n}
}

// CancelRemaining signals remaining children to cancel after join is satisfied.
func (j JoinPolicy) CancelRemaining() JoinPolicy {
	j.CancelSiblings = true
	return j
}
```

**Step 3: Implement activity.go**

```go
// activity.go
package flowstate

import (
	"context"
	"time"
)

// DispatchMode determines how an activity is dispatched.
type DispatchMode string

const (
	FireAndForget DispatchMode = "FIRE_AND_FORGET"
	AwaitResult   DispatchMode = "AWAIT_RESULT"
)

// Activity performs non-deterministic work outside the workflow transaction.
// Can contain any code: API calls, DB writes, file I/O, network requests.
// flowstate does NOT recover or replay activity state on failure.
type Activity interface {
	Name() string
	Execute(ctx context.Context, input ActivityInput) (*ActivityResult, error)
}

// ActivityInput is passed to Activity.Execute.
type ActivityInput struct {
	WorkflowType  string
	AggregateType string
	AggregateID   string
	CorrelationID string
	Params        map[string]any
	ScheduledAt   time.Time
}

// ActivityResult is returned from Activity.Execute.
type ActivityResult struct {
	Output map[string]any
	Error  string
}

// RetryPolicy configures automatic retry for activities.
type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
}

// ActivityInvocation tracks a dispatched activity.
type ActivityInvocation struct {
	ID             string
	ActivityName   string
	WorkflowType   string
	AggregateType  string
	AggregateID    string
	CorrelationID  string
	Mode           DispatchMode
	Input          ActivityInput
	RetryPolicy    *RetryPolicy
	ResultSignals  map[string]string
	Timeout        time.Duration
	Status         string
	Result         *ActivityResult
	Attempts       int
	MaxAttempts    int
	NextRetryAt    *time.Time
	ScheduledAt    time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
}

// Activity invocation statuses.
const (
	ActivityStatusScheduled = "SCHEDULED"
	ActivityStatusRunning   = "RUNNING"
	ActivityStatusCompleted = "COMPLETED"
	ActivityStatusFailed    = "FAILED"
	ActivityStatusTimedOut  = "TIMED_OUT"
)
```

**Step 4: Implement signal.go**

```go
// signal.go
package flowstate

// SignalInput is sent to engine.Signal() to trigger a transition.
type SignalInput struct {
	TargetAggregateType string
	TargetAggregateID   string
	SignalName          string
	Payload             map[string]any
	ActorID             string
}
```

**Step 5: Verify compilation**

```bash
go build ./...
```

Expected: PASS — all types compile.

**Step 6: Commit**

```bash
git add -A
git commit -m "feat: add types — PendingTask, ChildRelation, JoinPolicy, ActivityInvocation, SignalInput"
```

---

## Phase 2: Builder & Validation

### Task 4: Definition Types & State/Transition Structs

**Files:**
- Create: `definition.go`
- Test: `definition_test.go`

**Step 1: Write failing test**

```go
// definition_test.go
package flowstate

import "testing"

func TestTriggerTypeConstants(t *testing.T) {
	types := []TriggerType{
		TriggerDirect,
		TriggerSignal,
		TriggerTaskCompleted,
		TriggerChildCompleted,
		TriggerChildrenJoined,
		TriggerTimeout,
	}
	seen := make(map[TriggerType]bool)
	for _, tt := range types {
		if seen[tt] {
			t.Errorf("duplicate trigger type: %s", tt)
		}
		seen[tt] = true
	}
}

func TestDefinitionImmutability(t *testing.T) {
	def := &Definition{
		AggregateType: "order",
		WorkflowType:  "standard",
		States: map[string]StateDef{
			"CREATED": {Name: "CREATED", IsInitial: true},
		},
	}
	// Modifying the map after creation should not affect a properly built definition
	// (Build() should deep-copy). This test documents the expectation.
	if def.AggregateType != "order" {
		t.Error("unexpected aggregate type")
	}
}
```

**Step 2: Implement definition.go**

```go
// definition.go
package flowstate

// TriggerType determines how a transition is fired.
type TriggerType string

const (
	TriggerDirect          TriggerType = "DIRECT"
	TriggerSignal          TriggerType = "SIGNAL"
	TriggerTaskCompleted   TriggerType = "TASK_COMPLETED"
	TriggerChildCompleted  TriggerType = "CHILD_COMPLETED"
	TriggerChildrenJoined  TriggerType = "CHILDREN_JOINED"
	TriggerTimeout         TriggerType = "TIMEOUT"
)

// StateDef represents a named state in a workflow.
type StateDef struct {
	Name       string
	IsInitial  bool
	IsTerminal bool
	IsWait     bool
}

// Route represents a conditional target in a routed transition.
type Route struct {
	Condition Condition
	Target    string
	IsDefault bool
}

// ActivityDef describes an activity to dispatch on a transition.
type ActivityDef struct {
	Name          string
	Mode          DispatchMode
	RetryPolicy   *RetryPolicy
	ResultSignals map[string]string // result key → signal name
	Timeout       time.Duration
}

// TransitionDef represents a named edge between states.
type TransitionDef struct {
	Name               string
	Sources            []string
	Target             string
	Routes             []Route
	Event              string
	Guards             []Guard
	Activities         []ActivityDef
	TaskDef            *TaskDef
	ChildDef           *ChildDef
	ChildrenDef        *ChildrenDef
	TriggerType        TriggerType
	TriggerKey         string
	AllowSelfTransition bool
}

// Definition is an immutable, validated workflow definition.
// Created via the builder. Cannot be modified after Build().
type Definition struct {
	AggregateType string
	WorkflowType  string
	Version       int
	States        map[string]StateDef
	Transitions   map[string]TransitionDef
	InitialState  string
	TerminalStates []string
	warnings      []string
}

// Warnings returns validation warnings from Build().
func (d *Definition) Warnings() []string {
	return d.warnings
}
```

Note: Add `import "time"` at top of definition.go.

**Step 3: Run tests**

```bash
go test ./... -v
```

Expected: PASS.

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: add Definition, StateDef, TransitionDef, TriggerType"
```

---

### Task 5: Interfaces — Guard, Condition, Stores, Bus, Runner, Clock, Hooks

**Files:**
- Create: `guard.go`
- Create: `condition.go`
- Create: `store.go`
- Create: `taskstore.go`
- Create: `childstore.go`
- Create: `activitystore.go`
- Create: `txprovider.go`
- Create: `eventbus.go`
- Create: `activityrunner.go`
- Create: `clock.go`
- Create: `hooks.go`

**Step 1: Implement all interfaces**

Each file contains one interface as defined in the design doc Section 4. These are pure interface definitions with no logic — no tests needed (interfaces are tested via implementations).

`guard.go`:
```go
package flowstate

import "context"

// Guard checks preconditions before a transition. MUST be deterministic.
// Only read from aggregate and params. No API calls, no DB queries, no I/O.
// Return nil to pass. Return error to block.
type Guard interface {
	Check(ctx context.Context, aggregate any, params map[string]any) error
}
```

`condition.go`:
```go
package flowstate

import "context"

// Condition evaluates a routing decision. MUST be deterministic.
// Only read from aggregate and params. No I/O.
type Condition interface {
	Evaluate(ctx context.Context, aggregate any, params map[string]any) (bool, error)
}
```

`store.go`:
```go
package flowstate

import "context"

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
```

`taskstore.go`:
```go
package flowstate

import "context"

// TaskStore persists pending tasks for human-in-the-loop workflows.
type TaskStore interface {
	Create(ctx context.Context, tx any, task PendingTask) error
	Get(ctx context.Context, taskID string) (*PendingTask, error)
	GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]PendingTask, error)
	Complete(ctx context.Context, tx any, taskID, choice, actorID string) error
	ListPending(ctx context.Context) ([]PendingTask, error)
	ListExpired(ctx context.Context) ([]PendingTask, error)
}
```

`childstore.go`:
```go
package flowstate

import "context"

// ChildStore tracks parent-child workflow relationships.
type ChildStore interface {
	Create(ctx context.Context, tx any, relation ChildRelation) error
	GetByChild(ctx context.Context, childAggregateType, childAggregateID string) (*ChildRelation, error)
	GetByParent(ctx context.Context, parentAggregateType, parentAggregateID string) ([]ChildRelation, error)
	GetByGroup(ctx context.Context, groupID string) ([]ChildRelation, error)
	Complete(ctx context.Context, tx any, childAggregateType, childAggregateID, terminalState string) error
}
```

`activitystore.go`:
```go
package flowstate

import "context"

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

`txprovider.go`:
```go
package flowstate

import "context"

// TxProvider manages database transactions.
type TxProvider interface {
	Begin(ctx context.Context) (tx any, err error)
	Commit(ctx context.Context, tx any) error
	Rollback(ctx context.Context, tx any) error
}
```

`eventbus.go`:
```go
package flowstate

import "context"

// EventBus publishes domain events to external subscribers.
type EventBus interface {
	Emit(ctx context.Context, event DomainEvent) error
}
```

`activityrunner.go`:
```go
package flowstate

import "context"

// ActivityRunner dispatches activity invocations for async execution.
type ActivityRunner interface {
	Dispatch(ctx context.Context, invocation ActivityInvocation) error
}
```

`clock.go`:
```go
package flowstate

import "time"

// Clock provides deterministic time for the engine.
type Clock interface {
	Now() time.Time
}

// RealClock uses time.Now().
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }
```

`hooks.go`:
```go
package flowstate

import (
	"context"
	"time"
)

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

// NoopHooks is the default Hooks implementation. Does nothing.
type NoopHooks struct{}

func (NoopHooks) OnTransition(context.Context, TransitionResult, time.Duration)          {}
func (NoopHooks) OnGuardFailed(context.Context, string, string, string, error)            {}
func (NoopHooks) OnActivityDispatched(context.Context, ActivityInvocation)                 {}
func (NoopHooks) OnActivityCompleted(context.Context, ActivityInvocation, *ActivityResult) {}
func (NoopHooks) OnActivityFailed(context.Context, ActivityInvocation, error)              {}
func (NoopHooks) OnStuck(context.Context, WorkflowInstance, string)                        {}
```

**Step 2: Verify compilation**

```bash
go build ./...
```

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: add all interfaces — Guard, Condition, stores, EventBus, ActivityRunner, Clock, Hooks"
```

---

### Task 6: Fluent Builder + Build-Time Validation

**Files:**
- Create: `builder.go`
- Create: `validate.go`
- Create: `flowstate.go`
- Test: `builder_test.go`

**Step 1: Write failing tests for builder**

```go
// builder_test.go
package flowstate

import (
	"errors"
	"testing"
)

func TestBuildMinimalWorkflow(t *testing.T) {
	def, err := Define("order", "simple").
		Version(1).
		States(
			Initial("CREATED"),
			Terminal("DONE"),
		).
		Transition("complete",
			From("CREATED"),
			To("DONE"),
			Event("OrderCompleted"),
		).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.AggregateType != "order" {
		t.Errorf("expected order, got %s", def.AggregateType)
	}
	if def.WorkflowType != "simple" {
		t.Errorf("expected simple, got %s", def.WorkflowType)
	}
	if def.Version != 1 {
		t.Errorf("expected version 1, got %d", def.Version)
	}
	if def.InitialState != "CREATED" {
		t.Errorf("expected CREATED, got %s", def.InitialState)
	}
	if len(def.TerminalStates) != 1 || def.TerminalStates[0] != "DONE" {
		t.Errorf("expected [DONE], got %v", def.TerminalStates)
	}
}

func TestBuildNoInitialState(t *testing.T) {
	_, err := Define("order", "bad").
		States(Terminal("DONE")).
		Transition("x", From("A"), To("DONE"), Event("X")).
		Build()

	if !errors.Is(err, ErrNoInitialState) {
		t.Errorf("expected ErrNoInitialState, got %v", err)
	}
}

func TestBuildMultipleInitialStates(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), Initial("B"), Terminal("DONE")).
		Transition("x", From("A"), To("DONE"), Event("X")).
		Build()

	if !errors.Is(err, ErrMultipleInitialStates) {
		t.Errorf("expected ErrMultipleInitialStates, got %v", err)
	}
}

func TestBuildNoTerminalStates(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), State("B")).
		Transition("x", From("A"), To("B"), Event("X")).
		Build()

	if !errors.Is(err, ErrNoTerminalStates) {
		t.Errorf("expected ErrNoTerminalStates, got %v", err)
	}
}

func TestBuildUnreachableState(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), State("B"), Terminal("DONE")).
		Transition("x", From("A"), To("DONE"), Event("X")).
		Build()

	if !errors.Is(err, ErrUnreachableState) {
		t.Errorf("expected ErrUnreachableState, got %v", err)
	}
}

func TestBuildDeadEndState(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), State("B"), Terminal("DONE")).
		Transition("x", From("A"), To("B"), Event("X")).
		Build()

	if !errors.Is(err, ErrDeadEndState) {
		t.Errorf("expected ErrDeadEndState, got %v", err)
	}
}

func TestBuildUnknownState(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), Terminal("DONE")).
		Transition("x", From("A"), To("UNKNOWN"), Event("X")).
		Build()

	if !errors.Is(err, ErrUnknownState) {
		t.Errorf("expected ErrUnknownState, got %v", err)
	}
}

func TestBuildDuplicateTransition(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), Terminal("DONE")).
		Transition("x", From("A"), To("DONE"), Event("X")).
		Transition("x", From("A"), To("DONE"), Event("Y")).
		Build()

	if !errors.Is(err, ErrDuplicateTransition) {
		t.Errorf("expected ErrDuplicateTransition, got %v", err)
	}
}

func TestBuildDefaultVersion(t *testing.T) {
	def, err := Define("order", "simple").
		States(Initial("A"), Terminal("DONE")).
		Transition("x", From("A"), To("DONE"), Event("X")).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Version != 1 {
		t.Errorf("expected default version 1, got %d", def.Version)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./... -run TestBuild -v
```

Expected: FAIL — Define, Initial, Terminal, etc. not defined.

**Step 3: Implement flowstate.go (public entry points)**

```go
// flowstate.go
package flowstate

// Define starts building a workflow definition.
func Define(aggregateType, workflowType string) *DefBuilder {
	return &DefBuilder{
		aggregateType: aggregateType,
		workflowType:  workflowType,
		version:       1,
		states:        make(map[string]StateDef),
		transitions:   make(map[string]TransitionDef),
	}
}
```

**Step 4: Implement builder.go**

This is the full fluent builder. File will be ~200 lines. Key methods: `Version()`, `States()`, `Transition()`, `Build()`, plus all option functions (`From`, `To`, `Event`, `Guards`, `Dispatch`, `DispatchAndWait`, `OnSignal`, `OnTaskCompleted`, `OnChildCompleted`, `OnChildrenJoined`, `OnTimeout`, `EmitTask`, `SpawnChild`, `SpawnChildren`, `AllowSelfTransition`, `Route`, `When`, `Default`).

Each builder method appends to internal state. `Build()` calls `validate()` and returns an immutable `*Definition`.

**Step 5: Implement validate.go**

Graph analysis: BFS from initial for reachability, reverse BFS from terminals for dead-end detection, structural checks (counts, duplicates, references).

**Step 6: Run tests**

```bash
go test ./... -v
```

Expected: All PASS.

**Step 7: Commit**

```bash
git add -A
git commit -m "feat: add fluent builder + build-time validation (reachability, dead-ends, duplicates)"
```

---

## Phase 3: Engine — Basic Transitions

### Task 7: In-Memory Stores (memstore)

**Files:**
- Create: `memstore/event_store.go`
- Create: `memstore/instance_store.go`
- Create: `memstore/tx_provider.go`
- Create: `memstore/task_store.go`
- Create: `memstore/child_store.go`
- Create: `memstore/activity_store.go`
- Test: `memstore/event_store_test.go`
- Test: `memstore/instance_store_test.go`

**Step 1: Write failing tests for EventStore**

Test Append + ListByCorrelation + ListByAggregate.

**Step 2: Implement memstore**

All stores backed by `sync.Mutex` + `map[string][]T`. TxProvider uses a no-op transaction (`struct{}`). InstanceStore.Update checks `UpdatedAt` for optimistic locking.

**Step 3: Run tests, commit**

```bash
git commit -m "feat: add memstore — in-memory store implementations for TDD"
```

---

### Task 8: In-Memory EventBus (chanbus) + ActivityRunner (memrunner)

**Files:**
- Create: `chanbus/bus.go`
- Create: `memrunner/runner.go`
- Test: `chanbus/bus_test.go`
- Test: `memrunner/runner_test.go`

chanbus: fan-out to registered subscribers via Go channels.
memrunner: synchronous execution — calls `activity.Execute()` inline for testing.

```bash
git commit -m "feat: add chanbus + memrunner — in-memory EventBus and ActivityRunner"
```

---

### Task 9: Engine — Options, Registration, Basic Transition

**Files:**
- Create: `options.go`
- Create: `engine.go`
- Test: `engine_test.go`

**Step 1: Write failing test for basic transition**

```go
// engine_test.go
package flowstate

import (
	"context"
	"testing"
	// import memstore, chanbus, memrunner
)

func TestEngineBasicTransition(t *testing.T) {
	def, _ := Define("order", "simple").
		Version(1).
		States(Initial("CREATED"), Terminal("DONE")).
		Transition("complete", From("CREATED"), To("DONE"), Event("OrderCompleted")).
		Build()

	engine := NewEngine(
		WithEventStore(memEventStore),
		WithInstanceStore(memInstanceStore),
		WithTxProvider(memTxProvider),
		WithEventBus(chanBus),
	)
	engine.Register(def)

	ctx := context.Background()

	// Create instance first (engine.Transition should auto-create if not exists)
	result, err := engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NewState != "DONE" {
		t.Errorf("expected DONE, got %s", result.NewState)
	}
	if result.IsTerminal != true {
		t.Error("expected terminal")
	}
	if result.Event.EventType != "OrderCompleted" {
		t.Errorf("expected OrderCompleted, got %s", result.Event.EventType)
	}
}
```

**Step 2: Implement engine.go**

Engine struct with fields for all injected dependencies. `NewEngine()` with functional options. `Register()` stores definitions. `Transition()` implements the core flow:

1. Load/create workflow instance
2. Find matching transition by name + validate source state
3. Inject `Clock.Now()` into params as `_now`
4. Run guards sequentially
5. BEGIN TRANSACTION (TxProvider)
6. Snapshot state_before, compute state_after
7. Update aggregate (via callback or direct)
8. Append DomainEvent
9. Update WorkflowInstance
10. COMMIT
11. Dispatch activities (post-commit)
12. Emit to EventBus (post-commit)
13. Call Hooks.OnTransition

For the first iteration: no activities, no signals, no tasks, no children. Just basic state transitions with guards and events.

**Step 3: Run tests, iterate until green**

**Step 4: Commit**

```bash
git commit -m "feat: add Engine with basic Transition — guards, events, instance tracking"
```

---

### Task 10: Engine — Guard Evaluation + Event Chain

**Files:**
- Modify: `engine.go`
- Test: `engine_test.go` (add tests)

**Step 1: Write failing test for guard rejection**

```go
func TestEngineGuardRejectsTransition(t *testing.T) {
	// Guard that always fails
	def, _ := Define("order", "guarded").
		States(Initial("CREATED"), Terminal("DONE")).
		Transition("complete",
			From("CREATED"), To("DONE"), Event("OrderCompleted"),
			Guards(&alwaysFailGuard{reason: "not ready"}),
		).
		Build()
	// ... setup engine, attempt transition, expect ErrGuardFailed
}
```

**Step 2: Write test for event chain**

```go
func TestEngineEventChain(t *testing.T) {
	// Do two transitions, verify event chain by correlation_id
}
```

**Step 3: Implement, run tests, commit**

```bash
git commit -m "feat: add guard evaluation + event chain queries"
```

---

## Phase 4: Mermaid Export

### Task 11: Mermaid Diagram Generation

**Files:**
- Create: `mermaid.go`
- Test: `mermaid_test.go`

**Step 1: Write failing test**

```go
func TestMermaidExport(t *testing.T) {
	def, _ := Define("order", "simple").
		States(Initial("CREATED"), State("PAID"), Terminal("SHIPPED")).
		Transition("pay", From("CREATED"), To("PAID"), Event("Paid")).
		Transition("ship", From("PAID"), To("SHIPPED"), Event("Shipped")).
		Build()

	diagram := def.Mermaid()
	// assert contains "stateDiagram-v2"
	// assert contains "[*] --> CREATED"
	// assert contains "CREATED --> PAID : pay"
	// assert contains "SHIPPED --> [*]"
}
```

**Step 2: Implement, run, commit**

```bash
git commit -m "feat: add Mermaid stateDiagram export from Definition"
```

---

## Phase 5-11: Activities, Signals, Wait States, Routing, Children, Versioning, Observability

Each phase follows the same TDD pattern:

1. Write failing test
2. Verify it fails
3. Implement minimal code
4. Verify it passes
5. Commit

### Task 12-15: Activities (Dispatch, DispatchAndWait, RetryPolicy, ActivityRunner integration)
### Task 16: Signals (engine.Signal, OnSignal transitions)
### Task 17-18: Wait States (PendingTask, CompleteTask, OnTaskCompleted, OnTimeout, ProcessTimeouts)
### Task 19: Conditional Routing (Condition, Route, When, Default)
### Task 20-22: Child Workflows (SpawnChild, OnChildCompleted, ChildStore integration)
### Task 23-24: Parallel Children (SpawnChildren, JoinAll, JoinAny, JoinN, CancelRemaining)
### Task 25: Workflow Versioning (Version(), version coexistence, definition lookup by version)
### Task 26: Observability (Hooks integration, structured logging via slog)
### Task 27: Admin Recovery (RetryActivities, ForceState, Rewind + event chain entries)
### Task 28: Concurrency & Shutdown (ErrConcurrentModification handling, Engine.Shutdown)

Each task: ~30 min. Same test→implement→commit cycle.

---

## Phase 15: Test Utilities

### Task 29: testutil Package

**Files:**
- Create: `testutil/engine.go`
- Create: `testutil/assertions.go`
- Create: `testutil/activities.go`
- Create: `testutil/tasks.go`
- Create: `testutil/time.go`
- Create: `testutil/fixtures.go`
- Test: `testutil/engine_test.go`

Key helpers:
- `NewTestEngine(t)` — pre-wired with memstore + chanbus + memrunner + FakeClock
- `CompleteActivity(t, engine, name, aggID, result)`
- `CompleteTask(t, engine, aggID, choice)`
- `AdvanceTime(t, engine, duration)`
- `AssertState(t, engine, aggType, aggID, expected)`
- `AssertEventChain(t, engine, corrID, ...eventTypes)`
- `FakeClock` with `Advance(d)`

Fixtures:
- `OrderWorkflow` — simple linear
- `ApprovalChain` — human-in-the-loop
- `ParallelProcessing` — fan-out/fan-in

```bash
git commit -m "feat: add testutil — NewTestEngine, assertions, FakeClock, fixtures"
```

---

## Phase 16-21: Reference Implementations

### Task 30-31: pgxstore (PostgreSQL)

**Files:**
- Create: `pgxstore/event_store.go`, `instance_store.go`, `task_store.go`, `child_store.go`, `activity_store.go`, `tx_provider.go`
- Create: `pgxstore/migrations/001_flowstate.sql`
- Test: `pgxstore/integration_test.go` (requires PostgreSQL — skip in CI if not available)

SQL from design doc Section 21. Uses `embed.FS` for migrations.

```bash
git commit -m "feat: add pgxstore — PostgreSQL reference implementation with migrations"
```

### Task 32: sqlitestore

Same interfaces, adapted for SQLite syntax (no JSONB — use TEXT + json functions, no gen_random_uuid — use custom UUID generation).

```bash
git commit -m "feat: add sqlitestore — SQLite reference implementation"
```

### Task 33: dynamostore

DynamoDB-adapted store. Uses TransactWriteItems for TxProvider. Single-table design with PK/SK patterns.

```bash
git commit -m "feat: add dynamostore — DynamoDB reference implementation"
```

### Task 34: redisbus

Redis Streams EventBus. XADD on emit, XREADGROUP for consumers.

```bash
git commit -m "feat: add redisbus — Redis Streams EventBus implementation"
```

### Task 35: natsbus

NATS JetStream EventBus. Publish on emit, durable consumer for subscribers.

```bash
git commit -m "feat: add natsbus — NATS JetStream EventBus implementation"
```

### Task 36: asynqrunner + goroutinerunner

asynqrunner: wraps ActivityInvocation into asynq tasks. goroutinerunner: dispatches via `go func()` with errgroup for collection.

```bash
git commit -m "feat: add asynqrunner + goroutinerunner — ActivityRunner implementations"
```

---

## Phase 22: Integration Tests & Fixtures

### Task 37: Full Integration Test — Booking Workflow

**Files:**
- Test: `integration_test.go`

Complete booking workflow from design doc Section 22: initiate_payment → (activity completes) → confirm → check_in. Also test: cancel, trainer hold (wait state + task), payment timeout, guard rejection, event chain query, Mermaid export.

Uses `testutil.NewTestEngine(t)` — proves the full stack works end-to-end with in-memory backends.

```bash
git commit -m "test: add full booking workflow integration test"
```

---

## Task Dependency Graph

```
Task 1 (repo init)
  └── Task 2 (core types: events, instances, errors)
       └── Task 3 (core types: tasks, children, activities, signals)
            ├── Task 4 (definition types)
            │    └── Task 5 (interfaces)
            │         └── Task 6 (builder + validation)
            │              ├── Task 7 (memstore)
            │              │    └── Task 8 (chanbus + memrunner)
            │              │         └── Task 9 (engine — basic transition)
            │              │              └── Task 10 (guards + event chain)
            │              │                   ├── Task 11 (mermaid)
            │              │                   ├── Tasks 12-15 (activities)
            │              │                   │    └── Task 16 (signals)
            │              │                   │         └── Tasks 17-18 (wait states)
            │              │                   │              └── Task 19 (routing)
            │              │                   │                   └── Tasks 20-22 (child workflows)
            │              │                   │                        └── Tasks 23-24 (parallel children)
            │              │                   │                             └── Task 25 (versioning)
            │              │                   ├── Task 26 (hooks)
            │              │                   ├── Task 27 (admin recovery)
            │              │                   └── Task 28 (concurrency + shutdown)
            │              │
            │              └── Task 29 (testutil) — can start after Task 9
            │
            └── Tasks 30-36 (reference implementations) — can start after Task 28
                 └── Task 37 (integration tests) — after everything
```

Tasks 11, 26, 27, 28 are independent of each other and can be parallelized.
Tasks 30-36 are independent of each other and can be parallelized.

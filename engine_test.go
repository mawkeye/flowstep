package flowstate_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/adapters/chanbus"
	"github.com/mawkeye/flowstate/adapters/memstore"
	"github.com/mawkeye/flowstate/types"
)

type testHarness struct {
	engine        *flowstate.Engine
	eventStore    *memstore.EventStore
	instanceStore *memstore.InstanceStore
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(es),
		flowstate.WithInstanceStore(is),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
		flowstate.WithEventBus(chanbus.New()),
		flowstate.WithClock(flowstate.RealClock{}),
		flowstate.WithHooks(flowstate.NoopHooks{}),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return &testHarness{engine: engine, eventStore: es, instanceStore: is}
}

func TestEngineBasicTransition(t *testing.T) {
	def, err := flowstate.Define("order", "simple").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()
	result, err := h.engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NewState != "DONE" {
		t.Errorf("expected DONE, got %s", result.NewState)
	}
	if !result.IsTerminal {
		t.Error("expected terminal")
	}
	if result.Event.EventType != "OrderCompleted" {
		t.Errorf("expected OrderCompleted, got %s", result.Event.EventType)
	}
	if result.PreviousState != "CREATED" {
		t.Errorf("expected previous state CREATED, got %s", result.PreviousState)
	}
}

func TestEngineMultiStepTransition(t *testing.T) {
	def, err := flowstate.Define("order", "multi").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.State("PAID"),
			flowstate.Terminal("SHIPPED"),
		).
		Transition("pay", flowstate.From("CREATED"), flowstate.To("PAID"), flowstate.Event("OrderPaid")).
		Transition("ship", flowstate.From("PAID"), flowstate.To("SHIPPED"), flowstate.Event("OrderShipped")).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()

	result, err := h.engine.Transition(ctx, "order", "o-1", "pay", "user-1", nil)
	if err != nil {
		t.Fatalf("pay transition failed: %v", err)
	}
	if result.NewState != "PAID" {
		t.Errorf("expected PAID, got %s", result.NewState)
	}

	result, err = h.engine.Transition(ctx, "order", "o-1", "ship", "user-1", nil)
	if err != nil {
		t.Fatalf("ship transition failed: %v", err)
	}
	if result.NewState != "SHIPPED" {
		t.Errorf("expected SHIPPED, got %s", result.NewState)
	}
	if !result.IsTerminal {
		t.Error("SHIPPED should be terminal")
	}
}

func TestEngineInvalidTransition(t *testing.T) {
	def, err := flowstate.Define("order", "simple").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.State("PAID"),
			flowstate.Terminal("DONE"),
		).
		Transition("pay", flowstate.From("CREATED"), flowstate.To("PAID"), flowstate.Event("OrderPaid")).
		Transition("finish", flowstate.From("PAID"), flowstate.To("DONE"), flowstate.Event("OrderDone")).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()
	_, err = h.engine.Transition(ctx, "order", "o-1", "finish", "user-1", nil)
	if !errors.Is(err, flowstate.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestEngineAlreadyTerminal(t *testing.T) {
	def, err := flowstate.Define("order", "simple").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete", flowstate.From("CREATED"), flowstate.To("DONE"), flowstate.Event("OrderCompleted")).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()

	_, err = h.engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("first transition failed: %v", err)
	}

	_, err = h.engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if !errors.Is(err, flowstate.ErrAlreadyTerminal) {
		t.Errorf("expected ErrAlreadyTerminal, got %v", err)
	}
}

// alwaysFailGuard is a test guard that always fails.
type alwaysFailGuard struct {
	reason string
}

func (g *alwaysFailGuard) Check(_ context.Context, _ any, _ map[string]any) error {
	return fmt.Errorf("%s", g.reason)
}

func TestEngineGuardRejectsTransition(t *testing.T) {
	def, err := flowstate.Define("order", "guarded").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
			flowstate.Guards(&alwaysFailGuard{reason: "not ready"}),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()
	_, err = h.engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if !errors.Is(err, flowstate.ErrGuardFailed) {
		t.Errorf("expected ErrGuardFailed, got %v", err)
	}

	// Verify no event was appended
	events, _ := h.eventStore.ListByAggregate(ctx, "order", "o-1")
	if len(events) != 0 {
		t.Errorf("expected no events after guard rejection, got %d", len(events))
	}
}

// passingGuard is a test guard that always passes.
type passingGuard struct{}

func (g *passingGuard) Check(_ context.Context, _ any, _ map[string]any) error {
	return nil
}

func TestEngineGuardPasses(t *testing.T) {
	def, err := flowstate.Define("order", "guarded").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
			flowstate.Guards(&passingGuard{}),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()
	result, err := h.engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NewState != "DONE" {
		t.Errorf("expected DONE, got %s", result.NewState)
	}
}

func TestEngineEventChainByCorrelation(t *testing.T) {
	def, err := flowstate.Define("order", "chain").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.State("PAID"),
			flowstate.Terminal("SHIPPED"),
		).
		Transition("pay", flowstate.From("CREATED"), flowstate.To("PAID"), flowstate.Event("OrderPaid")).
		Transition("ship", flowstate.From("PAID"), flowstate.To("SHIPPED"), flowstate.Event("OrderShipped")).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()

	r1, err := h.engine.Transition(ctx, "order", "o-1", "pay", "user-1", nil)
	if err != nil {
		t.Fatalf("pay failed: %v", err)
	}

	r2, err := h.engine.Transition(ctx, "order", "o-1", "ship", "user-1", nil)
	if err != nil {
		t.Fatalf("ship failed: %v", err)
	}

	// Both events should share the same correlation ID
	if r1.Event.CorrelationID != r2.Event.CorrelationID {
		t.Errorf("expected same correlation ID, got %s and %s",
			r1.Event.CorrelationID, r2.Event.CorrelationID)
	}

	// Query event chain by correlation
	events, err := h.eventStore.ListByCorrelation(ctx, r1.Event.CorrelationID)
	if err != nil {
		t.Fatalf("list by correlation failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events in chain, got %d", len(events))
	}

	expectedTypes := []string{"OrderPaid", "OrderShipped"}
	for i, e := range events {
		if e.EventType != expectedTypes[i] {
			t.Errorf("event %d: expected %s, got %s", i, expectedTypes[i], e.EventType)
		}
	}

	// Query by aggregate should also return both
	aggEvents, err := h.eventStore.ListByAggregate(ctx, "order", "o-1")
	if err != nil {
		t.Fatalf("list by aggregate failed: %v", err)
	}
	if len(aggEvents) != 2 {
		t.Errorf("expected 2 events by aggregate, got %d", len(aggEvents))
	}
}

// --- Activity tests ---

type recordingActivity struct {
	name    string
	calls   int
	lastCtx context.Context
}

func (a *recordingActivity) Name() string { return a.name }
func (a *recordingActivity) Execute(ctx context.Context, _ types.ActivityInput) (*types.ActivityResult, error) {
	a.calls++
	a.lastCtx = ctx
	return &types.ActivityResult{Output: map[string]any{"ok": true}}, nil
}

func newTestHarnessWithActivities(t *testing.T, activities ...flowstate.Activity) (*testHarness, *memstore.ActivityStore) {
	t.Helper()
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	as := memstore.NewActivityStore()
	runner := newRecordingRunner(activities...)
	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(es),
		flowstate.WithInstanceStore(is),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
		flowstate.WithEventBus(chanbus.New()),
		flowstate.WithActivityStore(as),
		flowstate.WithActivityRunner(runner),
		flowstate.WithClock(flowstate.RealClock{}),
		flowstate.WithHooks(flowstate.NoopHooks{}),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return &testHarness{engine: engine, eventStore: es, instanceStore: is}, as
}

type recordingRunner struct {
	activities map[string]flowstate.Activity
	dispatched []string
}

func newRecordingRunner(activities ...flowstate.Activity) *recordingRunner {
	r := &recordingRunner{activities: make(map[string]flowstate.Activity)}
	for _, a := range activities {
		r.activities[a.Name()] = a
	}
	return r
}

func (r *recordingRunner) Dispatch(ctx context.Context, inv types.ActivityInvocation) error {
	r.dispatched = append(r.dispatched, inv.ActivityName)
	if a, ok := r.activities[inv.ActivityName]; ok {
		_, err := a.Execute(ctx, inv.Input)
		return err
	}
	return flowstate.ErrActivityNotRegistered
}

func TestEngineActivityDispatch(t *testing.T) {
	act := &recordingActivity{name: "send_email"}
	h, actStore := newTestHarnessWithActivities(t, act)

	def, err := flowstate.Define("order", "with_activity").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
			flowstate.Dispatch("send_email"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h.engine.Register(def)

	ctx := context.Background()
	result, err := h.engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.NewState != "DONE" {
		t.Errorf("expected DONE, got %s", result.NewState)
	}

	// Activity should have been dispatched
	if len(result.ActivitiesDispatched) != 1 || result.ActivitiesDispatched[0] != "send_email" {
		t.Errorf("expected [send_email] dispatched, got %v", result.ActivitiesDispatched)
	}

	// Activity should have been called
	if act.calls != 1 {
		t.Errorf("expected activity called once, got %d", act.calls)
	}

	// Activity invocation should be stored
	invocations, err := actStore.ListByAggregate(ctx, "order", "o-1")
	if err != nil {
		t.Fatalf("list invocations failed: %v", err)
	}
	if len(invocations) != 1 {
		t.Errorf("expected 1 invocation stored, got %d", len(invocations))
	}
}

func TestEngineMultipleActivities(t *testing.T) {
	act1 := &recordingActivity{name: "send_email"}
	act2 := &recordingActivity{name: "update_crm"}
	h, _ := newTestHarnessWithActivities(t, act1, act2)

	def, err := flowstate.Define("order", "multi_act").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
			flowstate.Dispatch("send_email"),
			flowstate.Dispatch("update_crm"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h.engine.Register(def)

	ctx := context.Background()
	result, err := h.engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ActivitiesDispatched) != 2 {
		t.Errorf("expected 2 activities dispatched, got %d", len(result.ActivitiesDispatched))
	}
	if act1.calls != 1 {
		t.Errorf("expected send_email called once, got %d", act1.calls)
	}
	if act2.calls != 1 {
		t.Errorf("expected update_crm called once, got %d", act2.calls)
	}
}

// --- Signal tests ---

func TestEngineSignalTransition(t *testing.T) {
	def, err := flowstate.Define("order", "signal_wf").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.State("PAYMENT_PENDING"),
			flowstate.Terminal("PAID"),
		).
		Transition("start_payment",
			flowstate.From("CREATED"),
			flowstate.To("PAYMENT_PENDING"),
			flowstate.Event("PaymentStarted"),
		).
		Transition("payment_received",
			flowstate.From("PAYMENT_PENDING"),
			flowstate.To("PAID"),
			flowstate.Event("PaymentReceived"),
			flowstate.OnSignal("payment_complete"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()

	// Move to PAYMENT_PENDING
	_, err = h.engine.Transition(ctx, "order", "o-1", "start_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("start_payment failed: %v", err)
	}

	// Send signal
	result, err := h.engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "order",
		TargetAggregateID:   "o-1",
		SignalName:          "payment_complete",
		ActorID:             "system",
	})
	if err != nil {
		t.Fatalf("signal failed: %v", err)
	}
	if result.NewState != "PAID" {
		t.Errorf("expected PAID, got %s", result.NewState)
	}
	if result.Event.EventType != "PaymentReceived" {
		t.Errorf("expected PaymentReceived, got %s", result.Event.EventType)
	}
}

func TestEngineSignalNoMatch(t *testing.T) {
	def, err := flowstate.Define("order", "signal_wf").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("Done"),
			flowstate.OnSignal("finish"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()

	// Signal with wrong name
	_, err = h.engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "order",
		TargetAggregateID:   "o-1",
		SignalName:          "wrong_signal",
		ActorID:             "system",
	})
	if !errors.Is(err, flowstate.ErrNoMatchingSignal) {
		t.Errorf("expected ErrNoMatchingSignal, got %v", err)
	}
}

// --- Wait State / Task tests ---

func newTestHarnessWithTasks(t *testing.T) (*testHarness, *memstore.TaskStore) {
	t.Helper()
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	ts := memstore.NewTaskStore()
	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(es),
		flowstate.WithInstanceStore(is),
		flowstate.WithTaskStore(ts),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
		flowstate.WithEventBus(chanbus.New()),
		flowstate.WithClock(flowstate.RealClock{}),
		flowstate.WithHooks(flowstate.NoopHooks{}),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return &testHarness{engine: engine, eventStore: es, instanceStore: is}, ts
}

func TestEngineWaitStateAndTaskCompletion(t *testing.T) {
	def, err := flowstate.Define("approval", "review").
		Version(1).
		States(
			flowstate.Initial("SUBMITTED"),
			flowstate.WaitState("AWAITING_REVIEW"),
			flowstate.Terminal("APPROVED"),
			flowstate.Terminal("REJECTED"),
		).
		Transition("submit_for_review",
			flowstate.From("SUBMITTED"),
			flowstate.To("AWAITING_REVIEW"),
			flowstate.Event("SubmittedForReview"),
			flowstate.EmitTask(types.TaskDef{
				Type:        "review_decision",
				Description: "Please review and approve or reject",
				Options:     []string{"approve", "reject"},
			}),
		).
		Transition("approve",
			flowstate.From("AWAITING_REVIEW"),
			flowstate.To("APPROVED"),
			flowstate.Event("Approved"),
			flowstate.OnTaskCompleted("review_decision"),
		).
		Transition("reject",
			flowstate.From("AWAITING_REVIEW"),
			flowstate.To("REJECTED"),
			flowstate.Event("Rejected"),
			flowstate.OnTaskCompleted("review_decision"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h, taskStore := newTestHarnessWithTasks(t)
	h.engine.Register(def)
	ctx := context.Background()

	// Submit for review — should create a pending task
	result, err := h.engine.Transition(ctx, "approval", "doc-1", "submit_for_review", "author", nil)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if result.NewState != "AWAITING_REVIEW" {
		t.Errorf("expected AWAITING_REVIEW, got %s", result.NewState)
	}
	if result.TaskCreated == nil {
		t.Fatal("expected a task to be created")
	}

	// Verify task in store
	tasks, err := taskStore.GetByAggregate(ctx, "approval", "doc-1")
	if err != nil {
		t.Fatalf("get tasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].TaskType != "review_decision" {
		t.Errorf("expected review_decision, got %s", tasks[0].TaskType)
	}

	// Complete the task with "approve"
	approveResult, err := h.engine.CompleteTask(ctx, tasks[0].ID, "approve", "reviewer-1")
	if err != nil {
		t.Fatalf("complete task failed: %v", err)
	}
	if approveResult.NewState != "APPROVED" {
		t.Errorf("expected APPROVED, got %s", approveResult.NewState)
	}
}

func TestEngineTaskReject(t *testing.T) {
	def, err := flowstate.Define("approval", "review").
		Version(1).
		States(
			flowstate.Initial("SUBMITTED"),
			flowstate.WaitState("AWAITING_REVIEW"),
			flowstate.Terminal("APPROVED"),
			flowstate.Terminal("REJECTED"),
		).
		Transition("submit_for_review",
			flowstate.From("SUBMITTED"),
			flowstate.To("AWAITING_REVIEW"),
			flowstate.Event("SubmittedForReview"),
			flowstate.EmitTask(types.TaskDef{
				Type:        "review_decision",
				Description: "Review it",
				Options:     []string{"approve", "reject"},
			}),
		).
		Transition("approve",
			flowstate.From("AWAITING_REVIEW"),
			flowstate.To("APPROVED"),
			flowstate.Event("Approved"),
			flowstate.OnTaskCompleted("review_decision"),
		).
		Transition("reject",
			flowstate.From("AWAITING_REVIEW"),
			flowstate.To("REJECTED"),
			flowstate.Event("Rejected"),
			flowstate.OnTaskCompleted("review_decision"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h, taskStore := newTestHarnessWithTasks(t)
	h.engine.Register(def)
	ctx := context.Background()

	_, err = h.engine.Transition(ctx, "approval", "doc-2", "submit_for_review", "author", nil)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	tasks, _ := taskStore.GetByAggregate(ctx, "approval", "doc-2")
	rejectResult, err := h.engine.CompleteTask(ctx, tasks[0].ID, "reject", "reviewer-1")
	if err != nil {
		t.Fatalf("complete task failed: %v", err)
	}
	if rejectResult.NewState != "REJECTED" {
		t.Errorf("expected REJECTED, got %s", rejectResult.NewState)
	}
}

// --- Conditional Routing tests ---

type amountCondition struct {
	threshold float64
}

func (c *amountCondition) Evaluate(_ context.Context, _ any, params map[string]any) (bool, error) {
	amount, ok := params["amount"].(float64)
	if !ok {
		return false, nil
	}
	return amount >= c.threshold, nil
}

func TestEngineConditionalRouting(t *testing.T) {
	def, err := flowstate.Define("order", "routed").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.State("STANDARD_REVIEW"),
			flowstate.State("VIP_REVIEW"),
			flowstate.Terminal("DONE"),
		).
		Transition("route_review",
			flowstate.From("CREATED"),
			flowstate.Event("ReviewRouted"),
			flowstate.Route(flowstate.When(&amountCondition{threshold: 1000}), flowstate.To("VIP_REVIEW")),
			flowstate.Route(flowstate.Default(), flowstate.To("STANDARD_REVIEW")),
		).
		Transition("finish_standard",
			flowstate.From("STANDARD_REVIEW"),
			flowstate.To("DONE"),
			flowstate.Event("Done"),
		).
		Transition("finish_vip",
			flowstate.From("VIP_REVIEW"),
			flowstate.To("DONE"),
			flowstate.Event("Done"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)
	ctx := context.Background()

	// Low amount -> STANDARD_REVIEW
	result, err := h.engine.Transition(ctx, "order", "o-1", "route_review", "user-1", map[string]any{"amount": 500.0})
	if err != nil {
		t.Fatalf("routing failed: %v", err)
	}
	if result.NewState != "STANDARD_REVIEW" {
		t.Errorf("expected STANDARD_REVIEW, got %s", result.NewState)
	}

	// High amount -> VIP_REVIEW
	result, err = h.engine.Transition(ctx, "order", "o-2", "route_review", "user-1", map[string]any{"amount": 2000.0})
	if err != nil {
		t.Fatalf("routing failed: %v", err)
	}
	if result.NewState != "VIP_REVIEW" {
		t.Errorf("expected VIP_REVIEW, got %s", result.NewState)
	}
}

func TestEngineConditionalRoutingNoMatch(t *testing.T) {
	// Route with condition but no default — should fail if condition doesn't match
	def, err := flowstate.Define("order", "no_default").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.State("VIP"),
			flowstate.Terminal("DONE"),
		).
		Transition("route",
			flowstate.From("CREATED"),
			flowstate.Event("Routed"),
			flowstate.Route(flowstate.When(&amountCondition{threshold: 1000}), flowstate.To("VIP")),
		).
		Transition("finish",
			flowstate.From("VIP"),
			flowstate.To("DONE"),
			flowstate.Event("Done"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)
	ctx := context.Background()

	_, err = h.engine.Transition(ctx, "order", "o-1", "route", "user-1", map[string]any{"amount": 100.0})
	if !errors.Is(err, flowstate.ErrNoMatchingRoute) {
		t.Errorf("expected ErrNoMatchingRoute, got %v", err)
	}
}

// --- Child Workflow tests ---

func newFullTestHarness(t *testing.T) (*testHarness, *memstore.ChildStore) {
	t.Helper()
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	cs := memstore.NewChildStore()
	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(es),
		flowstate.WithInstanceStore(is),
		flowstate.WithChildStore(cs),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
		flowstate.WithEventBus(chanbus.New()),
		flowstate.WithClock(flowstate.RealClock{}),
		flowstate.WithHooks(flowstate.NoopHooks{}),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return &testHarness{engine: engine, eventStore: es, instanceStore: is}, cs
}

func TestEngineSpawnChild(t *testing.T) {
	// Parent workflow spawns a child when transitioning
	parentDef, err := flowstate.Define("order", "parent_wf").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.State("PROCESSING"),
			flowstate.Terminal("DONE"),
		).
		Transition("start_processing",
			flowstate.From("CREATED"),
			flowstate.To("PROCESSING"),
			flowstate.Event("ProcessingStarted"),
			flowstate.SpawnChild(types.ChildDef{
				WorkflowType: "payment",
				InputFrom:    "order_data",
			}),
		).
		Transition("processing_done",
			flowstate.From("PROCESSING"),
			flowstate.To("DONE"),
			flowstate.Event("ProcessingDone"),
			flowstate.OnChildCompleted("payment"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build parent definition: %v", err)
	}

	childDef, err := flowstate.Define("payment", "payment_wf").
		Version(1).
		States(
			flowstate.Initial("PENDING"),
			flowstate.Terminal("COMPLETED"),
		).
		Transition("complete",
			flowstate.From("PENDING"),
			flowstate.To("COMPLETED"),
			flowstate.Event("PaymentCompleted"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build child definition: %v", err)
	}

	h, childStore := newFullTestHarness(t)
	h.engine.Register(parentDef)
	h.engine.Register(childDef)
	ctx := context.Background()

	// Start processing — should spawn child
	result, err := h.engine.Transition(ctx, "order", "o-1", "start_processing", "user-1", nil)
	if err != nil {
		t.Fatalf("start_processing failed: %v", err)
	}
	if result.NewState != "PROCESSING" {
		t.Errorf("expected PROCESSING, got %s", result.NewState)
	}
	if len(result.ChildrenSpawned) != 1 {
		t.Fatalf("expected 1 child spawned, got %d", len(result.ChildrenSpawned))
	}

	// Verify child relation in store
	relations, err := childStore.GetByParent(ctx, "order", "o-1")
	if err != nil {
		t.Fatalf("get by parent failed: %v", err)
	}
	if len(relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(relations))
	}
	if relations[0].ChildWorkflowType != "payment" {
		t.Errorf("expected payment child, got %s", relations[0].ChildWorkflowType)
	}

	// Complete the child workflow
	childAggID := relations[0].ChildAggregateID
	_, err = h.engine.Transition(ctx, "payment", childAggID, "complete", "system", nil)
	if err != nil {
		t.Fatalf("child complete failed: %v", err)
	}

	// Notify parent that child completed
	parentResult, err := h.engine.ChildCompleted(ctx, "payment", childAggID, "COMPLETED")
	if err != nil {
		t.Fatalf("child completed notification failed: %v", err)
	}
	if parentResult.NewState != "DONE" {
		t.Errorf("expected parent DONE, got %s", parentResult.NewState)
	}
}

func TestEngineSpawnChildren_JoinAll(t *testing.T) {
	// Parent spawns 3 children and waits for all to complete (JoinAll)
	parentDef, err := flowstate.Define("batch", "batch_wf").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.State("PROCESSING"),
			flowstate.Terminal("DONE"),
		).
		Transition("start_batch",
			flowstate.From("CREATED"),
			flowstate.To("PROCESSING"),
			flowstate.Event("BatchStarted"),
			flowstate.SpawnChildren(types.ChildrenDef{
				WorkflowType: "job",
				InputsFn: func(aggregate any) []map[string]any {
					return []map[string]any{
						{"job_id": "j1"},
						{"job_id": "j2"},
						{"job_id": "j3"},
					}
				},
				Join: types.JoinAll(),
			}),
		).
		Transition("batch_done",
			flowstate.From("PROCESSING"),
			flowstate.To("DONE"),
			flowstate.Event("BatchDone"),
			flowstate.OnChildrenJoined(),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build parent definition: %v", err)
	}

	childDef, err := flowstate.Define("job", "job_wf").
		Version(1).
		States(
			flowstate.Initial("PENDING"),
			flowstate.Terminal("COMPLETED"),
		).
		Transition("finish",
			flowstate.From("PENDING"),
			flowstate.To("COMPLETED"),
			flowstate.Event("JobFinished"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build child definition: %v", err)
	}

	h, childStore := newFullTestHarness(t)
	h.engine.Register(parentDef)
	h.engine.Register(childDef)
	ctx := context.Background()

	// Start batch — should spawn 3 children
	result, err := h.engine.Transition(ctx, "batch", "b-1", "start_batch", "user-1", nil)
	if err != nil {
		t.Fatalf("start_batch failed: %v", err)
	}
	if len(result.ChildrenSpawned) != 3 {
		t.Fatalf("expected 3 children spawned, got %d", len(result.ChildrenSpawned))
	}

	// All children should share a GroupID
	groupID := result.ChildrenSpawned[0].GroupID
	if groupID == "" {
		t.Fatal("expected non-empty GroupID")
	}
	for i, rel := range result.ChildrenSpawned {
		if rel.GroupID != groupID {
			t.Errorf("child %d has different GroupID: %s vs %s", i, rel.GroupID, groupID)
		}
	}

	// Complete first two children — parent should NOT transition yet
	for i := 0; i < 2; i++ {
		childAggID := result.ChildrenSpawned[i].ChildAggregateID
		_, err = h.engine.Transition(ctx, "job", childAggID, "finish", "system", nil)
		if err != nil {
			t.Fatalf("child %d finish failed: %v", i, err)
		}
		parentResult, err := h.engine.ChildCompleted(ctx, "job", childAggID, "COMPLETED")
		if err == nil {
			t.Fatalf("child %d: expected join not satisfied yet, but got result: %v", i, parentResult)
		}
		// Should get an error indicating join not yet satisfied
	}

	// Verify parent still in PROCESSING
	parentInst, err := h.instanceStore.Get(ctx, "batch", "b-1")
	if err != nil {
		t.Fatalf("get parent instance failed: %v", err)
	}
	if parentInst.CurrentState != "PROCESSING" {
		t.Errorf("expected parent still PROCESSING, got %s", parentInst.CurrentState)
	}

	// Complete third child — parent should now transition to DONE
	lastChildAggID := result.ChildrenSpawned[2].ChildAggregateID
	_, err = h.engine.Transition(ctx, "job", lastChildAggID, "finish", "system", nil)
	if err != nil {
		t.Fatalf("child 2 finish failed: %v", err)
	}
	parentResult, err := h.engine.ChildCompleted(ctx, "job", lastChildAggID, "COMPLETED")
	if err != nil {
		t.Fatalf("final child completed failed: %v", err)
	}
	if parentResult.NewState != "DONE" {
		t.Errorf("expected parent DONE, got %s", parentResult.NewState)
	}

	// Verify all children marked complete in store
	relations, err := childStore.GetByGroup(ctx, groupID)
	if err != nil {
		t.Fatalf("get by group failed: %v", err)
	}
	for _, rel := range relations {
		if rel.Status != "COMPLETED" {
			t.Errorf("expected child %s COMPLETED, got %s", rel.ChildAggregateID, rel.Status)
		}
	}
}

func TestEngineVersionCoexistence(t *testing.T) {
	// Register v1 and v2 of same workflow type for same aggregate.
	// Existing instances (created under v1) should continue using v1 transitions.
	// New instances should use v2.

	defV1, err := flowstate.Define("ticket", "support").
		Version(1).
		States(
			flowstate.Initial("OPEN"),
			flowstate.State("REVIEWING"),
			flowstate.Terminal("CLOSED"),
		).
		Transition("review",
			flowstate.From("OPEN"),
			flowstate.To("REVIEWING"),
			flowstate.Event("TicketReviewing"),
		).
		Transition("close",
			flowstate.From("REVIEWING"),
			flowstate.To("CLOSED"),
			flowstate.Event("TicketClosed"),
		).
		Build()
	if err != nil {
		t.Fatalf("v1 build failed: %v", err)
	}

	defV2, err := flowstate.Define("ticket", "support").
		Version(2).
		States(
			flowstate.Initial("OPEN"),
			flowstate.State("IN_PROGRESS"),
			flowstate.Terminal("RESOLVED"),
		).
		Transition("start_work",
			flowstate.From("OPEN"),
			flowstate.To("IN_PROGRESS"),
			flowstate.Event("WorkStarted"),
		).
		Transition("resolve",
			flowstate.From("IN_PROGRESS"),
			flowstate.To("RESOLVED"),
			flowstate.Event("TicketResolved"),
		).
		Build()
	if err != nil {
		t.Fatalf("v2 build failed: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(defV1)

	ctx := context.Background()

	// Create instance t-1 under v1, move to REVIEWING (non-terminal)
	_, err = h.engine.Transition(ctx, "ticket", "t-1", "review", "agent-1", nil)
	if err != nil {
		t.Fatalf("v1 review failed: %v", err)
	}

	// Now register v2 — should become the default for new instances
	h.engine.Register(defV2)

	// Existing instance t-1 (v1, in REVIEWING) should still use v1's "close" transition
	result, err := h.engine.Transition(ctx, "ticket", "t-1", "close", "agent-1", nil)
	if err != nil {
		t.Fatalf("v1 close on existing instance failed: %v", err)
	}
	if result.NewState != "CLOSED" {
		t.Errorf("expected CLOSED, got %s", result.NewState)
	}
	if result.Event.WorkflowVersion != 1 {
		t.Errorf("expected version 1 event, got %d", result.Event.WorkflowVersion)
	}

	// New instance t-2 should use v2 (has start_work transition, not close)
	result2, err := h.engine.Transition(ctx, "ticket", "t-2", "start_work", "agent-1", nil)
	if err != nil {
		t.Fatalf("v2 start_work failed: %v", err)
	}
	if result2.NewState != "IN_PROGRESS" {
		t.Errorf("expected IN_PROGRESS, got %s", result2.NewState)
	}
	if result2.Event.WorkflowVersion != 2 {
		t.Errorf("expected version 2 event, got %d", result2.Event.WorkflowVersion)
	}

	// t-2 should continue with v2 transitions
	result3, err := h.engine.Transition(ctx, "ticket", "t-2", "resolve", "agent-1", nil)
	if err != nil {
		t.Fatalf("v2 resolve failed: %v", err)
	}
	if result3.NewState != "RESOLVED" {
		t.Errorf("expected RESOLVED, got %s", result3.NewState)
	}

	// New instance t-3 should also use v2 — "close" from v1 should not work
	_, err = h.engine.Transition(ctx, "ticket", "t-3", "close", "agent-1", nil)
	if err == nil {
		t.Error("expected error: 'close' transition doesn't exist in v2")
	}
	if !errors.Is(err, flowstate.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

// recordingHooks captures hook calls for test assertions.
type recordingHooks struct {
	transitions []types.TransitionResult
	guardFails  []string
	activities  []string
}

func (h *recordingHooks) OnTransition(_ context.Context, result types.TransitionResult, _ time.Duration) {
	h.transitions = append(h.transitions, result)
}
func (h *recordingHooks) OnGuardFailed(_ context.Context, _, transitionName, guardName string, _ error) {
	h.guardFails = append(h.guardFails, transitionName+":"+guardName)
}
func (h *recordingHooks) OnActivityDispatched(_ context.Context, inv types.ActivityInvocation) {
	h.activities = append(h.activities, inv.ActivityName)
}
func (h *recordingHooks) OnActivityCompleted(context.Context, types.ActivityInvocation, *types.ActivityResult) {}
func (h *recordingHooks) OnActivityFailed(context.Context, types.ActivityInvocation, error)                    {}
func (h *recordingHooks) OnStuck(context.Context, types.WorkflowInstance, string)                              {}

func TestEngineHooksCalledOnTransition(t *testing.T) {
	hooks := &recordingHooks{}
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(es),
		flowstate.WithInstanceStore(is),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
		flowstate.WithEventBus(chanbus.New()),
		flowstate.WithHooks(hooks),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	def, err := flowstate.Define("order", "simple").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
		).
		Build()
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	engine.Register(def)
	_, err = engine.Transition(context.Background(), "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}

	if len(hooks.transitions) != 1 {
		t.Fatalf("expected 1 OnTransition call, got %d", len(hooks.transitions))
	}
	if hooks.transitions[0].NewState != "DONE" {
		t.Errorf("expected DONE in hook, got %s", hooks.transitions[0].NewState)
	}
}

func TestEngineHooksCalledOnGuardFailed(t *testing.T) {
	hooks := &recordingHooks{}
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(es),
		flowstate.WithInstanceStore(is),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
		flowstate.WithHooks(hooks),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	def, err := flowstate.Define("order", "guarded").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
			flowstate.Guards(&alwaysFailGuard{}),
		).
		Build()
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	engine.Register(def)
	_, err = engine.Transition(context.Background(), "order", "o-1", "complete", "user-1", nil)
	if err == nil {
		t.Fatal("expected guard failure")
	}

	if len(hooks.guardFails) != 1 {
		t.Fatalf("expected 1 OnGuardFailed call, got %d", len(hooks.guardFails))
	}
}

func TestEngineHooksCalledOnActivityDispatched(t *testing.T) {
	hooks := &recordingHooks{}
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	act := &recordingActivity{name: "send_email"}
	runner := newRecordingRunner(act)

	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(es),
		flowstate.WithInstanceStore(is),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
		flowstate.WithActivityRunner(runner),
		flowstate.WithActivityStore(memstore.NewActivityStore()),
		flowstate.WithHooks(hooks),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	def, err := flowstate.Define("order", "with_activity").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
			flowstate.Dispatch("send_email"),
		).
		Build()
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	engine.Register(def)
	_, err = engine.Transition(context.Background(), "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}

	if len(hooks.activities) != 1 {
		t.Fatalf("expected 1 OnActivityDispatched call, got %d", len(hooks.activities))
	}
	if hooks.activities[0] != "send_email" {
		t.Errorf("expected send_email, got %s", hooks.activities[0])
	}
}

func TestEngineForceState(t *testing.T) {
	h := newTestHarness(t)
	def, err := flowstate.Define("order", "simple").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.State("PROCESSING"),
			flowstate.Terminal("DONE"),
			flowstate.Terminal("CANCELLED"),
		).
		Transition("process",
			flowstate.From("CREATED"),
			flowstate.To("PROCESSING"),
			flowstate.Event("OrderProcessing"),
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
		t.Fatalf("build failed: %v", err)
	}

	h.engine.Register(def)
	ctx := context.Background()

	// Create instance and move to PROCESSING
	_, err = h.engine.Transition(ctx, "order", "o-1", "process", "user-1", nil)
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}

	// Force state to CANCELLED (bypasses normal transitions)
	result, err := h.engine.ForceState(ctx, "order", "o-1", "CANCELLED", "admin-1", "Customer requested cancel")
	if err != nil {
		t.Fatalf("force state failed: %v", err)
	}
	if result.NewState != "CANCELLED" {
		t.Errorf("expected CANCELLED, got %s", result.NewState)
	}
	if result.Event.EventType != "StateForced" {
		t.Errorf("expected StateForced event, got %s", result.Event.EventType)
	}

	// Verify instance is in CANCELLED state
	inst, err := h.instanceStore.Get(ctx, "order", "o-1")
	if err != nil {
		t.Fatalf("get instance failed: %v", err)
	}
	if inst.CurrentState != "CANCELLED" {
		t.Errorf("expected CANCELLED, got %s", inst.CurrentState)
	}
}

func TestEngineForceStateFromTerminal(t *testing.T) {
	h := newTestHarness(t)
	def, err := flowstate.Define("order", "simple").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
		).
		Build()
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	h.engine.Register(def)
	ctx := context.Background()

	// Move to terminal DONE
	_, err = h.engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	// Force state back to CREATED (admin recovery from terminal)
	result, err := h.engine.ForceState(ctx, "order", "o-1", "CREATED", "admin-1", "Reopening order")
	if err != nil {
		t.Fatalf("force state from terminal failed: %v", err)
	}
	if result.NewState != "CREATED" {
		t.Errorf("expected CREATED, got %s", result.NewState)
	}
}

func TestEngineShutdown(t *testing.T) {
	h := newTestHarness(t)
	def, err := flowstate.Define("order", "simple").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete",
			flowstate.From("CREATED"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
		).
		Build()
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	h.engine.Register(def)
	ctx := context.Background()

	// Normal operation works
	_, err = h.engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}

	// Shutdown the engine
	err = h.engine.Shutdown(ctx)
	if err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	// After shutdown, transitions should fail
	_, err = h.engine.Transition(ctx, "order", "o-2", "complete", "user-1", nil)
	if err == nil {
		t.Error("expected error after shutdown")
	}
	if !errors.Is(err, flowstate.ErrEngineShutdown) {
		t.Errorf("expected ErrEngineShutdown, got: %v", err)
	}
}

// TestEngineConcurrentRegister verifies no data race when Register is called
// concurrently with Transition (which calls definitionFor). Must be run with -race.
func TestEngineConcurrentRegister(t *testing.T) {
	def1, err := flowstate.Define("order", "v1").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete", flowstate.From("CREATED"), flowstate.To("DONE"), flowstate.Event("Completed")).
		Build()
	if err != nil {
		t.Fatalf("build def1: %v", err)
	}
	def2, err := flowstate.Define("order", "v2").
		Version(2).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete", flowstate.From("CREATED"), flowstate.To("DONE"), flowstate.Event("Completed")).
		Build()
	if err != nil {
		t.Fatalf("build def2: %v", err)
	}

	h := newTestHarness(t)
	if err := h.engine.Register(def1); err != nil {
		t.Fatalf("register def1: %v", err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrently register new versions
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if err := h.engine.Register(def2); err != nil {
				return
			}
		}
	}()

	// Concurrently trigger transitions (calls definitionFor internally)
	for i := 0; i < 50; i++ {
		_, _ = h.engine.Transition(ctx, "order", fmt.Sprintf("concurrent-%d", i), "complete", "user", nil)
	}

	wg.Wait()
}

// TestEngineSignalReturnsErrEngineShutdown verifies Signal returns ErrEngineShutdown after Shutdown.
func TestEngineSignalReturnsErrEngineShutdown(t *testing.T) {
	def, err := flowstate.Define("order", "signal-test").
		Version(1).
		States(
			flowstate.Initial("WAITING"),
			flowstate.Terminal("DONE"),
		).
		Transition("done",
			flowstate.From("WAITING"),
			flowstate.To("DONE"),
			flowstate.OnSignal("finish"),
			flowstate.Event("Finished"),
		).
		Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	h := newTestHarness(t)
	if err := h.engine.Register(def); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()

	// Create the instance first
	_, err = h.engine.Transition(ctx, "order", "sig-1", "done", "user", nil)
	// Transition via signal-driven transition from WAITING state
	// Instead, let's use Signal after Shutdown
	if err := h.engine.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	_, err = h.engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "order",
		TargetAggregateID:   "sig-1",
		SignalName:          "finish",
	})
	if err == nil {
		t.Fatal("expected error from Signal after shutdown")
	}
	if !errors.Is(err, flowstate.ErrEngineShutdown) {
		t.Errorf("expected ErrEngineShutdown, got: %v", err)
	}
}

// TestEngineForceStateReturnsErrEngineShutdown verifies ForceState returns ErrEngineShutdown after Shutdown.
func TestEngineForceStateReturnsErrEngineShutdown(t *testing.T) {
	def, err := flowstate.Define("order", "force-test").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.Terminal("DONE"),
		).
		Transition("complete", flowstate.From("CREATED"), flowstate.To("DONE"), flowstate.Event("Completed")).
		Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	h := newTestHarness(t)
	if err := h.engine.Register(def); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()

	if err := h.engine.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	_, err = h.engine.ForceState(ctx, "order", "f-1", "DONE", "admin", "test")
	if err == nil {
		t.Fatal("expected error from ForceState after shutdown")
	}
	if !errors.Is(err, flowstate.ErrEngineShutdown) {
		t.Errorf("expected ErrEngineShutdown, got: %v", err)
	}
}

// Ensure interfaces are compatible
var _ types.Guard = (*alwaysFailGuard)(nil)
var _ types.Guard = (*passingGuard)(nil)
var _ types.Condition = (*amountCondition)(nil)

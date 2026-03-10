package flowstep_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/adapters/chanbus"
	"github.com/mawkeye/flowstep/adapters/memstore"
	"github.com/mawkeye/flowstep/types"
)

type testHarness struct {
	engine        *flowstep.Engine
	eventStore    *memstore.EventStore
	instanceStore *memstore.InstanceStore
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(es),
		flowstep.WithInstanceStore(is),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
		flowstep.WithEventBus(chanbus.New()),
		flowstep.WithClock(flowstep.RealClock{}),
		flowstep.WithHooks(flowstep.NoopHooks{}),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return &testHarness{engine: engine, eventStore: es, instanceStore: is}
}

func TestEngineBasicTransition(t *testing.T) {
	def, err := flowstep.Define("order", "simple").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
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
	def, err := flowstep.Define("order", "multi").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PAID"),
			flowstep.Terminal("SHIPPED"),
		).
		Transition("pay", flowstep.From("CREATED"), flowstep.To("PAID"), flowstep.Event("OrderPaid")).
		Transition("ship", flowstep.From("PAID"), flowstep.To("SHIPPED"), flowstep.Event("OrderShipped")).
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
	def, err := flowstep.Define("order", "simple").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PAID"),
			flowstep.Terminal("DONE"),
		).
		Transition("pay", flowstep.From("CREATED"), flowstep.To("PAID"), flowstep.Event("OrderPaid")).
		Transition("finish", flowstep.From("PAID"), flowstep.To("DONE"), flowstep.Event("OrderDone")).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()
	_, err = h.engine.Transition(ctx, "order", "o-1", "finish", "user-1", nil)
	if !errors.Is(err, flowstep.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestEngineAlreadyTerminal(t *testing.T) {
	def, err := flowstep.Define("order", "simple").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete", flowstep.From("CREATED"), flowstep.To("DONE"), flowstep.Event("OrderCompleted")).
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
	if !errors.Is(err, flowstep.ErrAlreadyTerminal) {
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
	def, err := flowstep.Define("order", "guarded").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
			flowstep.Guards(&alwaysFailGuard{reason: "not ready"}),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)

	ctx := context.Background()
	_, err = h.engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if !errors.Is(err, flowstep.ErrGuardFailed) {
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
	def, err := flowstep.Define("order", "guarded").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
			flowstep.Guards(&passingGuard{}),
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
	def, err := flowstep.Define("order", "chain").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PAID"),
			flowstep.Terminal("SHIPPED"),
		).
		Transition("pay", flowstep.From("CREATED"), flowstep.To("PAID"), flowstep.Event("OrderPaid")).
		Transition("ship", flowstep.From("PAID"), flowstep.To("SHIPPED"), flowstep.Event("OrderShipped")).
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

func newTestHarnessWithActivities(t *testing.T, activities ...flowstep.Activity) (*testHarness, *memstore.ActivityStore) {
	t.Helper()
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	as := memstore.NewActivityStore()
	runner := newRecordingRunner(activities...)
	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(es),
		flowstep.WithInstanceStore(is),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
		flowstep.WithEventBus(chanbus.New()),
		flowstep.WithActivityStore(as),
		flowstep.WithActivityRunner(runner),
		flowstep.WithClock(flowstep.RealClock{}),
		flowstep.WithHooks(flowstep.NoopHooks{}),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return &testHarness{engine: engine, eventStore: es, instanceStore: is}, as
}

type recordingRunner struct {
	activities map[string]flowstep.Activity
	dispatched []string
}

func newRecordingRunner(activities ...flowstep.Activity) *recordingRunner {
	r := &recordingRunner{activities: make(map[string]flowstep.Activity)}
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
	return flowstep.ErrActivityNotRegistered
}

func TestEngineActivityDispatch(t *testing.T) {
	act := &recordingActivity{name: "send_email"}
	h, actStore := newTestHarnessWithActivities(t, act)

	def, err := flowstep.Define("order", "with_activity").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
			flowstep.Dispatch("send_email"),
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

	def, err := flowstep.Define("order", "multi_act").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
			flowstep.Dispatch("send_email"),
			flowstep.Dispatch("update_crm"),
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
	def, err := flowstep.Define("order", "signal_wf").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PAYMENT_PENDING"),
			flowstep.Terminal("PAID"),
		).
		Transition("start_payment",
			flowstep.From("CREATED"),
			flowstep.To("PAYMENT_PENDING"),
			flowstep.Event("PaymentStarted"),
		).
		Transition("payment_received",
			flowstep.From("PAYMENT_PENDING"),
			flowstep.To("PAID"),
			flowstep.Event("PaymentReceived"),
			flowstep.OnSignal("payment_complete"),
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
	def, err := flowstep.Define("order", "signal_wf").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("Done"),
			flowstep.OnSignal("finish"),
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
	if !errors.Is(err, flowstep.ErrNoMatchingSignal) {
		t.Errorf("expected ErrNoMatchingSignal, got %v", err)
	}
}

// --- Wait State / Task tests ---

func newTestHarnessWithTasks(t *testing.T) (*testHarness, *memstore.TaskStore) {
	t.Helper()
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	ts := memstore.NewTaskStore()
	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(es),
		flowstep.WithInstanceStore(is),
		flowstep.WithTaskStore(ts),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
		flowstep.WithEventBus(chanbus.New()),
		flowstep.WithClock(flowstep.RealClock{}),
		flowstep.WithHooks(flowstep.NoopHooks{}),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return &testHarness{engine: engine, eventStore: es, instanceStore: is}, ts
}

func TestEngineWaitStateAndTaskCompletion(t *testing.T) {
	def, err := flowstep.Define("approval", "review").
		Version(1).
		States(
			flowstep.Initial("SUBMITTED"),
			flowstep.WaitState("AWAITING_REVIEW"),
			flowstep.Terminal("APPROVED"),
			flowstep.Terminal("REJECTED"),
		).
		Transition("submit_for_review",
			flowstep.From("SUBMITTED"),
			flowstep.To("AWAITING_REVIEW"),
			flowstep.Event("SubmittedForReview"),
			flowstep.EmitTask(types.TaskDef{
				Type:        "review_decision",
				Description: "Please review and approve or reject",
				Options:     []string{"approve", "reject"},
			}),
		).
		Transition("approve",
			flowstep.From("AWAITING_REVIEW"),
			flowstep.To("APPROVED"),
			flowstep.Event("Approved"),
			flowstep.OnTaskCompleted("review_decision"),
		).
		Transition("reject",
			flowstep.From("AWAITING_REVIEW"),
			flowstep.To("REJECTED"),
			flowstep.Event("Rejected"),
			flowstep.OnTaskCompleted("review_decision"),
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
	def, err := flowstep.Define("approval", "review").
		Version(1).
		States(
			flowstep.Initial("SUBMITTED"),
			flowstep.WaitState("AWAITING_REVIEW"),
			flowstep.Terminal("APPROVED"),
			flowstep.Terminal("REJECTED"),
		).
		Transition("submit_for_review",
			flowstep.From("SUBMITTED"),
			flowstep.To("AWAITING_REVIEW"),
			flowstep.Event("SubmittedForReview"),
			flowstep.EmitTask(types.TaskDef{
				Type:        "review_decision",
				Description: "Review it",
				Options:     []string{"approve", "reject"},
			}),
		).
		Transition("approve",
			flowstep.From("AWAITING_REVIEW"),
			flowstep.To("APPROVED"),
			flowstep.Event("Approved"),
			flowstep.OnTaskCompleted("review_decision"),
		).
		Transition("reject",
			flowstep.From("AWAITING_REVIEW"),
			flowstep.To("REJECTED"),
			flowstep.Event("Rejected"),
			flowstep.OnTaskCompleted("review_decision"),
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
	def, err := flowstep.Define("order", "routed").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("STANDARD_REVIEW"),
			flowstep.State("VIP_REVIEW"),
			flowstep.Terminal("DONE"),
		).
		Transition("route_review",
			flowstep.From("CREATED"),
			flowstep.Event("ReviewRouted"),
			flowstep.Route(flowstep.When(&amountCondition{threshold: 1000}), flowstep.To("VIP_REVIEW")),
			flowstep.Route(flowstep.Default(), flowstep.To("STANDARD_REVIEW")),
		).
		Transition("finish_standard",
			flowstep.From("STANDARD_REVIEW"),
			flowstep.To("DONE"),
			flowstep.Event("Done"),
		).
		Transition("finish_vip",
			flowstep.From("VIP_REVIEW"),
			flowstep.To("DONE"),
			flowstep.Event("Done"),
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
	def, err := flowstep.Define("order", "no_default").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("VIP"),
			flowstep.Terminal("DONE"),
		).
		Transition("route",
			flowstep.From("CREATED"),
			flowstep.Event("Routed"),
			flowstep.Route(flowstep.When(&amountCondition{threshold: 1000}), flowstep.To("VIP")),
		).
		Transition("finish",
			flowstep.From("VIP"),
			flowstep.To("DONE"),
			flowstep.Event("Done"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	h := newTestHarness(t)
	h.engine.Register(def)
	ctx := context.Background()

	_, err = h.engine.Transition(ctx, "order", "o-1", "route", "user-1", map[string]any{"amount": 100.0})
	if !errors.Is(err, flowstep.ErrNoMatchingRoute) {
		t.Errorf("expected ErrNoMatchingRoute, got %v", err)
	}
}

// --- Child Workflow tests ---

func newFullTestHarness(t *testing.T) (*testHarness, *memstore.ChildStore) {
	t.Helper()
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	cs := memstore.NewChildStore()
	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(es),
		flowstep.WithInstanceStore(is),
		flowstep.WithChildStore(cs),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
		flowstep.WithEventBus(chanbus.New()),
		flowstep.WithClock(flowstep.RealClock{}),
		flowstep.WithHooks(flowstep.NoopHooks{}),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return &testHarness{engine: engine, eventStore: es, instanceStore: is}, cs
}

func TestEngineSpawnChild(t *testing.T) {
	// Parent workflow spawns a child when transitioning
	parentDef, err := flowstep.Define("order", "parent_wf").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PROCESSING"),
			flowstep.Terminal("DONE"),
		).
		Transition("start_processing",
			flowstep.From("CREATED"),
			flowstep.To("PROCESSING"),
			flowstep.Event("ProcessingStarted"),
			flowstep.SpawnChild(types.ChildDef{
				WorkflowType: "payment",
				InputFrom:    "order_data",
			}),
		).
		Transition("processing_done",
			flowstep.From("PROCESSING"),
			flowstep.To("DONE"),
			flowstep.Event("ProcessingDone"),
			flowstep.OnChildCompleted("payment"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build parent definition: %v", err)
	}

	childDef, err := flowstep.Define("payment", "payment_wf").
		Version(1).
		States(
			flowstep.Initial("PENDING"),
			flowstep.Terminal("COMPLETED"),
		).
		Transition("complete",
			flowstep.From("PENDING"),
			flowstep.To("COMPLETED"),
			flowstep.Event("PaymentCompleted"),
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
	parentDef, err := flowstep.Define("batch", "batch_wf").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PROCESSING"),
			flowstep.Terminal("DONE"),
		).
		Transition("start_batch",
			flowstep.From("CREATED"),
			flowstep.To("PROCESSING"),
			flowstep.Event("BatchStarted"),
			flowstep.SpawnChildren(types.ChildrenDef{
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
			flowstep.From("PROCESSING"),
			flowstep.To("DONE"),
			flowstep.Event("BatchDone"),
			flowstep.OnChildrenJoined(),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build parent definition: %v", err)
	}

	childDef, err := flowstep.Define("job", "job_wf").
		Version(1).
		States(
			flowstep.Initial("PENDING"),
			flowstep.Terminal("COMPLETED"),
		).
		Transition("finish",
			flowstep.From("PENDING"),
			flowstep.To("COMPLETED"),
			flowstep.Event("JobFinished"),
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

	defV1, err := flowstep.Define("ticket", "support").
		Version(1).
		States(
			flowstep.Initial("OPEN"),
			flowstep.State("REVIEWING"),
			flowstep.Terminal("CLOSED"),
		).
		Transition("review",
			flowstep.From("OPEN"),
			flowstep.To("REVIEWING"),
			flowstep.Event("TicketReviewing"),
		).
		Transition("close",
			flowstep.From("REVIEWING"),
			flowstep.To("CLOSED"),
			flowstep.Event("TicketClosed"),
		).
		Build()
	if err != nil {
		t.Fatalf("v1 build failed: %v", err)
	}

	defV2, err := flowstep.Define("ticket", "support").
		Version(2).
		States(
			flowstep.Initial("OPEN"),
			flowstep.State("IN_PROGRESS"),
			flowstep.Terminal("RESOLVED"),
		).
		Transition("start_work",
			flowstep.From("OPEN"),
			flowstep.To("IN_PROGRESS"),
			flowstep.Event("WorkStarted"),
		).
		Transition("resolve",
			flowstep.From("IN_PROGRESS"),
			flowstep.To("RESOLVED"),
			flowstep.Event("TicketResolved"),
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
	if !errors.Is(err, flowstep.ErrInvalidTransition) {
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
func (h *recordingHooks) OnPostCommitError(context.Context, string, error)                                     {}

func TestEngineHooksCalledOnTransition(t *testing.T) {
	hooks := &recordingHooks{}
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(es),
		flowstep.WithInstanceStore(is),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
		flowstep.WithEventBus(chanbus.New()),
		flowstep.WithHooks(hooks),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	def, err := flowstep.Define("order", "simple").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
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
	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(es),
		flowstep.WithInstanceStore(is),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
		flowstep.WithHooks(hooks),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	def, err := flowstep.Define("order", "guarded").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
			flowstep.Guards(&alwaysFailGuard{}),
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

	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(es),
		flowstep.WithInstanceStore(is),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
		flowstep.WithActivityRunner(runner),
		flowstep.WithActivityStore(memstore.NewActivityStore()),
		flowstep.WithHooks(hooks),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	def, err := flowstep.Define("order", "with_activity").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
			flowstep.Dispatch("send_email"),
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
	def, err := flowstep.Define("order", "simple").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PROCESSING"),
			flowstep.Terminal("DONE"),
			flowstep.Terminal("CANCELLED"),
		).
		Transition("process",
			flowstep.From("CREATED"),
			flowstep.To("PROCESSING"),
			flowstep.Event("OrderProcessing"),
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
	def, err := flowstep.Define("order", "simple").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
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
	def, err := flowstep.Define("order", "simple").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
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
	if !errors.Is(err, flowstep.ErrEngineShutdown) {
		t.Errorf("expected ErrEngineShutdown, got: %v", err)
	}
}

// TestEngineConcurrentRegister verifies no data race when Register is called
// concurrently with Transition (which calls definitionFor). Must be run with -race.
func TestEngineConcurrentRegister(t *testing.T) {
	def1, err := flowstep.Define("order", "v1").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete", flowstep.From("CREATED"), flowstep.To("DONE"), flowstep.Event("Completed")).
		Build()
	if err != nil {
		t.Fatalf("build def1: %v", err)
	}
	def2, err := flowstep.Define("order", "v2").
		Version(2).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete", flowstep.From("CREATED"), flowstep.To("DONE"), flowstep.Event("Completed")).
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
	def, err := flowstep.Define("order", "signal-test").
		Version(1).
		States(
			flowstep.Initial("WAITING"),
			flowstep.Terminal("DONE"),
		).
		Transition("done",
			flowstep.From("WAITING"),
			flowstep.To("DONE"),
			flowstep.OnSignal("finish"),
			flowstep.Event("Finished"),
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
	if !errors.Is(err, flowstep.ErrEngineShutdown) {
		t.Errorf("expected ErrEngineShutdown, got: %v", err)
	}
}

// TestEngineForceStateReturnsErrEngineShutdown verifies ForceState returns ErrEngineShutdown after Shutdown.
func TestEngineForceStateReturnsErrEngineShutdown(t *testing.T) {
	def, err := flowstep.Define("order", "force-test").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete", flowstep.From("CREATED"), flowstep.To("DONE"), flowstep.Event("Completed")).
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
	if !errors.Is(err, flowstep.ErrEngineShutdown) {
		t.Errorf("expected ErrEngineShutdown, got: %v", err)
	}
}

// failingEventBus always returns an error from Emit.
type failingEventBus struct{ err error }

func (b *failingEventBus) Emit(_ context.Context, _ types.DomainEvent) error { return b.err }

// captureHooks records OnPostCommitError calls.
type captureHooks struct {
	flowstep.NoopHooks
	postCommitErrors []flowstep.PostCommitWarning
}

func (h *captureHooks) OnPostCommitError(_ context.Context, operation string, err error) {
	h.postCommitErrors = append(h.postCommitErrors, flowstep.PostCommitWarning{Operation: operation, Err: err})
}

func TestEngineOnPostCommitErrorHookFires(t *testing.T) {
	def, err := flowstep.Define("order", "hook-test").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete", flowstep.From("CREATED"), flowstep.To("DONE"), flowstep.Event("Completed")).
		Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	busErr := fmt.Errorf("bus unavailable")
	hooks := &captureHooks{}
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(es),
		flowstep.WithInstanceStore(is),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
		flowstep.WithEventBus(&failingEventBus{err: busErr}),
		flowstep.WithClock(flowstep.RealClock{}),
		flowstep.WithHooks(hooks),
	)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	if err := engine.Register(def); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := context.Background()
	result, err := engine.Transition(ctx, "order", "o-hook-1", "complete", "user", nil)
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}

	// Hook should have fired once for EventBus.Emit
	if len(hooks.postCommitErrors) != 1 {
		t.Fatalf("expected 1 post-commit error, got %d", len(hooks.postCommitErrors))
	}
	if hooks.postCommitErrors[0].Operation != "EventBus.Emit" {
		t.Errorf("expected operation 'EventBus.Emit', got %q", hooks.postCommitErrors[0].Operation)
	}
	if !errors.Is(hooks.postCommitErrors[0].Err, busErr) {
		t.Errorf("expected bus error, got %v", hooks.postCommitErrors[0].Err)
	}

	// TransitionResult should contain the warning
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning in result, got %d", len(result.Warnings))
	}
	if result.Warnings[0].Operation != "EventBus.Emit" {
		t.Errorf("expected warning operation 'EventBus.Emit', got %q", result.Warnings[0].Operation)
	}
}

// TestEngineCompleteTaskReturnsErrEngineShutdown verifies CompleteTask returns ErrEngineShutdown after Shutdown.
func TestEngineCompleteTaskReturnsErrEngineShutdown(t *testing.T) {
	def, err := flowstep.Define("order", "completetask-shutdown-test").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete", flowstep.From("CREATED"), flowstep.To("DONE"), flowstep.Event("Completed")).
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

	_, err = h.engine.CompleteTask(ctx, "some-task-id", "approve", "user1")
	if err == nil {
		t.Fatal("expected error from CompleteTask after shutdown")
	}
	if !errors.Is(err, flowstep.ErrEngineShutdown) {
		t.Errorf("expected ErrEngineShutdown, got: %v", err)
	}
}

// Ensure interfaces are compatible
var _ types.Guard = (*alwaysFailGuard)(nil)
var _ types.Guard = (*passingGuard)(nil)
var _ types.Condition = (*amountCondition)(nil)

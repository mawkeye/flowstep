package flowstate_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

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

// Ensure Guard interface from types package is compatible
var _ types.Guard = (*alwaysFailGuard)(nil)
var _ types.Guard = (*passingGuard)(nil)

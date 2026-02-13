package flowstate_test

import (
	"context"
	"testing"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/adapters/chanbus"
	"github.com/mawkeye/flowstate/adapters/memstore"
)

func newTestEngine(t *testing.T) *flowstate.Engine {
	t.Helper()
	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(memstore.NewEventStore()),
		flowstate.WithInstanceStore(memstore.NewInstanceStore()),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
		flowstate.WithEventBus(chanbus.New()),
		flowstate.WithClock(flowstate.RealClock{}),
		flowstate.WithHooks(flowstate.NoopHooks{}),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return engine
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

	engine := newTestEngine(t)
	engine.Register(def)

	ctx := context.Background()
	result, err := engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
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

	engine := newTestEngine(t)
	engine.Register(def)

	ctx := context.Background()

	result, err := engine.Transition(ctx, "order", "o-1", "pay", "user-1", nil)
	if err != nil {
		t.Fatalf("pay transition failed: %v", err)
	}
	if result.NewState != "PAID" {
		t.Errorf("expected PAID, got %s", result.NewState)
	}
	if result.IsTerminal {
		t.Error("PAID should not be terminal")
	}

	result, err = engine.Transition(ctx, "order", "o-1", "ship", "user-1", nil)
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

	engine := newTestEngine(t)
	engine.Register(def)

	ctx := context.Background()
	_, err = engine.Transition(ctx, "order", "o-1", "finish", "user-1", nil)
	if err == nil {
		t.Fatal("expected error for invalid transition")
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

	engine := newTestEngine(t)
	engine.Register(def)

	ctx := context.Background()

	_, err = engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("first transition failed: %v", err)
	}

	_, err = engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err == nil {
		t.Fatal("expected error for already terminal workflow")
	}
}

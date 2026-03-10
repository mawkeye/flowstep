// Example 03: Signals (External Event Triggers)
//
// Demonstrates signal-triggered transitions — useful for payment callbacks,
// webhook events, or any async notification from an external system.
//
//   - OnSignal: declares that a transition is triggered by a named signal
//   - engine.Signal: sends a signal at runtime to trigger the matching transition
//
// Run from the module root:
//
//	go run ./examples/03-signals/
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/adapters/memstore"
	"github.com/mawkeye/flowstep/types"
)

func main() {
	// 1. Define the workflow
	//
	// States: CREATED → AWAITING_PAYMENT → CONFIRMED or CANCELLED
	//
	// The AWAITING_PAYMENT state has two signal-triggered transitions:
	//   "payment_succeeded" signal → CONFIRMED
	//   "payment_failed"    signal → CANCELLED
	def, err := flowstep.Define("booking", "booking_workflow").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("AWAITING_PAYMENT"),
			flowstep.Terminal("CONFIRMED"),
			flowstep.Terminal("CANCELLED"),
		).
		Transition("initiate_payment",
			flowstep.From("CREATED"),
			flowstep.To("AWAITING_PAYMENT"),
			flowstep.Event("PaymentInitiated"),
		).
		Transition("payment_ok",
			flowstep.From("AWAITING_PAYMENT"),
			flowstep.To("CONFIRMED"),
			flowstep.OnSignal("payment_succeeded"),
			flowstep.Event("BookingConfirmed"),
		).
		Transition("payment_fail",
			flowstep.From("AWAITING_PAYMENT"),
			flowstep.To("CANCELLED"),
			flowstep.OnSignal("payment_failed"),
			flowstep.Event("BookingCancelled"),
		).
		Build()
	if err != nil {
		log.Fatalf("build workflow: %v", err)
	}

	// Write Mermaid diagram
	md := "# Booking Workflow\n\n```mermaid\n" + types.Mermaid(def) + "```\n"
	if err := os.WriteFile("examples/03-signals/workflow.md", []byte(md), 0644); err != nil {
		log.Fatalf("write workflow.md: %v", err)
	}
	fmt.Println("Wrote Mermaid diagram to examples/03-signals/workflow.md")

	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(memstore.NewEventStore()),
		flowstep.WithInstanceStore(memstore.NewInstanceStore()),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
	)
	if err != nil {
		log.Fatalf("create engine: %v", err)
	}
	defer engine.Shutdown(context.Background())

	if err := engine.Register(def); err != nil {
		log.Fatalf("register workflow: %v", err)
	}

	ctx := context.Background()

	// 2. Path A: booking-1 succeeds via payment_succeeded signal
	fmt.Println("--- Booking 1: payment succeeds ---")

	result, err := engine.Transition(ctx, "booking", "booking-1", "initiate_payment", "user-1", nil)
	if err != nil {
		log.Fatalf("initiate_payment: %v", err)
	}
	fmt.Printf("Transition: %s -> %s\n", result.PreviousState, result.NewState)
	// Output: Transition: CREATED -> AWAITING_PAYMENT

	// External payment system sends a success signal
	result, err = engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "booking-1",
		SignalName:          "payment_succeeded",
		ActorID:             "payment-gateway",
		Payload:             map[string]any{"transaction_id": "txn-abc123"},
	})
	if err != nil {
		log.Fatalf("signal payment_succeeded: %v", err)
	}
	fmt.Printf("Signal:     payment_succeeded -> %s (terminal: %v)\n", result.NewState, result.IsTerminal)
	// Output: Signal: payment_succeeded -> CONFIRMED (terminal: true)

	// 3. Path B: booking-2 fails via payment_failed signal
	fmt.Println("\n--- Booking 2: payment fails ---")

	result, err = engine.Transition(ctx, "booking", "booking-2", "initiate_payment", "user-2", nil)
	if err != nil {
		log.Fatalf("initiate_payment booking-2: %v", err)
	}
	fmt.Printf("Transition: %s -> %s\n", result.PreviousState, result.NewState)

	// External payment system sends a failure signal
	result, err = engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "booking-2",
		SignalName:          "payment_failed",
		ActorID:             "payment-gateway",
		Payload:             map[string]any{"reason": "insufficient_funds"},
	})
	if err != nil {
		log.Fatalf("signal payment_failed: %v", err)
	}
	fmt.Printf("Signal:     payment_failed -> %s (terminal: %v)\n", result.NewState, result.IsTerminal)
	// Output: Signal: payment_failed -> CANCELLED (terminal: true)
}

// Example 01: Basic Linear Workflow
//
// Demonstrates the simplest possible flowstep setup:
//   - Define a workflow with states and transitions
//   - Create an engine with in-memory adapters
//   - Execute transitions and inspect results
//
// Run from the module root:
//
//	go run ./examples/01-basic-linear/
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
	// States:    CREATED (initial) → PROCESSING → DONE (terminal)
	//                         ↘            ↘
	//                          CANCELLED (terminal)
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
		log.Fatalf("build workflow: %v", err)
	}

	// 2. Write Mermaid diagram
	md := "# Order Workflow\n\n```mermaid\n" + types.Mermaid(def) + "```\n"
	if err := os.WriteFile("examples/01-basic-linear/workflow.md", []byte(md), 0644); err != nil {
		log.Fatalf("write workflow.md: %v", err)
	}
	fmt.Println("Wrote Mermaid diagram to examples/01-basic-linear/workflow.md")

	// 3. Create an engine — only three adapters are required
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

	// 3. Execute transitions
	//
	// First transition: CREATED → PROCESSING
	result, err := engine.Transition(ctx, "order", "order-1", "start_processing", "system", nil)
	if err != nil {
		log.Fatalf("start_processing: %v", err)
	}
	fmt.Printf("Transition: %s -> %s\n", result.PreviousState, result.NewState)
	// Output: Transition: CREATED -> PROCESSING

	// Second transition: PROCESSING → DONE
	result, err = engine.Transition(ctx, "order", "order-1", "complete", "system", nil)
	if err != nil {
		log.Fatalf("complete: %v", err)
	}
	fmt.Printf("Transition: %s -> %s (terminal: %v)\n", result.PreviousState, result.NewState, result.IsTerminal)
	// Output: Transition: PROCESSING -> DONE (terminal: true)

	// 4. A second order — cancelled mid-flight
	result, err = engine.Transition(ctx, "order", "order-2", "start_processing", "system", nil)
	if err != nil {
		log.Fatalf("start_processing order-2: %v", err)
	}
	fmt.Printf("Transition: %s -> %s\n", result.PreviousState, result.NewState)

	result, err = engine.Transition(ctx, "order", "order-2", "cancel", "user-1", nil)
	if err != nil {
		log.Fatalf("cancel: %v", err)
	}
	fmt.Printf("Transition: %s -> %s (terminal: %v)\n", result.PreviousState, result.NewState, result.IsTerminal)
	// Output: Transition: PROCESSING -> CANCELLED (terminal: true)
}

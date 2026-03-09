// Example 01: Basic Linear Workflow
//
// Demonstrates the simplest possible flowstate setup:
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

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/adapters/memstore"
	"github.com/mawkeye/flowstate/types"
)

func main() {
	// 1. Define the workflow
	//
	// States:    CREATED (initial) → PROCESSING → DONE (terminal)
	//                         ↘            ↘
	//                          CANCELLED (terminal)
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
		log.Fatalf("build workflow: %v", err)
	}

	// 2. Write Mermaid diagram
	md := "# Order Workflow\n\n```mermaid\n" + types.Mermaid(def) + "```\n"
	if err := os.WriteFile("examples/01-basic-linear/workflow.md", []byte(md), 0644); err != nil {
		log.Fatalf("write workflow.md: %v", err)
	}
	fmt.Println("Wrote Mermaid diagram to examples/01-basic-linear/workflow.md")

	// 3. Create an engine — only three adapters are required
	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(memstore.NewEventStore()),
		flowstate.WithInstanceStore(memstore.NewInstanceStore()),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
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

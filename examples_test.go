package flowstep_test

import (
	"context"
	"fmt"
	"log"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/adapters/memstore"
)

// Example demonstrates how to define a simple workflow and execute a transition.
func Example() {
	// 1. Define the workflow
	def, err := flowstep.Define("order", "simple_order").
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
		log.Fatal(err)
	}

	// 2. Initialize the engine with in-memory adapters
	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(memstore.NewEventStore()),
		flowstep.WithInstanceStore(memstore.NewInstanceStore()),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 3. Register the definition
	if err := engine.Register(def); err != nil {
		log.Fatal(err)
	}

	// 4. Execute a transition
	// Note: The engine automatically creates the instance if it doesn't exist
	// because the transition is from the Initial state.
	ctx := context.Background()
	result, err := engine.Transition(ctx, "order", "order-123", "complete", "actor-1", nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("New State: %s\n", result.NewState)
	fmt.Printf("Is Terminal: %v\n", result.IsTerminal)

	// Output:
	// New State: DONE
	// Is Terminal: true
}

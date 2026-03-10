// Example 06: Parallel Children with Fan-Out/Join + Signals
//
// The most complex example, combining:
//
//   - SpawnChildren: fan-out to N parallel child workflows
//   - JoinAll: resume parent after ALL children complete
//   - OnChildrenJoined: parent transition that fires when join policy is satisfied
//   - OnSignal / engine.Signal: external event triggers final state
//
// Scenario — purchase order with parallel item processing:
//
//	Parent ORDER:  CREATED → PROCESSING → AWAITING_ITEMS → AWAITING_PAYMENT → CONFIRMED/CANCELLED
//	Child ITEM:    CREATED → PROCESSING → DONE
//
// The order fans out to 3 item workflows. When all items finish (JoinAll), the
// parent moves to AWAITING_PAYMENT. An external payment signal then confirms
// or cancels the order.
//
// Run from the module root:
//
//	go run ./examples/06-parallel-children-signals/
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/adapters/memstore"
	"github.com/mawkeye/flowstep/types"
)

const itemWorkflowType = "item_workflow"

// buildOrderWorkflow defines the parent purchase order workflow.
func buildOrderWorkflow() (*types.Definition, error) {
	return flowstep.Define("purchase_order", "purchase_order_workflow").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PROCESSING"),
			flowstep.State("AWAITING_ITEMS"),
			flowstep.State("AWAITING_PAYMENT"),
			flowstep.Terminal("CONFIRMED"),
			flowstep.Terminal("CANCELLED"),
		).
		Transition("start",
			flowstep.From("CREATED"),
			flowstep.To("PROCESSING"),
			flowstep.Event("OrderStarted"),
		).
		Transition("fan_out",
			flowstep.From("PROCESSING"),
			flowstep.To("AWAITING_ITEMS"),
			flowstep.Event("ItemsFannedOut"),
			// SpawnChildren fans out to multiple parallel item workflows.
			// InputsFn receives nil (called with aggregate=nil by engine).
			// Return one map per child — here we spawn 3 items.
			flowstep.SpawnChildren(types.ChildrenDef{
				WorkflowType: itemWorkflowType,
				InputsFn: func(_ any) []map[string]any {
					return []map[string]any{
						{"item_id": "item-A", "sku": "SKU-001"},
						{"item_id": "item-B", "sku": "SKU-002"},
						{"item_id": "item-C", "sku": "SKU-003"},
					}
				},
				// JoinAll: parent resumes only after ALL 3 children complete.
				Join: types.JoinAll(),
			}),
		).
		// Fires when all children in the group have reached a terminal state.
		Transition("items_ready",
			flowstep.From("AWAITING_ITEMS"),
			flowstep.To("AWAITING_PAYMENT"),
			flowstep.OnChildrenJoined(),
			flowstep.Event("ItemsReady"),
		).
		// Payment signals
		Transition("payment_ok",
			flowstep.From("AWAITING_PAYMENT"),
			flowstep.To("CONFIRMED"),
			flowstep.OnSignal("payment_succeeded"),
			flowstep.Event("OrderConfirmed"),
		).
		Transition("payment_fail",
			flowstep.From("AWAITING_PAYMENT"),
			flowstep.To("CANCELLED"),
			flowstep.OnSignal("payment_failed"),
			flowstep.Event("OrderCancelled"),
		).
		Build()
}

// buildItemWorkflow defines the child item processing workflow.
func buildItemWorkflow() (*types.Definition, error) {
	// Aggregate type "item" is distinct from parent's "purchase_order".
	return flowstep.Define("item", itemWorkflowType).
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PROCESSING"),
			flowstep.Terminal("DONE"),
			flowstep.Terminal("FAILED"),
		).
		Transition("process",
			flowstep.From("CREATED"),
			flowstep.To("PROCESSING"),
			flowstep.Event("ItemProcessingStarted"),
		).
		Transition("complete",
			flowstep.From("PROCESSING"),
			flowstep.To("DONE"),
			flowstep.Event("ItemCompleted"),
		).
		Transition("fail",
			flowstep.From("CREATED", "PROCESSING"),
			flowstep.To("FAILED"),
			flowstep.Event("ItemFailed"),
		).
		Build()
}

func main() {
	orderDef, err := buildOrderWorkflow()
	if err != nil {
		log.Fatalf("build order workflow: %v", err)
	}
	itemDef, err := buildItemWorkflow()
	if err != nil {
		log.Fatalf("build item workflow: %v", err)
	}

	// Write Mermaid diagrams
	md := "# Purchase Order Workflow\n\n```mermaid\n" + types.Mermaid(orderDef) + "```\n\n" +
		"# Item Workflow\n\n```mermaid\n" + types.Mermaid(itemDef) + "```\n"
	if err := os.WriteFile("examples/06-parallel-children-signals/workflow.md", []byte(md), 0644); err != nil {
		log.Fatalf("write workflow.md: %v", err)
	}
	fmt.Println("Wrote Mermaid diagrams to examples/06-parallel-children-signals/workflow.md")

	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(memstore.NewEventStore()),
		flowstep.WithInstanceStore(memstore.NewInstanceStore()),
		flowstep.WithChildStore(memstore.NewChildStore()),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
	)
	if err != nil {
		log.Fatalf("create engine: %v", err)
	}
	defer engine.Shutdown(context.Background())

	if err := engine.Register(orderDef); err != nil {
		log.Fatalf("register order workflow: %v", err)
	}
	if err := engine.Register(itemDef); err != nil {
		log.Fatalf("register item workflow: %v", err)
	}

	ctx := context.Background()

	// 1. Start the parent order
	fmt.Println("--- Parent: purchase_order workflow ---")

	result, err := engine.Transition(ctx, "purchase_order", "po-1", "start", "system", nil)
	if err != nil {
		log.Fatalf("start: %v", err)
	}
	fmt.Printf("Parent: %s -> %s\n", result.PreviousState, result.NewState)

	// 2. Fan out — spawns 3 item children
	result, err = engine.Transition(ctx, "purchase_order", "po-1", "fan_out", "system", nil)
	if err != nil {
		log.Fatalf("fan_out: %v", err)
	}
	fmt.Printf("Parent: %s -> %s (%d children spawned)\n",
		result.PreviousState, result.NewState, len(result.ChildrenSpawned))

	children := result.ChildrenSpawned
	if len(children) != 3 {
		log.Fatalf("expected 3 children, got %d", len(children))
	}

	// 3. Process each child item workflow
	fmt.Println("\n--- Children: item workflows ---")

	for i, child := range children {
		// Child runs as aggregate type "item"
		r, err := engine.Transition(ctx, "item", child.ChildAggregateID, "process", "worker", nil)
		if err != nil {
			log.Fatalf("item %d process: %v", i, err)
		}
		fmt.Printf("Item %d (%s): %s -> %s\n", i+1, child.ChildAggregateID[:8], r.PreviousState, r.NewState)

		r, err = engine.Transition(ctx, "item", child.ChildAggregateID, "complete", "worker", nil)
		if err != nil {
			log.Fatalf("item %d complete: %v", i, err)
		}
		fmt.Printf("Item %d (%s): %s -> %s (terminal: %v)\n",
			i+1, child.ChildAggregateID[:8], r.PreviousState, r.NewState, r.IsTerminal)

		// Notify the engine this child is done.
		// child.ChildAggregateType = ChildrenDef.WorkflowType = "item_workflow".
		// The engine evaluates JoinAll and returns ErrNoMatchingSignal (join not yet
		// satisfied) until the LAST child completes — that is expected, not an error.
		joinResult, err := engine.ChildCompleted(ctx, child.ChildAggregateType, child.ChildAggregateID, "DONE")
		if err != nil && !errors.Is(err, flowstep.ErrNoMatchingSignal) {
			log.Fatalf("ChildCompleted item %d: %v", i, err)
		}
		if joinResult != nil {
			// Join satisfied — parent resumed
			fmt.Printf("\nJoin satisfied! Parent: AWAITING_ITEMS -> %s\n", joinResult.NewState)
		}
	}

	// 4. Payment signal confirms the order
	fmt.Println("\n--- Signal: payment_succeeded ---")

	result, err = engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "purchase_order",
		TargetAggregateID:   "po-1",
		SignalName:          "payment_succeeded",
		ActorID:             "payment-gateway",
	})
	if err != nil {
		log.Fatalf("signal payment_succeeded: %v", err)
	}
	fmt.Printf("Parent: AWAITING_PAYMENT -> %s (terminal: %v)\n", result.NewState, result.IsTerminal)
	// Output: Parent: AWAITING_PAYMENT -> CONFIRMED (terminal: true)
}

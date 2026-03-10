// Example 05: Single Child Workflow
//
// Demonstrates spawning a child workflow from a parent and resuming the parent
// when the child completes.
//
//   - SpawnChild: spawns a single child workflow when a transition fires
//   - OnChildCompleted: declares that the parent resumes when the child completes
//   - engine.ChildCompleted: notifies the engine that a child reached a terminal state
//
// The parent waits in WAITING_FOR_SHIPMENT until the child SHIPMENT workflow
// reaches DELIVERED, then the parent advances to COMPLETED.
//
// Important details about child workflow wiring:
//
//   - The child workflow must have a distinct aggregate type from the parent.
//     The engine uses aggregate type as the registry key, so both workflows
//     sharing the same aggregate type would overwrite each other.
//
//   - ChildDef.WorkflowType becomes ChildRelation.ChildAggregateType (set by engine).
//     OnChildCompleted(key) and engine.ChildCompleted(ctx, key, ...) must all use
//     the same string as ChildDef.WorkflowType.
//
//   - Child transitions are run with engine.Transition(ctx, childAggregateType, childAggregateID, ...)
//     where childAggregateType is the child workflow's aggregate type ("shipment"),
//     NOT the ChildRelation.ChildAggregateType ("shipment_workflow").
//
// Run from the module root:
//
//	go run ./examples/05-child-workflow/
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

const (
	// childWorkflowType is used in:
	//   - Define("shipment", childWorkflowType)  — workflow type of the child
	//   - ChildDef.WorkflowType                  — tells engine which workflow to spawn
	//   - OnChildCompleted(childWorkflowType)     — parent listens for this child type
	//   - engine.ChildCompleted(ctx, childWorkflowType, ...)  — notify parent
	childWorkflowType = "shipment_workflow"
)

func buildOrderWorkflow() (*types.Definition, error) {
	// Aggregate type: "order" — distinct from the child's "shipment"
	return flowstep.Define("order", "order_workflow").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PROCESSING"),
			flowstep.State("WAITING_FOR_SHIPMENT"),
			flowstep.Terminal("COMPLETED"),
			flowstep.Terminal("CANCELLED"),
		).
		Transition("start_processing",
			flowstep.From("CREATED"),
			flowstep.To("PROCESSING"),
			flowstep.Event("OrderStarted"),
		).
		Transition("ship",
			flowstep.From("PROCESSING"),
			flowstep.To("WAITING_FOR_SHIPMENT"),
			flowstep.Event("ShipmentStarted"),
			// SpawnChild creates a ChildRelation with:
			//   ChildAggregateType = ChildDef.WorkflowType = "shipment_workflow"
			//   ChildAggregateID   = engine-generated UUID
			flowstep.SpawnChild(types.ChildDef{
				WorkflowType: childWorkflowType,
			}),
		).
		// Resume parent when the child of type childWorkflowType completes.
		// OnChildCompleted key must match ChildDef.WorkflowType and the
		// childAggregateType argument passed to engine.ChildCompleted.
		Transition("shipment_delivered",
			flowstep.From("WAITING_FOR_SHIPMENT"),
			flowstep.To("COMPLETED"),
			flowstep.OnChildCompleted(childWorkflowType),
			flowstep.Event("OrderCompleted"),
		).
		Transition("cancel",
			flowstep.From("CREATED", "PROCESSING"),
			flowstep.To("CANCELLED"),
			flowstep.Event("OrderCancelled"),
		).
		Build()
}

func buildShipmentWorkflow() (*types.Definition, error) {
	// Aggregate type: "shipment" — must differ from parent's "order"
	return flowstep.Define("shipment", childWorkflowType).
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("IN_TRANSIT"),
			flowstep.Terminal("DELIVERED"),
			flowstep.Terminal("FAILED"),
		).
		Transition("pick_up",
			flowstep.From("CREATED"),
			flowstep.To("IN_TRANSIT"),
			flowstep.Event("ShipmentPickedUp"),
		).
		Transition("deliver",
			flowstep.From("IN_TRANSIT"),
			flowstep.To("DELIVERED"),
			flowstep.Event("ShipmentDelivered"),
		).
		Transition("fail",
			flowstep.From("CREATED", "IN_TRANSIT"),
			flowstep.To("FAILED"),
			flowstep.Event("ShipmentFailed"),
		).
		Build()
}

func main() {
	orderDef, err := buildOrderWorkflow()
	if err != nil {
		log.Fatalf("build order workflow: %v", err)
	}
	shipmentDef, err := buildShipmentWorkflow()
	if err != nil {
		log.Fatalf("build shipment workflow: %v", err)
	}

	// Write Mermaid diagrams
	md := "# Order Workflow\n\n```mermaid\n" + types.Mermaid(orderDef) + "```\n\n" +
		"# Shipment Workflow\n\n```mermaid\n" + types.Mermaid(shipmentDef) + "```\n"
	if err := os.WriteFile("examples/05-child-workflow/workflow.md", []byte(md), 0644); err != nil {
		log.Fatalf("write workflow.md: %v", err)
	}
	fmt.Println("Wrote Mermaid diagrams to examples/05-child-workflow/workflow.md")

	// ChildStore is required for child workflows
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
	if err := engine.Register(shipmentDef); err != nil {
		log.Fatalf("register shipment workflow: %v", err)
	}

	ctx := context.Background()

	// 1. Start the parent order workflow
	fmt.Println("--- Parent: order workflow ---")

	result, err := engine.Transition(ctx, "order", "order-1", "start_processing", "system", nil)
	if err != nil {
		log.Fatalf("start_processing: %v", err)
	}
	fmt.Printf("Parent: %s -> %s\n", result.PreviousState, result.NewState)

	// 2. Ship the order — this spawns the child shipment workflow
	result, err = engine.Transition(ctx, "order", "order-1", "ship", "system", nil)
	if err != nil {
		log.Fatalf("ship: %v", err)
	}
	fmt.Printf("Parent: %s -> %s\n", result.PreviousState, result.NewState)

	if len(result.ChildrenSpawned) == 0 {
		log.Fatal("expected child to be spawned")
	}
	child := result.ChildrenSpawned[0]
	fmt.Printf("Child spawned: workflowType=%s id=%s\n", child.ChildWorkflowType, child.ChildAggregateID)

	// 3. Run the child shipment workflow.
	//
	// The child runs as aggregate type "shipment" (its own Define aggregate type),
	// using the engine-generated ID from child.ChildAggregateID.
	fmt.Println("\n--- Child: shipment workflow ---")

	result, err = engine.Transition(ctx, "shipment", child.ChildAggregateID, "pick_up", "courier", nil)
	if err != nil {
		log.Fatalf("pick_up: %v", err)
	}
	fmt.Printf("Child:  %s -> %s\n", result.PreviousState, result.NewState)

	result, err = engine.Transition(ctx, "shipment", child.ChildAggregateID, "deliver", "courier", nil)
	if err != nil {
		log.Fatalf("deliver: %v", err)
	}
	fmt.Printf("Child:  %s -> %s (terminal: %v)\n", result.PreviousState, result.NewState, result.IsTerminal)

	// 4. Notify the engine that the child is done — this resumes the parent.
	//
	// child.ChildAggregateType = ChildDef.WorkflowType = "shipment_workflow"
	// This must match the key in OnChildCompleted(childWorkflowType).
	fmt.Println("\n--- Notify parent: child completed ---")

	result, err = engine.ChildCompleted(ctx, child.ChildAggregateType, child.ChildAggregateID, "DELIVERED")
	if err != nil {
		log.Fatalf("ChildCompleted: %v", err)
	}
	fmt.Printf("Parent: WAITING_FOR_SHIPMENT -> %s (terminal: %v)\n", result.NewState, result.IsTerminal)
	// Output: Parent: WAITING_FOR_SHIPMENT -> COMPLETED (terminal: true)
}
